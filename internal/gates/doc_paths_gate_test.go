package gates

// doc_paths_gate_test.go —— 「现状说明类文档里指向仓库内的路径必须真实存在」。
//
// 起因（2026-10-26 实测）：一轮大重构把 `internal/store` 拆进 `internal/store/sqlstore/`、
// `internal/provision` 迁到 `pkg/provision/`、controlplane 拆出 `grpc/` 与 `paginate/`、
// `plugin_remote.go` 迁进 `pluginhost/`。十几份**现状说明**文档仍在指向旧路径；
// 更严重的是 `docs/multi-os-support.md` 第 4 章把 `PlatformCapability` 写成"接口定义在
// internal/agent/platform/platform.go"，而该接口与包在代码里**完全不存在**（设计稿冒充现状）。
//
// 既有两道门禁都抓不到这类漂移：`capability_evidence_gate_test.go` 只扫 README/product-design
// 的**证据表**（这些引用不在表里），而 docs 没有任何链接/路径检查。
//
// 判定面**刻意收窄**——假阳性比没有门禁更糟，它会训练人绕过门禁：
//   - 只查 `docPathWhitelist` 里的现状说明类文档。历史/快照类一律不查：CHANGELOG、
//     COORDINATION、td*、design/、enterprise-gap-analysis、release-notes、INBOX/CLAIMS/
//     板面、tech-debt（后者还是 AGENTS.md 写明的禁区）。它们记录的是**当时的**路径，
//     改它等于改写历史。test-specification 暂缓：它按测试文件名逐条登记，需要逐条核实
//     改名去向，已登记为后续项，不在本轮硬改。
//   - 只认「以真实顶层目录开头 + 带扩展名 + 不含通配符/占位符/空格/非 ASCII」的反引号词元。
//     相对简写（如 `migrations/001_initial.sql`、`multischema/multi_schema.go`）不认：
//     它相对谁没有定义，认了全是假阳性（本轮普查的 115 条"疑似坏路径"里 100+ 是简写与历史件）。
//   - 运行时生成物经 `docPathAllow` 逐条豁免，**每条必须写原因**。
//   - 自证作用域：白名单文件必须存在、扫到的词元总数必须 ≥ `docTokenFloor`。
//     扫描面塌缩时"0 违规"是假绿——本仓已记过这个形态（判据看不见缺陷时，只有绿色结论）。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 现状说明类文档（读者会照着它们去找文件）。
var docPathWhitelist = []string{
	"DELIVERY.md",
	"README.md",
	"deploy/k8s/README.md",
	"docs/api-reference.md",
	"docs/api-specification.md",
	"docs/architecture.md",
	"docs/database-design.md",
	"docs/dr-runbook.md",
	"docs/multi-os-support.md",
	"docs/operations.md",
	"docs/product-design.md",
	"docs/product-roadmap.md",
	"docs/security-mechanism.md",
	"docs/test-specification.md",
	"plugins/remote-example/README.md",
	"proto/README.md",
}

// 运行时生成物：仓库里本就不存在，且是**正确**的不存在。每条都要写清为什么。
var docPathAllow = map[string]string{
	"deploy/docker/generated/alertmanager.yml": "由 deploy/docker/scripts/deploy.sh 在部署时渲染生成，仓库内不落盘",

	// —— docs/test-specification.md：2026-10-26 复核结果（见该文件的复核说明块）——
	// 前三条只出现在"已修正"的说明里（正文已换成新路径），保留它们是为了让读者知道旧路径去哪了。
	"internal/store/memory.go":     "仅出现在复核说明的『已修正』条目里；正文已改为 internal/store/memory/memory.go",
	"internal/tlsutil/tls_test.go": "仅出现在复核说明的『已修正』条目里；正文已改为 internal/tlsutil/tlsutil_test.go",
	"internal/notify/ssrf_test.go": "仅出现在复核说明的『已修正』条目里；正文已改为 internal/notify/egress_policy_test.go",
	// 后九条是**存量债务**：能力/用例已不在代码中，或改名后无法逐条对上。没有硬改（改了就是编造），
	// 豁免写在这里 ⇒ 新增漂移仍会判红，而债务本身在代码里可见、可被逐条消解。
	"internal/agent/exec_test.go":         "SSH 通道已不在代码里（grep crypto/ssh 于 internal/agent 命中 0）；命令注入现由 security_test.go / shell_whitelist_test.go 覆盖，该行待重写",
	"internal/agent/sftp_test.go":         "agent 已无 SFTP 实现（grep sftp 命中 0），该行待重写",
	"internal/authctx/jwt_sign_test.go":   "现无该文件；『弱密钥签发返回 error』未找到一一对应用例，待重写",
	"internal/controlplane/authz_test.go": "现为 authz_audit_test.go / apikey_auth_test.go；『普通用户访问 admin 403』未逐条对上，待重写",
	"internal/controlplane/csrf_test.go":  "CSRF 用例现分散在 server_middleware_extra_test.go，未逐条对上，待重写",
	"internal/cron/clock.go":              "FakeClock 全仓 grep 命中 0（internal/cron 无时钟注入），该断言已失效，待重写",
	"internal/grpcx/server_test.go":       "internal/grpcx 现为 service_test.go / stub_test.go；bufconn 在 grpcx 内命中 0，待重写",
	"internal/store/sql_tenant_test.go":   "租户注入用例现位于 internal/store/multischema/multi_schema_test.go，未逐条对上，待重写",
	"internal/store/sql_test.go":          "go-sqlmock 现用于 internal/cmdb/sql_test.go 等，store 包下无 sqlmock 用例，待重写",
}

