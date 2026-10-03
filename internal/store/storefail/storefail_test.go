package storefail

import (
	"testing"
)

// 本文件为随迁的纯 helper 单测（引用 storefail 未导出符号，故随包走）。

func TestExtractOpFromFormat(t *testing.T) {
	cases := []struct {
		name   string
		format string
		want   string
	}{
		{"常规形态", "[store] UpsertDevice 失败 %s: %v", "UpsertDevice"},
		{"半角冒号紧邻", "[store] RetireDevice: %v", "RetireDevice"},
		{"全角冒号紧邻", "[store] UpsertAgent：%v", "UpsertAgent"},
		{"全角冒号在词中", "[store] 写审计 失败：%v", "写审计"},
		{"无前缀", "UpsertDevice 失败", "unknown"},
		{"仅前缀", "[store] ", "unknown"},
		{"前缀后紧跟分隔", "[store] : %v", "unknown"},
		{"空串", "", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractOpFromFormat(tc.format); got != tc.want {
				t.Errorf("extractOpFromFormat(%q) = %q，期望 %q", tc.format, got, tc.want)
			}
		})
	}
}

func TestVerbSpans(t *testing.T) {
	cases := []struct {
		format string
		want   int // 动词个数
	}{
		{"[store] UpsertDevice 失败 %s: %v", 2},
		{"[store] CreateAPIKey 插入失败 (tenant=%s apikey=%s): %v", 3},
		{"[store] 迁移失败（第 %d/%d 次，%.0fs 后重试）: %v", 4},
		{"[store] 建索引 %s.%s 失败（非致命，可能缺列）: %v", 3},
		{"[store] 100%% 失败: %v", 1}, // 转义 percent 不占实参
		{"[store] 无动词", 0},
		{"[store] 截断的 %", 0}, // 畸形 verb 不得越界/误计
		{"[store] %q 与 %+v", 2},
	}
	for _, tc := range cases {
		if got := len(verbSpans(tc.format)); got != tc.want {
			t.Errorf("verbSpans(%q) 个数 = %d，期望 %d", tc.format, got, tc.want)
		}
	}
}

func TestTenantVerbIndex(t *testing.T) {
	cases := []struct {
		name   string
		format string
		want   int
		ok     bool
	}{
		{"tenant 在首位", "[store] CreateApp 插入失败 (tenant=%s id=%s): %v", 0, true},
		{"tenant 在第二位", "[store] DeleteApp 失败 (id=%s tenant=%s): %v", 1, true},
		{"tenant_id 变体", "[store] Foo 失败 (tenant_id=%s): %v", 0, true},
		{"无 tenant 标签", "[store] UpsertDevice 失败 %s: %v", 0, false},
		{"仅提到 tenant 字样", "[store] 按 tenant 维度聚合失败: %v", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, ok := tenantVerbIndex(tc.format)
			if ok != tc.ok || (ok && idx != tc.want) {
				t.Errorf("tenantVerbIndex(%q) = (%d, %v)，期望 (%d, %v)", tc.format, idx, ok, tc.want, tc.ok)
			}
		})
	}
}
