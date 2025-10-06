package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BotToken    string
	DatabaseURL string
	AdminIDs    []int64
	Location    *time.Location
	HTTPAddr    string
	LogLevel    string
	Env         string // dev|prod
	SentryDSN   string // пока не используем
}

func Load() (*Config, error) {
	tz := getenv("TZ", "Europe/Moscow")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.Local
	}

	adminIDs, err := parseIDs(os.Getenv("ADMIN_IDS"))
	if err != nil {
		return nil, fmt.Errorf("ADMIN_IDS: %w", err)
	}

	cfg := &Config{
		BotToken:    mustEnv("BOT_TOKEN"),
		DatabaseURL: mustEnv("DATABASE_URL"),
		AdminIDs:    adminIDs,
		Location:    loc,
		HTTPAddr:    getenv("HTTP_ADDR", ":8080"),
		LogLevel:    getenv("LOG_LEVEL", "info"),
		Env:         getenv("ENV", "dev"),
		SentryDSN:   os.Getenv("SENTRY_DSN"),
	}
	return cfg, nil
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic("required env " + k + " is empty")
	}
	return v
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func parseIDs(s string) ([]int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("bad id %q: %w", p, err)
		}
		out = append(out, n)
	}
	return out, nil
}
