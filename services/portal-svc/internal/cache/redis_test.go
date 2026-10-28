package cache

import (
	"errors"
	"testing"
	"time"
)

// disabledCache 直接构造降级态实例：行为用例不必每个都触发 New 的拨号探测
// （不可达地址会吃满 2s DialTimeout），New 的探测语义另有专门用例覆盖。
func disabledCache(prefix string) *Cache {
	return &Cache{prefix: prefix, ttl: defaultTTL, enabled: false}
}

// TestNew_GracefulFallback 钉住最重要的契约：Redis 不可达时 New 不 panic、不返回 nil，
// 而是进入降级态让服务继续用主数据源。
func TestNew_GracefulFallback(t *testing.T) {
	t.Setenv("REDIS_ADDR", "127.0.0.1:1")
	c := New("portal:")
	t.Cleanup(func() { _ = c.Close() })

	if c == nil {
		t.Fatal("Redis 不可达时 New 不应返回 nil")
	}
	if c.Enabled() {
		t.Fatal("Redis 不可达时应进入降级态")
	}
	if c.key("x") != "portal:x" {
		t.Errorf("prefix 未生效：%q", c.key("x"))
	}
}

func TestDisabled_ReadsMissAndWritesNoop(t *testing.T) {
	c := disabledCache("portal:")

	var dest map[string]string
	if c.Get("k", &dest) {
		t.Error("降级态 Get 必须返回 miss")
	}
	if c.Exists("k") {
		t.Error("降级态 Exists 必须返回 false")
	}

	// 写操作不得 panic，也不得阻塞。
	c.Set("k", map[string]string{"a": "b"})
	c.SetWithTTL("k", map[string]string{"a": "b"}, time.Minute)
	c.Delete("k")
	c.Set("unmarshalable", make(chan int))
}

func TestGetOrDefault_DisabledCallsFetch(t *testing.T) {
	c := disabledCache("portal:")

	var (
		calls int
		dest  struct {
			Name string `json:"name"`
		}
	)
	hit := c.GetOrDefault("user:1", &dest, func() (interface{}, error) {
		calls++
		return map[string]string{"name": "alice"}, nil
	})

	if hit {
		t.Error("降级态应返回 false（语义是 miss，即使 fetch 已执行）")
	}
	if calls != 1 {
		t.Errorf("fetch 调用次数 = %d，期望 1（必须回落主数据源）", calls)
	}
	if dest.Name != "alice" {
		t.Errorf("fetch 结果应填入 dest，得到 %+v", dest)
	}
}

func TestGetOrDefault_FetchError(t *testing.T) {
	c := disabledCache("portal:")
	sentinel := errors.New("db down")

	var dest map[string]string
	if hit := c.GetOrDefault("k", &dest, func() (interface{}, error) { return nil, sentinel }); hit {
		t.Error("fetch 失败应返回 false")
	}
	if dest != nil {
		t.Errorf("fetch 失败不应写入 dest，得到 %+v", dest)
	}
}

func TestGetOrDefault_UnmarshalableFetchResult(t *testing.T) {
	c := disabledCache("portal:")

	var dest map[string]string
	if hit := c.GetOrDefault("k", &dest, func() (interface{}, error) { return make(chan int), nil }); hit {
		t.Error("不可序列化的 fetch 结果应返回 false")
	}
}

func TestKeyPrefixAndTTL(t *testing.T) {
	c := disabledCache("portal:")
	if got := c.key("abc"); got != "portal:abc" {
		t.Errorf("key() = %q，期望 portal:abc（前缀隔离同库多服务）", got)
	}
	if c.ttl != defaultTTL {
		t.Errorf("默认 TTL = %v，期望 %v", c.ttl, defaultTTL)
	}
}

// TestNewWithTTL 用不可达地址走一遍构造（约 2s 拨号），只验证 TTL 覆盖与前缀保留。
func TestNewWithTTL(t *testing.T) {
	t.Setenv("REDIS_ADDR", "127.0.0.1:1")
	c := NewWithTTL("portal:", time.Minute)
	t.Cleanup(func() { _ = c.Close() })

	if c.ttl != time.Minute {
		t.Errorf("NewWithTTL 未生效：%v", c.ttl)
	}
	if c.key("x") != "portal:x" {
		t.Errorf("NewWithTTL 应保留前缀")
	}
}

func TestClose_NilClientSafe(t *testing.T) {
	c := disabledCache("portal:")
	if err := c.Close(); err != nil {
		t.Fatalf("Close(nil client): %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("重复 Close(nil client) 不应报错：%v", err)
	}
}
