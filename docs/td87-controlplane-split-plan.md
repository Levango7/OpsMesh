# TD-87 立项：`internal/controlplane` 父包拆包（2026-10-09 侦察与方案）

> 来源：TD-61 收口时拆分行（原行名「controlplane/store 父包拆分」，store 侧已三批闭合：
> `internal/store/` 顶层 113 → 20 文件，见 `docs/td61-store-split-plan.md`）。本文件是 controlplane 侧的立项材料。
> 状态：**仅侦察 + 方案，未动工**。用户 2026-10-09 确认「暂时只有一个会话在做」，故本文件不再需要「与在制线对齐」这一前置。

## 1. 硬数据（2026-10-09 实测）

| 指标 | 数值 |
|---|---|
| `internal/controlplane/` 顶层 `.go` | **182**（prod **89** + test **93**） |
| prod 行数 | **27,056** |
| **`*Server` 上的方法** | **498** |
| 其它类型上的方法 | 66 |
| 包级函数 | 159 |
| 已抽子包 | `backup` / `embed` / `factory` / `grpc` / `paginate` / `registry`（先例：内聚件可抽） |

**主题分布（prod 文件前缀，前列）**：`server*` 36、`auth*` 12、`k8s*` 9、`os*` 7、`metrics*` 7、`middleware*` 6、
`tenant*` 5、`service*` 5、`cmdb*` 5、`plugin*` 4 …（共 40+ 个域前缀）。

## 2. 形态分析：为什么**不能**照搬 store 的配方

store 三批能走「`git mv` + 包声明 + `aliases.go` 别名回导层 + 父包薄门面」，前提是**按后端切**且每个后端的
实现是「一个中心类型的方法集 + 可被别名回导的领域类型」（见 td61 方案 §5 的形态 A/B 修正）。

controlplane 的结构不同，且有两条硬约束：

1. **一个中心类型、498 个方法**：29 个域的 handler 全是 `func (s *Server) ...`，方法体普遍直读 `s.cfg` /
   `s.store` / `s.bus` / `s.metrics` / 各域私有字段。**方法过不了类型别名**（store 批已实测：方法随类型走），
   所以「按域搬文件」在同一个 `Server` 类型下不成立。
2. **测试面与实现同包**：93 个 test 文件大量使用未导出字段/未导出 helper（`srv := newTestServer(...)` 之类），
   与实现同包才能编译——拆包必须连同测试一起迁，而测试又反向依赖 `Server` 的内部状态。

⇒ 结论：controlplane 的拆包只有两条路——
**(a) 平移低耦合件**（不与 `Server` 的字段/方法耦合的文件），**(b) 组合式重构**（把域状态收进子结构体，
`Server` 组合它们，再把 `*Server` 方法改写为子结构体方法）。**(b) 才是「破 498 方法」的唯一路径**，但它是一次
真正的架构重构，不是机械搬迁。

## 3. 可平移面的实测清单

### 3.1 零 `Server` 耦合（`srv==0 && s.==0`）：7 件 / **1,717 行**

| 文件 | 行 | 包级函数 | 包内被引（次数/文件数） |
|---|---|---|---|
| `middleware_template_presets.go` | 436 | 0（纯数据表） | 10 / 2 |
| `os_template_presets_ext.go` | 387 | 0（纯数据表） | 3 / 2 |
| `os_template_presets.go` | 245 | 0（纯数据表） | 3 / 2 |
| `plugin_remote.go` | 233 | 5 | 11 / 2 |
| `auth_password.go` | 226 | 8 | 42 / 5 |
| `os_template_validate.go` | 122 | 8 | 57 / 6 |
| `metrics_cache.go` | 68 | 0 | 25 / 3 |

### 3.2 低耦合（`*Server` 方法 ≤2 且 `s.` 引用 ≤5）：13 件 / **约 2,400 行**

`service_proxy.go`(703/1 方法/11 包级函数)、`cmdb_collector.go`、`enterprise_ui.go`、`server_security.go`、
`auth_guard.go`、`dashboard.go`、`device_metrics.go`、`os_template_store.go`、`tenant_guard.go`、
`middleware_deploy.go`、`plugin_host.go`、`server_audits.go`、`auth_perms.go`。
（这批需要「保留少量方法的薄壳」或把那一两个方法先改写成「取 `s.xxx` 后调用子包函数」的形态。）

