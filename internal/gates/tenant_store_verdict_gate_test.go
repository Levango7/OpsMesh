package gates

// tenant_store_verdict_gate_test.go —— 「每个内存 store 都必须是租户隔离判据的**有结论项**」的门禁（D4）。
//
// 为什么要有它：本轮五维排查把 24 个内存 store（`sync.Mutex/RWMutex` + `map[...]` 字段的结构）逐条复核完，
// 结论是 21 个有机制（3 个走 TD-97 租户键，18 个走「对象 TenantID 字段 + 方法按租户过滤」）、
// 3 个「无租户维度但安全」（session / notify 平台级默认模板 / sqlstore 进程级缓存）——见
// docs/qa-sweep-2026-11-06.md §5.6。但**结论会腐化**：下一个新 store 作者如果不写租户维度，
// 既没有测试红、也没有门禁拦——和 BB-03 那次「56 处裸文本错误体悄悄长出来」是同一形态。
//
// 判据（四档，缺一档就会退化成摆设）：
//  1. 探测器自证：合成片段必须被认成 store（含 mutex + map 才算），且缺任一特征时不许认。
//  2. 扫描面自证：非测试 .go 文件数不得低于地板，且已知 store（internal/cmdb 的 MemoryCiStore）必须被扫到。
//  3. 判决表外出现任何新 store ⇒ 红：作者必须在本文件给出结论（tenant-key / tenant-field / no-tenant + 理由），
//     其中 tenant-key / tenant-field 还要**指出租户证据落在哪个文件/目录**（门禁会去那里找 token）。
//  4. 判决表过期 ⇒ 红：表里条目对应的 store 若已不存在（改名/删除/不再带 map+mutex），必须删行。
//
// 反例（曾真实发生）：D4 首轮只用单轨判据（只认 TD-97 复合键）⇒ 18 个「字段+过滤」型被误报成缺口；
// 本门禁因此把两轨都作为**合法结论**，但要求逐条写明证据位置——误报与漏报都不许靠猜。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeKind 是判决的种类。
type storeKind string

const (
	kindTenantKey   storeKind = "tenant-key"   // TD-97 复合键 tenant+"\x1f"+id（键即全部隔离信息）
	kindTenantField storeKind = "tenant-field" // 对象带 TenantID + 方法按租户过滤/拒绝
	kindNoTenant    storeKind = "no-tenant"    // 无租户维度但安全（必须给理由 + 证据位置可空）
)

// storeVerdict 是判决表一行。
type storeVerdict struct {
	kind     storeKind
	evidence string // 期望在 evidenceRef 里出现的 token（tenant-key 填 tenantKey/tenantPrefix/tenantSep；tenant-field 填 Tenant；no-tenant 留空）
	where    string // 证据所在文件或目录（仓库相对路径）；no-tenant 可留空
	note     string // 结论要点（写给人看，也给"过期即删"提供判断依据）
}

