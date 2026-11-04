package flightclient

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	flightv1 "flightbooking/gen/flight/v1"
)

const APIKeyHeader = "x-api-key"

type Config struct {
	Addr    string
	APIKey  string
	Retry   RetryConfig
	Breaker BreakerConfig
}

type Client struct {
	flightv1.FlightServiceClient
	conn    *grpc.ClientConn
	Breaker *Breaker
}

func New(cfg Config, log *slog.Logger) (*Client, error) {
	breaker := NewBreaker(cfg.Breaker, log)
	conn, err := grpc.NewClient(cfg.Addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDisableRetry(),
		grpc.WithChainUnaryInterceptor(
			breaker.UnaryClientInterceptor(),
			RetryInterceptor(cfg.Retry, log),
			apiKeyInterceptor(cfg.APIKey),
		),
	)
	if err != nil {
		return nil, err
	}
	return &Client{FlightServiceClient: flightv1.NewFlightServiceClient(conn), conn: conn, Breaker: breaker}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func apiKeyInterceptor(key string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(metadata.AppendToOutgoingContext(ctx, APIKeyHeader, key), method, req, reply, cc, opts...)
	}
}
