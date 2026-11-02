# 裁决单 #4（2026-11-01）：服务面（services/*）租户闸 fail-open 与内网暴露

> 性质：**只读取证 + 决策材料**，本线不改 `services/*` 任何代码。
> 取证者：机动线 workbuddy（用户选定「服务面租户闸 fail-open 取证」）。
> 复核办法：文末「复现命令」每条都能在本机一条命令重跑。

---

## 一、结论先行

1. **服务面 HTTP 的租户作用域主要由客户端自报值决定，不是由已验签身份决定。**
   12 处 transport 层租户来源里，`device-svc` 有 6 处直接取 `?tenantID=`，`task-svc` 有 2 处「中间件解析结果是 `default` 时改用 `?tenantID=`」，`gpu-svc`/`portal-svc` 各 1 处直接信 `X-Tenant-ID` 头。
2. **`tenant.Middleware` 是宽松模式：解不出租户就兜底成 `default`，从不拒绝。**
   同包已写好严格版 `tenant.RequireTenant`（无租户 ⇒ 403），但**全仓零调用点**（除 `pkg/tenant` 自身定义）——与本周 aiops 的 `SetAuthorize`、`docs/semantic-rulings-2026-10-30.md` 里「假字段做成真能力」是同一族：能力写好了，没人接。
3. **13 个 `services/*` 目录里，有 HTTP cmd 的是 12 个（第 13 个 `tf-provider` 不是 HTTP 服务）；这 12 个里只有 3 个挂了中间件（auth-svc / device-svc / task-svc），其余 9 个 HTTP 面完全没有身份中间件**（aio / alert / autoscaler / config / gpu / incident / log / portal / runbook）。
4. **最尖锐的一条不是「没挂中间件」，而是「挂了也不消费」**：`services/device-svc/internal/http/gateway.go` 全文没有一次 `tenant.TenantIDFromContext`，:75 只是注释里写「鉴权走 tenant.Middleware」——中间件解出的租户在该文件的所有端点上被忽略，作用域一律由 query 参数决定。**注释与实现相反，测试不会红。**
5. **内网没有隔离**：Helm 的 `observability.networkPolicy.enabled` 默认 `false`，`values-production.yaml` **没有覆盖它**，且模板只写控制面与 agent 两种 pod（微服务 pod 一条策略都没有）。⇒ 生产安装下，微服务 ClusterIP 端口对集群内任意 pod 开放。
6. **集群外暴露是收敛的，但「宿主机/集群内」这两个面没收敛**：Ingress 只路由 `*-controlplane:8080`（`deploy/helm/opsmesh/templates/ingress.yaml:30`）；`docker-compose.prod.yml` 共 **21 条端口映射，除控制面（`${CONTROLPLANE_HTTP_PORT:-8080}:8080`，绑全网卡）外，12 个微服务（8100-8111）与 MySQL/Redis/观测组件全部绑 `127.0.0.1`**（如 `- "127.0.0.1:${DEVICE_SVC_HTTP_PORT:-8101}:8101"`）——这是有意的加固（同文件 :80 注释「仅本机可达」），但代价是**同一宿主机上的任意进程都能用自报租户直连这些服务**。
7. **经由控制面的那条路是对的**（别把本单读成"服务面全线裸奔"）：`internal/controlplane/service_proxy.go:103-110` 在转发前**剥离**客户端的 `X-Tenant-ID`/`X-User-Id`/`X-User-Roles`/`Cookie`，再用已校验身份**重注入**，代理规则本身还带按方法分级的 `PermRules`（`internal/controlplane/svcproxy/proxy.go`）。所以缺口集中在**绕过代理直连**（pod 网络 / compose 宿主机 loopback / agent bootstrap 路径）这一侧。⇒ 这不是公网漏洞，而是**纵深缺失 + 内网即信任**。

**建议裁决**：按零信任（pod 即潜在攻击者，多租户 SaaS 模型）判 **P0-内网**；按「集群内网络可信」判 **P1**。两种口径的修法不同（P0 要 transport 强制身份 + 中间件消费 + NetworkPolicy；P1 只要把自报租户降级为「仅在无身份时生效」并给微服务补入站策略）。**这一句要 owner 拍，不是我能替定的。**

