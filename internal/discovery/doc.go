// Package discovery 提供控制面服务发现与负载均衡能力。
//
// 职责：agent 启动时发现控制面地址（静态/动态），并通过 balancer
// 做 failover/round-robin 负载均衡，实现多控制面高可用。
//
// 注意：本包与 internal/discover 不同——
//   - discovery = 控制面服务发现 + 负载均衡（agent finds controlplane endpoints）
//   - discover = 设备发现（scan network for devices to manage）
package discovery
