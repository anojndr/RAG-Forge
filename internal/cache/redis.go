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
	// Add connection pooling options for high concurrency
	rdb.Options().PoolSize = 500
	rdb.Options().MinIdleConns = 50
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

// Add MGetExtractedResults to RedisCache
func (c *RedisCache) MGetExtractedResults(ctx context.Context, keys []string) (map[string]*extractor.ExtractedResult, error) {
	if len(keys) == 0 {
		return make(map[string]*extractor.ExtractedResult), nil
	}
	results := make(map[string]*extractor.ExtractedResult, len(keys))
	vals, err := c.client.MGet(ctx, keys...).Result()
	if err != nil {
		// Don't treat redis.Nil as a critical error for MGET
		if err == redis.Nil {
			return results, nil
		}
		return nil, fmt.Errorf("redis MGET failed: %w", err)
	}

	for i, val := range vals {
		if val == nil {
			continue // Key not found
		}
		if strVal, ok := val.(string); ok && strVal != "" {
			// Use the pool to avoid allocation inside the loop
			pooledResult := extractor.ExtractedResultPool.Get().(*extractor.ExtractedResult)
			if err := json.Unmarshal([]byte(strVal), pooledResult); err == nil {
				results[keys[i]] = pooledResult
			} else {
				slog.Warn("RedisCache: MGET failed to unmarshal ExtractedResult", "key", keys[i], "error", err)
				// IMPORTANT: Put back in the pool if unmarshal fails
				extractor.ExtractedResultPool.Put(pooledResult)
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

// MSet is a batched/pipelined SET for Redis.
func (c *RedisCache) MSet(ctx context.Context, items map[string]interface{}, duration time.Duration) error {
	if len(items) == 0 {
		return nil
	}

	pipe := c.client.Pipeline()
	for key, value := range items {
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			slog.Warn("RedisCache MSet: Failed to marshal value, skipping item", "key", key, "error", err)
			continue
		}
		pipe.Set(ctx, key, jsonBytes, duration)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		slog.Warn("Redis Pipelined MSET failed", "error", err)
		return err
	}
	return nil
}
