// render.go 实现 Chart 模板渲染：调用 helm template 生成 Kubernetes manifest，
// 并解析多文档 YAML 为 RenderedTemplate 列表。
//
// 设计要点：
//   - RenderChart 封装 helm template 命令，支持 values 覆盖；
//   - 解析多文档 YAML（--- 分隔），提取 Source/kind/name；
//   - 不依赖外部 YAML 库，用字符串扫描解析（helm template 输出格式稳定）；
//   - 支持本地 chart 路径与远程 chart 引用（如 bitnami/mysql）。

package helm

import (
	"fmt"
	"strings"
)

// RenderOptions 是模板渲染选项。
type RenderOptions struct {
	Namespace   string                 // -n 参数（目标命名空间）
	ReleaseName string                 // release 名（必填，helm template 第一个位置参数）
	Values      map[string]interface{} // 覆盖值（写入临时 JSON 文件通过 -f 传递）
	ValuesFiles []string               // 额外 -f 文件路径列表
	SetPairs    []string               // --set KEY=VALUE 列表
	Kubeconfig  string                 // --kubeconfig（仅用于 OCI 拉取鉴权，本地 template 通常不需要）
	IncludeCRDs bool                   // --include-crds
	APIVersions []string               // --api-version（如 ["monitoring.coreos.com/v1"]）
}

// RenderedTemplate 是渲染后的单个 Kubernetes 资源。
type RenderedTemplate struct {
	Name       string `json:"name"    yaml:"name"`    // 来源文件路径（如 "mysql/templates/primary/statefulset.yaml"）
	Content    string `json:"content" yaml:"content"` // 完整 YAML 内容（含 Source 注释）
	Kind       string `json:"kind"    yaml:"kind"`    // 资源类型（Deployment/Service/ConfigMap 等）
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	// ResourceName 是 metadata.name 的值（从 YAML 内容提取）。
	ResourceName string `json:"resourceName,omitempty" yaml:"resourceName,omitempty"`
}

// RenderChart 渲染 chart 为 Kubernetes manifest 列表。
//
// chartPath 可以是：
//   - 本地 chart 目录路径（如 "./charts/mysql"）；
//   - 远程 chart 引用（如 "bitnami/mysql"）；
//   - 压缩包路径（如 "./mysql-9.10.0.tgz"）。
//
// opts.ReleaseName 必填；opts.Namespace 为空时使用 "default"。
// opts.Values 通过临时 JSON 文件传递（helm -f）。
func RenderChart(chartPath string, opts *RenderOptions) ([]*RenderedTemplate, error) {
	if chartPath == "" {
		return nil, fmt.Errorf("helm/render: chartPath 为空")
	}
	if opts == nil {
		opts = &RenderOptions{}
	}
	if opts.ReleaseName == "" {
		return nil, fmt.Errorf("helm/render: opts.ReleaseName 为空")
	}
	namespace := opts.Namespace
	if namespace == "" {
		namespace = "default"
	}

	cli := NewCLI(opts.Kubeconfig)

	// 合并 values：opts.Values 写入临时文件，与 opts.ValuesFiles 一起传给 -f。
	valuesFiles, cleanup, err := writeValuesTemp(opts.Values)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	valuesFiles = append(valuesFiles, opts.ValuesFiles...)

	// 构造 helm template 参数。
	args := []string{"template", opts.ReleaseName, chartPath, "-n", namespace}
	for _, f := range valuesFiles {
		args = append(args, "-f", f)
	}
	for _, kv := range opts.SetPairs {
		args = append(args, "--set", kv)
	}
	if opts.IncludeCRDs {
		args = append(args, "--include-crds")
	}
	for _, av := range opts.APIVersions {
		args = append(args, "--api-version", av)
	}

	raw, err := cli.Run(args...)
	if err != nil {
		return nil, fmt.Errorf("helm/render: 渲染 chart %q 失败: %w", chartPath, err)
	}
	return ParseMultiDocYAML(raw), nil
}