// tenantStoreVerdicts 是**当前全仓内存 store 的租户隔离判决表**（初始 24 条来自 §5.6 复核）。
// 键 = "<仓库相对路径>::<类型名>"。新 store 未入表 ⇒ 门禁红；表内条目消失 ⇒ 门禁红（过期即删）。
var tenantStoreVerdicts = map[string]storeVerdict{
	// —— A 类：TD-97 租户键（键即隔离信息）——
	"internal/ai/gateway/prompt.go::PromptTemplateStore":      {kindTenantKey, "tenantKey", "internal/ai/gateway/prompt.go", "TD-97：tenantKey(ctx,id) 复合键，跨租户不可见"},
	"internal/ai/gateway/rag.go::InMemoryVectorStore":         {kindTenantKey, "tenantKey", "internal/ai/gateway/rag.go", "TD-97：同上（RAG 向量库；tenantKey/tenantPrefix 定义在同文件）"},
	"internal/knowledge/retrieval.go::InMemoryKnowledgeStore": {kindTenantKey, "tenantKey", "internal/knowledge/retrieval.go", "TD-97：同上（知识库）"},

	// —— B 类：对象租户字段 + 方法过滤 ——
	"internal/cmdb/memory.go::MemoryCiStore":                       {kindTenantField, "Tenant", "internal/cmdb/memory.go", "handler 全量传 actx.TenantID；另有 idTakenByOtherTenant 防跨租户 ID 冲突"},
	"internal/controlplane/flux.go::fluxAppStore":                  {kindTenantField, "Tenant", "internal/controlplane/flux.go", "requireTenantContext + 列表/读按 TenantID 过滤"},
	"internal/controlplane/server_batch.go::batchStore":            {kindTenantField, "Tenant", "internal/controlplane/server_batch.go", "对象带 TenantID；handler 取 actx.TenantID"},
	"internal/deploy/store.go::MemoryDeployStore":                  {kindTenantField, "Tenant", "internal/deploy/store.go", "行级租户隔离 + ErrTenantMismatch"},
	"internal/deploy/federation.go::MemoryFederationStore":         {kindTenantField, "Tenant", "internal/deploy/federation.go", "同族口径（ErrTenantMismatch）"},
	"internal/orchestration/store.go::MemoryWorkflowStore":         {kindTenantField, "Tenant", "internal/orchestration/store.go", "接口自带租户参数 + ErrWFTenantMismatch"},
	"internal/store/memory/memory.go::MemoryStore":                 {kindTenantField, "Tenant", "internal/store/memory/memory.go", "单体 store：租户对象 + 多数实体带 TenantID"},
	"internal/store/multischema/multi_schema.go::MultiSchemaStore": {kindTenantField, "Tenant", "internal/store/multischema/multi_schema.go", "按租户分区 store（agent/device/task → tenant）"},
	"services/alert-svc/internal/store/store.go::MemoryStore":      {kindTenantField, "Tenant", "services/alert-svc/internal/store/store.go", "微服务镜像单体口径"},
	"services/auth-svc/internal/store/store.go::MemoryStore":       {kindTenantField, "Tenant", "services/auth-svc/internal/store/store.go", "同上"},
	"services/config-svc/internal/store/store.go::MemoryStore":     {kindTenantField, "Tenant", "services/config-svc/internal/store/store.go", "同上"},
	"services/device-svc/internal/store/store.go::MemoryStore":     {kindTenantField, "Tenant", "services/device-svc/internal/store/store.go", "同上"},
	"services/portal-svc/internal/store/store.go::MemoryStore":     {kindTenantField, "Tenant", "services/portal-svc/internal/store/store.go", "每方法判 tenantID != \"\" && r.TenantID != tenantID"},
	"services/task-svc/internal/store/store.go::MemoryStore":       {kindTenantField, "Tenant", "services/task-svc/internal/store/store.go", "同上"},
	"services/gpu-svc/internal/store/store.go::MemoryStore":        {kindTenantField, "Tenant", "services/gpu-svc", "store 无 Tenant 字段，但 handler 走 tenantFromRequest + wl.TenantID 钉租户（证据在包内）"},

	// —— 无租户维度但安全（必须给理由）——
	"internal/store/session.go::InProcessSessionStore": {kindNoTenant, "", "", "按 JTI/改密令牌/调用方 key 索引——键本身全局唯一，不构成按租户分区的业务数据"},
	"internal/notify/template.go::TemplateStore":       {kindNoTenant, "", "", "引擎侧平台级默认模板（LoadDefaultTemplates）；对外 /api/v1/notify-templates 由带租户的 store 处理"},
	"internal/store/sqlstore/sql.go::SQLStore":         {kindNoTenant, "", "", "仅两张进程级缓存（deviceMetrics/agentSecretCache）；数据隔离由 SQL 层与租户过滤负责"},

	// —— 原「待裁定（issue #27）」，2026-11-14 裁决②（租户级数据）落地后改判 tenant-field ——
	"services/autoscaler-svc/internal/store/store.go::MemoryStore": {kindTenantField, "Tenant", "services/autoscaler-svc/internal/store/store.go", "issue #27 裁决②：MemoryStore 带 TenantID 并按租户过滤；真实数据面是未装配的 Evaluator（见 issue #48），已同步租户化"},
	"services/runbook-svc/internal/store/store.go::MemoryStore":    {kindTenantField, "Tenant", "services/runbook-svc/internal/store/store.go", "issue #27 裁决②：Runbook/ExecutionRecord 带 TenantID，逐方法过滤 + 空租户拒写 + 跨租户同 ID 不覆盖"},
	"services/incident-svc/internal/models/models.go::MemoryStore": {kindTenantField, "Tenant", "services/incident-svc/internal/models/models.go", "issue #27 裁决②：Incident 带 TenantID，逐方法过滤 + 空租户拒写；时间线归属由父单派生"},
}

