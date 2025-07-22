package cache

import (
	"context"
	"time"
	"web-search-api-for-llms/internal/extractor"
)

// Cache is the interface for a cache.
type Cache interface {
	GetExtractedResult(ctx context.Context, key string) (*extractor.ExtractedResult, bool)
	GetSearchURLs(ctx context.Context, key string) ([]string, bool)
	Set(ctx context.Context, key string, value interface{}, duration time.Duration)
	// Add this new method for batched lookups
	MGetExtractedResults(ctx context.Context, keys []string) (map[string]*extractor.ExtractedResult, error)
	MSet(ctx context.Context, items map[string]interface{}, duration time.Duration) error
}