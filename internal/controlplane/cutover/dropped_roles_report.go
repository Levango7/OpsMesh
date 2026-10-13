package cutover

// dropped_roles_report.go — migrate 的"丢失角色"提示行（2026-10-13 演练移交项）。
//
// 背景：DroppedRoles 一直算得很准（迁移只透传对侧存在的角色，缺的记入结构体），
// 但**CLI 汇总只打印条数、从不展开它** ⇒ 运维看不到"这个用户的 roleIDs 在对侧
// 被裁了"。用户侧的可见症状是 /me 返回 roleIDs:[]/permissions:null（演练实测：
// controlplane 持 role-operator 的用户搬过去后正是这个形态），而在迁移输出里
// 找不到任何对应行 ⇒ 排障时只能猜。本文件把该信息变成**必报的一行**。
//
// 纯函数化（与 ci-infra-death.sh/plaintextSecretKeys 同一文化）：cmd 层不可测，
// 判据放包里才能被夹具钉住——"汇总里到底报不报、报成什么样"这类契约，
// 历史上正因为只在 cmd 里 print 而没人能测。

import "fmt"

// DroppedRoleLines 生成"丢失角色"提示行（供 CLI 汇总打印；无丢失则返回空）。
//
// 覆盖 Created 与 Adopted 两类（收编路径同样可能带丢失角色——对侧已 active 但
// 角色面不全，演练里最隐蔽的正是这条）。每行一个用户，按输入顺序（先 Created
// 后 Adopted），行格式：
//
//	[丢失角色] <用户名>：<角色ID,...>（对侧无此角色；/me 的 roleIDs/permissions 不含它——控制面业务闸仍按本侧角色判，不受影响）
//
// 后缀那半句是给排障者的定向说明：不要以为业务面会跟着瞎（控制面的
// requireProd 按本侧/JWT 角色判），残的只是对侧 /me 的展示面。
func DroppedRoleLines(res *MigrateResult) []string {
	if res == nil {
		return nil
	}
	var out []string
	emit := func(u MigratedUser) {
		if len(u.DroppedRoles) == 0 {
			return
		}
		roles := ""
		for i, r := range u.DroppedRoles {
			if i > 0 {
				roles += ","
			}
			roles += r
		}
		out = append(out, fmt.Sprintf(
			"  [丢失角色] %s：%s（对侧无此角色；/me 的 roleIDs/permissions 不含它——控制面业务闸仍按本侧角色判，不受影响）",
			u.Username, roles))
	}
	for _, u := range res.Created {
		emit(u)
	}
	for _, u := range res.Adopted {
		emit(u)
	}
	return out
}
