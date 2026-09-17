// devicefp_test.go — 设备指纹采集与校验测试（TD-60）。
package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComputeDeviceFP(t *testing.T) {
	// 相同输入 → 相同指纹。
	fp1 := computeDeviceFP("Mozilla/5.0", "10.0.0.1", "ja3-abc")
	fp2 := computeDeviceFP("Mozilla/5.0", "10.0.0.1", "ja3-abc")
	if fp1 != fp2 {
		t.Error("相同输入应产生相同指纹")
	}
	// 不同 UA → 不同指纹。
	fp3 := computeDeviceFP("Chrome/120", "10.0.0.1", "ja3-abc")
	if fp1 == fp3 {
		t.Error("不同 UA 应产生不同指纹")
	}
	// 不同 IP → 不同指纹。
	fp4 := computeDeviceFP("Mozilla/5.0", "10.0.0.2", "ja3-abc")
	if fp1 == fp4 {
		t.Error("不同 IP 应产生不同指纹")
	}
	// 不同 TLS → 不同指纹。
	fp5 := computeDeviceFP("Mozilla/5.0", "10.0.0.1", "ja3-xyz")
	if fp1 == fp5 {
		t.Error("不同 TLS 指纹应产生不同设备指纹")
	}
	// 输出为 64 字符 hex（SHA-256）。
	if len(fp1) != 64 {
		t.Errorf("指纹应为 64 字符 hex，实际 %d", len(fp1))
	}
}

func TestCollectDeviceFP_ClientHeaderPriority(t *testing.T) {
	// X-Device-FP 头优先于服务端合成。
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Device-FP", "client-provided-fp")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.RemoteAddr = "10.0.0.1:12345"
	fp := collectDeviceFP(req)
	if fp != "client-provided-fp" {
		t.Errorf("应优先使用 X-Device-FP 头，实际 %s", fp)
	}
}

func TestCollectDeviceFP_ServerSynthesis(t *testing.T) {
	// 无 X-Device-FP 头时服务端合成。
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("X-TLS-Fingerprint", "ja3-abc")
	req.RemoteAddr = "10.0.0.1:12345"
	fp := collectDeviceFP(req)
	if fp == "" {
		t.Error("服务端合成应返回非空指纹")
	}
	if len(fp) != 64 {
		t.Errorf("合成指纹应为 64 字符 hex，实际 %d", len(fp))
	}
}

func TestDeviceFPManager_UnknownDeviceTriggersMFA(t *testing.T) {
	m := newDeviceFPManager(nil, true) // 纯内存模式，启用校验
	// 首次登录：未知设备 → needMFA=true。
	known, needMFA := m.checkAndRegister("user-1", "fp-a")
	if known {
		t.Error("首次登录设备应未知")
	}
	if !needMFA {
		t.Error("未知设备应触发 MFA")
	}
	// 第二次登录同一设备：已知 → needMFA=false。
	known2, needMFA2 := m.checkAndRegister("user-1", "fp-a")
	if !known2 {
		t.Error("已注册设备应已知")
	}
	if needMFA2 {
		t.Error("已知设备不应触发 MFA")
	}
	// 不同设备：未知 → needMFA=true。
	_, needMFA3 := m.checkAndRegister("user-1", "fp-b")
	if !needMFA3 {
		t.Error("新设备应触发 MFA")
	}
}

func TestDeviceFPManager_DisabledAllPass(t *testing.T) {
	m := newDeviceFPManager(nil, false) // 关闭校验
	known, needMFA := m.checkAndRegister("user-1", "fp-a")
	if !known {
		t.Error("功能关闭时所有设备应视为已知")
	}
	if needMFA {
		t.Error("功能关闭时不应触发 MFA")
	}
}

func TestDeviceFPManager_EmptyFPBackwardCompatible(t *testing.T) {
	m := newDeviceFPManager(nil, true)
	// 空指纹 → 视为已知（向后兼容旧客户端）。
	known, needMFA := m.checkAndRegister("user-1", "")
	if !known {
		t.Error("空指纹应视为已知（向后兼容）")
	}
	if needMFA {
		t.Error("空指纹不应触发 MFA")
	}
}
