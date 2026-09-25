// safego.go agent 常驻 goroutine 的 panic 兜底（CB-9）。
//
// 背景：agent 是部署在纳管设备上的常驻进程，worker/心跳/派发/取消/日志采集等
// 循环 goroutine 一旦 panic（如 nil map 写入、外部输入触发边界），默认行为是
// 整个 agent 进程崩溃——该设备上所有在执行任务中断、心跳消失被控制面判离线。
// 任务执行本体已有 exec.CommandContext 保护，但循环体内的业务代码
// （collectCmdbReport/collectDeviceMetrics/drainTasks 等）没有 recover。
//
// 设计：safeGo 包装循环入口，panic 被捕获后记日志（含 panic 值与堆栈）并
// 重启循环（带最小重启间隔防 panic 风暴打爆 CPU）；循环正常 return（ctx 取消）
// 时不重启。与控制面 recoveryMiddleware 同思路：单点 panic 不拖垮整个进程。
package agent

import (
	"context"
	"runtime/debug"
	"time"

	"github.com/Levango7/OpsMesh/internal/logx"
)

// safeGoRestartDelay panic 后重启循环前的最小等待间隔（防风暴）。
const safeGoRestartDelay = 5 * time.Second

// safeGo 启动一个 panic 安全的常驻循环 goroutine。
//
// name 用于日志定位（如 "worker"、"heartbeatLoop"）；fn 应为 for-select 循环，
// ctx 取消时 return 结束（此时不重启）。fn panic 时：记 Error 日志（含堆栈），
// 等待 safeGoRestartDelay 后重启 fn，循环继续提供能力（心跳/派发/取消等不中断）。
// 重启期间该能力短暂缺失（如心跳停 5s+），控制面按租约/归档阈值判定在线状态，
// 短暂缺失不会误判离线（archive-age 默认 1440 分钟）。
//
// 重启原因必须如实区分（实机发现）：若 fn 因「未配置/无事可做」提前 return 而未 cancel
// ctx，旧实现一律打印「panic 后重启」——实机每 5s 一条假 panic 告警（`logCollectLoop`
// 在未配置采集路径时立即 return），运维会去追一个根本不存在的崩溃。故此处按 recover
// 是否真的捕获到 panic 区分措辞；调用方更应避免启动「注定立即返回」的循环。
func safeGo(ctx context.Context, name string, fn func(ctx context.Context)) {
	go func() {
		for {
			panicked := true
			func() {
				defer func() {
					if r := recover(); r != nil {
						logx.Error(ctx, "agent 循环 panic 已捕获并将重启",
							nil, "loop", name, "panic", r,
							"stack", string(debug.Stack()))
						return
					}
					panicked = false // fn 正常 return（ctx 取消，或未配置导致提前退出）
				}()
				fn(ctx)
			}()
			select {
			case <-ctx.Done():
				return // 正常退出（ctx 取消），不重启
			case <-time.After(safeGoRestartDelay):
				if panicked {
					logx.Warn(ctx, "agent 循环 panic 后重启", "loop", name)
				} else {
					logx.Warn(ctx, "agent 循环提前退出，将重启", "loop", name)
				}
			}
		}
	}()
}
