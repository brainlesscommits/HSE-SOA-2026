package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	flightv1 "flightbooking/gen/flight/v1"
	"flightbooking/internal/flight"
	"flightbooking/internal/flight/migrations"
	"flightbooking/internal/platform"
)

type config struct {
	GRPCAddr      string
	DatabaseURL   string
	APIKey        string
	RedisAddrs    []string
	RedisMaster   string
	RedisPassword string
	RedisTimeout  time.Duration
	CacheTTL      time.Duration
}

func loadConfig() (config, error) {
	var env platform.Env
	cfg := config{
		GRPCAddr:    env.String("GRPC_ADDR", ":50051"),
		DatabaseURL: env.Required("DATABASE_URL"),
		APIKey:      env.Required("FLIGHT_SERVICE_API_KEY"),
		RedisAddrs:    env.List("REDIS_ADDRS", []string{"localhost:6379"}),
		RedisMaster:   env.String("REDIS_MASTER_NAME", ""),
		RedisPassword: env.String("REDIS_PASSWORD", ""),
		RedisTimeout:  env.Duration("REDIS_OP_TIMEOUT", 300*time.Millisecond),
		CacheTTL:      env.Duration("CACHE_TTL", 5*time.Minute),
	}
	return cfg, env.Err()
}

func main() {
	log := platform.NewLogger("flight-service")
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := platform.ConnectPostgres(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := platform.Migrate(ctx, pool, migrations.FS, log); err != nil {
		return err
	}
	rdb := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:                 cfg.RedisAddrs,
		MasterName:            cfg.RedisMaster,
		Password:              cfg.RedisPassword,
		DialTimeout:           cfg.RedisTimeout,
		ReadTimeout:           cfg.RedisTimeout,
		WriteTimeout:          cfg.RedisTimeout,
		ContextTimeoutEnabled: true,
		MaxRetries:            1,
	})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Warn("redis is unavailable at startup, cache will be bypassed until it recovers", "err", err)
	} else {
		log.Info("connected to redis", "addrs", cfg.RedisAddrs, "sentinel_master", cfg.RedisMaster)
	}

	svc := flight.NewService(flight.NewRepository(pool), flight.NewCache(rdb, cfg.CacheTTL, cfg.RedisTimeout, log), log)
	auth := flight.NewAPIKeyAuth(cfg.APIKey, log)
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(auth.Unary()),
		grpc.ChainStreamInterceptor(auth.Stream()),
	)
	flightv1.RegisterFlightServiceServer(srv, flight.NewGRPCServer(svc, log))
	reflection.Register(srv)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		srv.GracefulStop()
	}()
	log.Info("gRPC server listening", "addr", cfg.GRPCAddr)
	return srv.Serve(lis)
}
