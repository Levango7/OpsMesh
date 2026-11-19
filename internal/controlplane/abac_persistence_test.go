// abac_persistence_test.go 批次47 ABAC 策略持久化的端到端守卫。
//
// 覆盖：handleABAC 写穿 store、重启恢复、跨副本 5s（测试注入 10ms）刷新、跨副本删除、
// store 失败 → 500 且缓存不更新。全部用内存 store 共享实例模拟多副本/重启，
// 不依赖真库；不引入 >2s 的等待（轮询上限 2s）。
package controlplane

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

const abacPersistTenant = "t-abac-persist"

const abacPersistCreateBody = `{"action":"policy-create","subject":"alice","resource":"vm:42","actionAttr":"read"}`

const abacPersistEvalBody = `{"action":"evaluate","subject":"alice","resource":"vm:42","actionAttr":"read"}`

const abacPersistDeleteBody = `{"action":"policy-delete","subject":"alice","resource":"vm:42","actionAttr":"read"}`

// abacWaitForDecision 轮询直到 evaluate 达到期望决策或超时（上限 2s）。
func abacWaitForDecision(t *testing.T, s *Server, tenantID string, want bool) (string, string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		allowed, id, reason := s.evaluateABAC(tenantID, "alice", "vm:42", "read", "")
		if allowed == want {
			return id, reason
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待 ABAC 决策 allowed=%v 超时，最后得到 allowed=%v id=%q reason=%q", want, allowed, id, reason)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// policy-create 必须写穿 store；新 Server 共用同一 store 时启动装载即可恢复决策。
func TestABACPersistence_CreateWritesStoreAndSurvivesRestart(t *testing.T) {
	s := newTestServer()
	w := abacPostEvaluate(t, s, abacPersistTenant, abacPersistCreateBody)
	if w.Code != http.StatusAccepted {
		t.Fatalf("policy-create 应 202，得到 %d body=%s", w.Code, w.Body.String())
	}

	wantID := abacPolicyID(abacPersistTenant, "read", "alice", "vm:42")
	recs := s.store.ListABACPolicyRecords(abacPersistTenant)
	if len(recs) != 1 {
		t.Fatalf("store 应落 1 条策略，得到 %d 条", len(recs))
	}
	rec := recs[0]
	if rec.ID != wantID || rec.TenantID != abacPersistTenant ||
		rec.Subject != "alice" || rec.Resource != "vm:42" || rec.Action != "read" || rec.Effect != "allow" {
		t.Fatalf("落库字段与请求不一致: %+v", rec)
	}
	if rec.Version != 1 {
		t.Fatalf("首次创建 version=%d, want 1", rec.Version)
	}

	// 重复创建同一三元组：store 版本自增，缓存仍只有一条。
	w = abacPostEvaluate(t, s, abacPersistTenant, abacPersistCreateBody)
	if w.Code != http.StatusAccepted {
		t.Fatalf("重复 policy-create 应 202，得到 %d body=%s", w.Code, w.Body.String())
	}
	if recs = s.store.ListABACPolicyRecords(abacPersistTenant); len(recs) != 1 || recs[0].Version != 2 {
		t.Fatalf("重复创建后 store 应为 1 条 version=2，得到 %+v", recs)
	}

	// 模拟重启：新 Server 共用同一 store，启动装载后 evaluate 仍命中。
	s2 := &Server{store: s.store, cfg: &config.Config{Demo: true}}
	s2.initABACPoliciesFromStore()
	w = abacPostEvaluate(t, s2, abacPersistTenant, abacPersistEvalBody)
	if w.Code != http.StatusOK {
		t.Fatalf("重启后 evaluate 应 200，得到 %d body=%s", w.Code, w.Body.String())
	}
	resp := abacDecodeBody(t, w)
	if resp["decision"] != "allow" || resp["matchedPolicyID"] != wantID {
		t.Fatalf("重启后应命中持久化策略，得到 %v", resp)
	}
}

// 副本 A 的创建/删除经周期刷新收敛到副本 B（B 只回读自己见过的租户）。
func TestABACPersistence_CrossReplicaRefreshAndDelete(t *testing.T) {
	shared := store.NewMemoryStore().WithDemo(true)
	sA := &Server{store: shared, cfg: &config.Config{Demo: true}}
	sB := &Server{store: shared, cfg: &config.Config{Demo: true}}

	// B 先处理过一次该租户的评估（默认拒绝），刷新循环据此纳入该租户。
	if allowed, _, _ := sB.evaluateABAC(abacPersistTenant, "alice", "vm:42", "read", ""); allowed {
		t.Fatal("初始状态应为 default-deny")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sB.startABACPolicyRefresh(ctx, 10*time.Millisecond)

	// A 创建策略：B 在刷新后应看到并放行。
	w := abacPostEvaluate(t, sA, abacPersistTenant, abacPersistCreateBody)
	if w.Code != http.StatusAccepted {
		t.Fatalf("A policy-create 应 202，得到 %d body=%s", w.Code, w.Body.String())
	}
	wantID := abacPolicyID(abacPersistTenant, "read", "alice", "vm:42")
	if id, reason := abacWaitForDecision(t, sB, abacPersistTenant, true); id != wantID {
		t.Fatalf("B 命中策略 ID=%q want %q (reason=%q)", id, wantID, reason)
	}

	// A 删除策略：B 在刷新后应回到 default-deny。
	w = abacPostEvaluate(t, sA, abacPersistTenant, abacPersistDeleteBody)
	if w.Code != http.StatusAccepted {
		t.Fatalf("A policy-delete 应 202，得到 %d body=%s", w.Code, w.Body.String())
	}
	abacWaitForDecision(t, sB, abacPersistTenant, false)
	if recs := shared.ListABACPolicyRecords(abacPersistTenant); len(recs) != 0 {
		t.Fatalf("删除后 store 应为空，得到 %+v", recs)
	}
	if got := sB.listABACPolicies(abacPersistTenant); len(got) != 0 {
		t.Fatalf("删除后 B 缓存应为空，得到 %+v", got)
	}
}

// abacUpsertFailStore 模拟持久化后端写失败（只覆盖 upsert，其余方法委托真实 store）。
type abacUpsertFailStore struct {
	store.Store
}

func (s *abacUpsertFailStore) UpsertABACPolicyRecord(p store.ABACPolicyRecord) (store.ABACPolicyRecord, bool) {
	return p, false
}

// store 写失败必须 500，且缓存与 store 都不得出现该策略（不得假成功）。
func TestABACPersistence_StoreFailureReturns500AndKeepsCache(t *testing.T) {
	mem := store.NewMemoryStore().WithDemo(true)
	s := &Server{store: &abacUpsertFailStore{Store: mem}, cfg: &config.Config{Demo: true}}

	w := abacPostEvaluate(t, s, abacPersistTenant, abacPersistCreateBody)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("store 失败时 policy-create 应 500，得到 %d body=%s", w.Code, w.Body.String())
	}
	if got := s.listABACPolicies(abacPersistTenant); len(got) != 0 {
		t.Fatalf("store 失败不得更新缓存，得到 %+v", got)
	}
	if got := mem.ListABACPolicyRecords(abacPersistTenant); len(got) != 0 {
		t.Fatalf("store 失败不得落库，得到 %+v", got)
	}
	if allowed, _, _ := s.evaluateABAC(abacPersistTenant, "alice", "vm:42", "read", ""); allowed {
		t.Fatal("store 失败后仍应 default-deny")
	}
}
