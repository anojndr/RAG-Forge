package cache

import (
	"context"
	"hash/fnv"
	"time"
	"web-search-api-for-llms/internal/extractor"

	"github.com/patrickmn/go-cache"
)

const shardCount = 256 // A power of 2 is good. Adjust based on expected load.

type ShardedMemoryCache struct {
	shards []*cache.Cache
}

func NewShardedMemoryCache(defaultExpiration, cleanupInterval time.Duration) *ShardedMemoryCache {
	c := &ShardedMemoryCache{
		shards: make([]*cache.Cache, shardCount),
	}
	for i := 0; i < shardCount; i++ {
		c.shards[i] = cache.New(defaultExpiration, cleanupInterval)
	}
	return c
}

func (c *ShardedMemoryCache) getShard(key string) *cache.Cache {
	hasher := fnv.New64a()
	hasher.Write([]byte(key))
	// Bitwise AND is faster than modulo for powers of 2
	return c.shards[hasher.Sum64()&(shardCount-1)]
}

func (c *ShardedMemoryCache) GetExtractedResult(ctx context.Context, key string) (*extractor.ExtractedResult, bool) {
	shard := c.getShard(key)
	if val, found := shard.Get(key); found {
		if result, ok := val.(*extractor.ExtractedResult); ok {
			return result, true
		}
	}
	return nil, false
}

func (c *ShardedMemoryCache) GetSearchURLs(ctx context.Context, key string) ([]string, bool) {
	shard := c.getShard(key)
	if val, found := shard.Get(key); found {
		if urls, ok := val.([]string); ok {
			return urls, true
		}
	}
	return nil, false
}

func (c *ShardedMemoryCache) Set(ctx context.Context, key string, value interface{}, duration time.Duration) {
	shard := c.getShard(key)
	shard.Set(key, value, duration)
}