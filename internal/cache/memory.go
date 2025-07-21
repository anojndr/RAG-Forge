package cache

import (
	"context"
	"time"
	"web-search-api-for-llms/internal/extractor"

	"github.com/patrickmn/go-cache"
)

// MemoryCache is an in-memory cache that implements the Cache interface.
type MemoryCache struct {
	client *cache.Cache
}

// NewMemoryCache creates a new MemoryCache.
func NewMemoryCache(defaultExpiration, cleanupInterval time.Duration) *MemoryCache {
	return &MemoryCache{
		client: cache.New(defaultExpiration, cleanupInterval),
	}
}

// GetExtractedResult retrieves an ExtractedResult from the cache.
func (c *MemoryCache) GetExtractedResult(ctx context.Context, key string) (*extractor.ExtractedResult, bool) {
	if val, found := c.client.Get(key); found {
		if result, ok := val.(*extractor.ExtractedResult); ok {
			return result, true
		}
	}
	return nil, false
}

// GetSearchURLs retrieves a slice of URLs from the cache.
func (c *MemoryCache) GetSearchURLs(ctx context.Context, key string) ([]string, bool) {
	if val, found := c.client.Get(key); found {
		if urls, ok := val.([]string); ok {
			return urls, true
		}
	}
	return nil, false
}

// Set adds a value to the cache.
func (c *MemoryCache) Set(ctx context.Context, key string, value interface{}, duration time.Duration) {
	c.client.Set(key, value, duration)
}
