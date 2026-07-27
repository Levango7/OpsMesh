// Package version 暴露 OpsMesh 内核版本，供 --version 与镜像标签使用（P2-3）。
package version

// Version 是内核语义版本（U-05 同 binary 双模式共享）。
// 破坏性变更（如 gRPC ServiceName 改名）须在此升主版本。
var Version = "0.1.0"

// Commit / Date 由 CI 通过 -ldflags "-X opsmesh/internal/version.Commit=..." 注入。
var (
	Commit = "dev"
	Date   = "unknown"
)
