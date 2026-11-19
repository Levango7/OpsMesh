# MySQL 依赖中断 drill（issue #88 第一批）

`chaos-dependency-down.sh` 面向 **docker-compose + MySQL store** 的独立演练栈，验证：

1. 正常时 `/healthz`、`/readyz` 均为 200；
2. 停止 MySQL 后进程仍存活（healthz 200），readiness 在有界窗口内变 503；
3. `/api/v1/devices` 在 5 秒内必有响应（允许 200/401/403/5xx，不把鉴权结果当依赖故障）；
4. 启动 MySQL 后 readyz 在 90 秒内恢复 200；
5. 任何失败或 Ctrl-C 都经 EXIT trap 尝试启动 MySQL。

## 使用

必须在独立 drill 栈上运行，并显式传 compose project。默认 store=memory 的栈不适用：MySQL 停止不会改变 readiness，脚本会正确地判红。

```bash
export OPSMESH_CHAOS_PROJECT=opsmesh-chaos
export OPSMESH_CHAOS_BASE_URL=http://127.0.0.1:8080
bash deploy/scripts/chaos-dependency-down.sh mysql
```

可选环境变量：

| 变量 | 默认 | 用途 |
|---|---|---|
| `OPSMESH_CHAOS_COMPOSE_FILE` | 仓根 `docker-compose.yaml` | 演练 compose 文件 |
| `OPSMESH_CHAOS_HTTP_TIMEOUT` | `5` | 单次 HTTP 硬上限（秒） |
| `OPSMESH_CHAOS_DEGRADED_TIMEOUT` | `20` | 等待 readiness 变 503 的上限（秒） |
| `OPSMESH_CHAOS_RECOVERY_TIMEOUT` | `90` | 依赖恢复后等待 200 的上限（秒） |

脚本只输出状态码与耗时，不打印响应体，避免把敏感内容写进日志。`prod`/`production` 项目名会被直接拒绝。

## 离线判据自测

```bash
bash deploy/scripts/chaos-dependency-down.selftest.sh
```

自测以假 docker/curl 覆盖正向恢复、readiness 假阳性判红、失败后自动恢复，不接触真实 Docker 或网络。

## 边界

- 本批只有 MySQL；Redis 当前没有 `/readyz` 依赖判据，不能照搬「停依赖就 503」的期望。
- 这是手工 drill，尚未接入 CI；真实 compose 结果需记录 commit、项目名、时间与原始输出。
- 不验证数据不丢、SQL 重连后的完整业务读写、多副本 SSE；这些应作为后续独立场景，不能用本脚本替代。
