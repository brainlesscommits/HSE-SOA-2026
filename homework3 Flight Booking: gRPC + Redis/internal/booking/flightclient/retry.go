package flightclient

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RetryConfig struct {
	MaxAttempts int
	InitialBackoff time.Duration
	AttemptTimeout time.Duration
}

func retryable(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded:
		return true
	}
	return false
}

func RetryInterceptor(cfg RetryConfig, log *slog.Logger) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		backoff := cfg.InitialBackoff
		for attempt := 1; ; attempt++ {
			attemptCtx, cancel := context.WithTimeout(ctx, cfg.AttemptTimeout)
			err := invoker(attemptCtx, method, req, reply, cc, opts...)
			cancel()

			if err == nil || !retryable(err) {
				return err
			}
			if attempt >= cfg.MaxAttempts {
				log.Warn("gRPC call failed, retries exhausted", "method", method, "attempts", attempt, "code", status.Code(err).String())
				return err
			}
			if ctx.Err() != nil {
				return err
			}

			log.Warn("gRPC call failed, retrying", "method", method, "attempt", attempt,
				"code", status.Code(err).String(), "backoff", backoff)
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return status.FromContextError(ctx.Err()).Err()
			}
			backoff *= 2
		}
	}
}