// detectStores 在 content 里找「名字以 Store 结尾且 struct 体含 map 字段」的 struct，返回类型名。
//
// 判据为何是「名字 + map」而不是「mutex + map」（首版实测修正两次）：
//   - 要求名字以 Store 结尾：本门禁射程是**数据 store**（租户数据的归属容器）。仓内其余带 map 的结构
//     （Evaluator/Detector/Manager/Collector/Allocator/Engine/batchIndex… 实测多出 16 条）是运行期组件的
//     内部缓存/索引，不构成「某租户的数据」，强行入表只会把判决表变成噪声清单；仓内约定数据 store 一律
//     以 Store 结尾——命名即分类。
//   - 不强制 mutex：`InMemoryVectorStore` / `InMemoryKnowledgeStore` 只有 map、同步在别处
//     （或按只增不改使用），强制 mutex 会把这两个**确实需要判决**的 store 漏掉（首版实测漏 2 条）。
//
// 只看单文件内的 struct 体：跨文件拼装的结构不在射程——本门禁防的是**新建 store**，
// 而非证明全仓无遗漏（那是 §5.6 逐条读的工作）。
func detectStores(content string) []string {
	var found []string
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "type ") || !strings.Contains(trimmed, " struct") {
			continue
		}
		name := strings.Fields(strings.TrimPrefix(trimmed, "type "))[0]
		if !strings.HasSuffix(name, "Store") {
			continue
		}
		hasMap := false
		for j := i + 1; j < len(lines); j++ {
			body := lines[j]
			if strings.HasPrefix(strings.TrimSpace(body), "}") {
				break
			}
			if strings.Contains(body, "map[") {
				hasMap = true
			}
		}
		if hasMap {
			found = append(found, name)
		}
	}
	return found
}

// TestTenantStoreDetector 探测器自证：Store 名 + map 字段才认；缺任一特征都不认
// （缺 map = 不是内存 store；非 Store 名 = 运行期组件，不在射程）。
func TestTenantStoreDetector(t *testing.T) {
	pos := "package x\n\ntype ThingStore struct {\n\tmu  sync.RWMutex\n\tm   map[string]int\n}\n"
	if got := detectStores(pos); len(got) != 1 || got[0] != "ThingStore" {
		t.Fatalf("探测器漏报：got=%v", got)
	}
	neg1 := "package x\n\ntype ThingStore struct {\n\tmu  sync.RWMutex\n\tn   int\n}\n"
	neg2 := "package x\n\ntype Manager struct {\n\tm   map[string]int\n}\n"
	if got := detectStores(neg1); len(got) != 0 {
		t.Fatalf("Store 名但无 map 字段不应认出：got=%v", got)
	}
	if got := detectStores(neg2); len(got) != 0 {
		t.Fatalf("含 map 但非 Store 名（运行期组件）不应认出：got=%v", got)
	}
}

