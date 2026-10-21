package config

import (
	"os"
	"time"
)

type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   string
	AccessTTL   time.Duration
	RefreshTTL  time.Duration
	OrderLimit  time.Duration
	AdminEmail    string
	AdminPassword string
}

func Load() Config {
	return Config{
		Port:          getenv("PORT", "8080"),
		DatabaseURL:   getenv("DATABASE_URL", "postgres://marketplace:marketplace@postgres:5432/marketplace?sslmode=disable"),
		JWTSecret:     getenv("JWT_SECRET", "change-me-in-production"),
		AccessTTL:     duration("ACCESS_TTL", 20*time.Minute),
		RefreshTTL:    duration("REFRESH_TTL", 14*24*time.Hour),
		OrderLimit:    duration("ORDER_LIMIT", 1*time.Minute),
		AdminEmail:    os.Getenv("ADMIN_EMAIL"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
