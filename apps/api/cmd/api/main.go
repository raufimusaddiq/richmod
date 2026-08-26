package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/analytics"
	"github.com/raufimusaddiq/richmod/apps/api/internal/admin"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/budget"
	"github.com/raufimusaddiq/richmod/apps/api/internal/config"
	"github.com/raufimusaddiq/richmod/apps/api/internal/document"
	"github.com/raufimusaddiq/richmod/apps/api/internal/gmail"
	"github.com/raufimusaddiq/richmod/apps/api/internal/household"
	"github.com/raufimusaddiq/richmod/apps/api/internal/insight"
	"github.com/raufimusaddiq/richmod/apps/api/internal/ledger"
	"github.com/raufimusaddiq/richmod/apps/api/internal/operations"
	"github.com/raufimusaddiq/richmod/apps/api/internal/platform/database"
	"github.com/raufimusaddiq/richmod/apps/api/internal/platform/httpmw"
	"github.com/raufimusaddiq/richmod/apps/api/internal/review"
	"github.com/raufimusaddiq/richmod/apps/api/internal/settings"
	"github.com/raufimusaddiq/richmod/apps/api/internal/salary"
	"github.com/raufimusaddiq/richmod/apps/api/internal/telegram"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadAPI()
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	mux := http.NewServeMux()
	authHandler := auth.NewHandler(auth.NewService(pool), cfg.SecureCookie)
	analyticsHandler := analytics.NewHandler(pool)
	budgetHandler := budget.NewHandler(pool)
	ledgerHandler := ledger.NewHandler(pool)
	reviewHandler := review.NewHandler(pool)
	settingsHandler := settings.NewHandler(pool)
	salaryHandler := salary.NewHandler(pool)
	householdHandler := household.NewHandler(pool, cfg.TelegramBotUsername)
	adminHandler := admin.NewHandler(pool)
	documentHandler, err := document.NewHandler(pool, cfg.DocumentStoragePath)
	if err != nil {
		return err
	}
	insightHandler := insight.NewHandler(pool)
	operationsHandler := operations.NewHandler(pool, cfg.LLMGatewayBaseURL != "" && cfg.LLMGatewayAPIKey != "")
	telegramHandler := telegram.NewHandler(telegram.NewPostgreSQLStore(pool), cfg.TelegramWebhookSecret, cfg.TelegramBotToken)
	loginLimiter := httpmw.NewLimiter(10, time.Minute)
	webhookLimiter := httpmw.NewLimiter(300, time.Minute)
	var gmailHandler *gmail.Handler
	if cfg.GmailOAuthClientPath != "" || cfg.GmailMailbox != "" || cfg.GmailTokenKey != "" {
		client, err := gmail.LoadOAuthClient(cfg.GmailOAuthClientPath)
		if err != nil {
			return err
		}
		gmailHandler, err = gmail.NewHandler(pool, client, cfg.GmailMailbox, cfg.GmailTokenKey)
		if err != nil {
			return err
		}
		gmailHandler.ConfigurePubSub(cfg.GmailPubSubAudience, cfg.GmailPubSubServiceAccount)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, pingCancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer pingCancel()
		if err := pool.Ping(pingCtx); err != nil {
			http.Error(w, `{"status":"not_ready"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	mux.Handle("POST /api/v1/auth/login", loginLimiter.Handler(http.HandlerFunc(authHandler.Login)))
	mux.Handle("POST /api/v1/auth/dashboard-invites/accept", loginLimiter.Handler(http.HandlerFunc(authHandler.AcceptDashboardInvite)))
	mux.HandleFunc("POST /api/v1/auth/logout", authHandler.Logout)
	mux.Handle("GET /api/v1/auth/me", authHandler.RequireSession(http.HandlerFunc(authHandler.Me)))
	mux.Handle("GET /api/v1/household", authHandler.RequireSession(http.HandlerFunc(householdHandler.Get)))
	mux.Handle("GET /api/v1/household/members", authHandler.RequireSession(http.HandlerFunc(householdHandler.Members)))
	mux.Handle("POST /api/v1/household/members", authHandler.RequireSession(http.HandlerFunc(householdHandler.Members)))
	mux.Handle("PATCH /api/v1/household/members/{id}", authHandler.RequireSession(http.HandlerFunc(householdHandler.PatchMember)))
	mux.Handle("POST /api/v1/household/members/{id}/telegram-invite", authHandler.RequireSession(http.HandlerFunc(householdHandler.CreateInvite)))
	mux.Handle("DELETE /api/v1/household/members/{id}/telegram-invite", authHandler.RequireSession(http.HandlerFunc(householdHandler.RevokeInvite)))
	mux.Handle("POST /api/v1/household/members/{id}/dashboard-invite", authHandler.RequireSession(http.HandlerFunc(householdHandler.CreateDashboardInvite)))
	mux.Handle("DELETE /api/v1/household/members/{id}/dashboard-invite", authHandler.RequireSession(http.HandlerFunc(householdHandler.RevokeDashboardInvite)))
	mux.Handle("GET /api/v1/admin/users", authHandler.RequireSession(adminHandler.Require(http.HandlerFunc(adminHandler.Users))))
	mux.Handle("PATCH /api/v1/admin/users/{id}", authHandler.RequireSession(adminHandler.Require(http.HandlerFunc(adminHandler.PatchUser))))
	mux.Handle("GET /api/v1/admin/households", authHandler.RequireSession(adminHandler.Require(http.HandlerFunc(adminHandler.Households))))
	mux.Handle("GET /api/v1/admin/households/{householdId}/members", authHandler.RequireSession(adminHandler.Require(http.HandlerFunc(adminHandler.Members))))
	mux.Handle("POST /api/v1/admin/households/{householdId}/members", authHandler.RequireSession(adminHandler.Require(http.HandlerFunc(adminHandler.AddMember))))
	mux.Handle("POST /api/v1/transactions", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.CreateManualTransaction)))
	mux.Handle("GET /api/v1/transactions", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.ListTransactions)))
	mux.Handle("GET /api/v1/transactions/{id}", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.GetTransaction)))
	mux.Handle("POST /api/v1/transactions/{id}/confirm", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.ConfirmTransaction)))
	mux.Handle("POST /api/v1/transactions/{id}/void", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.VoidTransaction)))
	mux.Handle("GET /api/v1/transactions/{id}/evidence", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.Evidence)))
	mux.Handle("GET /api/v1/transactions/{id}/audit", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.Audit)))
	mux.Handle("GET /api/v1/reviews", authHandler.RequireSession(http.HandlerFunc(reviewHandler.List)))
	mux.Handle("POST /api/v1/reviews/{id}/confirm", authHandler.RequireSession(http.HandlerFunc(reviewHandler.Confirm)))
	mux.Handle("POST /api/v1/reviews/{id}/reject", authHandler.RequireSession(http.HandlerFunc(reviewHandler.Reject)))
	mux.Handle("POST /api/v1/reviews/{id}/merge", authHandler.RequireSession(http.HandlerFunc(reviewHandler.Merge)))
	mux.Handle("POST /api/v1/reviews/{id}/classify-transfer", authHandler.RequireSession(http.HandlerFunc(reviewHandler.ClassifyTransfer)))
	mux.Handle("POST /api/v1/reconciliation-merges/{id}/reverse", authHandler.RequireSession(http.HandlerFunc(reviewHandler.Unmerge)))
	mux.Handle("GET /api/v1/documents", authHandler.RequireSession(http.HandlerFunc(documentHandler.List)))
	mux.Handle("POST /api/v1/documents", authHandler.RequireSession(http.HandlerFunc(documentHandler.Upload)))
	mux.Handle("GET /api/v1/documents/{id}/content", authHandler.RequireSession(http.HandlerFunc(documentHandler.Content)))
	mux.Handle("GET /api/v1/documents/{id}/extraction", authHandler.RequireSession(http.HandlerFunc(documentHandler.Extraction)))
	mux.Handle("GET /api/v1/accounts", authHandler.RequireSession(http.HandlerFunc(settingsHandler.Accounts)))
	mux.Handle("POST /api/v1/accounts", authHandler.RequireSession(http.HandlerFunc(settingsHandler.Accounts)))
	mux.Handle("PATCH /api/v1/accounts/{id}", authHandler.RequireSession(http.HandlerFunc(settingsHandler.PatchAccount)))
	mux.Handle("GET /api/v1/categories", authHandler.RequireSession(http.HandlerFunc(settingsHandler.Categories)))
	mux.Handle("POST /api/v1/categories", authHandler.RequireSession(http.HandlerFunc(settingsHandler.Categories)))
	mux.Handle("PATCH /api/v1/categories/{id}", authHandler.RequireSession(http.HandlerFunc(settingsHandler.PatchCategory)))
	mux.Handle("GET /api/v1/merchants", authHandler.RequireSession(http.HandlerFunc(settingsHandler.Merchants)))
	mux.Handle("POST /api/v1/merchants", authHandler.RequireSession(http.HandlerFunc(settingsHandler.Merchants)))
	mux.Handle("POST /api/v1/merchants/{id}/aliases", authHandler.RequireSession(http.HandlerFunc(settingsHandler.CreateMerchantAlias)))
	mux.Handle("GET /api/v1/merchant-aliases", authHandler.RequireSession(http.HandlerFunc(settingsHandler.MerchantAliases)))
	mux.Handle("PATCH /api/v1/merchant-aliases/{id}", authHandler.RequireSession(http.HandlerFunc(settingsHandler.PatchMerchantAlias)))
	mux.Handle("GET /api/v1/known-accounts", authHandler.RequireSession(http.HandlerFunc(settingsHandler.KnownAccounts)))
	mux.Handle("POST /api/v1/known-accounts", authHandler.RequireSession(http.HandlerFunc(settingsHandler.KnownAccounts)))
	mux.Handle("PATCH /api/v1/known-accounts/{id}", authHandler.RequireSession(http.HandlerFunc(settingsHandler.PatchKnownAccount)))
	mux.Handle("GET /api/v1/analytics/overview", authHandler.RequireSession(http.HandlerFunc(analyticsHandler.Overview)))
	mux.Handle("GET /api/v1/analytics/spending", authHandler.RequireSession(http.HandlerFunc(analyticsHandler.Spending)))
	mux.Handle("GET /api/v1/analytics/cashflow", authHandler.RequireSession(http.HandlerFunc(analyticsHandler.Cashflow)))
	mux.Handle("GET /api/v1/salary/sources", authHandler.RequireSession(http.HandlerFunc(salaryHandler.Sources)))
	mux.Handle("POST /api/v1/salary/sources", authHandler.RequireSession(http.HandlerFunc(salaryHandler.Sources)))
	mux.Handle("GET /api/v1/analytics/cycle", authHandler.RequireSession(http.HandlerFunc(analyticsHandler.Cycle)))
	mux.Handle("GET /api/v1/analytics/cycle/daily", authHandler.RequireSession(http.HandlerFunc(analyticsHandler.CycleDaily)))
	mux.Handle("GET /api/v1/analytics/categories", authHandler.RequireSession(http.HandlerFunc(analyticsHandler.Categories)))
	mux.Handle("GET /api/v1/analytics/merchants", authHandler.RequireSession(http.HandlerFunc(analyticsHandler.Merchants)))
	mux.Handle("GET /api/v1/analytics/members", authHandler.RequireSession(http.HandlerFunc(analyticsHandler.Members)))
	mux.Handle("GET /api/v1/budgets", authHandler.RequireSession(http.HandlerFunc(budgetHandler.List)))
	mux.Handle("POST /api/v1/budgets", authHandler.RequireSession(http.HandlerFunc(budgetHandler.Create)))
	mux.Handle("PATCH /api/v1/budgets/{id}", authHandler.RequireSession(http.HandlerFunc(budgetHandler.Patch)))
	mux.Handle("GET /api/v1/insights", authHandler.RequireSession(http.HandlerFunc(insightHandler.List)))
	mux.Handle("POST /api/v1/insights/generate", authHandler.RequireSession(http.HandlerFunc(insightHandler.Generate)))
	mux.Handle("GET /api/v1/operations/status", authHandler.RequireSession(http.HandlerFunc(operationsHandler.Status)))
	mux.Handle("POST /webhooks/telegram", webhookLimiter.Handler(http.HandlerFunc(telegramHandler.Webhook)))
	if gmailHandler != nil {
		mux.Handle("GET /api/v1/integrations/gmail/connect", authHandler.RequireSession(http.HandlerFunc(gmailHandler.Connect)))
		mux.HandleFunc("GET /api/v1/integrations/gmail/callback", gmailHandler.Callback)
		mux.Handle("POST /webhooks/gmail/pubsub", webhookLimiter.Handler(http.HandlerFunc(gmailHandler.PubSub)))
	}
	var handler http.Handler = mux
	handler = httpmw.SameOrigin(cfg.WebOrigin, handler)
	handler = httpmw.AccessLog(logger, handler)
	handler = httpmw.RequestID(handler)
	handler = securityHeaders(handler)

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("api listening", "address", cfg.Address)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		return server.Shutdown(shutdownCtx)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
