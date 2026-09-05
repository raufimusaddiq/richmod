package config

import (
	"fmt"
	"net/url"
	"os"
)

type API struct {
	Address                        string
	DatabaseURL                    string
	WebOrigin                      string
	SessionKey                     string
	SecureCookie                   bool
	LLMGatewayBaseURL              string
	LLMGatewayAPIKey               string
	LLMGatewayProtocol             string
	TelegramWebhookSecret          string
	TelegramBotUsername            string
	EmailIngressHMACSecret         string
	EmailIngressDomain             string
	EmailIngressTrustedAuthservIDs string
	DocumentStoragePath            string
}

func LoadAPI() (API, error) {
	cfg := API{
		Address:                        valueOr("API_ADDR", ":8080"),
		DatabaseURL:                    os.Getenv("DATABASE_URL"),
		WebOrigin:                      valueOr("WEB_ORIGIN", "http://localhost:3000"),
		SessionKey:                     os.Getenv("SESSION_SECRET"),
		LLMGatewayBaseURL:              os.Getenv("LLM_GATEWAY_BASE_URL"),
		LLMGatewayAPIKey:               os.Getenv("LLM_GATEWAY_API_KEY"),
		LLMGatewayProtocol:             valueOr("LLM_GATEWAY_PROTOCOL", "responses"),
		TelegramWebhookSecret:          os.Getenv("TELEGRAM_WEBHOOK_SECRET"),
		TelegramBotUsername:            os.Getenv("TELEGRAM_BOT_USERNAME"),
		EmailIngressHMACSecret:         os.Getenv("EMAIL_INGRESS_HMAC_SECRET"),
		EmailIngressDomain:             valueOr("EMAIL_INGRESS_DOMAIN", "richmod.link"),
		EmailIngressTrustedAuthservIDs: os.Getenv("EMAIL_INGRESS_TRUSTED_AUTHSERV_IDS"),
		DocumentStoragePath:            valueOr("DOCUMENT_STORAGE_PATH", "/var/lib/finance/attachments"),
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
