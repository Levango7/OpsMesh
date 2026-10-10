// store_adapter.go OS 模板与 store 持久化模型之间的转换（TD-87 批 2 第三批·切片 3：
// 自父包 os_template_store.go 迁入——类型（types.go）与转换同住一个包，边界最清楚）。
package presets

import (
	"encoding/json"

	"github.com/Levango7/OpsMesh/internal/store"
)

// ByID 按 ID 查找预置模板，未找到返回 nil。
func ByID(id string) *OSTemplate {
	for _, table := range [][]OSTemplate{OSTemplatesCore, OSTemplatesExt} {
		for i := range table {
			if table[i].ID == id {
				return &table[i]
			}
		}
	}
	return nil
}

// ToStore 将 controlplane.OSTemplate 转换为 store.OSTemplate。
// 整个 OSTemplate 序列化为 JSON 存入 Config 字段；store.OSTemplate 的 Name/OS 冗余存储便于 SQL 过滤。
func ToStore(t *OSTemplate, tenantID string) *store.OSTemplate {
	if t == nil {
		return nil
	}
	cfg, _ := json.Marshal(t)
	return &store.OSTemplate{
		ID:       t.ID,
		TenantID: tenantID,
		Name:     t.Name,
		OS:       t.OS,
		Config:   string(cfg),
	}
}

// FromStore 将 store.OSTemplate 反转换为 controlplane.OSTemplate（从 Config 反序列化）。
// Config 为空或反序列化失败时，用 store 行的 ID/Name/OS 构造最小 OSTemplate（向后兼容）。
func FromStore(st *store.OSTemplate) *OSTemplate {
	if st == nil {
		return nil
	}
	if st.Config == "" {
		return &OSTemplate{ID: st.ID, Name: st.Name, OS: st.OS}
	}
	var t OSTemplate
	if err := json.Unmarshal([]byte(st.Config), &t); err != nil {
		return &OSTemplate{ID: st.ID, Name: st.Name, OS: st.OS}
	}
	// 以 store 行的 ID/Name/OS 为准（防 Config 中过期值）。
	if st.ID != "" {
		t.ID = st.ID
	}
	if st.Name != "" {
		t.Name = st.Name
	}
	if st.OS != "" {
		t.OS = st.OS
	}
	return &t
}
