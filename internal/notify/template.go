
// Package notify 通知模板（M2-2）。
//
// 本文件实现通知模板：标题/正文支持 Go text/template 变量替换（{{.AlertName}} 等），
// 渲染后产出 *Message 供渠道发送。TemplateStore 提供按 ID/Type 管理多模板的能力。
package notify

import (
	"bytes"
	"fmt"
	"sync"
	"text/template"
	"time"
)

// ============================================================================
// 通知模板
// ============================================================================

// Template 通知模板：标题与正文支持 text/template 变量替换。
//
// 字段语义：
//   - ID：模板唯一标识（如 "alert-critical"）；TemplateStore 按 ID 索引。
//   - Name：模板人类可读名称（如 "Critical 告警模板"）。
//   - Type：模板类型（"alert"/"task"/"device"/"system"）；按类型批量查询。
//   - Title：模板标题（支持变量替换，如 "[OpsMesh][{{.Severity}}] {{.DeviceID}}"）。
//   - Body：模板正文（支持变量替换）。
//   - Format：正文格式（"markdown"/"text"/"html"）；渲染后填入 Message.Format。
type Template struct {
	ID    string // 模板唯一标识
	Name  string // 模板名称
	Type  string // alert / task / device / system
	Title string // 模板标题（支持变量替换）
	Body  string // 模板正文（支持变量替换）
	Format string // markdown / text / html
}

// Render 用 text/template 渲染模板，产出 *Message。
//
// data 为模板变量上下文（如 map[string]interface{}{"AlertName":"CPU高", "Severity":"critical"}）。
// 渲染失败（模板语法错误或变量缺失）时返回 error，不返回部分渲染结果。
//
// 渲染后的 Message：
//   - Title/Body：渲染后的字符串
//   - Format：模板 Format
//   - Severity/Source/Timestamp：从 data 中提取（若 data 为 map 且含对应键）
//   - Data：原始 data（供渠道定制渲染）
func (t *Template) Render(data interface{}) (*Message, error) {
	title, err := renderTemplateString("title", t.Title, data)
	if err != nil {
		return nil, fmt.Errorf("notify: render template %q title: %w", t.ID, err)
	}
	body, err := renderTemplateString("body", t.Body, data)
	if err != nil {
		return nil, fmt.Errorf("notify: render template %q body: %w", t.ID, err)
	}
	msg := &Message{
		Title:  title,
		Body:   body,
		Format: t.Format,
		Data:   data,
	}
	// 从 map data 中提取常用字段（Severity/Source/Timestamp），便于渠道默认渲染。
	if m, ok := data.(map[string]interface{}); ok {
		if v, ok := m["Severity"].(string); ok {
			msg.Severity = v
		}
		if v, ok := m["Source"].(string); ok {
			msg.Source = v
		}
		if v, ok := m["Timestamp"].(time.Time); ok {
			msg.Timestamp = v
		}
	}
	return msg, nil
}

// renderTemplateString 用 text/template 渲染单个字符串。
// name 为模板名（错误定位用），tmplStr 为模板字符串，data 为变量上下文。
// 启用 missingkey=error 选项：map 缺失键时返回 error（而非默认的 <no value>），
// 便于在渲染期发现变量缺失问题。
func renderTemplateString(name, tmplStr string, data interface{}) (string, error) {
	if tmplStr == "" {
		return "", nil
	}
	tmpl, err := template.New(name).Option("missingkey=error").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ============================================================================
// 模板存储
// ============================================================================

// TemplateStore 模板存储：按 ID 索引模板，支持按 Type 批量查询。
//
// 并发安全：sync.RWMutex 保护内部 map。Notifier 持有一个 TemplateStore 实例，
// 运行期可通过 Add/Remove 动态增删模板（如热加载配置）。
type TemplateStore struct {
	mu        sync.RWMutex
	templates map[string]*Template // key: template ID
}

// NewTemplateStore 构造空模板存储。
func NewTemplateStore() *TemplateStore {
	return &TemplateStore{templates: make(map[string]*Template)}
}

// Add 添加或替换模板（按 ID 覆盖）。t 为 nil 时静默跳过。
func (s *TemplateStore) Add(t *Template) {
	if t == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.templates[t.ID] = t
}

// Remove 删除模板（不存在时静默跳过）。
func (s *TemplateStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.templates, id)
}

// Get 按 ID 查询模板。不存在时返回 nil。
func (s *TemplateStore) Get(id string) *Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.templates[id]
}

// ByType 按 Type 批量查询模板。无匹配时返回 nil。
func (s *TemplateStore) ByType(typ string) []*Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Template
	for _, t := range s.templates {
		if t.Type == typ {
			out = append(out, t)
		}
	}
	return out
}

// All 返回所有模板（用于调试/导出）。
func (s *TemplateStore) All() []*Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Template, 0, len(s.templates))
	for _, t := range s.templates {
		out = append(out, t)
	}
	return out
}

// Size 返回当前模板数量（调试/监控用）。
func (s *TemplateStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.templates)
}

// RenderByID 按 ID 查询模板并渲染。模板不存在时返回 error。
func (s *TemplateStore) RenderByID(id string, data interface{}) (*Message, error) {
	t := s.Get(id)
	if t == nil {
		return nil, fmt.Errorf("notify: template %q not found", id)
	}
	return t.Render(data)
}

// ============================================================================
// 内置默认模板
// ============================================================================

// DefaultAlertTemplate 内置告警模板（markdown 格式）。
// 渲染上下文需提供：Severity / DeviceID / AgentID / Message / CreatedAt（time.Time）。
var DefaultAlertTemplate = &Template{
	ID:     "alert-default",
	Name:   "默认告警模板",
	Type:   "alert",
	Title:  "[OpsMesh][{{.Severity}}] {{.DeviceID}}",
	Body:   "**严重级别**: {{.Severity}}\n**设备**: {{.DeviceID}}\n**Agent**: {{.AgentID}}\n**时间**: {{.CreatedAt}}\n\n{{.Message}}",
	Format: "markdown",
}

// DefaultTaskTemplate 内置任务模板（markdown 格式）。
// 渲染上下文需提供：TaskID / Status / DeviceID / Message。
var DefaultTaskTemplate = &Template{
	ID:     "task-default",
	Name:   "默认任务模板",
	Type:   "task",
	Title:  "[OpsMesh Task][{{.Status}}] {{.TaskID}}",
	Body:   "**任务ID**: {{.TaskID}}\n**状态**: {{.Status}}\n**设备**: {{.DeviceID}}\n\n{{.Message}}",
	Format: "markdown",
}

// DefaultSystemTemplate 内置系统事件模板（text 格式）。
// 渲染上下文需提供：Event / Source / Message。
var DefaultSystemTemplate = &Template{
	ID:     "system-default",
	Name:   "默认系统模板",
	Type:   "system",
	Title:  "[OpsMesh System] {{.Event}}",
	Body:   "事件: {{.Event}}\n来源: {{.Source}}\n\n{{.Message}}",
	Format: "text",
}

// LoadDefaultTemplates 将内置默认模板加载到 store（启动期调用）。
// 已存在同 ID 模板会被覆盖（保证默认模板始终可用）。
func (s *TemplateStore) LoadDefaultTemplates() {
	s.Add(DefaultAlertTemplate)
	s.Add(DefaultTaskTemplate)
	s.Add(DefaultSystemTemplate)
}