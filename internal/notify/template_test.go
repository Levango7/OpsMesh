package notify

import (
	"strings"
	"testing"
	"time"
)

// TestTemplate_Render 验证模板变量替换。
func TestTemplate_Render(t *testing.T) {
	tmpl := &Template{
		ID:     "test-1",
		Name:   "测试模板",
		Type:   "alert",
		Title:  "[OpsMesh][{{.Severity}}] {{.DeviceID}}",
		Body:   "级别: {{.Severity}}\n设备: {{.DeviceID}}\n消息: {{.Message}}",
		Format: "markdown",
	}
	data := map[string]interface{}{
		"Severity": "critical",
		"DeviceID": "dev-1",
		"Message":  "CPU 95%",
	}
	msg, err := tmpl.Render(data)
	if err != nil {
		t.Fatalf("Render err = %v", err)
	}
	if msg.Title != "[OpsMesh][critical] dev-1" {
		t.Fatalf("Title = %q, want [OpsMesh][critical] dev-1", msg.Title)
	}
	if !strings.Contains(msg.Body, "CPU 95%") {
		t.Fatalf("Body missing variable: %s", msg.Body)
	}
	if !strings.Contains(msg.Body, "dev-1") {
		t.Fatalf("Body missing device: %s", msg.Body)
	}
	if msg.Format != "markdown" {
		t.Fatalf("Format = %q, want markdown", msg.Format)
	}
}

// TestTemplate_Render_ExtractFields 验证从 map data 提取 Severity/Source/Timestamp。
func TestTemplate_Render_ExtractFields(t *testing.T) {
	tmpl := &Template{
		ID:     "test-2",
		Title:  "{{.Title}}",
		Body:   "{{.Body}}",
		Format: "markdown",
	}
	ts := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	data := map[string]interface{}{
		"Title":     "x",
		"Body":      "y",
		"Severity":  "warning",
		"Source":    "agent-1",
		"Timestamp": ts,
	}
	msg, err := tmpl.Render(data)
	if err != nil {
		t.Fatalf("Render err = %v", err)
	}
	if msg.Severity != "warning" {
		t.Fatalf("Severity = %q, want warning", msg.Severity)
	}
	if msg.Source != "agent-1" {
		t.Fatalf("Source = %q, want agent-1", msg.Source)
	}
	if !msg.Timestamp.Equal(ts) {
		t.Fatalf("Timestamp = %v, want %v", msg.Timestamp, ts)
	}
}

// TestTemplate_Render_EmptyFields 验证空模板字段返回空字符串。
func TestTemplate_Render_EmptyFields(t *testing.T) {
	tmpl := &Template{ID: "empty", Format: "text"}
	msg, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("Render err = %v", err)
	}
	if msg.Title != "" || msg.Body != "" {
		t.Fatalf("empty template should render empty title/body: %+v", msg)
	}
	if msg.Format != "text" {
		t.Fatalf("Format = %q, want text", msg.Format)
	}
}

// TestTemplate_Render_SyntaxError 验证模板语法错误返回 error。
func TestTemplate_Render_SyntaxError(t *testing.T) {
	tmpl := &Template{
		ID:    "bad",
		Title: "{{.Severity", // 缺少 }} 闭合
		Body:  "x",
	}
	_, err := tmpl.Render(map[string]interface{}{"Severity": "critical"})
	if err == nil {
		t.Fatal("expected syntax error, got nil")
	}
}

// TestTemplate_Render_MissingVar 验证变量缺失返回 error。
func TestTemplate_Render_MissingVar(t *testing.T) {
	tmpl := &Template{
		ID:    "missing",
		Title: "{{.NonExistent}}",
		Body:  "x",
	}
	_, err := tmpl.Render(map[string]interface{}{"Severity": "critical"})
	if err == nil {
		t.Fatal("expected missing variable error, got nil")
	}
}

