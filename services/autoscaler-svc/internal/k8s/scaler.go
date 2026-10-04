package k8s

// Scaler 是扩缩容执行器的唯一接口，也是"这次到底动没动集群"的唯一判定面。
//
// 为什么要有这个接口（#61 造假面之二，2026-10-04）：本包此前的 `Client` 是一个
// 进程内 `map[string]int32`，包注释自己写着"In production, this would use
// client-go"，但 main.go 无条件把它当真实执行器装配。于是
// `POST /api/v1/scale` 返回 `{"status":"ok","message":"scaled default/api to 5 replicas"}`
// 时，**没有任何集群被改动过**，重启即丢，而调用方（前端、runbook、告警剧本）读到的是
// 成功口径。修复方式不是"删掉模拟"（开发/演示确实需要它），而是：
//   - 让执行器身份成为可查询的一等信息（Executor / Simulated）；
//   - 让每一次响应与每一条决策历史都带上这个身份；
//   - 让"明确要求真实执行却配不上"成为启动期硬失败，而不是静默退回 map。
type Scaler interface {
	// GetReplicas 返回目标 Deployment 的当前副本数。
	GetReplicas(deployment, namespace string) (int32, error)

	// SetReplicas 把目标 Deployment 的副本数设为 replicas。
	SetReplicas(deployment, namespace string, replicas int32) error

	// Executor 返回执行器标识，用于给调用方与决策历史标注"这次是真改动还是模拟"。
	// 取值见 ExecutorSimulated / ExecutorKubeconfig / ExecutorInCluster。
	Executor() string
}

// 执行器标识常量。**字符串即对外口径**，不要改成中文或加空格——
// 决策历史、API 响应与出厂告警都按这些字面量匹配。
const (
	// ExecutorSimulated：进程内 map。不触碰任何集群，重启即丢。仅适合开发与演示。
	ExecutorSimulated = "memory-simulated"

	// ExecutorKubeconfig：通过 kubeconfig 连接远端 APIServer 真实改副本数。
	ExecutorKubeconfig = "k8s-kubeconfig"

	// ExecutorInCluster：Pod 内 ServiceAccount 身份，真实改副本数。
	ExecutorInCluster = "k8s-in-cluster"
)

// IsSimulated 报告某执行器标识是否属于"无副作用"那一类。
//
// 判定集中在这一处：service 与 handler 都要据此给响应打标注，散着写迟早分叉。
func IsSimulated(executor string) bool { return executor == ExecutorSimulated }
