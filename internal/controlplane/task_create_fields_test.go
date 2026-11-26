package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// TestHandleCreateTask_PreservesPathAndContent 钉前后端契约：
// TasksView 表单会提交 path/content；文件任务的路径与内容、服务任务的服务名
// 必须原样进入任务存储，不能只在响应或执行阶段丢失。
func TestHandleCreateTask_PreservesPathAndContent(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg", TenantID: "t1"})
	s := &Server{store: st, requireAuth: false, cfg: &config.Config{TaskMaxRetries: 3, Demo: true}}

	cases := []struct {
		name        string
		typ         string
		command     string
		path        string
		content     string
		wantPath    string
		wantContent string
	}{
		{name: "file task keeps target path and body", typ: "file", command: "", path: "/tmp/opsmesh-contract.txt", content: "hello", wantPath: "/tmp/opsmesh-contract.txt", wantContent: "hello"},
		{name: "service task keeps service name in path", typ: "service", command: "status", path: "nginx", wantPath: "nginx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{
				"agentID": a.AgentID, "type": tc.typ, "command": tc.command,
				"path": tc.path, "content": tc.content,
			})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(body))
			req.Header.Set("X-Tenant-ID", "t1")
			req.Header.Set("X-User-Id", "u1")
			rec := httptest.NewRecorder()
			s.handleCreateTask(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("create = %d, want 201; body=%s", rec.Code, rec.Body.String())
			}
			var created proto.Task
			if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}
			stored := st.TaskByID(created.TaskID)
			if stored == nil {
				t.Fatal("创建后任务应可从存储读取")
			}
			if stored.Path != tc.wantPath {
				t.Fatalf("Path = %q, want %q", stored.Path, tc.wantPath)
			}
			if stored.Content != tc.wantContent {
				t.Fatalf("Content = %q, want %q", stored.Content, tc.wantContent)
			}
		})
	}
}

// TestHandleCreateTask_IdempotencyPreservesPathContent 覆盖幂等创建分支：
// 带 IdempotencyKey 时 path/content 同样不得丢失。
func TestHandleCreateTask_IdempotencyPreservesPathContent(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg", TenantID: "t1"})
	s := &Server{store: st, requireAuth: false, cfg: &config.Config{TaskMaxRetries: 3, Demo: true}}
	body, _ := json.Marshal(map[string]string{
		"agentID": a.AgentID, "type": "file", "command": "",
		"path": "/tmp/opsmesh-idem.txt", "content": "idem-body", "idempotencyKey": "file-idem-1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec := httptest.NewRecorder()
	s.handleCreateTask(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created proto.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	stored := st.TaskByID(created.TaskID)
	if stored == nil || stored.Path != "/tmp/opsmesh-idem.txt" || stored.Content != "idem-body" {
		t.Fatalf("幂等创建丢失 path/content: %+v", stored)
	}
}