---

## 二、事实（逐条 file:line，均为本机现查）

### 2.1 注册面（实测，非估算）
- `services/**` 非测试 Go 文件里 `HandleFunc("...")` 共 **155 条注册，分布在 22 个文件**。
  注册数 Top：`aio-svc/cmd/aio-svc/main.go` 27、`portal-svc/internal/handler/handler.go` 16、`gpu-svc/internal/handler/handler.go` 15、`config-svc/cmd/config-svc/main.go` 13、`auth-svc/internal/http/gateway.go` 11、`autoscaler-svc/internal/handler/handler.go` 10、`incident-svc/internal/handler/handler.go` 8、`device-svc/cmd/device-svc/main.go` 7、`runbook-svc/internal/handler/handler.go` 7、`log-svc/cmd/log-svc/main.go` 5、`task-svc/internal/http/gateway_approval.go` 5、`alert-svc/internal/server/escalation_http.go` 4、`task-svc/internal/http/gateway.go` 4。
- **更正留痕**：本线 CLAIM（`docs/CLAIMS.md` 2026-11-01T18:41:00Z）写的「87 条」是 `services/*/{internal,pkg}/**/*.go` 这个 glob 漏了 `cmd/` 与更深层目录所得的低估值，**以 155 为准**；旧行不回改，纠正写在这里。

### 2.2 身份中间件挂没挂
| 服务 | HTTP 面是否挂身份中间件 | 证据 |
|---|---|---|
| auth-svc | 挂（宽松） | `services/auth-svc/cmd/auth-svc/main.go:200` |
| device-svc | 挂（宽松），**但 handler 不消费** | `services/device-svc/cmd/device-svc/main.go:234`；`services/device-svc/internal/http/gateway.go`（全文无 `TenantIDFromContext`） |
| task-svc | 挂（宽松），handler 消费但可被 query 覆盖 | `services/task-svc/cmd/task-svc/main.go:232`；`services/task-svc/internal/http/gateway.go:94,166,394` |
| aio / alert / autoscaler / config / gpu / incident / log / portal / runbook | **未挂** | 逐 cmd `main.go` 无 `tenant.Middleware`/`RequireTenant`/`authMiddleware`（alert-svc 只有 SecurityHeaders/compress/trace：`services/alert-svc/cmd/alert-svc/main.go:168-176`） |

- 严格版存在但无人用：`pkg/tenant/tenant.go:350-361`（`RequireTenant` ⇒ 无租户 403）对比 `pkg/tenant/tenant.go:331-346`（`Middleware` ⇒ 无租户兜底 `default`，注释自陈「宽松模式」）。

### 2.3 自报租户的 12 处来源（transport 层）
- `services/device-svc/cmd/device-svc/main.go:195`
- `services/device-svc/internal/http/gateway.go:111, 242, 289, 394, 490, 611`
- `services/task-svc/internal/http/gateway.go:99, 166, 394`
- `services/gpu-svc/internal/handler/handler.go:477`
- `services/portal-svc/internal/handler/handler.go:736`

其中「不传即不校验」的判定形状：`services/device-svc/internal/http/gateway.go:243`、`:290` —— `if tenantID != "" && dev.TenantID != tenantID { 403 }`。
即：**带 `?tenantID=` 才检查归属，省略该参数就跳过归属检查**（同一函数里 `dev` 是路径参数指定的任意设备）。

### 2.4 store/engine 层的 `if tenantID != "" &&` 共 44 处
抽样：`services/device-svc/internal/store/store.go:153,293,372,406,419,459,508`、`services/config-svc/internal/store/store.go:151,320,420,432,494,506`、`services/alert-svc/internal/{engine/engine.go:162,engine/evaluate.go:128,escalation/escalation.go:160,218,store/store.go:80,189}`、`services/log-svc/internal/store/store.go:78,119`、`services/log-svc/pkg/logstore/logstore.go:161`、`services/portal-svc/internal/store/store.go:98`。
**这一层多数是合法的「可选过滤」**（列表接口允许不传租户＝平台内全量），风险不在这里，而在 **2.3 那 12 处 transport 把空值直接喂下来**——所以下游 44 处的存在只说明"空租户一路通畅"，不是 44 个独立缺陷。**这条区分很重要，别按 44 报数量。**

