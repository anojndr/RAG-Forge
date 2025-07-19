package cache

import "time"

// Cache is the interface for a cache.
type Cache interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{}, duration time.Duration)
}