// RenderChartWithCLI 使用指定 CLI 渲染 chart（便于测试注入 mock CLI）。
func RenderChartWithCLI(cli HelmCLI, chartPath string, opts *RenderOptions) ([]*RenderedTemplate, error) {
	if cli == nil {
		return RenderChart(chartPath, opts)
	}
	if chartPath == "" {
		return nil, fmt.Errorf("helm/render: chartPath 为空")
	}
	if opts == nil {
		opts = &RenderOptions{}
	}
	if opts.ReleaseName == "" {
		return nil, fmt.Errorf("helm/render: opts.ReleaseName 为空")
	}
	namespace := opts.Namespace
	if namespace == "" {
		namespace = "default"
	}

	valuesFiles, cleanup, err := writeValuesTemp(opts.Values)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	valuesFiles = append(valuesFiles, opts.ValuesFiles...)

	args := []string{"template", opts.ReleaseName, chartPath, "-n", namespace}
	for _, f := range valuesFiles {
		args = append(args, "-f", f)
	}
	for _, kv := range opts.SetPairs {
		args = append(args, "--set", kv)
	}
	if opts.IncludeCRDs {
		args = append(args, "--include-crds")
	}
	for _, av := range opts.APIVersions {
		args = append(args, "--api-version", av)
	}

	raw, err := cli.Run(args...)
	if err != nil {
		return nil, fmt.Errorf("helm/render: 渲染 chart %q 失败: %w", chartPath, err)
	}
	return ParseMultiDocYAML(raw), nil
}

// =============================================================================
// 多文档 YAML 解析（不依赖外部 YAML 库）
// =============================================================================

// ParseMultiDocYAML 解析多文档 YAML 字符串为 RenderedTemplate 列表。
//
// helm template 输出格式：
//
//	---
//	# Source: mysql/templates/primary/statefulset.yaml
//	apiVersion: apps/v1
//	kind: StatefulSet
//	metadata:
//	  name: mysql-primary
//	...
//	---
//	# Source: mysql/templates/primary/svc.yaml
//	apiVersion: v1
//	kind: Service
//	...
//
// 解析策略：
//   - 按 "\n---\n" 或行首 "---" 分割文档；
//   - 每个文档提取 "# Source:" 行作为 Name；
//   - 提取 "kind:" 行作为 Kind；
//   - 提取 "apiVersion:" 行作为 APIVersion；
//   - 提取 "name:" 行（metadata.name）作为 ResourceName；
//   - 跳过空文档与纯注释文档。
func ParseMultiDocYAML(raw string) []*RenderedTemplate {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	// 标准化：按 "---" 分割，保留每个文档内容。
	docs := splitYAMLDocuments(raw)
	out := make([]*RenderedTemplate, 0, len(docs))
	for _, doc := range docs {
		tpl := parseSingleYAMLDoc(doc)
		if tpl == nil {
			continue
		}
		out = append(out, tpl)
	}
	return out
}

// splitYAMLDocuments 按 "---" 分割多文档 YAML。
//
// helm template 输出以 "---\n" 开头，每个文档间也用 "---" 分隔。
// 此函数返回每个文档的原始文本（含 Source 注释）。
func splitYAMLDocuments(raw string) []string {
	// 标准化行尾，按行处理。
	lines := strings.Split(raw, "\n")
	var docs []string
	var cur strings.Builder
	hasContent := false

	flush := func() {
		if hasContent {
			docs = append(docs, cur.String())
		}
		cur.Reset()
		hasContent = false
	}

	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		// 检测文档分隔符：行首 "---" 或 "..."（YAML 文档结束符）。
		if isYAMLDocSeparator(trimmed) {
			flush()
			continue
		}
		cur.WriteString(trimmed)
		cur.WriteString("\n")
		// 含非注释、非空行则标记有内容。
		stripped := strings.TrimSpace(trimmed)
		if stripped != "" && !strings.HasPrefix(stripped, "#") {
			hasContent = true
		}
	}
	flush()
	return docs
}

