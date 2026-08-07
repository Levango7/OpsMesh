# Changelog

本文件记录 OpsMesh 所有重要变更。格式参考 [Keep a Changelog](https://keepachangelog.com/)。

## [Unreleased] — 2026-08-07

### 评估报告 10 问题修复 + CI 修复

#### 新增
- **`--health` 子命令**（`cmd/opsmesh/main.go`）：独立健康检查，GET `/healthz` → exit 0/1，供 docker-compose healthcheck 使用（不依赖 curl/shell）
- **Refresh Token 持久化存储接口**（`internal/store/store.go`）：`RefreshTokenStore` 接口 + `RefreshToken` 模型，MemoryStore / SQLStore / MultiSchemaStore 三实现
- **Cookie Secure + 设备指纹绑定**（`internal/controlplane/auth.go`）：refresh token 哈希存储、设备指纹 IP+UA 绑定、`--cookie-secure` flag（Production 模式默认 true）
- **前端最小测试集**（`internal/controlplane/web/tests/`）：vitest + jsdom，32 个测试（auth 15 + api 17）
- **CI 覆盖率门禁**（`.github/workflows/ci.yml`）：整体 ≥40%（build-test job）、store 包 ≥60%（integration job）

#### 变更
- **Go 版本统一 1.26**：go.mod `1.26.0`、Dockerfile/Dockerfile.agent `golang:1.26`、README `Go 1.26+`
- **server.go 按路由域拆分**：2483 行 → ~1225 行，拆为 `server_devices.go` / `server_tasks.go` / `server_alerts.go` / `server_audits.go` / `server_deploy.go`
- **auth.go 按 handler 拆分**：拆为 `auth_login.go` / `auth_users.go` / `auth_roles.go` / `auth_perms.go`
- **sql.go 按领域拆分**：1261 行 → ~542 行，拆为 `sql_devices.go` / `sql_tasks.go` / `sql_alerts.go` / `sql_audits.go` / `sql_tokens.go` / `sql_templates.go` / `sql_legacy.go`
- **DELIVERY.md 数据刷新**：行数 / 包数 / 依赖数 / 功能矩阵更新至最新

#### 修复
- **CI `secrets` 上下文 bug**：`secrets` 不能在 `if` 条件中引用（GitHub Actions 限制），改用 `env` 中转 + guard step，修复全部 28 次 CI 运行失败
- **删除 `opsmesh.exe`**：69MB 二进制误提交，已从仓库删除并加入 .gitignore

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