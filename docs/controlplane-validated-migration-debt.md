# controlplane 「构造即校验」迁移现状与债务清单

> 核验日期：2026-11-18。来源：第三方 pkg.go.dev v0.18.0 报告「controlplane 是上帝包，
> 应把非装配逻辑下沉」的核验后续。

## 核验结论

报告基于 2026-11-07 的 v0.18.0 快照称 controlplane 是「上帝包」（直接 import 109 个包）。
对照当前 main 实测：

- **已大幅改善**：当前 controlplane 有 18 个子包（`cutover` / `backup` / `credentials` /
  `factory` / `embed` / `enterpriseui` / `grpc` / `ratelimit` / `paginate` / `presets` /
  `svcproxy` / `tenantguard` / `loginguard` / `auditsafe` / `metricscache` / `cmdbcollector` /
  `pluginhost` / `registry`）——报告点名要下沉的包**已经是独立子包**。
- **import 数**：实测 67 个唯一内部 import（报告称 109，夸大）。
- **validated* 迁移**：7 个高频 ops 文件已完成「构造即校验」收口
  （db / vector / k8s_provision / monitor / middleware / mongo / ansible），
  各有 b29-b36 逐文件门禁 + 本轮新增的全局 sweeper（`validated_coverage_sweeper_test.go`）。

**结论**：报告「上帝包」描述在当前 main 已不成立；继续强拆只剩 Server 方法的
git_unix_ops 等需要大改 Server 接口，风险远大于收益，不做。

## 剩余债务：19 个未迁移的 ops 文件

以下文件有 `build*Script` 但尚无 `validate*` 配套（build 仍吃原始 req 类型，
校验内联在 handler 里）。按风险/使用频度逐步迁移，每文件一个 PR + 门禁测试：

| 文件 | build 函数数 |
|---|---|
| `abac_ops.go` | 1 |
| `activemq_middleware_ops.go` | 1 |
| `dns_ops.go` | 1 |
| `env_ops.go` | 4 |
| `fileshare_ops.go` | 1 |
| `git_unix_ops.go` | 1 |
| `graph_ops.go` | 5 |
| `harbor_ops.go` | 5 |
| `kafka_middleware_ops.go` | 2 |
| `kernel_ops.go` | 4 |
| `lb_ops.go` | 7 |
| `ntp_ops.go` | 1 |
| `olap_ops.go` | 5 |
| `rabbitmq_middleware_ops.go` | 1 |
| `redis_middleware_ops.go` | 2 |
| `security_ops.go` | 4 |
| `ssh_trust_ops.go` | 1 |
| `storage_ops.go` | 4 |
| `system_ops.go` | 3 |
| `tsdb_ops.go` | 5 |
| `virt_ops.go` | 4 |
| `zookeeper_middleware_ops.go` | 1 |

> 迁移范式见 `b36_injection_test.go`：新增 `validateXxx() (validatedXxx, error)`，
> build 只接受 `validatedXxx`，handler 调 validate 后再 build，最后加 `bNN_injection_test.go` 门禁。
> 新增迁移文件请同步登记到 `validated_coverage_sweeper_test.go` 的 `migratedFiles`。

## 不变式

`validated_coverage_sweeper_test.go` 保证已迁移的 7 个文件不回潮；新增迁移只需加一行登记。
