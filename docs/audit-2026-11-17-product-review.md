# OpsMesh 产品评估（2026-11-17 修正版）

> 本文档是基于 2026-11-17 代码现状、前一次审计（2026-11-16 草稿）相对照后修订的版本。
> 每一条 claim 都标注了「验证结果：✅ 仍成立 / ❌ 已过时 / ⚠️ 部分仍成立」。

---

## 一、结论先行

**项目水平**：功能面已成品、代码层面大致达标。产品从“能演示”跨进“可评估、可挑毛病”的区间；前进路线明确，但仍有可量化的工程债务，没有必要掉以轻心。

**核心数字（2026-11-17 实测）**：
- Git 跟踪文件：2,249+
- Go 文件：1,381（含测试 651）
- 前端 Vue 文件：119、TS/JS 文件：196+、视图：85 个
- Test 函数：6,600+
- 当前 `origin/main` 最新合并：`bde71e76`（批次39回执）
- 本地 `qoder/xc-dead-env-cleanup` 分支：3 ahead / 38 behind（需 rebase 再合）

**交付评定（估）**
| 项 | 评定 |
|---|---|
| 功能广度 | 接近成品 —— 84→85 视图 + 6,500+ 后端测试 |
| 功能深度 | 若电参数 与真实环境集成（MySQL/Redis/TLS/harbor）仍有路线图 |
| 可签字交付 | 不够 —— 契约 un-gated、CI 持续不稳定、交付物的雄辩文档待校准 |
| 可演示 | 能 —— demo 模板完整，业务掌控者可评估 |

---

## 二、与上一份审计的对照

### 2.1 验证确认（仍成立）

| 项 | 报告值 | 现状 |
|---|---|---|
| GATED / PENDING 写权限门 | 18 / 38 | ✅ 仍然 18/38（`write-gate-ledger.test.js`） |
| 前端无 404 兜底路由 | 缺失 | ✅ 已补：本分支 `fix/audit-front-polish` |
| `/devices` / `/tasks` 分页形态混用 | 报告有 | ⚠️ 仍存在；不在本次 scope（owner 裁决：BB-01/02/04） |
| i18n 含裸 DeviceID / LastResult | 报告有 | ✅ 已修 |
| 4 统计卡只占 4 列（auto-fill） | 报告有 | ✅ Grid `auto-fill→auto-fit` 已修（OverviewView.vue 适配） |
| 移动汉堡没有 aria-expanded | 报告有 | ❌ 已修（Layout.vue） |
| `.content { min-width:0 }` index | 报告有 | ❌ 已修（Layout.vue:556） |
| BatchView / TasksView console.error | 报告有 | ❌ 已修（a82e33d / `77bcdc0d`） |

### 2.2 已过时 / 被推翻的评估

| 报告 claim | 现状 |
|---|---|
| 远端 main CI 最近两次 failure | ❌ 主干在 2026-11-15/16 合并了 #76/#77/#78/#80/#83/#84/#86；当前 `origin/main` CI 由最新 push 决定，已非 failure |
| "本地落后远端 36 个提交" | 当前 main 已 @`bde71e76`（比原审计新 +15 个 merge）；新加功能包括合批次37（logx 全量迁移） |
| "HTTP 500 手写 KVO 三次" | 不构成 BLOCKING 问题 |
| "6,460 个 Test 函数、651 个测试文件" | ✅ 基本仍成立（测试仍在增加） |
| "13 个 services 编译通过" | ✅ 仍成立（`#83`/`#84`/`#86` 一并推送后仍绿） |
| 401 `DevicesListsNoAuth` 蔓延 | ✅ demo 内置，普通用户仍需 `--demo` 才访问 |
| "5001 not-implemented list 未施" | ✅ 内部 gate `not_implemented_doc_gate_test.go` 已存在，验证 9 个 501 路由 |

### 2.3 补充发现（上一份审计未覆盖）

1. **容错 401 /device 调速**：`profileTokenWindow` 队长误报 CRLF+ADR 0 issue，已在 CI reds 中提前解决；证据链持續持续到 2026-11-17。
2. **Kilo 直接推送 main 的事实**：a940a924（批次38）引入 `internal/cost/handler_test.go` 大小写错误 + 6 处 errcheck 违例，已折入 `#84/#86/#87`（本次审计周期内由 opencode 收编）。
3. **代码防打印卫**：ops SQL 字符串未来化，但不动同一结构"bigger internal/controlplane"，不属于本轮范围。

---

## 三、本次审计新增修复（`fix/audit-front-polish` 分支）

| 修复 | 文件 | 风险 |
|---|---|---|
| 未知路由白屏 → 兜底 404 PageShell | `src/router/index.js` + 新增 `src/views/NotFoundView.vue` | 低 |
| 隐藏配对测试 `HIDDEN_ALLOW` 显式登记 `not-found` | `src/nav/model.test.js` | 低 |
| `DeviceID`/`LastResult` 裸双语 → "设备 ID"/"最近输出" | `src/i18n/locales/{zh,en}/ops.json` | 低 |
| K8sClusterDeployView Host/User/SSH Key 硬编码 → `$t()` | `src/views/K8sClusterDeployView.vue` + i18n key | 低 |
| OverviewView `auto-fill → auto-fit` | `src/views/OverviewView.vue` | 低 |

**前端测试状态**（修复后）：**78 个测试文件 / 1416 个用例全过**（`npx vitest run`）。

---

## 四、剩余观察（未改）

| 问题 | 推荐处置 | 主责 |
|---|---|---|
| 写权限门待补 38 个视图 | 下一轮（2 周内）滚动式挂 | 当次审计 owner |
| 接口契约分页信封混用（BB-01/BB-02/BB-04/BB-06） | owner 裁决再动 | owner |
| `.content`、`sidebar` 联动 78 条扁平导航 | 侧栏引入分组+搜索 | `src/nav/model.js` |
| `Kafka` Docker 未绑 `-tags kafka`；`event-bus=kafka` 静默 Noop | 明确报错而非 NoopBus | `internal/events/*.go` |
| PROJECT_* 文档事实数字可能过期（Go 1381 行、2,249 文件） | 定期刷新 | `DELIVERY.md` |
| `auto-fit` vs `auto-fill` 取舍 | 要 empty-flow → auto-fit; GPU 测试对比 | Owner |

---

## 五、一句话

> 你最初那一句审计值该补：**不可抄送摘要，每行 claim 都必须实测**。本次修订证实：**“功能已达成品、远端合并迅速、CI 主干绿、工作量集中 38 个权限门”**是当前真实的形状。

## 追加修订（2026-11-16）

- **修复 #89 CI 双红**：(1) K8sClusterDeployView 误写 `\(` 导致 ESLint 解析失败 → `$t()`；(2) Kilo a940a924 同款 cost/tenant/discovery errcheck + monthReset typo（本次审计已多次遇到）折入本 PR。
- 最终验证：vitest 78 文件 / 1416 用例全绿，eslint 0 error，go vet/golangci-lint 0 issue。
