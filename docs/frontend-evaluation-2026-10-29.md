# 前端评估快照（2026-10-29）

> **这是 dated 快照，不是活文档。**规模与验证数字于下列 HEAD 实测，命令见末节，可重跑。
> 它取代 `docs/evaluation-report.md`（2026-09-16 快照）的「前端」部分——那份已与今日仓库不一致。
> 前端现行事实源：`CHANGELOG.md` [Unreleased] + `docs/ui-design.md` + 两个棘轮守卫。

## 基线

| 项 | 值 |
|---|---|
| HEAD | `8650077`（main） |
| 测量时间 | 2026-10-29 |
| 前端根 | `web/enterprise/`（Vue 3 + Vite，无第三方 UI 框架） |

## 已交付（五批，全部已推 main）

| 批次 | commit | 内容 |
|---|---|---|
| 1 | `3562ec4` | 覆盖补全：468 条后端 REST 路由审计 ⇒ 181 条缺口定性闭合；InfraConsole 数据驱动目录 **60 域 / 333 动作**（6 个分片文件 `src/api/infra-catalog/`，字段与枚举逐条抄 Go handler req struct，handler 声明但未读取的字段显式标「预留」，不虚构枚举） |
| 2 | `3381af2` | 布局统一：`PageShell` 四波 **78 页迁完**（24/26/13/15），豁免账 **82 → 3**（余下仅登录/注册/首登改密 3 个全屏认证页，位于 app 壳之外，永久豁免）；侧栏改**路由表单一来源**（`src/nav/model.js`，9 组 78 入口，同 `meta.navKey` 家族合并，38 条已实现但全站零入口的路由入栏） |
| 3 | `360115e` | 全站裸键清零：98 视图/组件 × zh/en 双语核验，修 12 条真缺键；新增 `src/i18n/naked-keys.test.js` 守卫（字面量键双语解析 + 负向自测） |
| 4 | `1b3851e` | 裸 `<button>` 全站迁 `<Button>` 组件 |
| 5 | `7cfef2f` | **CSS 共享原语收编**：`styles/pages.css` 成为 13 组原语的唯一权威；`styles/k8s.css` 只留 k8s 专属类；`assets/tokens.css` 同名定义与响应式规则移除（全局层不再靠 main.js 的 import 顺序决胜）；78 个视图的 scoped 副本清零 |

## 真缺陷（用户级，均已修）

1. **InfraConsole 契约断链**：任务卡片/轮询/复制读 `result.data.id`，而 API 层返回 `{s,j}` 包装、后端 202 体是 `{taskID}` ⇒ 全部 task 型动作（100+ 条）的「任务已下发」卡片无任务号、「查询结果」拉不到输出。改读 `data.j.taskID`。教训：**mock 必须从真实 API 层抄形状，不得手编**（旧 mock 曾虚构 `data.id` 掩盖缺陷）。
2. **K8s 两页 i18n 裸键**：视图调 `k8sDeploy.*` / `k8sLifecycle.*`，翻译埋在 `k8s.col.*` / `k8s.lifecycle.*` ⇒ 两页头部与全部标签渲染成原始键名。迁 50 键并补 title/desc。
3. **K8sLifecycleView 裸 fetch**：6 处 `fetch('/api/v1/…')` 绕过统一 API 层 ⇒ 会话过期不触发静默刷新/重定向、无超时、错误归一失效。已在 `src/api/k8s.js` 增 6 个 lifecycle 包装（契约注释含请求/响应形状）。
4. **K8s 集群部署路由双形注册**：`/k8s/clusters/deploy` 曾只注册带 `{id}` 形式，id-less 前端调用 404；改为两种形式都注册。
5. **CloudAccountsView `<Header>` 从未 import** ⇒ 页头空壳多年。

## 架构现状（收编后，含新踩到的坑）

**CSS 单一来源**：`pages.css` owns `.card/.card__title/.card h3/.field/.field label/.btnbar/.flowbar` 及其 `.field` 后代/`.row/.row-actions/.muted/.msg/.hint/.poll-err/.status-*/.node-row`，含原生响应式与 `.card` 暗色规则（后者从 TasksView 局部覆盖上移）。

**变体走显式修饰类，不靠各页自写**：原 67 处 scoped 携带 canonical 没有的属性（`margin-top` 48 / `min-width` 17 / `box-shadow` 10 等），一刀删会造成渲染回退 ⇒ 收敛为 `.card--raised`/`.card--flat`/`.btnbar--bottom`/`.btnbar--end`/`.btnbar--grid-row`/`.field--fixed`/`.flowbar--inline`/`.status-row--divided`，删除时同步在模板给**全部**实例挂载（原 scoped 规则本就作用于该文件全部实例，故渲染等价）。`SecretsView` 的 `.row` 拿栅格当横向 flex 用（语义冲突），改名 `.secret-cols` 保留观感。

