package cache

import (
	"context"
	"sync"
	"time"
	"web-search-api-for-llms/internal/extractor"

	"github.com/cespare/xxhash/v2"
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
		// Pass -1 for cleanupInterval to prevent go-cache from starting its own janitor.
		c.shards[i] = cache.New(defaultExpiration, -1)
	}
	return c
}

func (c *ShardedMemoryCache) getShard(key string) *cache.Cache {
	// Use xxhash for faster hashing
	hasher := xxhash.New()
	_, _ = hasher.Write([]byte(key))
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

// MGetExtractedResults retrieves multiple ExtractedResults from the sharded cache concurrently.
func (c *ShardedMemoryCache) MGetExtractedResults(ctx context.Context, keys []string) (map[string]*extractor.ExtractedResult, error) {
	if len(keys) == 0 {
		return make(map[string]*extractor.ExtractedResult), nil
	}

	// Group keys by shard index
	keysByShard := make([][]string, shardCount)
	for _, key := range keys {
		hasher := xxhash.New()
		_, _ = hasher.Write([]byte(key))
		shardIndex := hasher.Sum64() & (shardCount - 1)
		keysByShard[shardIndex] = append(keysByShard[shardIndex], key)
	}

	resultsMap := make(map[string]*extractor.ExtractedResult)
	var mu sync.Mutex // To protect the results map
	var wg sync.WaitGroup

	// Concurrently get keys from each shard that has them
	for i, shardKeys := range keysByShard {
		if len(shardKeys) > 0 {
			wg.Add(1)
			go func(shard *cache.Cache, keys []string) {
				defer wg.Done()
				for _, key := range keys {
					if val, found := shard.Get(key); found {
						if result, ok := val.(*extractor.ExtractedResult); ok {
							mu.Lock()
							resultsMap[key] = result
							mu.Unlock()
						}
					}
				}
			}(c.shards[i], shardKeys)
		}
	}

	wg.Wait()
	return resultsMap, nil
}
// MSet provides a batched write for the sharded in-memory cache.
// Note: This is not a true pipelined operation like in Redis, but it satisfies the interface.
func (c *ShardedMemoryCache) MSet(ctx context.Context, items map[string]interface{}, duration time.Duration) error {
	// In-memory cache doesn't have a pipeline, so we just iterate.
	// This is still better than calling Set individually from the handler.
	for key, value := range items {
		c.Set(ctx, key, value, duration)
	}
	return nil
}

// DeleteExpired manually deletes expired items from all shards.
func (c *ShardedMemoryCache) DeleteExpired() {
	for _, shard := range c.shards {
		shard.DeleteExpired()
	}
}