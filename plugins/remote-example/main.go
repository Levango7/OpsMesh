// plugins/remote-example 是"独立进程 + HTTP 契约"插件运行时的示例与验收载体（TD-62）。
//
// 它**不是**一个演示用的假实现：控制面这边的契约就是照它写的，
// 报告里的端到端证据（deny 阻断 PUT、allow 改写落库）都由这个二进制真实跑出来。
// 客户写自己的插件时，可以直接拿它当模板：任何语言、任何进程，只要满足契约。
//
// 契约全文见同目录 README.md。
//
// 构建：go build -o opsmesh-plugin-example ./plugins/remote-example
// 运行：OPSMESH_PLUGIN_EXAMPLE_TOKEN=... ./opsmesh-plugin-example
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	// 与 internal/plugin/remote.go 的 remoteBodyLimit 同一条判据：
	// 控制面侧限的是响应体，插件侧必须同样限请求体，否则一个超大 payload 就能打穿示例进程。
	maxBody = 1 << 20 // 1 MiB
)

// hookRequest 与 internal/plugin 的 hookRequest 同构（跨语言的契约就在这里）。
type hookRequest struct {
	Plugin  string          `json:"plugin"`
	Hook    string          `json:"hook"`
	Name    string          `json:"name,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type hookResponse struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

type server struct {
	token string
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	// 状态码已发出，写失败无从补救；本仓 errcheck 对 ResponseWriter.Write 有显式豁免。
	_, _ = w.Write([]byte("ok\n"))
}

// hook 是唯一的扩展点入口：三种结局——放行、拒绝、鉴权失败。
func (s *server) hook(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) != 1 {
		// 401 而非 403/200：控制面把非 2xx 记成 outcome="error"，
		// 令牌配错必须显性失败，不能被当成"插件没意见"。
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}
	var req hookRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	decision, reason := policy(req)
	log.Printf("hook=%s name=%s decision=%s reason=%q", req.Hook, req.Name, decision, reason)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(hookResponse{Decision: decision, Reason: reason})
}

// policy 是这个示例插件的"业务"：三条演示用的准入规则。
func policy(req hookRequest) (string, string) {
	switch req.Hook {
	case "config.preSet":
		// 演示改写以外的否决：不允许把 maxTenants 设成 0（等价于关掉多租户容量）。
		var p struct {
			MaxTenants *int `json:"maxTenants"`
		}
		if len(req.Payload) > 0 && json.Unmarshal(req.Payload, &p) == nil && p.MaxTenants != nil && *p.MaxTenants == 0 {
			return "deny", "maxTenants=0 会移除租户容量上限策略"
		}
		return "allow", ""
	case "config.postSet":
		// post 阶段不可阻断：示例明确返回 allow，让这条腿的语义留在日志里可核对。
		return "allow", ""
	case "task.preClaim":
		// 演示按 agent 身份做领取前准入（负载是 agentID 字符串）。
		var agentID string
		if len(req.Payload) > 0 && json.Unmarshal(req.Payload, &agentID) == nil && strings.HasPrefix(agentID, "blocked-") {
			return "deny", "agent 标签 blocked-* 禁止领取任务"
		}
		return "allow", ""
	default:
		// 未知扩展点：宁可让控制面看到 error 也不静默放行（清单漂移的可见化）。
		return "deny", "unknown hook " + req.Hook
	}
}

func main() {
	addr := flag.String("addr", "127.0.0.1:9101", "监听地址（默认只绑环回：这是控制面回调端点，不该对外）")
	tokenEnv := flag.String("token-env", "OPSMESH_PLUGIN_EXAMPLE_TOKEN", "令牌所在环境变量名（令牌不进命令行，避免出现在 ps/日志里）")
	flag.Parse()

	token := strings.TrimSpace(os.Getenv(*tokenEnv))
	if token == "" {
		// 没有令牌就跑不起来：与控制面同一条判据（无鉴权的插件调用不可验证调用方）。
		log.Fatalf("环境变量 %s 为空：独立进程插件必须经令牌互相鉴权", *tokenEnv)
	}
	host, port, err := net.SplitHostPort(*addr)
	if err != nil {
		log.Fatalf("--addr 格式应为 host:port：%v", err)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		log.Printf("警告：监听 %s 把扩展点入口暴露到了非环回接口——该端点能否决平台配置写入，"+
			"请仅在已有反代/TLS 的前提下使用", net.JoinHostPort(host, port))
	}

	s := &server{token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("/hook", s.hook)
	mux.HandleFunc("/health", s.health)

	srv := &http.Server{
		Addr:              net.JoinHostPort(host, port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// 关闭失败要说出来：示例插件是给宿主作者看的，静默吞错会被当成"可以忽略"。
		if err := srv.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "优雅关闭失败（5s 内有连接未完成）：%v\n", err)
		}
	}()

	fmt.Fprintf(os.Stderr, "opsmesh-plugin-example 监听 %s（token-env=%s）\n", srv.Addr, *tokenEnv)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("监听失败：%v", err)
	}
}
