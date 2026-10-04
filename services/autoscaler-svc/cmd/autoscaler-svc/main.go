package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	applog "github.com/Levango7/OpsMesh/pkg/log"
	"github.com/Levango7/OpsMesh/pkg/metrics"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/evaluator"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/handler"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/k8s"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/prometheus"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/service"
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
	reader := prometheus.NewClient(cfg.PrometheusURL)
	scaler, err := buildScaler(cfg)
	if err != nil {
		// 装配失败必须硬退出：本服务此前的形态是"无条件用内存 map，API 照样返回成功"，
		// 于是运维以为在扩容、实际什么都没动。
		lgr.Fatalf("扩缩容执行器装配失败: %v", err)
	}
	if k8s.IsSimulated(scaler.Executor()) {
		log.Printf("WARN｜扩缩容执行器=memory-simulated：scale 动作只改本进程内存，**不会改动任何集群**，重启即丢。" +
			"需要真实执行请设 AUTOSCALER_K8S_EXECUTOR=in-cluster（Pod 内）或 kubeconfig + AUTOSCALER_KUBECONFIG")
	} else {
		log.Printf("扩缩容执行器=%s（会真实改动集群副本数）", scaler.Executor())
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
		log.Printf("Starting autoscaler-svc HTTP server on :%d", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			lgr.Fatalf("HTTP server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
