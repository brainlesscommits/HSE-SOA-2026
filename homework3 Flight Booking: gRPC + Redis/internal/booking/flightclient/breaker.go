package flightclient

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	}
	return "UNKNOWN"
}

type BreakerConfig struct {
	FailureThreshold int
	Window time.Duration
	OpenTimeout time.Duration
}

var ErrCircuitOpen = status.Error(codes.Unavailable, "flight service is temporarily unavailable: circuit breaker is open")

type Breaker struct {
	cfg BreakerConfig
	log *slog.Logger
	now func() time.Time

	mu            sync.Mutex
	state         State
	failures      []time.Time
	openedAt      time.Time
	probeInFlight bool
}

func NewBreaker(cfg BreakerConfig, log *slog.Logger) *Breaker {
	return &Breaker{cfg: cfg, log: log, now: time.Now}
}

func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Breaker) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		probe, err := b.allow()
		if err != nil {
			b.log.Warn("call rejected by circuit breaker", "method", method)
			return err
		}
		err = invoker(ctx, method, req, reply, cc, opts...)
		b.record(probe, outcomeOf(err))
		return err
	}
}

func (b *Breaker) allow() (probe bool, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == StateOpen && b.now().Sub(b.openedAt) >= b.cfg.OpenTimeout {
		b.transition(StateHalfOpen)
	}
	switch b.state {
	case StateOpen:
		return false, ErrCircuitOpen
	case StateHalfOpen:
		if b.probeInFlight {
			return false, ErrCircuitOpen
		}
		b.probeInFlight = true
		return true, nil
	}
	return false, nil
}

type outcome int

const (
	success outcome = iota
	failure
	neutral
)

func outcomeOf(err error) outcome {
	switch status.Code(err) {
	case codes.OK:
		return success
	case codes.Unavailable, codes.DeadlineExceeded, codes.Internal, codes.Unknown:
		return failure
	case codes.Canceled:
		return neutral
	}
	return success
}

func (b *Breaker) record(probe bool, o outcome) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if probe {
		b.probeInFlight = false
		switch o {
		case success:
			b.transition(StateClosed)
		case failure:
			b.transition(StateOpen)
		}
		return
	}
	if b.state != StateClosed || o != failure {
		return
	}

	now := b.now()
	cutoff := now.Add(-b.cfg.Window)
	kept := b.failures[:0]
	for _, t := range b.failures {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	b.failures = append(kept, now)
	if len(b.failures) >= b.cfg.FailureThreshold {
		b.transition(StateOpen)
	}
}

func (b *Breaker) transition(to State) {
	from := b.state
	b.state = to
	b.failures = b.failures[:0]
	if to == StateOpen {
		b.openedAt = b.now()
	}
	b.log.Warn("circuit breaker state changed",
		"from", from.String(), "to", to.String(),
		"failure_threshold", b.cfg.FailureThreshold, "window", b.cfg.Window, "open_timeout", b.cfg.OpenTimeout)
}