// evidenceHasToken 在 file-or-dir 范围（目录递归、跳过 _test.go）里找 token。
func evidenceHasToken(t *testing.T, root, rel, token string) bool {
	t.Helper()
	if token == "" {
		return true
	}
	target := filepath.Join(root, rel)
	hit := false
	err := filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(raw), token) {
			hit = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("读证据位置失败 %s: %v", rel, err)
	}
	return hit
}

// TestTenantStoreVerdictsComplete 主判据：判决表外无新 store、表内条目不得过期、证据 token 必须在位。
func TestTenantStoreVerdictsComplete(t *testing.T) {
	root := repoRoot(t)
	scanRoots := []string{filepath.Join(root, "internal"), filepath.Join(root, "services")}

	scanned := 0
	detected := map[string]bool{} // "<rel>::<Type>"
	var offenders []string

	for _, scanRoot := range scanRoots {
		err := filepath.Walk(scanRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				base := filepath.Base(path)
				if base == "vendor" || base == "node_modules" || base == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			scanned++
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, typeName := range detectStores(string(raw)) {
				key := rel + "::" + typeName
				detected[key] = true
				if _, ok := tenantStoreVerdicts[key]; !ok {
					offenders = append(offenders, key)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("扫描 %s 失败: %v", scanRoot, err)
		}
	}

	// 判据 2：扫描面自证。
	const minGoFiles = 400
	if scanned < minGoFiles {
		t.Fatalf("扫描面自证失败：只扫到 %d 个非测试 .go 文件（地板 %d）——结论不可信", scanned, minGoFiles)
	}
	if !detected["internal/cmdb/memory.go::MemoryCiStore"] {
		t.Fatalf("扫描面自证失败：已知 store internal/cmdb/memory.go::MemoryCiStore 未被识别——探测器或路径坏了")
	}

	// 判据 3：表外新 store ⇒ 红。
	if len(offenders) > 0 {
		t.Fatalf("发现判决表外的内存 store（新 store 必须在本文件给出租户隔离结论）：\n  %s\n"+
			"做法：读实现后加一行 tenantStoreVerdicts —— tenant-key / tenant-field（并指出证据文件或目录）/ "+
			"no-tenant（必须写理由）；三选一犹豫时按 docs/qa-sweep-2026-11-06.md §5.6 的双轨判据走。",
			strings.Join(offenders, "\n  "))
	}

	// 判据 4：表内条目过期 ⇒ 红；tenant-* 的证据 token 必须在位。
	var stale []string
	for key, v := range tenantStoreVerdicts {
		rel, _, ok := splitKey(key)
		if !ok {
			stale = append(stale, key+"（键格式应为 <path>::<Type>）")
			continue
		}
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			stale = append(stale, key+"（文件已不存在）")
			continue
		}
		if !detected[key] {
			stale = append(stale, key+"（已不再是 mutex+map 形态的 store——改名/重构后就该删行）")
			continue
		}
		if (v.kind == kindTenantKey || v.kind == kindTenantField) && v.where == "" {
			stale = append(stale, key+"（tenant-* 结论必须写 where=证据位置）")
			continue
		}
		if (v.kind == kindTenantKey || v.kind == kindTenantField) && !evidenceHasToken(t, root, v.where, v.evidence) {
			stale = append(stale, fmt.Sprintf("%s（证据位置 %s 里找不到 token %q——结论与代码已脱节）", key, v.where, v.evidence))
			continue
		}
		if v.kind == kindNoTenant && strings.TrimSpace(v.note) == "" {
			stale = append(stale, key+"（no-tenant 结论必须写理由）")
		}
	}
	if len(stale) > 0 {
		t.Fatalf("判决表过期/脱节，必须回改 tenantStoreVerdicts：\n  %s", strings.Join(stale, "\n  "))
	}
}

func splitKey(key string) (rel, typeName string, ok bool) {
	i := strings.LastIndex(key, "::")
	if i <= 0 || i+2 >= len(key) {
		return "", "", false
	}
	return key[:i], key[i+2:], true
}
