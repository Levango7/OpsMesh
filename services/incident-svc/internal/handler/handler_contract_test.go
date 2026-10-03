package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/incident-svc/internal/aggregate"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/service"
)

// TestListIncidents_EmptyListIsJSONArray 契约守护：空事故列表必须序列化为 [] 而非
// null。转正运行时验收（2026-10-03）在真实栈上实测到本域返回 null、其余域返回 []，
// 前端 v-for/.map() 会直接炸——此处固化该契约防回潮。
func TestListIncidents_EmptyListIsJSONArray(t *testing.T) {
	st := models.NewMemoryStore()
	svc := service.NewService(st, aggregate.NewEngine(5*time.Minute))
	h := NewHandler(svc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	if body == "null" {
		t.Fatalf("空列表序列化成了 null，前端列表会崩：%s", body)
	}
	if body != "[]" {
		t.Fatalf("空列表响应 = %s，期望 []", body)
	}
	var arr []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("响应不是合法 JSON 数组: %v", err)
	}
	if len(arr) != 0 {
		t.Fatalf("期望 0 条，实际 %d", len(arr))
	}
}
