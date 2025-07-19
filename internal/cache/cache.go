package cache

import (
	"context"
	"time"
)

// Cache is the interface for a cache.
type Cache interface {
	Get(ctx context.Context, key string) (interface{}, bool)
	Set(ctx context.Context, key string, value interface{}, duration time.Duration)
}