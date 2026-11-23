// Package taskvalidate 提供控制面 HTTP 与内嵌 gRPC TaskService 共用的命令校验。
package taskvalidate

import (
	"errors"
	"fmt"
	"strings"
)

// MaxCommandLen 单条任务命令的最大字节数（与既有控制面 4096 上限一致）。
const MaxCommandLen = 4096

// ValidateCommand 校验 shell 命令，拒绝换行、命令分隔、替换、管道和后台执行。
// 规则与原 controlplane/server_tasks.go 的 validateCommand 一致，防止内联命令注入。
func ValidateCommand(command string) error {
	if command == "" {
		return errors.New("command is empty")
	}
	if len(command) > MaxCommandLen {
		return fmt.Errorf("command too long (max %d bytes, got %d)", MaxCommandLen, len(command))
	}
	if strings.Contains(command, "\n") {
		return errors.New("command contains newline metacharacter ('\\n'): rejected")
	}
	if strings.Contains(command, "\r") {
		return errors.New("command contains carriage return metacharacter ('\\r'): rejected")
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
	if strings.Contains(command, "|") {
		return errors.New("command contains pipe metacharacter ('|'): rejected")
	}
	cmd := command
	cmd = strings.ReplaceAll(cmd, ">&", "")
	cmd = strings.ReplaceAll(cmd, "&>", "")
	cmd = strings.ReplaceAll(cmd, "&&", "")
	if strings.Contains(cmd, "&") {
		return errors.New("command contains background operator metacharacter ('&'): rejected")
	}
	return nil
}
