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
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/config"
	"github.com/raufimusaddiq/richmod/apps/api/internal/gmail"
	"github.com/raufimusaddiq/richmod/apps/api/internal/ledger"
	"github.com/raufimusaddiq/richmod/apps/api/internal/platform/database"
	"github.com/raufimusaddiq/richmod/apps/api/internal/settings"
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
	ledgerHandler := ledger.NewHandler(pool)
	settingsHandler := settings.NewHandler(pool)
	telegramHandler := telegram.NewHandler(telegram.NewPostgreSQLStore(pool), cfg.TelegramWebhookSecret)
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
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.Login)
	mux.HandleFunc("POST /api/v1/auth/logout", authHandler.Logout)
	mux.Handle("GET /api/v1/auth/me", authHandler.RequireSession(http.HandlerFunc(authHandler.Me)))
	mux.Handle("POST /api/v1/transactions", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.CreateManualTransaction)))
	mux.Handle("GET /api/v1/transactions", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.ListTransactions)))
	mux.Handle("GET /api/v1/transactions/{id}", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.GetTransaction)))
	mux.Handle("POST /api/v1/transactions/{id}/confirm", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.ConfirmTransaction)))
	mux.Handle("POST /api/v1/transactions/{id}/void", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.VoidTransaction)))
	mux.Handle("GET /api/v1/transactions/{id}/evidence", authHandler.RequireSession(http.HandlerFunc(ledgerHandler.Evidence)))
	mux.Handle("GET /api/v1/accounts", authHandler.RequireSession(http.HandlerFunc(settingsHandler.Accounts)))
	mux.Handle("POST /api/v1/accounts", authHandler.RequireSession(http.HandlerFunc(settingsHandler.Accounts)))
	mux.Handle("GET /api/v1/categories", authHandler.RequireSession(http.HandlerFunc(settingsHandler.Categories)))
	mux.Handle("POST /api/v1/categories", authHandler.RequireSession(http.HandlerFunc(settingsHandler.Categories)))
	mux.Handle("GET /api/v1/merchants", authHandler.RequireSession(http.HandlerFunc(settingsHandler.Merchants)))
	mux.Handle("POST /api/v1/merchants", authHandler.RequireSession(http.HandlerFunc(settingsHandler.Merchants)))
	mux.Handle("POST /api/v1/merchants/{id}/aliases", authHandler.RequireSession(http.HandlerFunc(settingsHandler.CreateMerchantAlias)))
	mux.Handle("GET /api/v1/analytics/overview", authHandler.RequireSession(http.HandlerFunc(analyticsHandler.Overview)))
	mux.HandleFunc("POST /webhooks/telegram", telegramHandler.Webhook)
	if gmailHandler != nil {
		mux.Handle("GET /api/v1/integrations/gmail/connect", authHandler.RequireSession(http.HandlerFunc(gmailHandler.Connect)))
		mux.HandleFunc("GET /api/v1/integrations/gmail/callback", gmailHandler.Callback)
		mux.HandleFunc("POST /webhooks/gmail/pubsub", gmailHandler.PubSub)
	}

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           securityHeaders(mux),
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
