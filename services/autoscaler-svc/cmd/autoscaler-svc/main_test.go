package main

// main_test.go 钉住装配面（buildScaler）。
//
// 为什么这个函数值得单独测：造假面出在这里——此前 main 无条件
// `k8s.NewClient()`，于是"用内存 map 冒充集群执行器"这件事在业务层完全不可见。
// 修复的关键不在于加了真实实现，而在于**配错/配不上时不许退回模拟**。
// 只测业务层的标签会漏掉这条：装配写错一样能让全链路显示 simulated。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/k8s"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/pkg/config"
)

func TestBuildScaler_DefaultIsSimulatedButLabelled(t *testing.T) {
	// 出厂 compose 栈没有集群凭据，默认必须是模拟且**自称**模拟（不能自称真实）。
	s, err := buildScaler(&config.Config{K8sExecutor: ""})
	if err != nil {
		t.Fatalf("buildScaler(空): %v", err)
	}
	if s.Executor() != k8s.ExecutorSimulated {
		t.Fatalf("默认执行器 = %q, want %q", s.Executor(), k8s.ExecutorSimulated)
	}
	if !k8s.IsSimulated(s.Executor()) {
		t.Fatal("默认执行器必须被判定为模拟")
	}
}

func TestBuildScaler_UnknownValueFails(t *testing.T) {
	// 拼错取值不该得到"服务能跑、但永远模拟"。
	_, err := buildScaler(&config.Config{K8sExecutor: "in-cluser"})
	if err == nil {
		t.Fatal("未知 AUTOSCALER_K8S_EXECUTOR 必须报错")
	}
	if !strings.Contains(err.Error(), "AUTOSCALER_K8S_EXECUTOR") {
		t.Fatalf("错误文案应点明是哪个变量配错：%v", err)
	}
}

func TestBuildScaler_KubeconfigModeWithoutPathFails(t *testing.T) {
	// 明确要求 kubeconfig 执行器却没给路径 ⇒ 硬失败，绝不静默降级为内存 map。
	_, err := buildScaler(&config.Config{K8sExecutor: "kubeconfig", KubeConfig: ""})
	if err == nil {
		t.Fatal("kubeconfig 模式缺路径必须报错（不许静默退回模拟）")
	}
}

func TestBuildScaler_KubeconfigModeWithUnparseableFileFails(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "kc.yaml")
	if err := os.WriteFile(p, []byte("not: a: kubeconfig: [\n"), 0o600); err != nil {
		t.Fatalf("写测试 kubeconfig: %v", err)
	}
	if _, err := buildScaler(&config.Config{K8sExecutor: "kubeconfig", KubeConfig: p}); err == nil {
		t.Fatal("无法解析的 kubeconfig 必须报错")
	}
}

func TestBuildScaler_InClusterOutsideClusterFails(t *testing.T) {
	// 单测进程不在 Pod 里，rest.InClusterConfig() 必然失败——这条断言要的就是"失败"，
	// 而不是回落到模拟执行器。
	if _, err := buildScaler(&config.Config{K8sExecutor: "in-cluster"}); err == nil {
		t.Fatal("集群外运行 in-cluster 模式必须报错")
	}
}
