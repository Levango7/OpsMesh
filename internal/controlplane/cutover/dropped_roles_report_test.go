package cutover

// dropped_roles_report_test.go — "丢失角色"提示行的判据（2026-10-13 演练移交项）。
//
// 为什么值得单测：DroppedRoles 一直算得准，但汇总**从不展开它** ⇒ 用户 /me 出现
// roleIDs:[]/permissions:null 时迁移输出里无迹可查（演练实测，controlplane 持
// role-operator 的用户搬过去正是这个形态）。该信息从"只躺在结构体里"变成
// "CLI 必报一行"是本笔的全部内容；判据不测，下次有人删打印行时没有任何东西会红。

import (
	"strings"
	"testing"
)

func TestDroppedRoleLines(t *testing.T) {
	// 1. 无丢失 ⇒ 空（不打印噪音行）。
	if lines := DroppedRoleLines(&MigrateResult{}); len(lines) != 0 {
		t.Fatalf("无丢失角色时不应有提示行，实得 %v", lines)
	}
	// 2. nil 结果 ⇒ 空（防御）。
	if lines := DroppedRoleLines(nil); len(lines) != 0 {
		t.Fatalf("nil 结果应返回空，实得 %v", lines)
	}

	// 3. Created 与 Adopted 都报（收编路径同样可能带丢失角色——演练里最隐蔽的正是
	// 这条：对侧已 active、名册补上了，没人再看角色面）。
	res := &MigrateResult{
		Created: []MigratedUser{
			{Username: "alice", DroppedRoles: []string{"role-operator"}},
			{Username: "carol"}, // 无丢失：不报
		},
		Adopted: []MigratedUser{
			{Username: "bob", DroppedRoles: []string{"role-operator", "role-auditor"}},
		},
	}
	lines := DroppedRoleLines(res)
	if len(lines) != 2 {
		t.Fatalf("应报 2 行（alice/bob），实得 %d：%v", len(lines), lines)
	}
	// 顺序：先 Created 后 Adopted（与汇总其它段落一致，运维读起来有确定性）。
	if !strings.Contains(lines[0], "alice") || !strings.Contains(lines[1], "bob") {
		t.Fatalf("行序应为 alice 然后 bob，实得 %v", lines)
	}
	// 用户名与角色 ID 必须点名（只说"有用户丢了角色"等于没说）。
	for _, want := range []string{"alice", "role-operator", "bob", "role-auditor"} {
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, want) {
			t.Errorf("提示行应点名 %q，实得：%v", want, lines)
		}
	}
	// 排障定向说明必须在（否则运维会以为业务面也残了——其实控制面按本侧角色判）。
	if !strings.Contains(lines[0], "控制面业务闸仍按本侧角色判") {
		t.Errorf("提示行应带排障定向说明，实得：%s", lines[0])
	}
	// carol 无丢失 ⇒ 不出现在任何行里。
	if strings.Contains(strings.Join(lines, "\n"), "carol") {
		t.Errorf("无丢失角色的用户不该被点名，实得 %v", lines)
	}
}

// TestDroppedRoleLines_DryRunAlsoReports 干跑同样报（dry-run 的目的就是让运维
// 在建号前看到全貌；丢失角色正是"建号前最该看到"的信息之一——migrate.go 在
// DryRun 分支同样填 Created+DroppedRoles）。
func TestDroppedRoleLines_DryRunAlsoReports(t *testing.T) {
	res := &MigrateResult{
		Created: []MigratedUser{
			{Username: "dave", DroppedRoles: []string{"role-operator"}},
		},
	}
	lines := DroppedRoleLines(res)
	if len(lines) != 1 || !strings.Contains(lines[0], "dave") {
		t.Fatalf("干跑结果也应报丢失角色，实得 %v", lines)
	}
}
