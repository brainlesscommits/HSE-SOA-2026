package flight

import (
	"context"
	"crypto/subtle"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const APIKeyHeader = "x-api-key"

type APIKeyAuth struct {
	key []byte
	log *slog.Logger
}

func NewAPIKeyAuth(key string, log *slog.Logger) *APIKeyAuth {
	return &APIKeyAuth{key: []byte(key), log: log}
}

func (a *APIKeyAuth) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := a.authenticate(ctx, info.FullMethod); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func (a *APIKeyAuth) Stream() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := a.authenticate(ss.Context(), info.FullMethod); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

func (a *APIKeyAuth) authenticate(ctx context.Context, method string) error {
	md, _ := metadata.FromIncomingContext(ctx)
	keys := md.Get(APIKeyHeader)
	if len(keys) == 0 {
		a.log.Warn("unauthenticated call: missing api key", "method", method)
		return status.Error(codes.Unauthenticated, "missing "+APIKeyHeader+" metadata")
	}
	if subtle.ConstantTimeCompare([]byte(keys[0]), a.key) != 1 {
		a.log.Warn("unauthenticated call: invalid api key", "method", method)
		return status.Error(codes.Unauthenticated, "invalid api key")
	}
	return nil
}
