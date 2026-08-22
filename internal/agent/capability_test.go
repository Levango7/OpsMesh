package agent

import "testing"

// TestCapabilityNote ：显式能力矩阵——Linux 全能力，非 Linux 仅 shell 且明示 service/rlimit 不可用。
func TestCapabilityNote(t *testing.T) {
	lin := capabilityNote("linux")
	if !contains(lin, "全能力") || !contains(lin, "systemctl") {
		t.Fatalf("linux note = %q, want 全能力 + systemctl", lin)
	}
	win := capabilityNote("windows")
	if !contains(win, "shell") || !contains(win, "systemctl") {
		t.Fatalf("windows note = %q, want shell + systemctl 说明", win)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