### 3.3 不可平移：其余 ~69 件 / 约 23,000 行 —— 498 个 `*Server` 方法所在处

## 4. 建议的批次分解

- **批 1（低风险，机械）**：抽 `internal/controlplane/<name>/` 承载 §3.1 的 7 件（建议按性质分两个包：
  `presets`（三张模板预置表 + 校验）/ `secretutil`（`auth_password`）/ `remoteext`（`plugin_remote`）/
  `metricscache`——或统一一个 `helpers` 包，按最终命名再定）。父包对**导出名**留别名/薄包装，
  对未导出名改引用点（每个符号只涉及 2~6 个文件，改动面有界）。判据与 store 批同款：
  build/vet/`go test`（全控制面包 + 其依赖）/`golangci-lint`/`-race`/部署门禁。
- **批 2（低风险，少量改写）**：§3.2 的 13 件，先处理「1~2 个方法」的（把方法体收成
  「取字段 → 调子包函数」），再整件平移。预期父包 prod 文件 89 → 约 69。
- **批 3+（组合式重构，需单独决策）**：按域引入 `type tasksAPI struct{ srv *Server }` 之类的组合，
  把 498 个方法按域分组迁出。**本文件不建议立即启动**，理由见 §5。

## 5. 价值评估（诚实版）与「不做」的理由

- **批 1+2 的真实收益**：4,100 行 / 20 件离开父包，父包 prod 文件 89 → ~69、行数 27.0k → ~22.9k；
  **导航成本有实质改善但有限**（剩余 ~69 件仍是最大的单包）。风险低（store 三批已验证同款配方）。
- **批 3 的收益与代价**：能真正把「一个 498 方法的类型」拆开，但它是**架构重构**——每个域的 handler 要改
  接收者、测试要跟着改 `newTestServer` 的构造方式、`internal/controlplane` 的公共面（`Server` 及其方法）
  被大量外部引用（`cmd/`、测试、E2E）。**收益是组织性的、代价是全域回归面**，与 store 批「按后端切」的
  低耦合前提不同。
- **结论（建议）**：TD-87 以**批 1+2 收口**，批 3 列为「需明确诉求才启动」——它更像一次产品级的架构演进，
  不是债务清偿；在 27k 行单包里继续加域（并行线 2026-10-09 期间 180 → 182 文件）说明该包仍在演进期，
  先做低风险平移、把批 3 留给「域边界稳定后」更划算。
- **明确不做**：不为拆包而给 `Server` 加一层「域接口 + 注册表」的间接层——那是把 498 个方法的复杂度
  转移到 498 个跳转上，收益为负（store 批 §9.5 对「删 shim」的裁定同款思考）。

## 6. 动工前置（批 1 开跑前要核）

1. `cmd/` 与 E2E/前端黑盒是否引用 §3.1/§3.2 的符号（若引用，需要父包留导出别名）；
2. 与「引擎/模板预置表」相关的**契约测试**（模板渲染、validator）在拆包后是否仍覆盖同一路径；
3. 93 个 test 文件的 `newTestServer` 系列 helper 归属（测试 helper 按包各自持有的既有惯例，见
   `internal/store/multischema/test_helpers_test.go` 的先例）。

---

## 7. 批 1 执行记录（2026-10-09，已落地：7 件 / 4 个新包）

### 7.1 实际搬迁面（与 §4 批 1 预估的差异）

| 新包 | 迁入文件 | 行数（含测试） | 导出面 |
|---|---|---|---|
| `presets` | `middleware_template_presets.go`、`os_template_presets.go`、`os_template_presets_ext.go`、`os_template_validate.go`、`types.go`（新，装模板格式类型）、`shell_safe_test.go` | 1,307 | `MiddlewareTemplates` / `OSTemplatesCore` / `OSTemplatesExt` + 8 个校验/渲染函数 |
| `pluginhost` | `remote.go`（原 `plugin_remote.go`）、`remote_test.go` | 403 | `InitPluginHost` / `LoadPluginManifest` |
| `credentials` | `password.go`（原 `auth_password.go`）、`password_test.go` | 406 | 9 个口令/凭据函数 |
| `metricscache` | `cache.go`（原 `metrics_cache.go`）、`cache_test.go` | 184 | `AppCounts` / `AppCountsCache`（`Resolve`/`Invalidate`） |

