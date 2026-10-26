// middleware_version_test.go 校验中间件版本解析与 {image} 占位符替换。
package controlplane

import (
	"strings"
	"testing"
)

// TestResolveMiddlewareVersion_Default 验证未指定版本时回落到默认版本。
func TestResolveMiddlewareVersion_Default(t *testing.T) {
	tpl := middlewareTemplateByID("mysql")
	if tpl == nil {
		t.Fatal("预置模板 mysql 不存在")
	}
	v := resolveMiddlewareVersion(tpl, "")
	if v == nil {
		t.Fatal("未指定版本时应回落到默认版本")
	}
	if v.Version != "8.0" || v.DockerImage != "mysql:8.0" {
		t.Fatalf("默认版本 = %+v, want 8.0/mysql:8.0", v)
	}
	// 未匹配的版本号同样回落到默认版本。
	v = resolveMiddlewareVersion(tpl, "99.9")
	if v == nil || v.Version != "8.0" {
		t.Fatalf("未匹配版本应回落到默认版本, got %+v", v)
	}
}

// TestResolveMiddlewareVersion_Selected 验证按版本号选中对应版本。
func TestResolveMiddlewareVersion_Selected(t *testing.T) {
	tpl := middlewareTemplateByID("mysql")
	if tpl == nil {
		t.Fatal("预置模板 mysql 不存在")
	}
	v := resolveMiddlewareVersion(tpl, "8.4")
	if v == nil || v.DockerImage != "mysql:8.4" {
		t.Fatalf("选中版本 = %+v, want 8.4/mysql:8.4", v)
	}
}

// TestResolveMiddlewareScript_ImagePlaceholder 验证 docker 脚本的 {image} 被替换为版本镜像。
func TestResolveMiddlewareScript_ImagePlaceholder(t *testing.T) {
	tpl := middlewareTemplateByID("mysql")
	if tpl == nil {
		t.Fatal("预置模板 mysql 不存在")
	}
	script, ok := resolveMiddlewareScript(tpl, "docker", "")
	if !ok {
		t.Fatal("应解析出 docker 脚本")
	}
	if strings.Contains(script.Deploy, "{image}") {
		t.Fatalf("默认版本部署脚本仍含 {image}: %s", script.Deploy)
	}
	if !strings.Contains(script.Deploy, "mysql:8.0") {
		t.Fatalf("默认版本部署脚本未使用 mysql:8.0: %s", script.Deploy)
	}
	script, ok = resolveMiddlewareScript(tpl, "docker", "8.4")
	if !ok {
		t.Fatal("应解析出 docker 脚本")
	}
	if !strings.Contains(script.Deploy, "mysql:8.4") || strings.Contains(script.Deploy, "mysql:8.0") {
		t.Fatalf("选中 8.4 时部署脚本镜像不正确: %s", script.Deploy)
	}
}

// TestResolveMiddlewareScript_SystemdNoImage 验证 systemd 脚本不受镜像占位符影响。
func TestResolveMiddlewareScript_SystemdNoImage(t *testing.T) {
	tpl := middlewareTemplateByID("nginx")
	if tpl == nil {
		t.Fatal("预置模板 nginx 不存在")
	}
	script, ok := resolveMiddlewareScript(tpl, "systemd", "")
	if !ok {
		t.Fatal("应解析出 systemd 脚本")
	}
	if strings.Contains(script.Deploy, "{image}") {
		t.Fatalf("systemd 脚本不应含 {image}: %s", script.Deploy)
	}
}

// TestSelectedMiddlewareVersion 验证版本号取值优先级：请求体优先于 params。
func TestSelectedMiddlewareVersion(t *testing.T) {
	if got := selectedMiddlewareVersion("8.4", map[string]string{"version": "8.2"}); got != "8.4" {
		t.Fatalf("版本优先级错误: got %q, want 8.4", got)
	}
	if got := selectedMiddlewareVersion("", map[string]string{"version": "8.2"}); got != "8.2" {
		t.Fatalf("应回落到 params.version: got %q, want 8.2", got)
	}
	if got := selectedMiddlewareVersion("", nil); got != "" {
		t.Fatalf("无版本时应返回空: got %q", got)
	}
}
