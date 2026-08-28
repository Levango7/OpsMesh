package cache

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// defaultRedisAddr is the default Redis address used when REDIS_ADDR env var is not set.
	defaultRedisAddr = "localhost:6379"
	// defaultTTL is the default expiration time for cached entries.
	defaultTTL = 5 * time.Minute
	// opTimeout is the timeout for individual Redis operations.
	opTimeout = 2 * time.Second
)

// Cache provides a Redis-backed caching layer with graceful fallback.
//
// When Redis is unavailable, all operations degrade gracefully:
// - Get/GetOrDefault return cache misses (nil/fallback) and log a warning.
// - Set/Delete/Exists log warnings and return without error.
// This ensures the service continues to function using the primary data source.
type Cache struct {
	client  *redis.Client
	prefix  string
	ttl     time.Duration
	enabled bool
}

// New creates a Cache instance configured via environment variables.
//
// The REDIS_ADDR env var controls the Redis server address (default "localhost:6379").
// If REDIS_ADDR is empty or Redis is unreachable, the Cache operates in disabled mode,
// where all reads return misses and all writes are no-ops (with logged warnings).
//
// The prefix parameter isolates this service's keys (e.g. "auth:", "device:").
func New(prefix string) *Cache {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = defaultRedisAddr
	}

	client := redis.NewClient(&redis.Options{
		Addr:        addr,
		DialTimeout: opTimeout,
		ReadTimeout: opTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	enabled := true
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[cache] Redis unreachable at %s (graceful fallback active): %v", addr, err)
		enabled = false
	}

	return &Cache{
		client:  client,
		prefix:  prefix,
		ttl:     defaultTTL,
		enabled: enabled,
	}
}

// NewWithTTL creates a Cache with a custom default TTL.
func NewWithTTL(prefix string, ttl time.Duration) *Cache {
	c := New(prefix)
	c.ttl = ttl
	return c
}

// key prepends the service prefix to the cache key.
func (c *Cache) key(k string) string {
	return c.prefix + k
}

// Get retrieves a cached value and unmarshals it into dest.
//
// Returns true if the cache hit and dest was populated.
// On cache miss or Redis error, returns false and logs a warning.
func (c *Cache) Get(key string, dest interface{}) bool {
	if !c.enabled {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	data, err := c.client.Get(ctx, c.key(key)).Bytes()
	if err != nil {
		if err != redis.Nil {
			log.Printf("[cache] Get %s failed (fallback to DB): %v", key, err)
		}
		return false
	}

	if err := json.Unmarshal(data, dest); err != nil {
		log.Printf("[cache] Get %s unmarshal error: %v", key, err)
		return false
	}
	return true
}

// Set stores a value in the cache with the default TTL.
//
// Values are JSON-serialized before storing.
// If Redis is unavailable, the operation is a no-op with a logged warning.
func (c *Cache) Set(key string, value interface{}) {
	c.SetWithTTL(key, value, c.ttl)
}

// SetWithTTL stores a value with a custom TTL.
func (c *Cache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	if !c.enabled {
		return
	}

	data, err := json.Marshal(value)
	if err != nil {
		log.Printf("[cache] Set %s marshal error: %v", key, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if err := c.client.Set(ctx, c.key(key), data, ttl).Err(); err != nil {
		log.Printf("[cache] Set %s failed: %v", key, err)
	}
}

// Delete removes a key from the cache.
//
// If Redis is unavailable, the operation is a no-op with a logged warning.
func (c *Cache) Delete(key string) {
	if !c.enabled {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if err := c.client.Del(ctx, c.key(key)).Err(); err != nil {
		log.Printf("[cache] Delete %s failed: %v", key, err)
	}
}

// Exists checks whether a key is present in the cache.
//
// Returns true only if the key exists and Redis is reachable.
func (c *Cache) Exists(key string) bool {
	if !c.enabled {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	n, err := c.client.Exists(ctx, c.key(key)).Result()
	if err != nil {
		log.Printf("[cache] Exists %s failed: %v", key, err)
		return false
	}
	return n > 0
}

// GetOrDefault attempts to fetch from cache; on miss, calls fetchFn and caches the result.
//
// This is the recommended pattern for cache-aside usage:
//
//	var user User
//	if cache.GetOrDefault("user:"+id, &user, func() (interface{}, error) {
//	    return db.GetUser(id)
//	}) {
//	    // cache hit
//	}
//
// Returns true on cache hit. On miss, fetchFn is invoked, the result is cached and
// unmarshaled into dest. If Redis is unavailable, fetchFn is always called directly.
func (c *Cache) GetOrDefault(key string, dest interface{}, fetchFn func() (interface{}, error)) bool {
	if c.Get(key, dest) {
		return true
	}

	val, err := fetchFn()
	if err != nil {
		return false
	}

	c.Set(key, val)

	// Re-marshal and unmarshal to populate dest (avoids reflection complexity).
	data, err := json.Marshal(val)
	if err != nil {
		log.Printf("[cache] GetOrDefault %s marshal error: %v", key, err)
		return false
	}
	if err := json.Unmarshal(data, dest); err != nil {
		log.Printf("[cache] GetOrDefault %s unmarshal error: %v", key, err)
		return false
	}
	return false
}

// Close gracefully shuts down the Redis connection.
func (c *Cache) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// Enabled reports whether the cache is connected to Redis and operational.
func (c *Cache) Enabled() bool {
	return c.enabled
}
