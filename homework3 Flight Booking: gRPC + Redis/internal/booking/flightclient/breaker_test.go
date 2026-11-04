package flightclient

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestBreaker(threshold int) (*Breaker, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	b := NewBreaker(BreakerConfig{FailureThreshold: threshold, Window: 10 * time.Second, OpenTimeout: 5 * time.Second}, discardLog)
	b.now = clock.now
	return b, clock
}

func call(b *Breaker, result error) (invoked bool, err error) {
	err = b.UnaryClientInterceptor()(context.Background(), "/test", nil, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			invoked = true
			return result
		})
	return invoked, err
}

var errUnavailable = status.Error(codes.Unavailable, "down")

func TestBreakerOpensAfterThresholdAndRejectsWithoutCalling(t *testing.T) {
	b, _ := newTestBreaker(3)
	for i := 0; i < 3; i++ {
		call(b, errUnavailable)
	}
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want OPEN", b.State())
	}
	invoked, err := call(b, nil)
	if invoked {
		t.Fatal("call reached the server while breaker is OPEN")
	}
	if !errors.Is(err, ErrCircuitOpen) || status.Code(err) != codes.Unavailable {
		t.Fatalf("err = %v, want ErrCircuitOpen", err)
	}
}

func TestBreakerIgnoresBusinessErrors(t *testing.T) {
	b, _ := newTestBreaker(2)
	for _, c := range []codes.Code{codes.NotFound, codes.ResourceExhausted, codes.InvalidArgument, codes.Canceled} {
		call(b, status.Error(c, "business"))
		call(b, status.Error(c, "business"))
	}
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want CLOSED", b.State())
	}
}

func TestBreakerCountsFailuresOnlyWithinWindow(t *testing.T) {
	b, clock := newTestBreaker(3)
	call(b, errUnavailable)
	call(b, errUnavailable)
	clock.advance(11 * time.Second)
	call(b, errUnavailable)
	call(b, errUnavailable)
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want CLOSED", b.State())
	}
	call(b, errUnavailable)
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want OPEN", b.State())
	}
}

func TestBreakerHalfOpenProbe(t *testing.T) {
	tests := []struct {
		name  string
		probe error
		want  State
	}{
		{"success closes", nil, StateClosed},
		{"business error closes", status.Error(codes.NotFound, "no flight"), StateClosed},
		{"failure reopens", errUnavailable, StateOpen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, clock := newTestBreaker(1)
			call(b, errUnavailable)
			clock.advance(5 * time.Second)

			if invoked, _ := call(b, tt.probe); !invoked {
				t.Fatal("probe call was not let through in HALF_OPEN")
			}
			if b.State() != tt.want {
				t.Fatalf("state = %v, want %v", b.State(), tt.want)
			}
		})
	}
}

func TestBreakerHalfOpenAllowsSingleProbe(t *testing.T) {
	b, clock := newTestBreaker(1)
	call(b, errUnavailable)
	clock.advance(5 * time.Second)

	probeStarted, releaseProbe := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.UnaryClientInterceptor()(context.Background(), "/test", nil, nil, nil,
			func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
				close(probeStarted)
				<-releaseProbe
				return nil
			})
	}()
	<-probeStarted

	if invoked, err := call(b, nil); invoked || !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("second call in HALF_OPEN: invoked=%v err=%v, want rejected", invoked, err)
	}
	close(releaseProbe)
	<-done
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want CLOSED", b.State())
	}
}
