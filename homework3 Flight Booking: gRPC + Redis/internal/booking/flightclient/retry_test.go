package flightclient

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRetryInterceptor(t *testing.T) {
	tests := []struct {
		name         string
		code         codes.Code
		wantAttempts int
	}{
		{"unavailable is retried", codes.Unavailable, 3},
		{"deadline exceeded is retried", codes.DeadlineExceeded, 3},
		{"invalid argument is not retried", codes.InvalidArgument, 1},
		{"not found is not retried", codes.NotFound, 1},
		{"resource exhausted is not retried", codes.ResourceExhausted, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interceptor := RetryInterceptor(RetryConfig{MaxAttempts: 3, InitialBackoff: time.Millisecond, AttemptTimeout: time.Second}, discardLog)
			attempts := 0
			err := interceptor(context.Background(), "/test", nil, nil, nil,
				func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
					attempts++
					return status.Error(tt.code, "fail")
				})
			if status.Code(err) != tt.code {
				t.Fatalf("code = %v, want %v", status.Code(err), tt.code)
			}
			if attempts != tt.wantAttempts {
				t.Fatalf("attempts = %d, want %d", attempts, tt.wantAttempts)
			}
		})
	}
}

func TestRetryInterceptorExponentialBackoffAndRecovery(t *testing.T) {
	interceptor := RetryInterceptor(RetryConfig{MaxAttempts: 3, InitialBackoff: 20 * time.Millisecond, AttemptTimeout: time.Second}, discardLog)
	var calls []time.Time
	err := interceptor(context.Background(), "/test", nil, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			calls = append(calls, time.Now())
			if len(calls) < 3 {
				return status.Error(codes.Unavailable, "down")
			}
			return nil
		})
	if err != nil {
		t.Fatalf("err = %v, want success on third attempt", err)
	}
	if len(calls) != 3 {
		t.Fatalf("attempts = %d, want 3", len(calls))
	}
	if d := calls[1].Sub(calls[0]); d < 20*time.Millisecond {
		t.Errorf("first backoff = %v, want >= 20ms", d)
	}
	if d := calls[2].Sub(calls[1]); d < 40*time.Millisecond {
		t.Errorf("second backoff = %v, want >= 40ms (doubled)", d)
	}
}

func TestRetryInterceptorAppliesAttemptTimeout(t *testing.T) {
	interceptor := RetryInterceptor(RetryConfig{MaxAttempts: 2, InitialBackoff: time.Millisecond, AttemptTimeout: 10 * time.Millisecond}, discardLog)
	attempts := 0
	err := interceptor(context.Background(), "/test", nil, nil, nil,
		func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			attempts++
			<-ctx.Done()
			return status.FromContextError(ctx.Err()).Err()
		})
	if status.Code(err) != codes.DeadlineExceeded || attempts != 2 {
		t.Fatalf("code = %v attempts = %d, want DEADLINE_EXCEEDED after 2 attempts", status.Code(err), attempts)
	}
}
