// jwt_devicefp_test.go — JWT device_fp claim 绑定测试（TD-60）。
package auth

import (
	"testing"
	"time"
)

func TestIssueTokenWithDeviceFP_ClaimsContainFP(t *testing.T) {
	eng := NewEngine("test-secret-32-chars-long!!!!!", 15*time.Minute, 7*24*time.Hour)
	token, _, err := eng.IssueTokenWithDeviceFP("user-1", "alice", []string{"admin"}, []string{"user:read"}, "fp-abc", 15*time.Minute)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	claims, err := eng.ValidateToken(token)
	if err != nil {
		t.Fatalf("验签 token 失败: %v", err)
	}
	if claims.DeviceFP != "fp-abc" {
		t.Errorf("claims.DeviceFP=%q, want fp-abc", claims.DeviceFP)
	}
}

func TestIssueToken_EmptyDeviceFPBackwardCompatible(t *testing.T) {
	eng := NewEngine("test-secret-32-chars-long!!!!!", 15*time.Minute, 7*24*time.Hour)
	// IssueToken（旧 API）不传 deviceFP → claimsDclaims.DeviceFP 为空。
	token, _, err := eng.IssueToken("user-1", "alice", []string{"admin"}, []string{"user:read"})
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	claims, err := eng.ValidateToken(token)
	if err != nil {
		t.Fatalf("验签 token 失败: %v", err)
	}
	if claims.DeviceFP != "" {
		t.Errorf("旧 API 签发的 token DeviceFP 应为空，实际 %q", claims.DeviceFP)
	}
}

func TestValidateToken_DeviceFPInClaims(t *testing.T) {
	eng := NewEngine("shared-secret-32-chars-long!!", 15*time.Minute, 7*24*time.Hour)
	// 模拟 controlplane 共享密钥验签：auth-svc 签发，controlplane 用同密钥验签。
	token, _, err := eng.IssueTokenWithDeviceFP("user-1", "bob", []string{"viewer"}, []string{"user:read"}, "device-hash-xyz", 15*time.Minute)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	// 用同一 Engine（同密钥）验签——模拟 controlplane ParseHSJWT。
	claims, err := eng.ValidateToken(token)
	if err != nil {
		t.Fatalf("验签失败: %v", err)
	}
	// claims 契约：sub/tenant/roles/exp/iat/device_fp 均存在。
	if claims.UserID != "user-1" {
		t.Errorf("sub=%q, want user-1", claims.UserID)
	}
	if claims.TenantID != "default" {
		t.Errorf("tenant=%q, want default", claims.TenantID)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "viewer" {
		t.Errorf("roles=%v, want [viewer]", claims.Roles)
	}
	if claims.DeviceFP != "device-hash-xyz" {
		t.Errorf("device_fp=%q, want device-hash-xyz", claims.DeviceFP)
	}
	if claims.JTI == "" {
		t.Error("jti 不应为空")
	}
	if claims.ExpiresAt.IsZero() {
		t.Error("exp 不应为零")
	}
}
