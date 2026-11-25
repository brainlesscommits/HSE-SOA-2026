package generator

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"cinema-analytics/internal/model"
	"cinema-analytics/internal/producer"
	"github.com/google/uuid"
)

var movies = []string{
	"movie-inception",
	"movie-interstellar",
	"movie-dune-2",
	"movie-oppenheimer",
	"movie-matrix",
	"movie-parasite",
	"movie-blade-runner-2049",
	"movie-arrival",
	"movie-dark-knight",
	"movie-spirited-away",
}

var devices = []string{
	model.DeviceMobile,
	model.DeviceDesktop,
	model.DeviceTV,
	model.DeviceTablet,
}

type Generator struct {
	producer *producer.KafkaProducer
	logger   *slog.Logger
	interval time.Duration
	userPool []string
}

func NewGenerator(prod *producer.KafkaProducer, logger *slog.Logger, interval time.Duration) *Generator {
	users := make([]string, 0, 80)
	for i := 1; i <= 80; i++ {
		users = append(users, fmt.Sprintf("user-%03d", i))
	}
	return &Generator{
		producer: prod,
		logger:   logger,
		interval: interval,
		userPool: users,
	}
}

func (g *Generator) SeedHistoricalData(ctx context.Context, days int) {
	if days <= 0 {
		return
	}
	g.logger.Info("seeding historical synthetic events", "days", days)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	now := time.Now().UTC()
	baseDay := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)

	totalPublished := 0
	for cohortOffset := days; cohortOffset >= 0; cohortOffset-- {
		cohortDay := baseDay.AddDate(0, 0, -cohortOffset)

		for u := 0; u < 8; u++ {
			userID := fmt.Sprintf("cohort-d%d-u%02d", cohortOffset, u)

			totalPublished += g.emitSession(ctx, rng, userID, cohortDay.Add(time.Duration(rng.Intn(360))*time.Minute))

			for d := 1; d <= cohortOffset; d++ {
				prob := 0.60 / (1.0 + 0.35*float64(d))
				if rng.Float64() < prob {
					dayTs := cohortDay.AddDate(0, 0, d).Add(time.Duration(rng.Intn(480)) * time.Minute)
					if dayTs.Before(now) {
						totalPublished += g.emitSession(ctx, rng, userID, dayTs)
					}
				}
			}
		}
	}
	g.logger.Info("historical seeding completed", "events_published", totalPublished)
}

func (g *Generator) Run(ctx context.Context) {
	g.logger.Info("starting synthetic event generator", "interval", g.interval.String())
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	ticker := time.NewTicker(g.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			g.logger.Info("synthetic event generator stopped")
			return
		case <-ticker.C:
			userID := g.userPool[rng.Intn(len(g.userPool))]
			g.emitSession(ctx, rng, userID, time.Now().UTC())
		}
	}
}

func (g *Generator) emitSession(ctx context.Context, rng *rand.Rand, userID string, startTime time.Time) int {
	sessionID := "sess-" + uuid.NewString()[:8]
	movieID := movies[rng.Intn(len(movies))]
	device := devices[rng.Intn(len(devices))]
	ts := startTime.UTC()

	events := make([]model.MovieEvent, 0, 6)

	if rng.Float64() < 0.40 {
		events = append(events, model.MovieEvent{
			EventID:         uuid.NewString(),
			UserID:          userID,
			MovieID:         movieID,
			EventType:       model.EventSearched,
			Timestamp:       ts,
			DeviceType:      device,
			SessionID:       sessionID,
			ProgressSeconds: 0,
		})
		ts = ts.Add(15 * time.Second)
	}

	events = append(events, model.MovieEvent{
		EventID:         uuid.NewString(),
		UserID:          userID,
		MovieID:         movieID,
		EventType:       model.EventViewStarted,
		Timestamp:       ts,
		DeviceType:      device,
		SessionID:       sessionID,
		ProgressSeconds: 0,
	})

	progress := int32(300 + rng.Intn(1500))

	if rng.Float64() < 0.50 {
		ts = ts.Add(time.Duration(progress) * time.Second)
		events = append(events, model.MovieEvent{
			EventID:         uuid.NewString(),
			UserID:          userID,
			MovieID:         movieID,
			EventType:       model.EventViewPaused,
			Timestamp:       ts,
			DeviceType:      device,
			SessionID:       sessionID,
			ProgressSeconds: progress,
		})

		ts = ts.Add(60 * time.Second)
		events = append(events, model.MovieEvent{
			EventID:         uuid.NewString(),
			UserID:          userID,
			MovieID:         movieID,
			EventType:       model.EventViewResumed,
			Timestamp:       ts,
			DeviceType:      device,
			SessionID:       sessionID,
			ProgressSeconds: progress,
		})
	}

	if rng.Float64() < 0.65 {
		extra := int32(1200 + rng.Intn(3600))
		progress += extra
		ts = ts.Add(time.Duration(extra) * time.Second)
		events = append(events, model.MovieEvent{
			EventID:         uuid.NewString(),
			UserID:          userID,
			MovieID:         movieID,
			EventType:       model.EventViewFinished,
			Timestamp:       ts,
			DeviceType:      device,
			SessionID:       sessionID,
			ProgressSeconds: progress,
		})

		if rng.Float64() < 0.35 {
			ts = ts.Add(5 * time.Second)
			events = append(events, model.MovieEvent{
				EventID:         uuid.NewString(),
				UserID:          userID,
				MovieID:         movieID,
				EventType:       model.EventLiked,
				Timestamp:       ts,
				DeviceType:      device,
				SessionID:       sessionID,
				ProgressSeconds: progress,
			})
		}
	}

	published := 0
	for i := range events {
		if ctx.Err() != nil {
			break
		}
		if err := g.producer.Publish(ctx, &events[i]); err != nil {
			g.logger.Error("generator failed to publish event", "error", err)
			continue
		}
		published++
	}
	return published
}
