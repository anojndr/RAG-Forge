package cache

import (
	"time"

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

// Get retrieves a value from the cache.
func (c *MemoryCache) Get(key string) (interface{}, bool) {
	return c.client.Get(key)
}

// Set adds a value to the cache.
func (c *MemoryCache) Set(key string, value interface{}, duration time.Duration) {
	c.client.Set(key, value, duration)
}