// isYAMLDocSeparator 判断行是否为 YAML 文档分隔符（--- 或 ...）。
func isYAMLDocSeparator(line string) bool {
	s := strings.TrimSpace(line)
	return s == "---" || s == "..."
}

// parseSingleYAMLDoc 解析单个 YAML 文档为 RenderedTemplate。
//
// 返回 nil 表示空文档或纯注释文档。
func parseSingleYAMLDoc(doc string) *RenderedTemplate {
	if strings.TrimSpace(doc) == "" {
		return nil
	}

	tpl := &RenderedTemplate{Content: doc}

	lines := strings.Split(doc, "\n")
	inMetadata := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 提取 Source 注释：# Source: <path>
		if strings.HasPrefix(trimmed, "# Source:") {
			tpl.Name = strings.TrimSpace(strings.TrimPrefix(trimmed, "# Source:"))
			continue
		}

		// 跳过其他注释与空行。
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// 顶层 key: value（缩进为 0）。
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			key, val, ok := splitYAMLKeyValue(trimmed)
			if !ok {
				inMetadata = false
				continue
			}
			switch key {
			case "kind":
				tpl.Kind = unquoteYAML(val)
				inMetadata = false
			case "apiVersion":
				tpl.APIVersion = unquoteYAML(val)
				inMetadata = false
			case "metadata":
				// metadata: 后面可能是空（块映射）或 inline。
				inMetadata = val == ""
			default:
				inMetadata = false
			}
			continue
		}

		// 缩进行：在 metadata 块内提取 name。
		if inMetadata {
			key, val, ok := splitYAMLKeyValue(trimmed)
			if ok && key == "name" && tpl.ResourceName == "" {
				tpl.ResourceName = unquoteYAML(val)
			}
		}
	}

	// 若无 Source 注释，用 resourceName + kind 兜底。
	if tpl.Name == "" {
		if tpl.ResourceName != "" && tpl.Kind != "" {
			tpl.Name = fmt.Sprintf("%s/%s", strings.ToLower(tpl.Kind), tpl.ResourceName)
		} else if tpl.Kind != "" {
			tpl.Name = strings.ToLower(tpl.Kind)
		}
	}

	// 若无 kind 且无 name，视为空文档。
	if tpl.Kind == "" && tpl.Name == "" {
		return nil
	}
	return tpl
}

// splitYAMLKeyValue 拆分 "key: value" 或 "key:" 为 (key, value, true)。
//
// 返回 ok=false 表示不是合法键值对（如列表项 "- item"）。
func splitYAMLKeyValue(s string) (key, val string, ok bool) {
	idx := strings.Index(s, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(s[:idx])
	val = strings.TrimSpace(s[idx+1:])
	// key 不应含空格（YAML 标量键）。
	if strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	return key, val, true
}

// unquoteYAML 去除 YAML 字符串引号（单引号或双引号）。
func unquoteYAML(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// =============================================================================
// 渲染结果统计与过滤
// =============================================================================

// RenderStats 是渲染结果的统计信息。
type RenderStats struct {
	Total         int            // 总资源数
	ByKind        map[string]int // 按类型统计
	WithSource    int            // 有 Source 注释的资源数
	WithoutSource int            // 无 Source 注释的资源数
}

// Stats 计算渲染结果的统计信息。
func Stats(templates []*RenderedTemplate) *RenderStats {
	st := &RenderStats{ByKind: make(map[string]int)}
	for _, t := range templates {
		st.Total++
		if t.Kind != "" {
			st.ByKind[t.Kind]++
		}
		if t.Name != "" {
			st.WithSource++
		} else {
			st.WithoutSource++
		}
	}
	return st
}

// FilterByKind 按类型过滤渲染结果。
func FilterByKind(templates []*RenderedTemplate, kinds ...string) []*RenderedTemplate {
	if len(kinds) == 0 {
		return templates
	}
	want := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}
	out := make([]*RenderedTemplate, 0, len(templates))
	for _, t := range templates {
		if want[t.Kind] {
			out = append(out, t)
		}
	}
	return out
}
