package version

import (
	"regexp"
	"testing"
)

// Version 是"版本源"之一：GET /version、--version 与镜像标签都读它，
// deploy/scripts/validate-deploy-assets.sh 第 1 节还会把它与 Chart.yaml
// appVersion / values-production / gitops segment 对账。这里只钉"形状"，
// 不钉具体号（每次切版都会改），防止误写 pre-release/dev 后缀或空值。
func TestVersionIsSemver(t *testing.T) {
	re := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	if !re.MatchString(Version) {
		t.Fatalf("Version = %q, 期望纯 X.Y.Z（对账脚本按此切分）", Version)
	}
}

// Commit/Date 在源码直构时保持如下默认值；CI 通过
// -X github.com/Levango7/OpsMesh/internal/version.Commit=... 覆盖。
// 写成 opsmesh/internal/version.* 时链接器会静默忽略（2026-09-26 事故），
// 因此"默认值非空"是能在此层守住的最低契约。
func TestBuildMetadataDefaults(t *testing.T) {
	if Commit == "" {
		t.Fatal("Commit 默认值不能为空（构建信息展示会输出空白）")
	}
	if Date == "" {
		t.Fatal("Date 默认值不能为空")
	}
	if Commit != "dev" || Date != "unknown" {
		t.Logf("构建元数据已被注入: Commit=%q Date=%q", Commit, Date)
	}
}
