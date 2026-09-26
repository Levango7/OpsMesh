// process_cpu_test.go — /proc/<pid>/stat 的 CPU 时间解析测试。
package metrics

import (
	"strings"
	"testing"
)

// TestParseProcessCPUTime_CommWithSpacesAndParens 覆盖最容易错位的形态：
// comm（第 2 字段）可以含空格与右括号，直接按空格切会让 utime/stime 整体错位。
// 内核约定"从最后一个 ')' 之后才是第 3 字段"，解析必须照此定位。
func TestParseProcessCPUTime_CommWithSpacesAndParens(t *testing.T) {
	// 字段序：pid comm state ppid pgrp session tty_nr tpgid flags
	//        minflt cminflt majflt cmajflt utime stime …
	mk := func(comm, utime, stime string) string {
		return strings.Join([]string{
			"42", "(" + comm + ")", "S", "1", "42", "42", "0", "-1", "0",
			"100", "0", "5", "0", utime, stime, "0", "0", "0", "0", "0", "0", "0",
		}, " ")
	}
	cases := []struct {
		name string
		in   string
		want float64
	}{
		{"普通 comm", mk("opsmesh", "150", "50"), 2.00},
		{"comm 含空格", mk("my op cmd (x)", "300", "200"), 5.00},
		{"comm 含右括号", mk("weird)name", "1", "99"), 1.00},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseProcessCPUTime([]byte(tc.in))
			if !ok {
				t.Fatalf("解析失败，样本=%q", tc.in)
			}
			if diff := got - tc.want; diff > 1e-9 || diff < -1e-9 {
				t.Fatalf("CPU 秒 = %v，期望 %v（utime+stime 取错字段？）", got, tc.want)
			}
		})
	}
}

// TestParseProcessCPUTime_Malformed 输入不足/无括号/非数字都必须拒绝，
// 而不是返回一个看似合理的 0 —— 假的 0 会让 rate() 显示"CPU 空闲"。
func TestParseProcessCPUTime_Malformed(t *testing.T) {
	for _, in := range []string{
		"",
		"no paren at all",
		"1 (a) S",
		"1 (a) S 1 2 3 4 5 6 7 8 9 10 11 notanumber 42 7 0",
	} {
		if v, ok := parseProcessCPUTime([]byte(in)); ok {
			t.Errorf("畸形输入 %q 竟解析成功（返回 %v）", in, v)
		}
	}
}
