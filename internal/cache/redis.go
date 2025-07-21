package cache

import (
	"context"
	"time"
	"web-search-api-for-llms/internal/extractor"
	"web-search-api-for-llms/internal/logger"

	"github.com/go-redis/redis/v8"
	jsoniter "github.com/json-iterator/go"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

// RedisCache is a Redis-backed cache that implements the Cache interface.
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache creates a new RedisCache.
func NewRedisCache(addr, password string, db int) *RedisCache {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &RedisCache{client: rdb}
}

// Get retrieves a value from the cache.
func (c *RedisCache) Get(ctx context.Context, key string) (interface{}, bool) {
	val, err := c.client.Get(ctx, key).Result()
	if err != nil { // Handles redis.Nil and other errors
		return nil, false
	}

	// We need a way to unmarshal back into the correct type.
	// A simple approach for this app is to assume it's always an ExtractedResult.
	var result extractor.ExtractedResult
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		// This could also be a simple string (for search results). Try that too.
		var urls []string
		if err2 := json.Unmarshal([]byte(val), &urls); err2 == nil {
			return urls, true
		}

		logger.LogError("RedisCache: Failed to unmarshal value for key %s: %v", key, err)
		return nil, false
	}
	return &result, true
}

// Set adds a value to the cache.
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, duration time.Duration) {
	// Marshal the value to JSON
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		logger.LogError("RedisCache: Failed to marshal value for key %s: %v", key, err)
		return
	}
	c.client.Set(ctx, key, jsonBytes, duration)
}
