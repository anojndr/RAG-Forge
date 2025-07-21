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

// GetExtractedResult retrieves an ExtractedResult from the cache.
func (c *RedisCache) GetExtractedResult(ctx context.Context, key string) (*extractor.ExtractedResult, bool) {
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return nil, false
	}
	var result extractor.ExtractedResult
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		logger.LogError("RedisCache: Failed to unmarshal ExtractedResult for key %s: %v", key, err)
		return nil, false
	}
	return &result, true
}

// GetSearchURLs retrieves a slice of URLs from the cache.
func (c *RedisCache) GetSearchURLs(ctx context.Context, key string) ([]string, bool) {
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return nil, false
	}
	var urls []string
	if err := json.Unmarshal([]byte(val), &urls); err != nil {
		logger.LogError("RedisCache: Failed to unmarshal URL slice for key %s: %v", key, err)
		return nil, false
	}
	return urls, true
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
