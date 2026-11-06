package cost

// budget_enforcement_test.go — P1-7（qoder 2026-11-05T16:55 交接）的钉子：
// GET /cost/budget 的响应体此前只有 usedToday/usedMonth 等数，客户端无从得知
// 这是**进程内计数**（重启归零、多副本各自计数≈额度×副本数），口径只活在
// gateway.go 的注释里。现在响应体带 enforcement/scope 两键；旧键原样扁平保留
// （嵌入字段提升，非破坏）——两个方向都要钉住。
import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleBudgetExposesEnforcementMetadata(t *testing.T) {
	h := NewCostHandler(NewBudgetManager(), NewModelRouter(), NewCostTracker())

	rec := httptest.NewRecorder()
	h.HandleBudget(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cost/budget", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /cost/budget 应 200，得到 %d %s", rec.Code, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (%s)", err, rec.Body.String())
	}

	// 新增两键：口径显式化
	if body["enforcement"] != "in-process" {
		t.Fatalf("enforcement 应为 in-process，得到 %v", body["enforcement"])
	}
	if body["scope"] != "per-tenant, per-replica" {
		t.Fatalf("scope 应为 per-tenant, per-replica，得到 %v", body["scope"])
	}

	// 旧键仍在且扁平（嵌入提升，形状非破坏）——现有前端 CostView 读 usedToday/usedToday 不迁移
	for _, k := range []string{"usedToday", "usedMonth", "dailyLimit", "monthlyLimit", "alertThreshold"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("旧键 %s 丢失——响应形状不得破坏", k)
		}
	}
}
