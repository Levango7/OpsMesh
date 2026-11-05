// perm_platform_test.go — TD-97 ③④ 平台级权限点的派生契约测试。
//
// 背景：platform:read 是按 ":read" 结尾的权限点，而 viewer 的派生规则是
// 「所有后缀 :read 全收」——不显式排除的话，每个 viewer 都会自动拿到跨租户
// 全量视图（TD-97 ④ 的最小权限直接废掉）。本文件把三条不变量钉住：
//  1. platform:read 在权限目录里（否则端点对所有人恒 403——仓库反复出现的形态）；
//  2. viewer/operator 的派生集**不含** platform:read（不给内置角色）；
//  3. admin 仍全量持有（平台运营本就要有人能看，按最小权限由 owner 显授账号）。
package model

import (
	"strings"
	"testing"
)

func TestPlatformReadallPermissionCatalog(t *testing.T) {
	found := false
	for _, p := range PermSpecs {
		if p.Name == "platform:readall" {
			found = true
		}
	}
	if !found {
		t.Fatal("platform:readall 不在权限目录——/ai/usage 的 scope=all 会对所有人恒 403")
	}
}

func TestPlatformReadallNotGrantedToBuiltinRoles(t *testing.T) {
	roles := RolePermissions()
	for _, role := range []string{"viewer", "operator"} {
		perms := roles[role]
		for _, p := range perms {
			if p == "platform:readall" {
				t.Fatalf("%s 被自动授予 platform:read——平台跨租户视图必须只由 owner 显授", role)
			}
		}
	}
	// admin 全量持有（平台运营视角的存在性保留）。
	adminPerms := roles["admin"]
	found := false
	for _, p := range adminPerms {
		if p == "platform:readall" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("admin 未持有 platform:read——平台视图将无任何人可用")
	}
}

// 扫描面自证：目录里确实存在「platform 组」条目（否则上面的found==false可能只是拼写差异）。
func TestPlatformGroupScanSurface(t *testing.T) {
	n := 0
	for _, p := range PermSpecs {
		if strings.HasPrefix(p.Name, "platform:") {
			n++
		}
	}
	if n == 0 {
		t.Fatal("扫描面异常：目录里没有任何 platform: 前缀条目")
	}
	t.Logf("platform 组条目数：%d", n)
}