### 2.5 部署面
- `deploy/helm/opsmesh/values.yaml:868-869` `networkPolicy: enabled: false`；`values-production.yaml` 无该键覆盖（现查 `grep networkPolicy values-production.yaml` 零命中）。
- `deploy/helm/opsmesh/templates/network-policy.yaml`：模板注释自称「默认拒绝所有入站」，实际只有 2 个策略对象——controlplane（ingress 8080/9090）与 agent（仅 egress），**微服务 pod 无策略**；且整份文件被 `{{- if .Values.observability.networkPolicy.enabled -}}` 包住，默认关闭 ⇒ 生产安装下一条策略都不下发。
- `deploy/helm/opsmesh/templates/ingress.yaml:30` 只把 `*-controlplane:8080` 挂进 Ingress；`deploy/k8s/deployments/*-deployment.yaml` 里微服务 Service 均 `type: ClusterIP`。
- `deploy/docker/docker-compose.prod.yml`：**21 条端口映射**，控制面绑全网卡 `${CONTROLPLANE_HTTP_PORT:-8080}:8080`，12 个微服务（8100–8111）与 MySQL/Redis/Grafana/Prometheus/Loki/Jaeger/OTel/Alertmanager 一律 `127.0.0.1:` 前缀（同文件多处注释「仅本机可达」）；`docker-compose.sim.yml`/`nullfix.yml` 无 `ports:`，`prod-proxy.yml` 有 2 处（代理与入口）。⇒ **宿主机本地进程是可达面**，不是零。
- 控制面代理侧的身份治理**已在代码里、不只是注释**：`internal/controlplane/service_proxy.go:103-110` 转发前 `Header.Del("X-Tenant-ID"/"X-User-Id"/"X-User-Roles"/"Cookie")`，再按已校验身份 `Header.Set(...)` 重注入；`internal/controlplane/svcproxy/proxy.go` 的规则表按方法分级（如 device 域 GET=`device:read`、DELETE=`device:delete`、POST `/provision`=`provision:execute`）。

---

## 三、方案（改动点 / 爆炸半径 / 测试面）

### 方案 A｜transport 一律以已验签租户为准，禁止自报值参与作用域判定
- 改动点：2.3 的 12 处；`device-svc` 6 处改为从 `tenant.TenantIDFromContext(r.Context())` 取（并真正消费中间件结果），`task-svc` 去掉 `:166/:394` 的「default 时被 query 覆盖」分支，`gpu-svc`/`portal-svc` 的信 header 改为信中间件解出的 ctx。
- 爆炸半径：任何**直连 pod** 且依赖 query/header 传租户的既有调用方会开始 403 或落到 default——含 device-sim 调试、其它服务互调、以及运维手工 curl。控制面代理路径（svcproxy 已注入身份头）不受影响。
- 测试面：`services/device-svc/internal/http` 与 `services/task-svc/internal/http` 的现有 httptest 需要补"带身份"夹具（我这边可同批交付）；各服务 cmd 的 main_test 需断言中间件确实在链上。

### 方案 B｜把宽松 `Middleware` 换成严格 `RequireTenant`（并给剩下 9 个服务挂上）
- 改动点：9 个 cmd 的 handler 链 + 3 处替换；`pkg/tenant` 无需改。
- 爆炸半径：**最容易把现网打死**——若某个内部调用方（agent 上报、服务互调、E2E 桩）本来不带租户身份，升级后立刻全量 403。需要先把调用方清点干净，或提供显式的「集群内服务间凭证」（HMAC/mTLS）作为过渡。
- 测试面：每服务加一条「无身份 ⇒ 403」用例 + 一条「带合法身份 ⇒ 200」用例（与我在控制面用的 401/403 双拍一致）。

