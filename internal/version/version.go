// Package version 暴露 OpsMesh 内核版本，供 --version 与镜像标签使用。
package version

// Version 是内核语义版本（同 binary 双模式共享）。
// 破坏性变更（如 gRPC ServiceName 改名）须在此升主版本。
// 它是"版本源"之一：`opsmesh --version` 与 GET /version 在源码直构（没有 -ldflags 注入）时
// 回的就是这个值，所以 deploy/scripts/validate-deploy-assets.sh 第 1 节把它和
// Chart.yaml appVersion / values-production / gitops segment 一起对账。
var Version = "0.12.0"

// Commit / Date 由 CI 注入。**注入路径必须写模块路径**
// `-X github.com/Levango7/OpsMesh/internal/version.Commit=...`；
// 写成 `opsmesh/internal/version.*` 时 Go 链接器**静默忽略**（构建照过、版本号恒为下面的默认值），
// 2026-09-26 实测确认，当时全仓三处 -X 都写错过而无人报错。
var (
	Commit = "dev"
	Date   = "unknown"
)
