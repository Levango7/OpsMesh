// Package tlsutil 的证书热重载实现。
//
// 设计目标：当证书文件被外部进程（certbot / k8s secret mount reload / 运维手动替换）变更时，
// 控制面无需重启即可更新 TLS 配置，避免长连接中断与短暂不可用窗口。
//
// 实现要点：
//   - 使用 github.com/fsnotify/fsnotify 监听证书/私钥文件的 WRITE/CREATE 事件；
//   - CertificateReloader 实现 tls.Config.GetCertificate 接口，gRPC/HTTP TLS 握手时按需返回最新证书；
//   - 防抖（debounce）：文件原子替换（写新文件 + rename）会触发多次事件，用 time.Timer 合并
//     100ms 窗口内的多次事件为一次 reload，避免在替换中途读到半成品文件；
//   - reload 失败时仅打日志，不替换旧证书，保持服务可用性（旧证书仍可继续服务已建立连接）；
//   - Close 关闭 watcher 与退出 goroutine，避免资源泄漏。
package tlsutil

import (
	"crypto/tls"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// reloadDebounce 是文件变更事件防抖窗口。
// 文件原子替换（写新文件 + rename）会在很短时间内触发多次 fsnotify 事件，
// 合并 100ms 内的多次事件为一次 reload，避免读到半成品文件。
const reloadDebounce = 100 * time.Millisecond

// CertificateReloader 实现 tls.Config.GetCertificate 接口，支持证书热重载。
//
// 字段说明：
//   - certFile / keyFile：证书与私钥文件路径，构造后不变；
//   - mu：保护 cert 字段的读写锁，GetCertificate 持读锁、reload 持写锁；
//   - cert：当前生效的 tls.Certificate，由 reload 原子替换；
//   - watcher：fsnotify Watcher，监听证书/私钥文件变更；
//   - closed：Close 时 close 该 channel，通知 watcher 循环退出；
//   - reloadAttempts：自构造以来 reload 尝试次数（含失败），供测试与运维
//     观测热重载活动——事件到达并过防抖后计数必然增加，据此可轮询等待
//     「事件已被处理」，替代在不同文件系统上延迟差异极大的固定 sleep。
type CertificateReloader struct {
	certFile       string
	keyFile        string
	mu             sync.RWMutex
	cert           tls.Certificate
	watcher        *fsnotify.Watcher
	closed         chan struct{}
	reloadAttempts atomic.Int64
	wg             sync.WaitGroup
}

// NewCertificateReloader 构造 CertificateReloader：初始加载证书 + 启动 watcher 监听变更。
//
// 失败情形：
//   - 初始证书加载失败：返回 error，调用方应 fail-fast（启动期证书缺失属配置错误）；
//   - watcher 创建失败：返回 error（系统资源限制 / 平台不支持）；
//   - 单个文件 AddWatch 失败：返回 error（路径不存在 / 权限不足）。
//
// 成功后后台 goroutine 持续监听文件变更，触发防抖 reload。
// 调用方应在退出时调用 Close 释放资源。
func NewCertificateReloader(certFile, keyFile string) (*CertificateReloader, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	r := &CertificateReloader{
		certFile: certFile,
		keyFile:  keyFile,
		cert:     cert,
		watcher:  watcher,
		closed:   make(chan struct{}),
	}

	// 监听证书与私钥文件本身（覆盖原地写入场景）。
	// 同时监听父目录（覆盖 rename 原子替换场景：rename 后 inode 变化，原文件 watcher 失去目标，
	// 目录监听可捕获 rename 事件再次触发 reload）。
	for _, p := range []string{certFile, keyFile} {
		if err := watcher.Add(p); err != nil {
			_ = watcher.Close()
			return nil, err
		}
		if dir := filepath.Dir(p); dir != "" && dir != "." {
			// 目录监听失败不致命（可能权限不足），仅记日志跳过；文件本身监听已覆盖原地写入。
			if err := watcher.Add(dir); err != nil {
				log.Printf("tlsutil: 监听证书父目录失败（跳过 rename 场景监听）: %v", err)
			}
		}
	}

	go r.watchLoop()
	return r, nil
}

// watchLoop 是后台 watcher 事件循环：读取事件 → 防抖 → reload。
//
// 防抖实现：收到首个事件后启动 100ms 计时器，期间到达的后续事件重置计时器，
// 计时器到期后执行一次 reload。这样把短时间内多次事件合并为一次 reload。
//
// 退出条件：closed channel 关闭（Close 调用）或 watcher.Events/Errors 关闭。
func (r *CertificateReloader) watchLoop() {
	r.wg.Add(1)
	defer r.wg.Done()
	var debounce *time.Timer
	for {
		// debounce 为 nil 时 timerC 为 nil channel（永久阻塞），避免误触发 reload。
		var timerC <-chan time.Time
		if debounce != nil {
			timerC = debounce.C
		}
		select {
		case <-r.closed:
			return
		case event, ok := <-r.watcher.Events:
			if !ok {
				return
			}
			// 仅关心写入/创建/重命名/删除事件，忽略 chmod 等无关事件。
			// 删除事件也触发 reload（文件可能被 rename 替换，新文件随后创建会再触发一次）。
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			// 防抖：首个事件启动计时器，后续事件重置计时器，到期后执行一次 reload。
			if debounce == nil {
				debounce = time.NewTimer(reloadDebounce)
			} else {
				if !debounce.Stop() {
					// 计时器已触发且 timer.C 中有值，排空避免下一轮误触发。
					select {
					case <-debounce.C:
					default:
					}
				}
				debounce.Reset(reloadDebounce)
			}
		case err, ok := <-r.watcher.Errors:
			if !ok {
				return
			}
			// watcher 错误通常为底层 OS 错误（路径删除/权限变更），打日志后继续；
			// 不退出循环，避免瞬时错误导致热重载永久失效。
			log.Printf("tlsutil: watcher 错误（继续监听）: %v", err)
		case <-timerC:
			// 计时器到期：执行一次 reload，重置 debounce 状态。
			debounce = nil
			r.reload()
		}
	}
}

// GetCertificate 实现 tls.Config.GetCertificate 接口，返回当前证书。
//
// gRPC/HTTP TLS 握手时由 tls 包回调，持读锁取 cert。
// 与 tls.Config.Certificates 互斥使用：设置了 GetCertificate 后 Certificates 字段被忽略。
//
// 必须返回**副本的地址**，不能 `return &r.cert`：`defer RUnlock()` 在本次返回后就解锁，
// 而调用方（crypto/tls 与测试）拿到的若是指向结构体字段的别名，它之后的每次解引用都发生在
// 锁外 —— 与 reload() 在写锁里做的 `r.cert = cert` 整体覆盖构成数据竞态
// （2026-10-08 CI 与本机 -race 均实测复现：race detected，写点 reloader.go:182、读点经由本函数）。
// 副本共享底层 [][]byte / PrivateKey，但 reload 从不原地改这些切片，只换整个结构体，故复制即安全。
func (r *CertificateReloader) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cert := r.cert
	return &cert, nil
}