### 方案 C｜NetworkPolicy 覆盖微服务，并把默认值在生产口径打开
- 改动点：`network-policy.yaml` 补一组微服务 pod 的 ingress（只允许 controlplane pod + 同命名空间需要的端口）；`values-production.yaml` 显式 `enabled: true`；模板注释「默认拒绝所有入站」要改成如实描述（当前只对 controlplane/agent 成立）。
- 爆炸半径：**不改代码、不回滚风险最低**，但依赖 CNI 支持——Calico/Cilium 有效，部分托管集群（含 kind/flannel）静默不生效；compose 部署不受影响（本就没发布端口）。所以它只能是纵深，不能当主防线。
- 测试面：`deploy/scripts/validate-deploy-assets.sh` 加一条「Chart 渲染必须含微服务 NetworkPolicy」的结构断言；kind 集群实测一次（我这边有 `kind-local-staging` 的坑清单可复用）。

### 方案 D（本线建议）｜B′ 分两步 + C 作纵深
1. 先做**不改变可达性的收口**：A（自报租户降级为"仅无身份时生效"）+ 把 `device-svc` 的注释与实现对齐（现在注释承诺了一个不存在的消费）；
2. 再单独评 B（严格中间件），配一份"谁在直连微服务"的实测清单（`kubectl` 连接审计或临时日志）后再切；
3. C 作为纵深与合规项同批做，但文档明确"不作为主防线"。
理由：A/C 的失败模式是"有人 403 了"（可见、可回滚），B 的失败模式是"整片服务间调用静默断"（难定位），顺序不该反过来。

---

## 四、需要 owner 回答的问题（答不上来就别先动代码）

1. **威胁模型**：OpsMesh 是否把「集群内任意 pod」视为潜在攻击者？（决定 P0-内网 vs P1）
2. **谁在直连微服务**：除控制面 svcproxy 外，是否有 agent/第三方/服务互调直接打这些 HTTP 端口？（决定 B 能不能切严格模式）
3. **`?tenantID=` 是不是公开契约**：`docs/api-reference.md` 里有 **56 处 `tenantID`**（现查），说明这个自报参数**已经被写成文档契约**——A 方案对这些调用方是破坏性变更，需要版本策略与迁移说明。（对比：控制面侧本批改动的 `/api/v1/ai|knowledge|cost|aiops|flux` 端点在该文档里是 0 命中，属"从未被文档承诺"，两边口径不同，别混。）
4. **多租户是否真在跑**：单租户部署（大量用户实际是 default）下，本单的实际风险等级会降一档——需要真实安装画像。
5. **gRPC 面同批还是另批**：`TD-50`（alert-svc 未鉴权 gRPC 出厂默认）与本单同源不同路径，是否合并成一个"服务面身份统一"专项？

---

## 五、未核清单（别把我没说的当成已确认）

- 155 条注册**没有逐条**判定"是否消费 ctx 租户"——我只对齐了 12 处租户来源与 3 个挂了中间件的服务。逐端点矩阵（含写/删/执行类动作的归属校验有无）是下一步。
- `services/*` 的 **gRPC** 拦截器现状未审（`pkg/grpcwire`、alert-svc 的 TD-50 除外）。
- 未验证"控制面 svcproxy 是否覆盖了所有 155 条"——若某条端点既不在代理规则里、又无身份中间件，它就只能被内网直连访问，那部分要单列。
- compose/sim 场景下的实际可达性（容器网络内跨服务）未做运行时验证。
- 9 个"未挂中间件"的判定基于 cmd main 的 grep；若某服务在库内部自包中间件（我未在 handler 文件里看到 auth 符号，但只查了同文件符号表），需按文件逐个确认。

---

## 六、复现命令

