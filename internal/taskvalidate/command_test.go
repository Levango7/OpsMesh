package taskvalidate

import (
	"strings"
	"testing"
)

func TestValidateCommand(t *testing.T) {
	valid := []string{"echo ok", "echo ok && df -h", "echo ok >&2", "echo ok &> /tmp/out"}
	for _, command := range valid {
		if err := ValidateCommand(command); err != nil {
			t.Errorf("合法命令 %q 被拒绝: %v", command, err)
		}
	}
	invalid := []string{
		"", strings.Repeat("x", MaxCommandLen+1), "echo one\necho two", "echo one\recho two",
		"echo one; echo two", "echo $(id)", "echo `id`", "echo ok | sh", "sleep 5 &",
	}
	for _, command := range invalid {
		if err := ValidateCommand(command); err == nil {
			t.Errorf("危险命令 %q 必须被拒绝", command)
		}
	}
}
