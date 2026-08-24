// Package discover 提供设备网络发现能力。
//
// 职责：扫描指定网段，发现可纳管的设备（SSH/agent 已安装），
// 返回设备清单供控制面注册。
//
// 注意：本包与 internal/discovery 不同——
//   - discover = 设备发现（scan network for devices to manage）
//   - discovery = 控制面服务发现 + 负载均衡（agent finds controlplane endpoints）
package discover
