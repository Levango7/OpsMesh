# Changelog

本文件记录 OpsMesh 所有重要变更。格式参考 [Keep a Changelog](https://keepachangelog.com/)。

## [Unreleased] — 2026-08-08

### P0：严重问题修复（5 项，阻塞生产发布）

#### 修复
- **`web/enterprise/src/api/auth.js`**：添加缺失的 logout API 方法（清除前端 token + 调用后端 logout 端点）
- **README.md + docs/deployment-guide.md**：补充企业版前端独立部署说明（npm run build → 静态文件部署到 Nginx/CDN）
- **`deploy/helm/opsmesh/templates/agent-daemonset.yaml`**：agent 数据卷从 emptyDir 改为 hostPath（重启不丢失 agent.id）
- **`deploy/helm/opsmesh/values.yaml` + `values-production.yaml`**：镜像 tag 从 "0.1" 修正为 "latest"（与 CI 推送的 :sha tag 一致）
- **`internal/store/sql_templates.go`**：移除 UTF-8 BOM（导致 MySQL 首字节乱码）

### P1：重要问题修复（10 项，影响质量）

#### i18n 全面覆盖
- **12 个 Vue 组件**：TasksView/AlertsView/DevicesView/DeviceDetailView/CMDBView/WorkflowsView/DeploysView/LogsView/UsersView/K8sManageView/MiddlewareDeployView/OSOptimizeView — 所有硬编码中文提取到 i18n（zh.json/en.json 从 459 键扩展到 629 键，结构完全对称）
- **DataTable/Pagination 组件**：空状态文本、翻页按钮 i18n 化
- **MiddlewareDeployView**：fmtTime 从硬编码 'zh-CN' 改为动态 locale（随 i18n 语言切换）

#### 路由守卫 + i18n 回退
- **auth.js**：添加 ready Promise + initialized flag，解决路由守卫竞态条件
- **router/index.js**：守卫改为 async，await auth.ready
- **main.js + App.vue**：启动时调用 fetchMe，移除重复调用
- **i18n/index.js**：添加 FALLBACK_LANG='zh' 回退机制（缺失键回退到中文）

#### API 文档修正
- **docs/api-reference.md**：修正 4 处不一致（/readyz 端点缺失、/metrics 端口、/healthz 格式、/api/v1/me 字段名）

#### 测试补全
- **前端测试**：新增 vitest + @vue/test-utils + jsdom，88 个测试（auth 28 + i18n 22 + DataTable 20 + Pagination 18）
- **internal/k8s 测试**：覆盖率 14.8% → 90.9%（30 个测试，Clientset 字段改为 Interface 接口支持 fake 注入）
- **internal/orchestration 测试**：覆盖率 30.1% → 71.2%（17 个新测试）
- **cmd/opsmesh 测试**：覆盖率 0% → 73.9%（20 个测试，重构提取 runMain()/versionString()）

#### 可观测性
- **Helm ServiceMonitor + PrometheusRule**：新增两个监控模板 + 3 条告警规则（agent 离线 / 任务失败率高 / 队列堆积）
- **/healthz 深度检查 + /readyz**：healthz 增加 store ping 深度检查，新增 /readyz 就绪探针端点
- **metrics 指标扩充**：HTTP 延迟直方图 + HTTP 计数器 + Go runtime 指标（零依赖手写，不引入 prometheus 客户端库）

### P2：次要问题修复（10 项，技术债务清理）

#### 代码质量
- **flow.js 拆分**：2714 行 / 106 导出的单体 JS 文件按业务域拆分为 13 个模块 + barrel re-export（零风险，main.js 无需修改）
- **app.legacy.js 删除**：64.8KB 完全未引用死代码清除（零引用确认后删除）

#### 部署加固
- **operator/Dockerfile**：Go 版本从 1.22 对齐到 1.26（与主 Dockerfile/go.mod 一致）
- **systemd hardening**：两个 service 文件添加 19 条安全指令（NoNewPrivileges/ProtectSystem/ProtectHome/PrivateTmp/ProtectKernelTunables/ProtectKernelModules/ProtectControlGroups/RestrictAddressFamilies/RestrictNamespaces/SystemCallFilter 等）
- **Helm Ingress + HPA 模板**：新增 templates/ingress.yaml 和 templates/hpa.yaml，对接 values.yaml 中已有的 ingress 和 autoscaling 配置段（生产环境 HPA 默认开启：minReplicas=2, maxReplicas=10）

#### 供应链安全
- **cosign 镜像签名**：CI image job 新增条件性 cosign sign 步骤（需 COSIGN_PRIVATE_KEY secret，未配置时自动跳过）
- **Base image digest pinning**：3 个 Dockerfile 添加 digest pinning 最佳实践注释 + 新建 docs/image-pinning.md 指南

#### 测试补全
- **internal/tlsutil 测试**：覆盖率 0% → 91.7%（17 个测试，覆盖 ServerCreds/ClientCreds/HTTPClientTLSConfig/HTTPServerTLSConfig 全部 4 个导出函数）

#### 文档
- **README 架构图**：更新 ASCII 架构图，新增企业版 Vue3 前端、K8s Operator、联邦 mTLS、SSE 实时推送、多租户 Schema 隔离、ELK/Loki 日志集成、Prometheus 监控告警等；通信模型表格补充 SSE/联邦/Metrics 三行

#### 验证
- `go build ./...` ✅
- `go vet ./...` ✅
- `go test -timeout 300s ./...` ✅
- `npm run build` ✅
- `npx vitest run` ✅ 88/88 通过

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