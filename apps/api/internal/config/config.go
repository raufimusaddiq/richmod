package config

import (
	"fmt"
	"os"
)

type API struct {
	Address     string
	DatabaseURL string
	WebOrigin   string
	SessionKey  string
}

func LoadAPI() (API, error) {
	cfg := API{
		Address:     valueOr("API_ADDR", ":8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		WebOrigin:   valueOr("WEB_ORIGIN", "http://localhost:3000"),
		SessionKey:  os.Getenv("SESSION_SECRET"),
	}
	if cfg.DatabaseURL == "" {
		return API{}, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.SessionKey) < 32 {
		return API{}, fmt.Errorf("SESSION_SECRET must contain at least 32 bytes")
	}
	return cfg, nil
}

func valueOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
