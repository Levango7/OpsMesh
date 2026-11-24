// middleware_template_presets_test.go 校验预置中间件模板目录的完整性。
//
// 覆盖：
//   - 目录基础字段（ID/Name/Category/Version/Description）与 ID 唯一性
//   - 版本目录：每个模板至少一个版本、有且仅有一个默认版本、镜像非空
//   - docker 部署脚本统一使用 {image} 占位符，不再硬编码镜像
//   - docker/systemd 双部署脚本完整
package presets

import (
	"strings"
	"testing"
)

// TestMiddlewareTemplates_Catalog 校验预置中间件目录的基础字段与 ID 唯一性。
func TestMiddlewareTemplates_Catalog(t *testing.T) {
	if len(MiddlewareTemplates) < 30 {
		t.Fatalf("预置中间件模板数量 = %d, want >= 29", len(MiddlewareTemplates))
	}
	seen := make(map[string]bool, len(MiddlewareTemplates))
	hasApache := false
	for _, tpl := range MiddlewareTemplates {
		switch {
		case tpl.ID == "":
			t.Error("模板缺少 ID")
		case tpl.Name == "":
			t.Errorf("模板 %q 缺少 Name", tpl.ID)
		case tpl.Category == "":
			t.Errorf("模板 %q 缺少 Category", tpl.ID)
		case tpl.Version == "":
			t.Errorf("模板 %q 缺少 Version", tpl.ID)
		case tpl.Description == "":
			t.Errorf("模板 %q 缺少 Description", tpl.ID)
		}
		if seen[tpl.ID] {
			t.Errorf("模板 ID 重复: %q", tpl.ID)
		}
		seen[tpl.ID] = true
		if tpl.ID == "apache" {
			hasApache = true
		}
	}
	if !hasApache {
		t.Error("中间件目录应包含 apache 模板")
	}
}

// TestMiddlewareTemplates_VersionCatalog 校验每个模板的版本目录。
func TestMiddlewareTemplates_VersionCatalog(t *testing.T) {
	for _, tpl := range MiddlewareTemplates {
		if len(tpl.Versions) == 0 {
			t.Errorf("模板 %q 缺少版本目录", tpl.ID)
			continue
		}
		defaults := 0
		for i, v := range tpl.Versions {
			if v.Version == "" {
				t.Errorf("模板 %q 第 %d 个版本缺少 Version", tpl.ID, i)
			}
			if v.DockerImage == "" {
				t.Errorf("模板 %q 版本 %q 缺少 DockerImage", tpl.ID, v.Version)
			}
			if !v.Default {
				continue
			}
			defaults++
			if i != 0 {
				t.Errorf("模板 %q 的默认版本 %q 不是首个版本", tpl.ID, v.Version)
			}
		}
		if defaults != 1 {
			t.Errorf("模板 %q 的默认版本数量 = %d, want 1", tpl.ID, defaults)
		}
		if tpl.Versions[0].Version != tpl.Version {
			t.Errorf("模板 %q 的 Version(%q) 与默认版本(%q) 不一致", tpl.ID, tpl.Version, tpl.Versions[0].Version)
		}
	}
}

// TestMiddlewareTemplates_DockerDeployUsesImagePlaceholder 校验 docker 部署脚本统一使用 {image} 占位符。
func TestMiddlewareTemplates_DockerDeployUsesImagePlaceholder(t *testing.T) {
	for _, tpl := range MiddlewareTemplates {
		script, ok := tpl.Scripts["docker"]
		if !ok {
			t.Errorf("模板 %q 缺少 docker 脚本", tpl.ID)
			continue
		}
		if !strings.Contains(script.Deploy, "{image}") {
			t.Errorf("模板 %q 的 docker 部署脚本缺少 {image} 占位符: %s", tpl.ID, script.Deploy)
		}
		for _, v := range tpl.Versions {
			if v.DockerImage == "" {
				continue
			}
			if strings.Contains(script.Deploy, v.DockerImage) {
				t.Errorf("模板 %q 的 docker 部署脚本仍硬编码镜像 %q", tpl.ID, v.DockerImage)
			}
		}
	}
}