**§4 预估被实测修正两处**：

1. **模板类型必须随迁**：`OSTemplate`/`OSParam`（原在 `os_optimize.go`）、`Middleware*` 三个（原在 `middleware_deploy.go`）
   是**控制面自己的运行时模板格式**（不是 store 持久化模型），平移数据/校验就必须连类型一起走。
   父包新增 `template_shim.go` 以**类型别名**回导这 5 个名字 ⇒ 父包内数十处结构体字面量/字段访问/
   与 store 模型的显式转换**零改动**（与 store 三批同款配方）。
2. **`metrics_cache.go` 是「零 Server 耦合」判定的假阳性**：它只在注释里提到 Server，真正结构是
   `appCounts` 值类型 + `appCountsCache`（自带方法）。判据修正：**判「零耦合」不能只 grep `*Server`/`s.`，
   还要看是否以参数形式接收 `*Server`、是否引用 `Server` 的任何非方法形态**。

### 7.2 测试侧的处置（三分法，判据同 store 批）

- **随包迁**：`shell_safe_test.go`（专测 `ValidateShellSafeValues`）、`metrics_cache_test.go` 的 5 个缓存用例、
  `pluginhost` 的 4 个 manifest/宿主用例、`auth_extra_test.go` 的 9 个 `TestEnforceInitialCredentials_*` + 助手。
- **留父包 + 限定引用**：`os_optimize_test.go`（60 个用例里多数是 Server handler）、`plugin_remote_test.go` 的
  Server 级用例与 `TestInitPluginHostDisabledIsNoOp`（用父包全局 `PluginManager()`——**测试不跨包 import 父包**，
  这是硬边界）、`auth_extra_test.go` 其余混合用例、`tenant_users_test.go`。
- **按包各持一份**：`assertPluginMetric`（父包与新包各一份，同 `recordingBus`/`countDevices` 的既有惯例）。

### 7.3 验证口径与结果（全绿）

- 全仓 `go build ./...` 绿；`go vet ./internal/controlplane/...` 零告警；
- `go test -count=1 ./internal/controlplane/...`：父包 44.9s + 8 个子包**全 ok / 0 FAIL**；
- `golangci-lint run ./internal/controlplane/...`：**0 issues**（顺带清掉父包 `changePasswordMinLen`
  随函数迁走后变成的 unused 常量）；`gofmt -l` 净；
- `-race -count=1 ./internal/controlplane/...`：**零 DATA RACE**（父包 290.6s / credentials 148.4s / grpc 80.2s / backup 58.1s，全部 ok）；
- 部署资产门禁：PASS=76 FAIL=0 SKIP=1（初跑即过，未改部署面）。

### 7.4 收口数据（可复核）

- 父包顶层 `.go`：**182 → 175**（prod 89 → 83，test 93 → 92）；父包 prod 行 **27,056 → 25,221**。
- 四个新包合计 2,300 行（含其测试）。
- 剩余（§4 批 2）：§3.2 的 13 件低耦合文件（`service_proxy.go` / `cmdb_collector.go` / `enterprise_ui.go` /
  `server_security.go` / `auth_guard.go` / `dashboard.go` / `device_metrics.go` / `os_template_store.go` /
  `tenant_guard.go` / `middleware_deploy.go` / `plugin_host.go` / `server_audits.go` / `auth_perms.go`），
  其中 `plugin_host.go`/`auth_guard.go`/`auth_perms.go` 可与批 1 的 `pluginhost`/`credentials` 合流。

### 7.5 本批教训（供批 2 用）

1. **「零耦合」的判据要写全**：`*Server`/`s.` 两种形态之外，还有「以参数接收 `*Server`」与「只在注释里提 Server」两类，
   前者不可平迁、后者是假阳性（本批一例）。
2. **类型随迁 + 父包别名回导 = 零改动**：这是 store 批的配方在 controlplane 侧的再次验证——
   但前提是「类型是格式/契约」而不是「类型是中心类型的宿主」。
3. **测试的硬边界是「是否引用父包全局/未导出」**：引用即留父包（`TestInitPluginHostDisabledIsNoOp` 因 `PluginManager()` 回迁），
   这是 §3 结论「测试与实现同包」的具体化。

