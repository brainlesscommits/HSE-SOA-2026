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

	"github.com/jackc/pgx/v5/pgxpool"

	"marketplace/internal/api"
	"marketplace/internal/apperr"
	"marketplace/internal/auth"
	"marketplace/internal/config"
	"marketplace/internal/httpx"
	"marketplace/internal/logging"
	"marketplace/internal/middleware"
	"marketplace/internal/repository"
	"marketplace/internal/service"
)

func main() {
	cfg := config.Load()
	logger := logging.New()
	slog.SetDefault(logger)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fatal("database pool creation failed", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		fatal("database ping failed", err)
	}

	db := &repository.DB{Pool: pool}
	tokens := auth.New(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)
	authService := service.NewAuthService(db, tokens)

	if cfg.AdminEmail != "" && cfg.AdminPassword != "" {
		created, err := authService.EnsureAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword)
		if err != nil {
			fatal("admin bootstrap failed", err)
		}
		logger.Info("admin bootstrap", "email", cfg.AdminEmail, "created", created)
	}

	server := &httpx.Server{
		Auth:     authService,
		Tokens:   tokens,
		Products: service.NewProductService(db),
		Orders:   service.NewOrderService(db, cfg.OrderLimit),
		Promos:   service.NewPromoService(db),
	}

	spec, err := api.GetSwagger()
	if err != nil {
		fatal("openapi spec load failed", err)
	}
	validation, err := middleware.Validation(spec)
	if err != nil {
		fatal("openapi validator init failed", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		apperr.Write(w, apperr.New(http.StatusNotFound, api.NOTFOUND, "endpoint not found", nil))
	})
	handler := api.HandlerWithOptions(server, api.StdHTTPServerOptions{
		BaseRouter:       mux,
		Middlewares:      []api.MiddlewareFunc{validation, middleware.JWT(tokens)},
		ErrorHandlerFunc: httpx.ParamError,
	})

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           middleware.Logging(logger)(handler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-shutdownCtx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}()

	logger.Info("marketplace listening", "addr", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal("server failed", err)
	}
}

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}
