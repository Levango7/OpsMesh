# 依赖故障与恢复一致性矩阵（issue #152，#88 后续）

`chaos-recovery-matrix.sh` 在 **真实 docker-compose 栈** 上执行四类依赖故障注入，
验证「进程存活 → 有界降级 → 有界恢复 → 业务写读与数据一致性」的完整闭环，
并把每类故障的注入方式、故障期断言、恢复断言、业务一致性、原始日志位置落成结构化报告。

与 `chaos-dependency-down.sh`（#88 第一批）的关系：后者只验证 MySQL 停止后 `/readyz` 摘流；
本矩阵在其上补齐业务读写一致性、Redis/Kafka 故障语义与网络分区，是 #152 的交付物。

## 场景与判据

| 场景 | 注入 | 故障期断言 | 恢复断言 | 业务一致性 |
|---|---|---|---|---|
| mysql | `compose stop mysql` | `/healthz` 200；`/readyz` 有界 503；API 有界响应；业务写有界失败且不伪装 201 | `/readyz` 有界回 200 | 故障前创建的 API key 恢复后可读；新写 201；`/api/v1/audit/verify` 200；审计链含故障前/后事件 |
| redis | `compose stop redis` | 既有 JWT 会话继续有效；logout best-effort；新登录不被限流误伤（fail-open） | 登出后旧会话有界变 401（黑名单重新生效） | 会话不丢；黑名单故障期豁免是 `RedisSessionStore` 既定容错语义，非数据丢失 |
| kafka | `compose stop kafka` | 业务写有界（SQL 落库不依赖 broker）；`audit/verify` 200；故障期事件入 SQL 审计链 | 新事件有界入 topic；基线事件仍在；控制面容器不重启 | SQL 侧一致；bus 侧故障期事件去向被明确记录（见「已知边界」） |
| partition | `docker network disconnect` mysql / agent | `/healthz` 200；`/readyz` 有界 503；写有界失败；agent 心跳中断 | `/readyz` 有界回 200；业务写恢复；agent `live` 有界收敛 | 故障期写有界失败；恢复后新写成功 |

报告为 `$ARTIFACTS/report.md`，原始日志为 `$ARTIFACTS/<scenario>.log` 与 `kafka-d*.log`。
机制断言失败 ⇒ 对应行 `FAIL`、脚本退出码非零；产品行为与理想不符但机制健康 ⇒ 记 `FINDING`，不判红。

## 使用

必须在独立 drill 栈上运行，显式传 compose project。默认 store=memory 的栈不适用。

```bash
export OPSMESH_CHAOS_PROJECT=opsmesh-chaos
export OPSMESH_CHAOS_COMPOSE_FILE=/path/to/isolated/docker-compose.yaml   # 可选，默认仓根
export OPSMESH_CHAOS_BASE_URL=http://127.0.0.1:18082                       # 按隔离栈端口
export OPSMESH_CHAOS_ARTIFACTS=/path/to/artifacts                          # 报告与原始日志
bash deploy/scripts/chaos-recovery-matrix.sh
```

`OPSMESH_CHAOS_SCENARIOS` 可选子集（如 `mysql,kafka`）。`prod`/`production` 项目名直接拒绝。

隔离栈生成参考（端口/镜像/project 全部独立，凭据随机 0600，不碰其他项目容器）：
复制仓根 `docker-compose.yaml`，改三处宿主端口为独立高位端口（如 18082/19090/19091）、
镜像名加独立前缀、`build.context` 指向仓绝对路径，配合随机 `MYSQL_*`/`OPSMESH_JWT_SECRET` env 文件。
`docker compose --project-name <独立名> --file <生成文件> --env-file <0600 env> up -d --build`。

## 凭据与敏感数据处理

- 首登 `admin/admin123` 触发 `mustChangePassword`：脚本用一次性 `changePasswordToken` 改密为随机口令
  （满足大小写/数字/特殊字符复杂度），存 `$ARTIFACTS/admin-password.txt`（0600）供同一一次性栈复跑；
  会话 Cookie 存 `admin.jar`（0600），退出时清理。
- 只输出状态码/耗时/资源 ID/资源名；不打印响应体、JWT、Cookie、口令。
- API key 创建响应含一次性明文 key：只提取 id，临时文件用后即删。
- artifacts 目录默认 `chaos-artifacts/`（已入 .gitignore），不要提交。

## Windows / Git Bash 注意

- 脚本内喂给 curl 的 `-o/-b/-c` 路径一律 `cygpath -w` 转换：`mktemp` 的 `/tmp/...` 与 `/f/...`
  是 MSYS 路径，Windows 原生 curl.exe 不认，否则 body/cookie 写到别处、断言读到空文件。
- 空响应体不写 `/dev/null`（Windows curl 会 error 23），统一落 `$ARTIFACTS/.body-scratch`。
- `docker exec` 的容器内路径前已 `export MSYS_NO_PATHCONV=1`，防止 `/opt/...` 被改写成 Windows 路径。

## 离线判据自测

```bash
bash deploy/scripts/chaos-recovery-matrix.selftest.sh
```

假 docker/curl 覆盖：prod 守卫拒绝、MySQL 场景正向（停→有界降级→恢复→业务一致性→报告 PASS）、
反向（readyz 假阳性不摘流 ⇒ 判红且 EXIT trap 仍恢复依赖）。不接触真实 Docker 或网络。

## 已知边界（不因此判红，须随报告如实记录）

- **F-152-kafka-1**：Kafka 故障期 bus 事件丢失。`NewKafkaBusWithWAL`（WAL 兜底+后台重放）存在，
  但生产路径 `events.New`（server.go / backup.go）走 `newKafkaBus`，未接 WAL；
  各 handler 调用 `s.bus.Publish(...)` 忽略返回错误。SQL 审计链（`s.audit`）不受影响、独立落库。
  该发现的修复属独立产品改动（接线 WAL 或引入持久化重放），不在本 drill 脚本内擅自实施。
- **F-152-healthz-1**：`/healthz` 是深度检查（`pingStore`），MySQL 中断/分区时返回 503（实测）。
  Helm `controlplane-deployment.yaml` 把它同时当 `livenessProbe`：依赖故障会让 K8s 判存活失败并重启控制面。
  存活判据应钉「进程不退出、容器不重启」（本矩阵的 `assert_outage_healthz` 即查容器 Running），
  liveness 宜与深度 readiness 分离——属产品/部署决策，不在本 drill 内擅自改。
- **F-152-partition-1（间歇）**：`docker network disconnect` 可能丢掉 compose 注册的服务别名，
  朴素 `docker network connect` 不恢复别名 ⇒ 控制面 `lookup mysql: no such host`、readyz 持续 503。
  实测两次：一次持续不收敛（须带别名重连才恢复），一次朴素 connect 即收敛——**恢复不可靠**。
  分区恢复建议带别名重连（`docker network connect --alias <svc>`）或 `compose up -d --force-recreate <svc>`。
  本矩阵的分区恢复即先观察朴素 connect 是否收敛，不收敛则带别名重连兜底。
- Redis 无 `/readyz` 依赖判据：恢复用「登出使会话失效」的行为断言，而非容器 healthy。
- 单副本、单 broker、无 TLS 的演示栈形态；不覆盖多副本 SSE、长跑 soak、生产 TLS——属 #97/#153 范围。
- 网络分区用 `docker network disconnect` 模拟依赖/代理失联，非真实链路丢包/延迟模型。