// reload 重新加载证书文件，更新内部 cert。
//
// 失败处理：仅打日志，不替换旧证书。这样旧证书继续服务已建立连接与新建连接，
// 避免因临时文件损坏/半成品导致服务完全不可用。运维可通过日志发现 reload 失败并修复。
//
// 并发安全：持写锁更新 cert。GetCertificate 持读锁不会读到半更新状态。
func (r *CertificateReloader) reload() {
	r.reloadAttempts.Add(1)
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		// reload 失败：保持旧证书，仅打日志。避免半成品文件导致服务不可用。
		log.Printf("tlsutil: 证书热重载失败，保持旧证书: %v", err)
		return
	}
	r.mu.Lock()
	r.cert = cert
	r.mu.Unlock()
	log.Printf("tlsutil: 证书热重载成功")
}

// ReloadAttempts 返回自构造以来 reload 的尝试次数（含失败）。
//
// 用途：测试与运维观测。文件变更事件经防抖后必然使计数增加，
// 故「等待计数达到 N」是「事件已被 watcher 处理」的确定性信号，
// 可替代固定 sleep——后者在事件面延迟大的环境（如本机 Windows）
// 会把「事件尚未到达」误判为「reload 未发生」（TD-82）。
func (r *CertificateReloader) ReloadAttempts() int64 {
	return r.reloadAttempts.Load()
}

// Close 关闭 watcher 与退出 watchLoop goroutine，释放资源。
//
// 多次调用安全：用 closed channel 的非阻塞 close 模式避免重复 close panic。
// watchLoop 通过 closed channel 退出，goroutine 不泄漏。
// 先关闭 closed channel 通知 watchLoop 退出，再等待其完成（wg.Wait），
// 最后关闭 watcher——避免 watcher 被关闭时 watchLoop 仍在访问（消除 -race 检测到的数据竞态）。
func (r *CertificateReloader) Close() error {
	select {
	case <-r.closed:
		// 已关闭，直接返回。
	default:
		close(r.closed)
	}
	r.wg.Wait()
	return r.watcher.Close()
}
