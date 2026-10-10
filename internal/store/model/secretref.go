// secretref.go secrets 表的取值形状契约（TD-88 方向裁定：只存引用）。
package model

import (
	"fmt"

	"github.com/Levango7/OpsMesh/internal/secrets"
)

// RequireSecretReference 校验写入 secrets 表的取值必须是 ${provider:key} 引用。
//
// # 为什么是「拒绝明文」而不是「加密明文」
//
// TD-88 的方向裁定是**引用格式优先**：凭据本体放进 --secret-provider 指向的来源（env/file/vault，
// 见 docs/security-mechanism.md §6.1–§6.5），库里只留引用 ⇒ 密钥**不随库同机落盘**。
// 若退而把明文加密后入库（库级静态加密），密钥与库仍在同一台机上，是该侧较弱的一档。
//
// # 为什么校验放在这里（而不是各后端/各消费方）
//
// 该表经自查在生产代码里**消费方为零**（`internal/controlplane` 零引用），唯一的写入方是 store 接口；
// 把契约钉在**中性层的一处**，任何未来消费方都自动受保护，且三个后端（memory/sql/multischema）
// 只需各调一行，不会出现「三处形状判断各自漂移」。
//
// 判据复用 internal/secrets.IsReference（与 ResolveSecret 的非引用分支同源，形状判断只有一份实现）。
func RequireSecretReference(value string) error {
	if secrets.IsReference(value) {
		return nil
	}
	// 不回显值本身（长度可用于区分「传了空」与「传了明文」两类误用）。
	return fmt.Errorf("secrets 只接受 ${provider:key} 引用（TD-88 方向裁定）：明文值一律拒绝，"+
		"请把凭据放进 --secret-provider 的来源并用引用写入；收到 %d 字节的非引用值", len(value))
}
