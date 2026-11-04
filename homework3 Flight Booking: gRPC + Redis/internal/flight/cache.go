package flight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache struct {
	rdb       redis.UniversalClient
	ttl       time.Duration
	opTimeout time.Duration
	log       *slog.Logger
}

func NewCache(rdb redis.UniversalClient, ttl, opTimeout time.Duration, log *slog.Logger) *Cache {
	return &Cache{rdb: rdb, ttl: ttl, opTimeout: opTimeout, log: log}
}

func flightKey(id int64) string { return fmt.Sprintf("flight:%d", id) }

func searchKey(origin, destination string, date *time.Time) string {
	d := "any"
	if date != nil {
		d = date.Format(time.DateOnly)
	}
	return fmt.Sprintf("search:%s:%s:%s", origin, destination, d)
}

func (c *Cache) GetFlight(ctx context.Context, id int64) (Flight, bool) {
	var f Flight
	return f, c.get(ctx, flightKey(id), &f)
}

func (c *Cache) SetFlight(ctx context.Context, f Flight) {
	c.set(ctx, flightKey(f.ID), f)
}

func (c *Cache) GetSearch(ctx context.Context, q SearchQuery) ([]Flight, bool) {
	var flights []Flight
	return flights, c.get(ctx, searchKey(q.Origin, q.Destination, q.Date), &flights)
}

func (c *Cache) SetSearch(ctx context.Context, q SearchQuery, flights []Flight) {
	c.set(ctx, searchKey(q.Origin, q.Destination, q.Date), flights)
}

func (c *Cache) InvalidateFlight(ctx context.Context, f Flight) {
	date := f.DepartureDate()
	keys := []string{
		flightKey(f.ID),
		searchKey(f.Origin, f.Destination, &date),
		searchKey(f.Origin, f.Destination, nil),
	}
	ctx, cancel := context.WithTimeout(ctx, c.opTimeout)
	defer cancel()
	if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
		c.log.Error("cache invalidation failed", "keys", keys, "err", err)
		return
	}
	c.log.Info("cache invalidated", "keys", keys)
}

func (c *Cache) get(ctx context.Context, key string, dst any) bool {
	ctx, cancel := context.WithTimeout(ctx, c.opTimeout)
	defer cancel()
	data, err := c.rdb.Get(ctx, key).Bytes()
	switch {
	case errors.Is(err, redis.Nil):
		c.log.Info("cache miss", "key", key)
		return false
	case err != nil:
		c.log.Warn("cache read failed, falling back to database", "key", key, "err", err)
		return false
	}
	if err := json.Unmarshal(data, dst); err != nil {
		c.log.Warn("corrupted cache entry", "key", key, "err", err)
		return false
	}
	c.log.Info("cache hit", "key", key)
	return true
}

func (c *Cache) set(ctx context.Context, key string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		c.log.Error("cache encode failed", "key", key, "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, c.opTimeout)
	defer cancel()
	if err := c.rdb.Set(ctx, key, data, c.ttl).Err(); err != nil {
		c.log.Warn("cache write failed", "key", key, "err", err)
		return
	}
	c.log.Debug("cache stored", "key", key, "ttl", c.ttl)
}
