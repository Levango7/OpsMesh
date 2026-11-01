package gates

// authz_wiring_gate_test.go 是「鉴权能力写了必须有人接」的门禁。
//
// 为什么要有它（本周两次实证，都不是靠人想起来的）：
//   - `internal/aiops.Handler.SetAuthorize` 定义齐备、注释齐全，但**全仓没有调用点**，
//     于是 6 条 AIOps 端点（含 POST /aiops/execute：把修复方案下发成 agent 任务）匿名可达。
//     接线补上之后，同类形态又被查出第二处：
//   - `pkg/tenant.RequireTenant`（严格版：解不出租户即 403）全仓零调用点，12 个有 HTTP cmd
//     的服务统一挂的是宽松版 `tenant.Middleware`（兜底 default、从不拒绝）；
//     `pkg/auth` 更是备好了整套服务间鉴权中间件（AuthMiddleware / RequirePermission /
//     ServiceAuthMiddleware）而无人使用——该包实际只被用到 `ValidateServiceToken` 一个符号。
//     见 docs/semantic-rulings-2026-11-01.md 第一节 2/3 条。
//
// 「能力已实现但未接线」之所以反复出现：编译、vet、lint、单测全都看不见缺失的调用点，
// 而注释与命名会让人以为防线已存在。本用例把「零调用点的鉴权 API」变成构建期红。
//
// 判据（三档，缺一档就会退化成摆设）：
//  1. 扫描面自证：必须扫到足量的鉴权 API 定义，且已知接线的符号确实被算成「有调用点」——
//     否则「0 违规」只代表扫描器坏了（本仓门禁已记过这个形态）。
//  2. 零调用点 ⇒ 红，除非在 authzWiringExempt 里有**带原因**的豁免。
//  3. 豁免过期 ⇒ 红：豁免项一旦真的被接线（或定义消失），台账必须回改，
//     否则债务会藏在豁免里悄悄变质（对齐 deploy/scripts/changelog-fold-exempt.tsv 的做法）。

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// isAuthzCapabilityName 认定「鉴权/加固能力 API」的名字形状。刻意只认导出符号：
// 未导出的 recoveryMiddleware/corsMiddleware/handleXxxAuthorize 之类是本地装配函数或 handler，
// 不构成"可被别人接的能力"，把它们算进来只会逼出一份没有信息量的豁免清单。
func isAuthzCapabilityName(name string) bool {
	if name == "" || name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	switch {
	case strings.HasPrefix(name, "Require"), strings.HasPrefix(name, "Enforce"):
		return true
	case name == "SetAuthorize" || strings.HasSuffix(name, "Authorize"):
		return true
	case name == "Middleware" || strings.HasSuffix(name, "Middleware"):
		return true
	}
	return false
}

