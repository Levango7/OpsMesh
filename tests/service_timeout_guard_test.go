package tests

// service_timeout_guard_test.go — 静态守卫：每个微服务的 http.Server 必须显式设
// ReadHeaderTimeout（G112 Slowloris 防护）。
//
// 背景（2026-10-11 实测）：12 个服务里 auth-svc / aio-svc / alert-svc 三家只设了
// ReadTimeout+WriteTimeout 而漏了 ReadHeaderTimeout。该组合**不能**防慢速头部攻击：
// ReadTimeout 覆盖「读整个请求（含体）」，但攻击方可把头部以任意低速率拖延，把连接
// 占用接近 ReadTimeout 而 header 迟迟不结束——只有 ReadHeaderTimeout 约束这一小段。
// 三家均已补齐，并用本测试守住「不得再漏」。
//
// # 判据只查非注释行（重要）
//
// 若对整个文件做字符串包含判断，**注释里提到该词也会算命中**——把字段删掉、只留
// 注释解释「这里应该有」时判据照样放行。首版就是这样失明的（变异检验抓到：
// 删掉 aio-svc 的字段后仍 ok）。故必须逐行剥掉 // 注释后再判断。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot 从当前工作目录向上查找含 go.work 的目录（仓库根）。
// 不用相对路径 "../.."：那样在 go test 的工作目录语义下不稳定
// （Windows 上解析出过 F:\Nexus 而非仓库根）。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("获取工作目录失败: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("向上查找未找到 go.work（仓库根）")
		}
		dir = parent
	}
}

// stripComments 去掉行注释与块注释，只留可执行代码。
// 目的是让「判据必须由代码满足」而非被注释喂饱。
func stripComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func TestServiceHTTPServersSetReadHeaderTimeout(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatalf("读取 services 目录失败: %v", err)
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := filepath.Join("services", e.Name(), "cmd", e.Name(), "main.go")
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue // 无 main.go 的服务（如纯库模块）不适用
		}
		src := string(b)
		if !strings.Contains(src, "http.Server{") {
			continue // 未自建 HTTP server
		}
		checked++
		code := stripComments(src)
		if !strings.Contains(code, "ReadHeaderTimeout") {
			t.Errorf("%s 构造了 http.Server 但未设 ReadHeaderTimeout（非注释行亦无）——"+
				"ReadTimeout 不覆盖慢速头部攻击（G112）；请补 15s（与 device/task-svc 一致）", rel)
		}
	}
	if checked < 12 {
		t.Errorf("只检查到 %d 个服务的 http.Server——预期 >=12（services/ 下 12 个自建 HTTP 服务的模块）；"+
			"扫描面塌缩会导致本守卫空转", checked)
	}
}