```bash
# 155 条注册 / 22 文件
grep -rn 'HandleFunc(' services --include=*.go | grep -v _test | wc -l
# 中间件挂载点（应为 3）
grep -rn 'tenant.Middleware(' --include=*.go . | grep -v _test
# 严格版是否有调用点（应为 0，除定义）
grep -rn 'RequireTenant\b' --include=*.go . | grep -v 'pkg/tenant/'
# device-svc 是否消费 ctx 租户（应为 0 命中）
grep -n 'TenantIDFromContext' services/device-svc/internal/http/gateway.go
# NetworkPolicy 默认与覆盖
grep -n 'networkPolicy' -A2 deploy/helm/opsmesh/values.yaml
grep -n 'networkPolicy' deploy/helm/opsmesh/values-production.yaml
# compose 的宿主端口映射（21 条，除控制面外均绑 127.0.0.1）
awk '/ports:/{f=1;next} f&&/^ *- /{print;f=0} f&&/^ *#/{next} f&&!/^ *- /{f=0}' deploy/docker/docker-compose.prod.yml | grep -c 127.0.0.1
# 代理侧身份剥离与重注入（应为真代码而非注释）
grep -n 'Header.Del("X-Tenant-ID")' internal/controlplane/service_proxy.go
```

---

## 附一：NetworkPolicy 在真实 CNI 上的回归矩阵（2026-11-02 追加，kindnet 实测）

> 本节是对 §2.5（事实 5：内网没有隔离）的后续验证：模板修改后**实跑**，不是纸面推断。
> 环境：kind v1.32.2（节点 v1.32.2），CNI=kindnet（默认）。命名空间：`opsmesh`（被测）/ `monitoring`（外部 Prometheus 形态）/ `ctrl`（策略强制力正反对照）。
> 端口：chart 默认 `httpPort=8081`、`grpcPort=50052`（注意与 compose 的 8101/8102 不同，探针按 chart 默认）。

### 强制力对照（先证明 kindnet 真的执行策略，避免"全绿=没生效"）

| 场景 | 结果 |
|---|---|
| `ctrl` ns 有 deny-all 时 c2→c1:9000 | 超时（BLOCKED） |
| 删除 deny-all 后同一探测 | 连通（OPEN） |
| 重新贴回 | 再次超时（BLOCKED） |

### 状态 A：`metricsFrom` 为空（chart 默认，不放行任何抓取来源）

| 路径 | 实测 | 期望 |
|---|---|---|
| cp→device:8081 / :50052 | OPEN / OPEN | OPEN（svcproxy 转发 HTTP+gRPC） |
| cp→task:8081 / :50052 | OPEN / OPEN | OPEN |
| agent→device:8081 | OPEN | OPEN（自举） |
| agent→device:50052 | BLOCKED | BLOCKED（自举只走 HTTP） |
| intruder(同 ns)→device:8081 / :50052 / task:8081 | BLOCKED ×3 | BLOCKED |
| task(同 ns)→device:8081 | BLOCKED | BLOCKED（无服务互调放行） |
| prom(monitoring ns)→device/task:8081 | BLOCKED / BLOCKED | BLOCKED（未声明抓取来源） |
| device→cp:8080、device→mysql:3306 | OPEN / OPEN | OPEN |

本态 21 条探测全部与期望一致。判据：busybox `nc -z -w 3 <ip> <port>` 退出码 0=OPEN、非 0=BLOCKED；探针监听用 `nc -lk -p PORT -e /bin/cat`（busybox 不支持 `nc -c`，早期的 `-c` 写法会让监听静默退出、把"没人监听"误判成 BLOCKED）。

### 状态 B：`metricsFrom = monitoring/prometheus`（显式放行抓取来源）

| 路径 | 实测 |
|---|---|
| prom→device:8081 / task:8081 | OPEN / OPEN（显式配置的效果） |
| intruder(同 ns)→device:8081 | BLOCKED（peer 不匹配仍拦） |
| cp→device:8081、agent→device:8081 | OPEN / OPEN（不受影响） |

### 翻生产默认 true 的实测阻断项

控制面进程需要出向外部 HTTPS（LLM 网关 `internal/ai/gateway/providers.go`、告警 webhook `internal/controlplane/webhook.go`、federation 对端 `internal/controlplane/federation.go`），而策略 egress 只放行 DNS/MySQL/Redis/自家服务：

| 源 | 目标 | 实测 |
|---|---|---|
| intruder（未被策略选中，基线） | api.kilo.ai:443 | OPEN |
| cp（被策略选中） | api.kilo.ai:443 | BLOCKED |
| cp / intruder | 10.96.0.1:443（集群 API Service） | OPEN / OPEN（本 kind 形态下未被拦；**不作为任何保证**） |

