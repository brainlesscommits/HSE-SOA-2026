package producer

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"cinema-analytics/internal/model"
	"cinema-analytics/internal/schema"
	"github.com/segmentio/kafka-go"
)

type KafkaProducer struct {
	writer      *kafka.Writer
	encoder     *schema.Encoder
	logger      *slog.Logger
	maxRetries  int
	baseBackoff time.Duration
}

func NewKafkaProducer(brokers []string, topic string, encoder *schema.Encoder, logger *slog.Logger) *KafkaProducer {
	writer := &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Topic:                  topic,
		Balancer:               &kafka.Hash{},
		RequiredAcks:           kafka.RequireAll,
		Async:                  false,
		BatchTimeout:           10 * time.Millisecond,
		WriteTimeout:           10 * time.Second,
		ReadTimeout:            10 * time.Second,
		AllowAutoTopicCreation: false,
	}

	return &KafkaProducer{
		writer:      writer,
		encoder:     encoder,
		logger:      logger,
		maxRetries:  5,
		baseBackoff: 200 * time.Millisecond,
	}
}

func (p *KafkaProducer) Publish(ctx context.Context, ev *model.MovieEvent) error {
	payload, err := p.encoder.Encode(ev)
	if err != nil {
		return err
	}

	msg := kafka.Message{
		Key:   []byte(ev.UserID),
		Value: payload,
		Time:  ev.Timestamp,
	}

	var lastErr error
	backoff := p.baseBackoff

	for attempt := 1; attempt <= p.maxRetries; attempt++ {
		err := p.writer.WriteMessages(ctx, msg)
		if err == nil {
			p.logger.Info("event published",
				"event_id", ev.EventID,
				"event_type", ev.EventType,
				"user_id", ev.UserID,
				"movie_id", ev.MovieID,
				"timestamp", ev.Timestamp.Format(time.RFC3339Nano),
			)
			return nil
		}

		lastErr = err
		p.logger.Warn("kafka publish failed, retrying",
			"attempt", attempt,
			"max_retries", p.maxRetries,
			"event_id", ev.EventID,
			"backoff", backoff.String(),
			"error", err,
		)

		if attempt == p.maxRetries {
			break
		}

		jitter := time.Duration(rand.Int63n(int64(backoff/2 + 1)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff + jitter):
			backoff *= 2
		}
	}

	return fmt.Errorf("failed to publish event %s after %d attempts: %w", ev.EventID, p.maxRetries, lastErr)
}

func (p *KafkaProducer) Close() error {
	return p.writer.Close()
}
