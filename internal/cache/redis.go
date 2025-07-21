package cache

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"web-search-api-for-llms/internal/extractor"

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
		if err != redis.Nil {
			slog.Warn("Redis GET failed", "key", key, "error", err)
		}
		return nil, false
	}
	var result extractor.ExtractedResult
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		slog.Warn("RedisCache: Failed to unmarshal ExtractedResult", "key", key, "error", err)
		return nil, false
	}
	return &result, true
}

// GetSearchURLs retrieves a slice of URLs from the cache.
// MGetExtractedResults retrieves multiple ExtractedResults from cache using MGET.
// It returns a map of key to result for found items.
func (c *RedisCache) MGetExtractedResults(ctx context.Context, keys []string) (map[string]*extractor.ExtractedResult, error) {
	if len(keys) == 0 {
		return make(map[string]*extractor.ExtractedResult), nil
	}
	results := make(map[string]*extractor.ExtractedResult)
	vals, err := c.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("redis MGET failed: %w", err)
	}

	for i, val := range vals {
		if val == nil {
			continue // Key not found
		}
		if strVal, ok := val.(string); ok {
			var result extractor.ExtractedResult
			if err := json.Unmarshal([]byte(strVal), &result); err == nil {
				results[keys[i]] = &result
			} else {
				slog.Warn("RedisCache: MGET failed to unmarshal ExtractedResult", "key", keys[i], "error", err)
			}
		}
	}
	return results, nil
}

// GetSearchURLs retrieves a slice of URLs from the cache.
func (c *RedisCache) GetSearchURLs(ctx context.Context, key string) ([]string, bool) {
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		if err != redis.Nil {
			slog.Warn("Redis GET failed for search URLs", "key", key, "error", err)
		}
		return nil, false
	}
	var urls []string
	if err := json.Unmarshal([]byte(val), &urls); err != nil {
		slog.Warn("RedisCache: Failed to unmarshal URL slice", "key", key, "error", err)
		return nil, false
	}
	return urls, true
}

// Set adds a value to the cache.
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, duration time.Duration) {
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		slog.Warn("RedisCache: Failed to marshal value", "key", key, "error", err)
		return
	}
	if err := c.client.Set(ctx, key, jsonBytes, duration).Err(); err != nil {
		slog.Warn("Redis SET failed", "key", key, "error", err)
	}
}
