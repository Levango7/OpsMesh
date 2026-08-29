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

// rawErrInResponse 匹配 WriteJSON 响应体内直接内插 err.Error() 的模式。
var rawErrInResponse = regexp.MustCompile(`"error":\s*(err\.Error\(\)|"[^"]*"\s*\+\s*err\.Error\(\))`)

// TestNoRawErrInServerErrorResponses 扫描 controlplane 全部非测试 go 文件，
// 断言任何携带 err.Error() 的响应都不是 5xx 状态码。
func TestNoRawErrInServerErrorResponses(t *testing.T) {
	// 找到 internal/controlplane 源码目录（测试工作目录为包根）。
	root := "."
	if _, err := os.Stat("server.go"); err != nil {
		// go test 以绝对路径运行时的兜底（无需处理，t.Fatalf 提示）。
		t.Fatalf("无法定位 controlplane 源码目录（工作目录 %s）", root)
	}

	var violations []string
	err5xx := regexp.MustCompile(`StatusInternalServerError|StatusBadGateway|StatusServiceUnavailable|StatusGatewayTimeout`)
	_ = filepath.Walk(root, func(p string, info os.FileInfo, werr error) error {
		if werr != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, ln := range lines {
			if rawErrInResponse.MatchString(ln) && err5xx.MatchString(ln) {
				violations = append(violations, fmtLoc(p, i+1, ln))
			}
		}
		return nil
	})
	if len(violations) > 0 {
		t.Errorf("发现 %d 处 5xx 响应携带原始 err.Error()（须改为 writeInternalError）：\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

func fmtLoc(p string, line int, ln string) string {
	trimmed := strings.TrimSpace(ln)
	if len(trimmed) > 100 {
		trimmed = trimmed[:100] + "..."
	}
	return fmt.Sprintf("%s:%d: %s", p, line, trimmed)
}
