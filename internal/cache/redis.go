package cache

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
)

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
func (c *RedisCache) Get(key string) (interface{}, bool) {
	val, err := c.client.Get(context.Background(), key).Result()
	if err == redis.Nil {
		return nil, false
	} else if err != nil {
		return nil, false
	}
	return val, true
}

// Set adds a value to the cache.
func (c *RedisCache) Set(key string, value interface{}, duration time.Duration) {
	c.client.Set(context.Background(), key, value, duration)
}