⇒ `values-production.yaml` 保持 `enabled: false`；翻 true 的前置＝先加显式可配置的外部出向放行（见该文件注释与门禁 `TestNetworkPolicyProductionFlipRequiresExternalEgress`）。

### 复现（关键步骤）

```bash
# 1) kind 单节点集群 + 三个 ns；探针 pod 标签必须与 chart 选择器一致：
#    app.kubernetes.io/{name: opsmesh, instance: <release>, component: <controlplane|agent|device-svc|task-svc|...>}
kind create cluster --name np-wb
kubectl create ns opsmesh && kubectl create ns monitoring && kubectl create ns ctrl
# 探针容器的监听命令（busybox）：nc -lk -p 8081 -e /bin/cat

# 2) 渲染并只贴策略；-n 不能省，否则策略落到 default ns，opmesh 里的探测会"全通"
helm template t deploy/helm/opsmesh -n opsmesh -s templates/network-policy.yaml \
  --set observability.networkPolicy.enabled=true \
  --set services.device_svc.enabled=true --set services.task_svc.enabled=true > np.yaml
kubectl delete netpol -n opsmesh --all
kubectl apply -n opsmesh -f np.yaml
kubectl get netpol -n opsmesh   # 应 4 条

# 3) 判据：退出码 0=OPEN，非 0=BLOCKED
kubectl exec -n opsmesh cp -- nc -z -w 3 <devicePodIP> 8081

# 4) 第二态：加 metricsFrom 覆盖文件后重渲染、重贴、复测 prom→svc
```

## 附二：翻生产 true 之后的复测（2026-11-02 追加，kindnet）

> 附一的结论是"策略生效时控制面外部 HTTPS 被拦，故不能翻 true"。本附二是补上
> `observability.networkPolicy.controlplaneEgressExtra` 之后的复测：生产 values 已 `enabled: true`。
> 环境同附一（kind v1.32.2 / kindnet）；渲染命令：

```bash
helm template t deploy/helm/opsmesh -n opsmesh -s templates/network-policy.yaml \
  -f deploy/helm/opsmesh/values-production.yaml \
  --set services.device_svc.enabled=true --set services.task_svc.enabled=true
```

渲染 4 条策略（controlplane / agent / device-svc / task-svc）；策略正文里没有空白 `podSelector`
（对渲染文本 `grep -c 'podSelector: {}'` 会命中 1 处，那是说明"旧写法已删"的注释行）。

### 外部出向（`controlplaneEgressExtra`：`0.0.0.0/0` except RFC1918，仅 443/TCP）

| 源 | 目标 | 实测 | 判读 |
|---|---|---|---|
| intruder（未被策略选中，基线） | api.kilo.ai:443 | OPEN | 外部可达性成立 |
| cp（被选中且声明了出向） | api.kilo.ai:443 | OPEN | 由附一的 BLOCKED 变为连通 |
| intruder（基线） | mysql:443（Pod IP 10.244.x） | OPEN | 监听可用、mysql 无策略 |
| cp | mysql:443（Pod IP 10.244.x） | BLOCKED | `except 10.0.0.0/8` 生效 |

### 内部矩阵（与附一一致，未回退）

| 路径 | 实测 |
|---|---|
| cp→device:8081 / :50052、cp→task:8081 | OPEN / OPEN / OPEN |
| agent→device:8081 | OPEN |
| agent→device:50052 | BLOCKED |
| cp→mysql:3306、device→cp:8080、device→mysql:3306 | OPEN / OPEN / OPEN |
| intruder(同 ns)→device:8081、prom(monitoring ns)→device:8081 | BLOCKED / BLOCKED |
| device→mysql:443 | BLOCKED（微服务 egress 只开 3306/6379） |

**结论**：生产 values 保持 `enabled: true` 有实测支撑；安装方仍需按实际环境核对两类例外——
私网 IP 或非 443 端点的外部依赖要自行补 `controlplaneEgressExtra` 规则；微服务 `/metrics`
抓取要显式配置 `metricsFrom`（默认空=不放行）。