**⚠️ 守卫开发中踩到两个真坑**（写进 spec 注释，防后人重踩）：
- 按 `;` 切分 CSS 会把上条规则的 `}` 带进下一条选择器（`} .card__title`），导致**每个 style 块只抓到第一条规则**、守卫形同虚设——必须括号配对解析；
- 按类名前缀猜会误伤 `.card h4`/`.field-hint`/`.card-grid` 等页面专属派生类——必须按归一化选择器串精确匹配。

**残留（下一批）**：`.modal-*` 家族仍寄生 `styles/k8s.css`，被 9 个视图 scoped 遮蔽，其中 `.modal-mask` 存在 **z-index 50 vs 9999 的真实行为分歧**（K8sClustersView/ResourcesView）。另有跨文件重复的派生类待处置：`.field > label`（26 文件同文）、`.row-2`（10 文件）、`.card h4`（2 文件）。

## 验证（本次实测，非 CHANGELOG 自述）

| 项 | 结果 |
|---|---|
| `npx vitest run` | **61 文件 / 1306 用例全绿** |
| `npx eslint src --no-fix` | **0 输出**（无错无警告） |
| `npx vite build` | 通过 |
| `npx playwright test` | **80/80 全绿** |
| `src/styles/__tests__/shared-primitives.spec.js` | 118 条（含 3 条守卫自检，防解析退化） |
| CI run 40832484922 | **Frontend (Vue3 Enterprise) 腿 success** |

> CI `build-test` 腿红在 `internal/controlplane/tenant.go:161` gofmt，系 `b733a72` 引入，**与前端无关**；因此 services/security/image/E2E 等下游腿被 skip。`security` 腿另有的红为 Trivy 文件系统扫描命中 go-jose `CVE-2026-116587`（存量间接依赖，漏洞库更新暴露）。两条均已板面通报 owner，前端线不代改。

## 协作与领地状态

- `web/enterprise/` 的 ZCode claim（10-27 19:00Z）已于本快照同日以 `[DONE]` 收口，板面 issues=0。
- kilo 于 10-28 22:00Z claim `internal/controlplane,web/enterprise,internal/store`（ttl=24h）；实测其近 16 个提交零前端文件，与本线前端提交文件集不相交。
- 后续前端改动前须复核板面 ACTIVE CLAIMS，避免再撞领地。

## 本次未核实（禁止当作已证伪或已证实）

未跑 `go test` / `go test -race` / `helm install` / compose 全栈 / `verify-runtime.sh`；未对 HEAD 跑 GitHub Actions 全 13 腿；未做安全动态扫描（`docs/security-issues.md` 停在 2026-08-07）；未核 codecov 当前百分比是否仍 ≥45%；未复跑 `upgrade-rehearsal.sh`；`.modal-*` 收编的 z-index 收敛未做视觉回归（无截图基线，Playwright 为功能性断言）。

## 复算命令

以下均为本次实测跑法（前端命令需先 `cd web/enterprise`，因为 vitest/eslint/playwright 的配置与该目录耦合）：

```
git rev-parse --short HEAD
git log --oneline -6
cd web/enterprise && npx vitest run            # 61 文件 / 1306 用例
cd web/enterprise && npx eslint src --no-fix    # 期望 0 输出
cd web/enterprise && npx vite build
cd web/enterprise && npx playwright test        # 期望 80/80
python scripts/coord-gate.py                   # 期望 issues=0
gh run list -R Levango7/OpsMesh --limit 3
gh api repos/Levango7/OpsMesh/actions/runs/<id>/jobs --jq '.jobs[] | "\(.conclusion)\t\(.name)"'
grep -cE "^## \[Unreleased\] — " CHANGELOG.md   # 3（注意：grep -c "^## \[Unreleased\]" 会得 4——
                                                #   L1442 是正文提及该字符串，不是块头，必须带上 " — "）
sed -n '8p' docs/tech-debt.md                   # 台账标记行
ls -d internal/*/ | wc -l                       # 43
```

单条守卫的状态：`cd web/enterprise && npx vitest run src/styles/__tests__/shared-primitives.spec.js`
（期望 118 passed；其中 3 条是守卫自检，若解析退化会先红在自己身上）。
