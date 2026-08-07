# Changelog

本文件记录 OpsMesh 所有重要变更。格式参考 [Keep a Changelog](https://keepachangelog.com/)。

## [Unreleased] — 2026-08-07

### 文档完善：README flag 全量说明 + API 参考 + 部署指南

#### 新增
- **`docs/api-reference.md`**：完整 API 参考文档，覆盖全部 HTTP REST 端点（认证/用户/角色/权限/设备/agent/任务/告警/部署/编排/CMDB/OS 优化/中间件/K8s/审计/日志/联邦/SSE/bootstrap）与 gRPC API，每端点含方法、路径、请求体、响应体、示例
- **`docs/deployment-guide.md`**：完整部署指南，覆盖 docker-compose 一键启动、Docker 镜像、Systemd service、Helm Chart（values.yaml/values-production.yaml 配置对照）、K8s Operator（CRD）、生产环境检查清单（安全/性能/高可用/可观测）、多租户 schema 隔离部署、联邦 mTLS 部署

#### 变更
- **`README.md` 配置参考全量补全**：原 38 条 flag 说明扩充为 **75 条全量 flag**，按功能分组（基础/存储/安全/网络/告警/日志/调度/纳管八大组），每条含 Flag、类型、默认值、环境变量、说明五列；与 `internal/config/config.go` 中 flag 定义逐一对应

---

## [0.1.1] — 2026-08-07

### 部署配置对齐修复 + 测试覆盖率提升 + 前端企业版功能对齐 + 性能优化 + 安全加固

#### 部署配置对齐修复
- **docker-compose.yaml**：controlplane 启用 `--store=mysql` + `--advertise-addr=http://controlplane:8080`，对齐 Helm values；安全相关 flag 环境变量映射注释完整（cookie-secure/public-register/allow-public-register/federation-*/provision-secret/grpc-require-signature/trust-proxy/jwt-public-key/metrics-allow-cidr）
- **systemd env 模板**：`opsmesh-controlplane.env` 补全全部安全加固项注释（federation mTLS/grpc-require-signature/trust-proxy/jwt-public-key/client-ca/metrics-allow-cidr），与 config.go flag 一一映射
- **Helm values-production.yaml**：3 副本 + mysql + TLS + require-auth + cookie-secure + podAntiAffinity + 资源放大，与 systemd 生产配置对齐

#### 测试覆盖率提升
- **CI 覆盖率门禁**：整体 ≥40%（build-test job）、store 包 ≥60%（integration job）
- **前端最小测试集**：vitest + jsdom，32 个测试（auth 15 + api 17）

#### 前端企业版功能对齐
- **Vue3 企业版前端**：与原生 JS 个人版功能对齐，独立化静态资源（web/assets/*）

#### 性能优化
- **server.go 按路由域拆分**：2483 行 → ~1225 行，拆为 server_devices/server_tasks/server_alerts/server_audits/server_deploy
- **auth.go 按 handler 拆分**：拆为 auth_login/auth_users/auth_roles/auth_perms
- **sql.go 按领域拆分**：1261 行 → ~542 行，拆为 sql_devices/sql_tasks/sql_alerts/sql_audits/sql_tokens/sql_templates/sql_legacy

#### 安全加固
- **`--health` 子命令**：独立健康检查，GET /healthz → exit 0/1，供 docker-compose healthcheck（不依赖 curl/shell）
- **Refresh Token 持久化**：RefreshTokenStore 接口 + 三实现（Memory/SQL/MultiSchema），哈希存储 + 设备指纹绑定
- **Cookie Secure**：`--cookie-secure` flag，生产模式默认 true
- **请求体限流**：统一 1 MiB 上限（http.MaxBytesReader）
- **登录防爆破**：令牌桶限流 + 失败锁账号
- **metrics 访问控制**：`--metrics-allow-cidr` CIDR 白名单
- **联邦 mTLS + HMAC 签名**：`--federation-*` 通道硬化

#### 修复
- **CI `secrets` 上下文 bug**：改用 env 中转 + guard step，修复全部 28 次 CI 运行失败
- **删除 `opsmesh.exe`**：69MB 二进制误提交，已删除并加入 .gitignore

#### 变更
- **Go 版本统一 1.26**：go.mod / Dockerfile / README 一致
- **DELIVERY.md 数据刷新**：行数/包数/依赖数/功能矩阵更新至最新

#### 验证
- `go build ./...` ✅ 0 错误
- `go vet ./...` ✅ 0 警告
- `go test -timeout 180s ./...` ✅ 全部通过
- `npx vitest run` ✅ 32/32 通过

---

## [0.1.0] — 2026-08-01 ~ 2026-08-06

### 初始版本


#### 核心功能
- 控制面 + Agent 双模式架构（HTTP REST + gRPC）
- 设备纳管 / 任务执行 / 告警监控 / 审计日志
- 用户中心 + RBAC + JWT 认证
- OS 基础优化（14+ 预置模板）+ 中间件自动化部署（15+ 模板）
- K8s 多集群管理（client-go 集成）
- 多租户 schema 隔离 + 控制面联邦
- SSE 实时推送 + ELK/Loki 日志集成
- Helm Chart + docker-compose + systemd 部署
- Vue3 企业版前端 + 原生 JS 个人版前端
- K8s Operator（CRD + controller-runtime）
- protobuf 契约 + buf breaking 检查