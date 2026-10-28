package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
)

// 本文件只测同步引擎的调度与配置语义；默认同步函数会调 gateway，
// 因此凡是执行函数的分支都用 RegisterSyncFunc 注入替身。

func TestNewSyncEngine_DefaultConfigs(t *testing.T) {
	e := NewSyncEngine(NewInMemoryKnowledgeStore(), nil)
	cfgs := e.GetConfigs()
	if len(cfgs) != 5 {
		t.Fatalf("默认配置数量 = %d，期望 5", len(cfgs))
	}
	seen := make(map[KnowledgeSource]bool, len(cfgs))
	for _, c := range cfgs {
		seen[c.Source] = true
		if !c.Enabled {
			t.Errorf("%s 默认应启用", c.Source)
		}
		if c.Interval != time.Hour {
			t.Errorf("%s 默认周期 = %v，期望 1h", c.Source, c.Interval)
		}
		if c.BatchSize != 100 {
			t.Errorf("%s 默认批次 = %d，期望 100", c.Source, c.BatchSize)
		}
	}
	for _, want := range []KnowledgeSource{SourceCMDB, SourceAlert, SourceRunbook, SourceGit, SourceConfluence} {
		if !seen[want] {
			t.Errorf("缺少默认配置：%s", want)
		}
	}
}

func TestGetConfigs_ReturnsCopy(t *testing.T) {
	e := NewSyncEngine(NewInMemoryKnowledgeStore(), nil)
	cfgs := e.GetConfigs()
	cfgs[0].Enabled = false
	cfgs[0].Interval = 1

	again := e.GetConfigs()
	for _, c := range again {
		if c.Source == cfgs[0].Source {
			if !c.Enabled || c.Interval != time.Hour {
				t.Errorf("GetConfigs 返回值被外部修改后影响了引擎内部配置：%+v", c)
			}
			return
		}
	}
	t.Fatal("未找到被修改的配置项")
}

func TestUpdateConfig(t *testing.T) {
	e := NewSyncEngine(NewInMemoryKnowledgeStore(), nil)

	e.UpdateConfig(SourceCMDB, false, 5*time.Minute)
	cfg := findConfig(t, e, SourceCMDB)
	if cfg.Enabled || cfg.Interval != 5*time.Minute {
		t.Errorf("UpdateConfig 未生效：%+v", cfg)
	}

	// interval<=0 表示只改开关，保留原周期。
	e.UpdateConfig(SourceCMDB, true, 0)
	cfg = findConfig(t, e, SourceCMDB)
	if !cfg.Enabled || cfg.Interval != 5*time.Minute {
		t.Errorf("interval<=0 不应覆盖周期：%+v", cfg)
	}

	// 未知源是静默空操作（不新增配置）。
	e.UpdateConfig(KnowledgeSource("nope"), true, time.Minute)
	if n := len(e.GetConfigs()); n != 5 {
		t.Errorf("未知源不应新增配置，得到 %d 条", n)
	}
}

func findConfig(t *testing.T, e *SyncEngine, source KnowledgeSource) *SyncConfig {
	t.Helper()
	for _, c := range e.GetConfigs() {
		if c.Source == source {
			return c
		}
	}
	t.Fatalf("未找到配置：%s", source)
	return nil
}

func TestSyncNow_Errors(t *testing.T) {
	e := NewSyncEngine(NewInMemoryKnowledgeStore(), nil)

	if _, err := e.SyncNow(context.Background(), KnowledgeSource("nope")); err == nil ||
		!strings.Contains(err.Error(), "not registered") {
		t.Fatalf("未注册源应报 not registered，得到 %v", err)
	}

	e.UpdateConfig(SourceCMDB, false, 0)
	if _, err := e.SyncNow(context.Background(), SourceCMDB); err == nil ||
		!strings.Contains(err.Error(), "disabled") {
		t.Fatalf("停用源应报 disabled，得到 %v", err)
	}
}

func TestSyncNow_CustomFunc(t *testing.T) {
	e := NewSyncEngine(NewInMemoryKnowledgeStore(), nil)
	var ran int
	e.RegisterSyncFunc(SourceCMDB, func(context.Context, KnowledgeStore, *gateway.Gateway) (int, error) {
		ran++
		return 7, nil
	})

	count, err := e.SyncNow(context.Background(), SourceCMDB)
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	if count != 7 || ran != 1 {
		t.Errorf("SyncNow = (%d, ran=%d)，期望 (7, 1)", count, ran)
	}
	if cfg := findConfig(t, e, SourceCMDB); cfg.LastSync.IsZero() {
		t.Error("成功同步后应记录 LastSync")
	}

	sentinel := errors.New("boom")
	e.RegisterSyncFunc(SourceAlert, func(context.Context, KnowledgeStore, *gateway.Gateway) (int, error) {
		return 0, sentinel
	})
	if _, err := e.SyncNow(context.Background(), SourceAlert); !errors.Is(err, sentinel) {
		t.Fatalf("同步函数报错应原样返回，得到 %v", err)
	}
	if cfg := findConfig(t, e, SourceAlert); !cfg.LastSync.IsZero() {
		t.Error("同步失败时不应更新 LastSync")
	}
}

func TestSyncAll_AggregatesResults(t *testing.T) {
	e := NewSyncEngine(NewInMemoryKnowledgeStore(), nil)
	boom := errors.New("boom")

	for _, src := range []KnowledgeSource{SourceCMDB, SourceAlert, SourceRunbook, SourceGit, SourceConfluence} {
		e.RegisterSyncFunc(src, func(context.Context, KnowledgeStore, *gateway.Gateway) (int, error) {
			if src == SourceAlert {
				return 0, boom
			}
			return 3, nil
		})
	}
	e.UpdateConfig(SourceGit, false, 0) // 停用的源应被跳过

	results, err := e.SyncAll(context.Background())
	if err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if results[SourceCMDB] != 3 || results[SourceRunbook] != 3 || results[SourceConfluence] != 3 {
		t.Errorf("成功源计数不对：%v", results)
	}
	if results[SourceAlert] != -1 {
		t.Errorf("失败源应记 -1，得到 %d", results[SourceAlert])
	}
	if _, ok := results[SourceGit]; ok {
		t.Error("停用源不应出现在结果中")
	}
}