// TestTemplateStore_AddGetRemove 验证模板存储增删查。
func TestTemplateStore_AddGetRemove(t *testing.T) {
	s := NewTemplateStore()
	t1 := &Template{ID: "t1", Name: "T1", Type: "alert", Title: "a", Body: "b"}
	t2 := &Template{ID: "t2", Name: "T2", Type: "task", Title: "c", Body: "d"}

	s.Add(t1)
	s.Add(t2)
	if s.Get("t1") != t1 {
		t.Fatal("Get(t1) should return t1")
	}
	if s.Get("t2") != t2 {
		t.Fatal("Get(t2) should return t2")
	}
	if s.Get("nonexistent") != nil {
		t.Fatal("Get(nonexistent) should return nil")
	}

	s.Remove("t1")
	if s.Get("t1") != nil {
		t.Fatal("Get(t1) should return nil after Remove")
	}
}

// TestTemplateStore_AddNil 验证 Add(nil) 静默跳过。
func TestTemplateStore_AddNil(t *testing.T) {
	s := NewTemplateStore()
	s.Add(nil) // 不应 panic
	if s.Size() != 0 {
		t.Fatalf("Size = %d, want 0", s.Size())
	}
}

// TestTemplateStore_ByType 验证按类型批量查询。
func TestTemplateStore_ByType(t *testing.T) {
	s := NewTemplateStore()
	s.Add(&Template{ID: "a1", Type: "alert"})
	s.Add(&Template{ID: "a2", Type: "alert"})
	s.Add(&Template{ID: "t1", Type: "task"})
	s.Add(&Template{ID: "s1", Type: "system"})

	alerts := s.ByType("alert")
	if len(alerts) != 2 {
		t.Fatalf("ByType(alert) = %d, want 2", len(alerts))
	}
	tasks := s.ByType("task")
	if len(tasks) != 1 {
		t.Fatalf("ByType(task) = %d, want 1", len(tasks))
	}
	none := s.ByType("nonexistent")
	if none != nil {
		t.Fatalf("ByType(nonexistent) = %v, want nil", none)
	}
}

// TestTemplateStore_RenderByID 验证按 ID 渲染。
func TestTemplateStore_RenderByID(t *testing.T) {
	s := NewTemplateStore()
	s.Add(&Template{ID: "r1", Title: "[{{.Level}}]", Body: "{{.Msg}}", Format: "text"})

	msg, err := s.RenderByID("r1", map[string]interface{}{"Level": "warn", "Msg": "hello"})
	if err != nil {
		t.Fatalf("RenderByID err = %v", err)
	}
	if msg.Title != "[warn]" {
		t.Fatalf("Title = %q, want [warn]", msg.Title)
	}
	if msg.Body != "hello" {
		t.Fatalf("Body = %q, want hello", msg.Body)
	}

	// 不存在的模板
	_, err = s.RenderByID("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent template")
	}
}

// TestTemplateStore_LoadDefault 验证加载内置默认模板。
func TestTemplateStore_LoadDefault(t *testing.T) {
	s := NewTemplateStore()
	s.LoadDefaultTemplates()
	if s.Get("alert-default") == nil {
		t.Fatal("alert-default template missing")
	}
	if s.Get("task-default") == nil {
		t.Fatal("task-default template missing")
	}
	if s.Get("system-default") == nil {
		t.Fatal("system-default template missing")
	}
}

// TestDefaultAlertTemplate_Render 验证内置告警模板渲染。
func TestDefaultAlertTemplate_Render(t *testing.T) {
	data := map[string]interface{}{
		"Severity":  "critical",
		"DeviceID":  "dev-1",
		"AgentID":   "agent-1",
		"Message":   "CPU 95%",
		"CreatedAt": "2026-08-11 12:00:00",
	}
	msg, err := DefaultAlertTemplate.Render(data)
	if err != nil {
		t.Fatalf("Render err = %v", err)
	}
	if !strings.Contains(msg.Title, "critical") {
		t.Fatalf("Title missing severity: %s", msg.Title)
	}
	if !strings.Contains(msg.Body, "CPU 95%") {
		t.Fatalf("Body missing message: %s", msg.Body)
	}
}
