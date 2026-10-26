// Package aigateway 提示词模板管理
package aigateway

import (
	"fmt"
	"sync"
	"time"
)

// PromptTemplate 提示词模板
type PromptTemplate struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Category    string            `json:"category"` // system/user/tool/few-shot
	Content     string            `json:"content"`
	Variables   []string          `json:"variables"` // 模板变量名
	Version     int               `json:"version"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// PromptTemplateStore 提示词模板存储
type PromptTemplateStore struct {
	mu        sync.RWMutex
	templates map[string]*PromptTemplate
}

func NewPromptTemplateStore() *PromptTemplateStore {
	return &PromptTemplateStore{
		templates: make(map[string]*PromptTemplate),
	}
}

func (s *PromptTemplateStore) Create(tmpl *PromptTemplate) (*PromptTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if tmpl.ID == "" {
		return nil, fmt.Errorf("template ID is required")
	}
	if _, exists := s.templates[tmpl.ID]; exists {
		return nil, fmt.Errorf("template %s already exists", tmpl.ID)
	}

	now := time.Now()
	tmpl.CreatedAt = now
	tmpl.UpdatedAt = now
	tmpl.Version = 1
	s.templates[tmpl.ID] = tmpl
	return tmpl, nil
}

func (s *PromptTemplateStore) Get(id string) (*PromptTemplate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tmpl, ok := s.templates[id]
	if !ok {
		return nil, false
	}
	cp := *tmpl
	return &cp, true
}

func (s *PromptTemplateStore) List(category string) []*PromptTemplate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*PromptTemplate, 0)
	for _, t := range s.templates {
		if category != "" && t.Category != category {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	return out
}

func (s *PromptTemplateStore) Update(id string, content string, variables []string) (*PromptTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tmpl, ok := s.templates[id]
	if !ok {
		return nil, fmt.Errorf("template %s not found", id)
	}

	tmpl.Content = content
	tmpl.Variables = variables
	tmpl.Version++
	tmpl.UpdatedAt = time.Now()
	return tmpl, nil
}

func (s *PromptTemplateStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.templates[id]; !ok {
		return false
	}
	delete(s.templates, id)
	return true
}

// Render 渲染模板，替换变量
func (t *PromptTemplate) Render(vars map[string]string) string {
	result := t.Content
	for k, v := range vars {
		result = replaceAll(result, "{{"+k+"}}", v)
	}
	return result
}

func replaceAll(s, old, new string) string {
	// 简化实现
	result := s
	for {
		idx := indexOf(result, old)
		if idx < 0 {
			break
		}
		result = result[:idx] + new + result[idx+len(old):]
	}
	return result
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
