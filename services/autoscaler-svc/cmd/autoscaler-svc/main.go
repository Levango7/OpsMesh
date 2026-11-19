package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Levango7/OpsMesh/internal/logx"
	applog "github.com/Levango7/OpsMesh/pkg/log"
	"github.com/Levango7/OpsMesh/pkg/metrics"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/evaluator"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/handler"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/k8s"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/prometheus"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/pkg/config"
)

// buildScaler 按配置装配扩缩容执行器。三条规则：
//
//  1. `simulated` 是默认且合法的（开发、演示，以及出厂 compose 栈里没有集群凭据这种
//     形态），但它只改进程内存，所以启动日志必须把这件事说明白；
//  2. **明确要求真实执行却建不起来 ⇒ 返回错误**，调用方据此退出。此前的缺陷正是
//     无条件用内存 map 还对外报"已扩容"，绝不能再退回静默降级；
//  3. 取值拼错 ⇒ 报错。`AUTOSCALER_K8S_EXECUTOR=in-cluser` 这种笔误不该得到
//     "服务能跑、但永远模拟"。
func buildScaler(cfg *config.Config) (k8s.Scaler, error) {
	switch cfg.K8sExecutor {
	case "", "simulated":
		return k8s.NewClient(), nil
	case "kubeconfig":
		return k8s.NewFromKubeconfig(cfg.KubeConfig)
	case "in-cluster":
		return k8s.NewInCluster()
	default:
		return nil, fmt.Errorf("未知的 AUTOSCALER_K8S_EXECUTOR=%q（可选 simulated | kubeconfig | in-cluster）", cfg.K8sExecutor)
	}
}

func main() {
	// P1-6 结构化日志统一：把标准库 log 接入统一 JSON 管道（级别由 OPSMESH_LOG_LEVEL 控制）。
	// 必须最先调用——早于任何日志输出。
	lgr := applog.Init("autoscaler-svc")
	cfg := config.Load()

	metrics.Init("autoscaler-svc")

	eng := evaluator.NewEvaluator(nil)

	// 持久化接线（issue #48 裁决①）：DSN 非空即装配 MySQL store，装载存量规则并写穿；
	// 装配失败 fail-fast——"声明了持久化却装载不进来"比启动失败更难发现（假持久化）。
	// 未配置 DSN = 纯内存（单副本/开发形态），行为与接线前逐字相同。
	if cfg.DSN != "" {
		st, err := store.NewMySQLStore(cfg.DSN)
		if err != nil {
			lgr.Fatalf("autoscaler 持久化装配失败（issue #48 裁决①）：DSN 已配置但建连/迁移不通过，"+
				"声明了持久化却装载不进来比启动失败更难发现。err=%v", err)
		}
		if err := eng.SetStore(st); err != nil {
			lgr.Fatalf("autoscaler 存量规则装载失败: %v", err)
		}
		logx.Info(context.Background(), "autoscaler 持久化已接线（存量规则已装载，重启不丢）",
			"service", "autoscaler-svc")
	} else {
		logx.Info(context.Background(), "autoscaler 未配置 DSN：规则/决策仅内存（重启即丢），持久化未启用",
			"service", "autoscaler-svc")
	}

	reader := prometheus.NewClient(cfg.PrometheusURL)
	scaler, err := buildScaler(cfg)
	if err != nil {
		// 装配失败必须硬退出：本服务此前的形态是"无条件用内存 map，API 照样返回成功"，
		// 于是运维以为在扩容、实际什么都没动。
		lgr.Fatalf("扩缩容执行器装配失败: %v", err)
	}
	if k8s.IsSimulated(scaler.Executor()) {
		logx.Warn(context.Background(), "扩缩容执行器=memory-simulated：scale 动作只改本进程内存，**不会改动任何集群**，重启即丢。"+
			"需要真实执行请设 AUTOSCALER_K8S_EXECUTOR=in-cluster（Pod 内）或 kubeconfig + AUTOSCALER_KUBECONFIG",
			"service", "autoscaler-svc", "executor", scaler.Executor())
	} else {
		logx.Info(context.Background(), "扩缩容执行器已启用（会真实改动集群副本数）", "service", "autoscaler-svc", "executor", scaler.Executor())
	}
	svc := service.NewService(eng, reader, scaler)
	h := handler.NewHandler(svc)

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.GetHandler())
	h.RegisterRoutes(mux)

	httpServer := &http.Server{
		ReadHeaderTimeout: 15 * time.Second, // G112 Slowloris 防护
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           metrics.HTTPMiddleware(mux),
	}

	go func() {
		logx.Info(context.Background(), "Starting autoscaler-svc HTTP server", "service", "autoscaler-svc", "port", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			lgr.Fatalf("HTTP server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logx.Info(context.Background(), "Shutting down...", "service", "autoscaler-svc")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logx.Error(shutdownCtx, "HTTP server shutdown error", err, "service", "autoscaler-svc")
	}

	logx.Info(context.Background(), "Server stopped", "service", "autoscaler-svc")
}