// TestMiddlewareTemplate_OracleExists 校验 oracle 模板存在且支持 docker/systemd 部署。
func TestMiddlewareTemplate_OracleExists(t *testing.T) {
	var found bool
	for _, tpl := range MiddlewareTemplates {
		if tpl.ID != "oracle" {
			continue
		}
		found = true
		hasDocker := false
		hasSystemd := false
		for _, dt := range tpl.DeployTypes {
			if dt == "docker" {
				hasDocker = true
			}
			if dt == "systemd" {
				hasSystemd = true
			}
		}
		if !hasDocker || !hasSystemd {
			t.Errorf("oracle 模板部署方式 = %v, want docker & systemd", tpl.DeployTypes)
		}
		if _, ok := tpl.Scripts["docker"]; !ok {
			t.Error("oracle 模板缺少 docker 脚本")
		}
		if _, ok := tpl.Scripts["systemd"]; !ok {
			t.Error("oracle 模板缺少 systemd 脚本")
		}
	}
	if !found {
		t.Fatal("未找到 oracle 模板")
	}
}

func TestMiddlewareTemplate_AmbariExists(t *testing.T) {
	var found bool
	for _, tpl := range MiddlewareTemplates {
		if tpl.ID != "ambari" {
			continue
		}
		found = true
		hasDocker := false
		hasSystemd := false
		for _, dt := range tpl.DeployTypes {
			if dt == "docker" {
				hasDocker = true
			}
			if dt == "systemd" {
				hasSystemd = true
			}
		}
		if !hasDocker || !hasSystemd {
			t.Errorf("ambari 模板部署方式 = %v, want docker & systemd", tpl.DeployTypes)
		}
		if _, ok := tpl.Scripts["docker"]; !ok {
			t.Error("ambari 模板缺少 docker 脚本")
		}
		if _, ok := tpl.Scripts["systemd"]; !ok {
			t.Error("ambari 模板缺少 systemd 脚本")
		}
	}
	if !found {
		t.Fatal("未找到 ambari 模板")
	}
}

func TestMiddlewareTemplate_ClouderaManagerExists(t *testing.T) {
	var found bool
	for _, tpl := range MiddlewareTemplates {
		if tpl.ID != "cloudera-manager" {
			continue
		}
		found = true
		hasDocker := false
		hasSystemd := false
		for _, dt := range tpl.DeployTypes {
			if dt == "docker" {
				hasDocker = true
			}
			if dt == "systemd" {
				hasSystemd = true
			}
		}
		if !hasDocker || !hasSystemd {
			t.Errorf("cloudera-manager 模板部署方式 = %v, want docker & systemd", tpl.DeployTypes)
		}
		if _, ok := tpl.Scripts["docker"]; !ok {
			t.Error("cloudera-manager 模板缺少 docker 脚本")
		}
		if _, ok := tpl.Scripts["systemd"]; !ok {
			t.Error("cloudera-manager 模板缺少 systemd 脚本")
		}
	}
	if !found {
		t.Fatal("未找到 cloudera-manager 模板")
	}
}
func TestMiddlewareTemplates_DeployScriptsComplete(t *testing.T) {
	for _, tpl := range MiddlewareTemplates {
		for _, dt := range []string{"docker", "systemd"} {
			script, ok := tpl.Scripts[dt]
			if !ok {
				t.Errorf("模板 %q 缺少 %s 脚本", tpl.ID, dt)
				continue
			}
			if script.Deploy == "" || script.Verify == "" || script.Uninstall == "" {
				t.Errorf("模板 %q 的 %s 脚本不完整: %+v", tpl.ID, dt, script)
			}
		}
		if len(tpl.DeployTypes) != 2 {
			t.Errorf("模板 %q 的部署方式 = %v, want [docker systemd]", tpl.ID, tpl.DeployTypes)
		}
	}
}

func TestMiddlewareTemplate_MooseFSAndJuiceFSExist(t *testing.T) {
	for _, id := range []string{"moosefs", "juicefs"} {
		var found bool
		for _, tpl := range MiddlewareTemplates {
			if tpl.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("未找到 %s 模板", id)
		}
	}
}
