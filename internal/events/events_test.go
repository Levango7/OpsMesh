package events

import (
	"context"
	"testing"
)

// TestBusKinds 验证 New 现在统一返回 stampingBus 包装（工程3），
// 不再直接是底层 Bus；任意 kind 的 Publish 都不应 panic。
func TestBusKinds(t *testing.T) {
	for _, kind := range []string{"noop", "log", "", "kafka"} {
		if _, ok := New(kind, "", "").(stampingBus); !ok {
			t.Fatalf("kind %q should be wrapped in stampingBus", kind)
		}
	}
	if err := New("log", "", "").Publish(context.Background(), Event{Action: "register", Level: LevelInfo}); err != nil {
		t.Fatalf("Publish err: %v", err)
	}
}

// TestStamp_SetsVersion 验证未显式指定版本时自动加盖当前契约版本。
func TestStamp_SetsVersion(t *testing.T) {
	e := stamp(Event{Action: "register"})
	if e.Version != SchemaVersion {
		t.Fatalf("stamp did not set version: got %q want %q", e.Version, SchemaVersion)
	}
	if SchemaVersion == "" {
		t.Fatal("SchemaVersion must not be empty")
	}
}

// TestStamp_PreservesExplicit 验证调用方已显式指定版本时不被覆盖（演进兼容）。
func TestStamp_PreservesExplicit(t *testing.T) {
	e := stamp(Event{Action: "register", Version: "0.9.0"})
	if e.Version != "0.9.0" {
		t.Fatalf("stamp overrode explicit version: got %q", e.Version)
	}
}