// 词元下界：本白名单实测 303 条（2026-10-26）。取 200 给正常改写留余量，
// 但足以在"白名单被清空/正则写坏/读不到文件"时判红。
const docTokenFloor = 200

var (
	docBacktick = regexp.MustCompile("`([^`\n]+)`")
	// 末段必须带**已知文件扩展名**。只要求"有点 + 有斜杠"会把 Go 符号
	// （`internal/cron.Manager`、`internal/approval.Engine`、`internal/secrets.IsReference`）
	// 当成路径——本轮首跑就是这么误报 3 条的（布尔假阳性的代价：会训练人绕过门禁）。
	docPathLike = regexp.MustCompile(`^[\w.@-]+(/[\w.@-]+)+\.(go|sh|yml|yaml|md|json|js|vue|sql|proto|tpl|tmpl|conf|css|ts|html|txt|csv)$`)
	docTopDirs  = map[string]bool{
		"internal": true, "cmd": true, "pkg": true, "services": true, "web": true,
		"deploy": true, "operator": true, "proto": true, "scripts": true,
		"api": true, "docs": true, "templates": true, ".github": true,
	}
)

func TestDocPathsExist(t *testing.T) {
	root := repoRoot(t)

	// 白名单为空时 `scanned != len(whitelist)` 是 0==0、抓不到——单列一条。
	if len(docPathWhitelist) == 0 {
		t.Fatalf("白名单为空——扫描面塌缩，判红（这种情形下「0 违规」是假绿）")
	}

	scanned, tokens := 0, 0
	var violations int

	for _, doc := range docPathWhitelist {
		full := filepath.Join(root, filepath.FromSlash(doc))
		b, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("白名单文档读不到：%s: %v（扫描面缺失，判红而不是跳过）", doc, err)
		}
		scanned++
		for _, line := range strings.Split(string(b), "\n") {
			for _, m := range docBacktick.FindAllStringSubmatch(line, -1) {
				tok := strings.TrimSpace(m[1])
				if tok == "" || !isASCII(tok) {
					continue
				}
				if strings.ContainsAny(tok, " \t*{}<>$") {
					continue // 通配符/占位符/命令片段，不是可判定的路径
				}
				tok = strings.TrimSuffix(tok, "/")
				if !docPathLike.MatchString(tok) {
					continue
				}
				first := strings.SplitN(tok, "/", 2)[0]
				if !docTopDirs[first] {
					continue // 相对简写：相对谁没有定义，不作为断言
				}
				if _, ok := docPathAllow[tok]; ok {
					continue
				}
				tokens++
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(tok))); err != nil {
					violations++
					t.Errorf("%s 声称 `%s` 存在，实际不存在（重构/改名后的漂移——读者会按它去找文件）",
						doc, tok)
				}
			}
		}
	}

	// 自证作用域：这份门禁一旦读不到文件或正则失效，就会对着空集判绿。
	if scanned != len(docPathWhitelist) {
		t.Fatalf("只读到 %d/%d 份白名单文档——扫描面塌缩，判红", scanned, len(docPathWhitelist))
	}
	if tokens < docTokenFloor {
		t.Fatalf("只扫到 %d 条候选路径（下界 %d）——判据在空转，判红（是正则/白名单坏了，不是文档变干净了）",
			tokens, docTokenFloor)
	}

	if violations == 0 {
		t.Logf("文档路径门禁：%d 份现状说明、%d 条仓库内路径全部存在（含 %d 条逐条写明原因的豁免）",
			scanned, tokens, len(docPathAllow))
	}
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}
