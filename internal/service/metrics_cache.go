package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"worktimesync/internal/ai"
)

type MetricsCache struct {
	rdb     *redis.Client
	ttl     time.Duration
	channel string
}

func NewMetricsCache(rdb *redis.Client) *MetricsCache {
	return &MetricsCache{
		rdb:     rdb,
		ttl:     15 * time.Minute,
		channel: "metrics:invalidate",
	}
}

func (c *MetricsCache) key(empID uuid.UUID) string {
	return fmt.Sprintf("metrics:emp:%s:v1", empID)
}

func (c *MetricsCache) Get(ctx context.Context, empID uuid.UUID) (*ai.Metrics, bool) {
	if c.rdb == nil {
		return nil, false
	}
	raw, err := c.rdb.Get(ctx, c.key(empID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false
		}
		return nil, false
	}
	var m ai.Metrics
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	return &m, true
}

func (c *MetricsCache) Set(ctx context.Context, empID uuid.UUID, m ai.Metrics) {
	if c.rdb == nil {
		return
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = c.rdb.Set(ctx, c.key(empID), raw, c.ttl).Err()
}

func (c *MetricsCache) Invalidate(ctx context.Context, empID uuid.UUID) {
	if c.rdb == nil {
		return
	}
	_ = c.rdb.Del(ctx, c.key(empID)).Err()
	_ = c.rdb.Publish(ctx, c.channel, empID.String()).Err()
}

func (c *MetricsCache) SubscribeInvalidations(ctx context.Context) (<-chan uuid.UUID, func()) {
	out := make(chan uuid.UUID, 32)
	if c.rdb == nil {
		close(out)
		return out, func() {}
	}
	pubsub := c.rdb.Subscribe(ctx, c.channel)
	go func() {
		defer close(out)
		ch := pubsub.Channel()
		for msg := range ch {
			if id, err := uuid.Parse(msg.Payload); err == nil {
				select {
				case out <- id:
				default:
				}
			}
		}
	}()
	return out, func() { _ = pubsub.Close() }
}
