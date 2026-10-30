package cache

import (
	"errors"
	"testing"
	"time"
)

// newDisabledCache 直接构造降级模式实例：该包的核心契约是 Redis 不可用时
// 服务照常工作，绝大多数用例不该为此真连一次 Redis（每次 2s 超时）。
func newDisabledCache(t *testing.T, prefix string) *Cache {
	t.Helper()
	c := &Cache{prefix: prefix, ttl: defaultTTL, enabled: false}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// New 必须在 Redis 连不上时降级而不是返回半可用实例。
func TestNewDetectsUnreachableRedis(t *testing.T) {
	t.Setenv("REDIS_ADDR", "127.0.0.1:1")
	c := New("gpu:")
	t.Cleanup(func() { _ = c.Close() })
	if c.Enabled() {
		t.Fatal("REDIS_ADDR=127.0.0.1:1 不应连上 Redis，期望降级模式")
	}
}

func TestDisabledCacheDegradesGracefully(t *testing.T) {
	c := newDisabledCache(t, "gpu:")
	if got := c.key("dev:1"); got != "gpu:dev:1" {
		t.Fatalf("key 前缀 = %q, want gpu:dev:1", got)
	}

	var v struct{ Name string }
	if c.Get("dev:1", &v) {
		t.Fatal("降级模式下 Get 应返回 miss")
	}
	if c.Exists("dev:1") {
		t.Fatal("降级模式下 Exists 应为 false")
	}
	// 写操作必须是 no-op 且不 panic。
	c.Set("dev:1", &v)
	c.SetWithTTL("dev:1", &v, time.Second)
	c.Delete("dev:1")
	if c.Get("dev:1", &v) {
		t.Fatal("降级模式下写入后不应读到")
	}
}

func TestGetOrDefaultFallsBackToFetch(t *testing.T) {
	c := newDisabledCache(t, "gpu:")

	calls := 0
	var dest struct {
		Name string `json:"name"`
	}
	hit := c.GetOrDefault("dev:1", &dest, func() (interface{}, error) {
		calls++
		return struct {
			Name string `json:"name"`
		}{Name: "gpu-0"}, nil
	})
	if hit {
		t.Fatal("降级模式下 GetOrDefault 应返回 false（未命中缓存）")
	}
	if calls != 1 {
		t.Fatalf("降级模式下 fetchFn 应被调用且只调一次, got %d", calls)
	}
	if dest.Name != "gpu-0" {
		t.Fatalf("fetchFn 结果未回填 dest: %+v", dest)
	}
}

func TestGetOrDefaultFetchError(t *testing.T) {
	c := newDisabledCache(t, "gpu:")
	var dest struct{}
	hit := c.GetOrDefault("dev:1", &dest, func() (interface{}, error) {
		return nil, errors.New("db down")
	})
	if hit {
		t.Fatal("fetchFn 失败时应返回 false")
	}
}

func TestNewWithTTL(t *testing.T) {
	c := newDisabledCache(t, "gpu:")
	if c.ttl != defaultTTL {
		t.Fatalf("默认 TTL = %v, want %v", c.ttl, defaultTTL)
	}
	t.Setenv("REDIS_ADDR", "127.0.0.1:1")
	c2 := NewWithTTL("gpu:", time.Second)
	t.Cleanup(func() { _ = c2.Close() })
	if c2.ttl != time.Second {
		t.Fatalf("自定义 TTL = %v", c2.ttl)
	}
}