var (
	funcDecl   = regexp.MustCompile(`^func\s+(?:\(([^)]*)\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	packageDir = regexp.MustCompile(`^package\s+([A-Za-z0-9_]+)`)
)

// authzWiringExempt 是已知的「未接线」债务台账：每条必须写原因，且只在当前确实零调用点时有效。
var authzWiringExempt = map[string]string{
	"pkg/tenant.RequireTenant":             "裁决单 #4 方案 B 未拍：严格模式（无租户即 403）已实现但 12 个服务全挂宽松版 Middleware；切换会打死直连调用方，等 owner 定威胁模型后再动（docs/semantic-rulings-2026-11-01.md §三 方案 B）",
	"pkg/tenant.(Manager).QuotaMiddleware": "与 TD-97① 同源：配额中间件已写好，但用量/扣减的计量入口（RecordUsage 类）在生产代码里零调用点，接它等于接一个空转的账本",
	"pkg/auth.AuthMiddleware":              "pkg/auth 整套服务间鉴权中间件无人接：该包实际只被 pkg/tenant/auth_bridge.go 与 task-svc 用 ValidateServiceToken 一个符号；接线属裁决单 #4 的 A/B 方案范围",
	"pkg/auth.RequirePermission":           "同上——一个按权限名把关的 RBAC 中间件躺在 pkg 里，而真正的权限闸是 controlplane 的 requireProd；两份定义并存本身就是漂移风险，待 owner 裁「合并到 pkg/auth」还是「删除避免误用」",
	"pkg/auth.ServiceAuthMiddleware":       "同 pkg/auth 其余两条：服务间 mTLS/token 的 HTTP 装配点无人用",
}

// authzWiredMustFind 是扫描面的对照拍：这些符号确实已接线，扫不到就说明特征或路径失效了。
var authzWiredMustFind = map[string]string{
	"pkg/tenant.Middleware":                                       "服务面唯一在用的租户中间件",
	"pkg/compress.Middleware":                                     "与鉴权无关但同为 *Middleware 命名，用来验证扫描与限定名统计没写坏",
	"internal/aiops.(Handler).SetAuthorize":                       "本周刚接线的注入入口——它正是本门禁要防的那个缺陷",
	"internal/controlplane/credentials.EnforceInitialCredentials": "首启凭据加固，已接线（device/controlplane 启动路径）",
}

type authzDef struct {
	qual string // 限定名：pkg/<dir>.Name 或 pkg/<dir>.(Recv).Name
	pkg  string // Go 包名（用于限定名匹配调用点）
	recv string // 接收者类型名，可为空
	name string
	file string
	line int
}

// authzQual 组装限定名。用**目录路径 + 符号**做 key，避免不同包的同名符号（tenant.Middleware
// 与 compress.Middleware）互相顶掉——这也是对照拍能验出「限定名统计没写坏」的原因。
func authzQual(relFile, recv, name string) string {
	dir := filepath.ToSlash(filepath.Dir(relFile))
	if recv == "" {
		return fmt.Sprintf("%s.%s", dir, name)
	}
	// 接收者写成 "(Handler)" 形式：取类型名的最后一个词，指针类型不带包名。
	return fmt.Sprintf("%s.(%s).%s", dir, recv, name)
}

func TestAuthzCapabilityAPIsAreWired(t *testing.T) {
	root := repoRoot(t)

	walkRoots := []string{"internal", "pkg", "cmd", "services", "operator", "plugins"}
	var defs []authzDef
	var files int
	byQual := map[authzDef]bool{}
	lineCache := map[string][]string{}

	for _, w := range walkRoots {
		base := filepath.Join(root, w)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				rel = path
			}
			rel = filepath.ToSlash(rel)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			lines := strings.Split(string(data), "\n")
			lineCache[rel] = lines
			files++
			pkgName := ""
			for _, l := range lines {
				if m := packageDir.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
					pkgName = m[1]
					break
				}
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			for i, l := range lines {
				if strings.HasPrefix(strings.TrimSpace(l), "//") {
					continue
				}
				m := funcDecl.FindStringSubmatch(l)
				if m == nil {
					continue
				}
				recvType, name := m[1], m[2]
				if !isAuthzCapabilityName(name) {
					continue
				}
				recvShort := ""
				if recvType != "" {
					fields := strings.Fields(recvType)
					recvShort = strings.TrimPrefix(fields[len(fields)-1], "*")
				}
				d := authzDef{qual: authzQual(rel, recvShort, name), pkg: pkgName, recv: recvShort, name: name, file: rel, line: i + 1}
				defs = append(defs, d)
				byQual[d] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("扫描 %s 失败: %v", w, err)
		}
	}

	if len(defs) < 12 {
		t.Fatalf("只扫到 %d 个鉴权 API 定义（Go 文件 %d 个）——扫描面塌了，本门禁会空转判绿", len(defs), files)
	}

	// 逐个定义数调用点（非测试文件、非定义行、非注释行）。
	calls := map[string][]string{}
	seenQual := map[string]bool{}
	for d := range byQual {
		seenQual[d.qual] = true
	}
	for d := range byQual {
		var sites []string
		for rel, lines := range lineCache {
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}
			bareCall := regexp.MustCompile(`(?:^|[^A-Za-z0-9_.])` + regexp.QuoteMeta(d.name) + `\(`)
			dotCall := regexp.MustCompile(`\.` + regexp.QuoteMeta(d.name) + `\(`)
			for i, l := range lines {
				s := strings.TrimSpace(l)
				if s == "" || strings.HasPrefix(s, "//") {
					continue
				}
				if funcDecl.MatchString(s) {
					// 定义行本身不算调用；同名不同包的定义也不算。
					continue
				}
				if rel == d.file {
					// 同包内可直接引用函数名（无需包名前缀）。
					if bareCall.MatchString(s) {
						sites = append(sites, fmt.Sprintf("%s:%d", rel, i+1))
					}
					continue
				}
				// 跨包：必须看到 包名.名( 或 变量.名( （方法）。
				if strings.Contains(s, d.pkg+"."+d.name+"(") || dotCall.MatchString(s) {
					sites = append(sites, fmt.Sprintf("%s:%d", rel, i+1))
				}
			}
		}
		calls[d.qual+":"+d.file] = sites
	}

	var violations, stale []string
	wiredCount := 0
	for d := range byQual {
		key := d.qual + ":" + d.file
		n := len(calls[key])
		_, exempted := authzWiringExempt[d.qual]
		if n == 0 {
			if !exempted {
				violations = append(violations, fmt.Sprintf("%s（定义 %s:%d）", d.qual, d.file, d.line))
			}
			continue
		}
		wiredCount++
		if exempted {
			stale = append(stale, fmt.Sprintf("%s 已接线 %d 处，但仍在豁免台账里（例：%s）——请把台账那行删掉",
				d.qual, n, calls[key][0]))
		}
	}

	// 对照拍：已知接线的符号必须真的被扫到且被算成有调用点。
	for qual, why := range authzWiredMustFind {
		found := false
		for d := range byQual {
			if d.qual == qual {
				found = true
				if len(calls[d.qual+":"+d.file]) == 0 {
					t.Errorf("对照拍失败：%s（%s）被扫到了却没有调用点——限定名匹配逻辑已失效", qual, why)
				}
				break
			}
		}
		if !found {
			t.Errorf("对照拍失败：没扫到已接线的 %s（%s）——扫描特征或目录假设变了", qual, why)
		}
	}

	// 台账一致性：豁免了不存在的东西 = 后门。
	known := map[string]bool{}
	for q := range seenQual {
		known[q] = true
	}
	var ghosts []string
	for q := range authzWiringExempt {
		if !known[q] {
			ghosts = append(ghosts, q)
		}
	}

	sort.Strings(violations)
	sort.Strings(stale)
	sort.Strings(ghosts)
	if len(violations) > 0 {
		t.Errorf("以下鉴权能力 API 在非测试代码里零调用点（写了没人接＝防线不存在）：%s"+
			"——要么接线，要么在 authzWiringExempt 里登记原因并说明为什么本轮不接",
			strings.Join(violations, "；"))
	}
	if len(stale) > 0 {
		t.Errorf("豁免台账过期：%s", strings.Join(stale, "；"))
	}
	if len(ghosts) > 0 {
		t.Errorf("豁免台账里有幽灵项（定义已不存在或限定名写错）：%s", strings.Join(ghosts, "；"))
	}
	t.Logf("鉴权 API：定义 %d，已接线 %d，豁免 %d（须逐条有原因）", len(defs), wiredCount, len(authzWiringExempt))
}
