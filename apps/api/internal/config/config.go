package config

import (
	"fmt"
	"net/url"
	"os"
)

type API struct {
	Address                   string
	DatabaseURL               string
	WebOrigin                 string
	SessionKey                string
	SecureCookie              bool
	LLMGatewayBaseURL         string
	LLMGatewayAPIKey          string
	TelegramWebhookSecret     string
	GmailOAuthClientPath      string
	GmailMailbox              string
	GmailTokenKey             string
	GmailPubSubAudience       string
	GmailPubSubServiceAccount string
}

func LoadAPI() (API, error) {
	cfg := API{
		Address:                   valueOr("API_ADDR", ":8080"),
		DatabaseURL:               os.Getenv("DATABASE_URL"),
		WebOrigin:                 valueOr("WEB_ORIGIN", "http://localhost:3000"),
		SessionKey:                os.Getenv("SESSION_SECRET"),
		LLMGatewayBaseURL:         os.Getenv("LLM_GATEWAY_BASE_URL"),
		LLMGatewayAPIKey:          os.Getenv("LLM_GATEWAY_API_KEY"),
		TelegramWebhookSecret:     os.Getenv("TELEGRAM_WEBHOOK_SECRET"),
		GmailOAuthClientPath:      os.Getenv("GMAIL_OAUTH_CLIENT_PATH"),
		GmailMailbox:              os.Getenv("GMAIL_MAILBOX"),
		GmailTokenKey:             os.Getenv("GMAIL_TOKEN_ENCRYPTION_KEY"),
		GmailPubSubAudience:       os.Getenv("GMAIL_PUBSUB_AUDIENCE"),
		GmailPubSubServiceAccount: os.Getenv("GMAIL_PUBSUB_SERVICE_ACCOUNT"),
	}
	if cfg.DatabaseURL == "" {
		return API{}, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.SessionKey) < 32 {
		return API{}, fmt.Errorf("SESSION_SECRET must contain at least 32 bytes")
	}
	origin, err := url.Parse(cfg.WebOrigin)
	if err != nil || origin.Host == "" {
		return API{}, fmt.Errorf("WEB_ORIGIN must be an absolute URL")
	}
	cfg.SecureCookie = origin.Scheme == "https"
	return cfg, nil
}

func valueOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
