// http_infra_leak_test.go SEC-1 守护测试：防止 err.Error() 内部细节再次直接回吐客户端。
//
// 背景：审计曾发现 60+ 处 500 路径直接把 store/SQL/文件路径错误回吐客户端
// （表名/SQL 片段/部署拓扑泄露），已统一改为 writeInternalError/writeSanitizedError。
// 剩余 4xx 路径经逐源核查均为固定校验文案（客户端输入回显/sentinel 错误），无泄露面。
//
// 本测试静态扫描 handler 源码，锁定两条不变量：
//  1. 500（StatusInternalServerError）响应禁止携带 err.Error()——必须走 writeInternalError；
//  2. 502/503/504 响应禁止携带 err.Error()。
//
// 4xx 路径不锁（固定校验文案，且现有 134 处依赖该契约的测试会覆盖）。
package controlplane

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// rawErrInResponse 匹配响应体中携带原始 err.Error() 的模式。
//
// 覆盖三种写法（缺一即会漏）：
//  1. 内联：        {"error": err.Error()}
//  2. 字符串拼接：   writeProxyErrorJSON(w, 502, "service backend error: "+err.Error())
//  3. 前缀拼接：     writeJSON(w, 500, map[string]string{"error": "internal: " + err.Error()})
//
// 第 2 种曾真实漏过：service_proxy 的 502 路径把上游 host:port 与失败原因
// 回吐给客户端（部署拓扑泄露），而当时的两条正则都要求 `"error":` 紧邻
// err.Error()，拼接写法不在匹配范围内——门禁绿灯，缺陷仍在。
var rawErrInResponse = regexp.MustCompile(`"error":\s*(err\.Error\(\)|"[^"]*"\s*\+\s*err\.Error\(\))`)

// rawErrAnyPositionInResponse 匹配**任意参数位置**携带 err.Error() 的调用。
// 用于抓 writeProxyErrorJSON(w, 502, "... " + err.Error()) 这类非 "error" 键形态。
var rawErrAnyPositionInResponse = regexp.MustCompile(`\+\s*err\.Error\(\)`)

// TestNoRawErrInServerErrorResponses 扫描**全仓**非测试 go 文件，
// 断言任何携带 err.Error() 的响应都不是 5xx 状态码。
//
// ⚠️ 扫描面历史教训（2026-10-04）：本测试此前 root="."，即只覆盖 internal/controlplane
// 一个包。它给出"错误泄露已收口"的绿灯，而 internal/cmdb(16 处)、internal/deploy(11 处)、
// internal/orchestration(6 处)、internal/logstore(1 处) 合计 30+ 处同类问题**全在门禁视野外**——
// 这正是"门禁本身成了盲区"的典型：存在一个绿的守护测试，比没有测试更容易让人放心。
// 现在改为遍历仓库根，覆盖所有出网 handler 包。
func TestNoRawErrInServerErrorResponses(t *testing.T) {
	// 定位仓库根（go test 以包目录为工作目录，需向上回溯到含 go.mod 的目录）。
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("无法定位仓库根：%v", err)
	}

	// 只扫承担 HTTP 出网职责的包：controlplane / cmdb / deploy / orchestration /
	// logstore。刻意不扫 internal/store（纯数据层，无 HTTP 响应）、pkg/ 与 cmd/
	// 下的工具类二进制（含 device-sim 等模拟器，错误文案不面向 HTTP 客户端）。
	scanned, violations := 0, []string{}
	err5xx := regexp.MustCompile(`StatusInternalServerError|StatusBadGateway|StatusServiceUnavailable|StatusGatewayTimeout`)
	for _, pkgDir := range leakScanDirs {
		dir := filepath.Join(root, pkgDir)
		if _, serr := os.Stat(dir); serr != nil {
			t.Fatalf("待扫描目录不存在：%s（门禁扫描面漂移会使本测试假绿）", dir)
		}
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, werr error) error {
			if werr != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			scanned++
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			for i, ln := range strings.Split(string(data), "\n") {
				// 命中条件：同一行既出现 5xx 状态码，又把 err 拼进响应。
				// 两条正则取并——rawErrInResponse 覆盖 "error" 键形态，
				// rawErrAnyPositionInResponse 覆盖任意参数位置的拼接形态。
				if !err5xx.MatchString(ln) {
					continue
				}
				if rawErrInResponse.MatchString(ln) || rawErrAnyPositionInResponse.MatchString(ln) {
					violations = append(violations, fmtLoc(rel, i+1, ln))
				}
			}
			return nil
		})
	}
	// 扫描面自证：若目录被误配导致一个文件都没扫到，本测试必须判红，
	// 否则"扫到 0 处违规"会被误读成"干净"。
	if scanned == 0 {
		t.Fatalf("扫描面为空（0 个 go 文件），门禁已失效——检查 leakScanDirs")
	}
	if len(violations) > 0 {
		t.Errorf("发现 %d 处 5xx 响应携带原始 err.Error()（须改为固定文案 + 仅进日志）：\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
	t.Logf("错误泄露门禁扫描面：%d 个文件（%s）", scanned, strings.Join(leakScanDirs, ", "))
}

// leakScanDirs 是错误泄露门禁的扫描面。改动此列表须同步更新文件头注释。
var leakScanDirs = []string{
	"internal/controlplane",
	"internal/cmdb",
	"internal/deploy",
	"internal/orchestration",
	"internal/logstore",
}

// repoRoot 从包目录向上回溯定位含 go.mod 的仓库根。
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 10; i++ { // 最多上溯 10 层，足够覆盖 internal/<pkg> 这类嵌套
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("自 %s 上溯 10 层未找到 go.mod", mustGetwd())
}

func mustGetwd() string {
	d, _ := os.Getwd()
	return d
}

func fmtLoc(p string, line int, ln string) string {
	trimmed := strings.TrimSpace(ln)
	if len(trimmed) > 100 {
		trimmed = trimmed[:100] + "..."
	}
	return fmt.Sprintf("%s:%d: %s", p, line, trimmed)
}
