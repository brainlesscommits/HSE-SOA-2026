package platform

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Env struct {
	errs []string
}

func (e *Env) String(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func (e *Env) Required(key string) string {
	v := os.Getenv(key)
	if v == "" {
		e.errs = append(e.errs, key+" is required")
	}
	return v
}

func (e *Env) Int(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		e.errs = append(e.errs, fmt.Sprintf("%s: expected positive integer, got %q", key, v))
		return def
	}
	return n
}

func (e *Env) Duration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		e.errs = append(e.errs, fmt.Sprintf("%s: expected positive duration (e.g. 100ms, 5m), got %q", key, v))
		return def
	}
	return d
}

func (e *Env) List(key string, def []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (e *Env) Err() error {
	if len(e.errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid configuration: %s", strings.Join(e.errs, "; "))
}

func NewLogger(service string) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(os.Getenv("LOG_LEVEL"))); err != nil {
		level = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(h).With("service", service)
}
