# TD-95 方向①「彻底解」= 统一发布方 — 设计评审稿

- 日期：2026-10-14　状态：**设计先行，未施工**（发布链代码一行未改）
- 关联：tech-debt `TD-95`（方向②已落地、v0.15.0 首跑实证绿）；门禁 §17（`validate-deploy-assets.sh`）；协调板第九十则
- 作者：本线（另一条并行 agent 线维护 release 链与 §17，见文末开放问题）

## 0. 一句话

真正的"彻底解"不是"把两个镜像搬进 release.yml"，而是**把 tag 的对外发布职责收敛到单一 workflow**，让发布物回核（`verify-artifacts`）需要核对的每一个对外产物都在**同一张 needs 图**内产出——顺带拆掉现存的"同一 tag 两个 Release 作者"隐藏纠缠。

## 1. 当前真实形状（出处为据，tip `e249774` 基线）

一个 tag push **同时触发两条 workflow**：`ci.yml`（`on.push.tags: v*`）与 `release.yml`（`on.push.tags: v*`）。对外产物按设计**分裂**在两条里：

| 产物 | 现在由谁产 | 位置 |
|---|---|---|
| 版本化二进制 tar.gz + checksums + **创建 Release** | ci.yml `release` job（goreleaser，`.goreleaser.yml: release{draft:false}`） | ci.yml:1506，`needs:[build-test,integration,security,services,proto,frontend,race,release-dryrun]`，`if: startsWith(ref,'refs/tags/v')` |
| `opsmesh-binary` / `opsmesh-agent` 镜像（build `:<sha>`→Trivy→syft SBOM→cosign sign+attest→**retag `:<version>`+`:latest`**→digest 自验→GitOps 写回） | ci.yml `image`(:779) / `image-agent`(:1178)，各 `needs: security`、~400 行 | retag 已在 gate（security）之后 |
| 12 微服务镜像 build + promote（`:<sha>`→`:<version>`+`:latest`） | release.yml `build-and-push`(:33) / `promote`(:269) | matrix.service，**不含 binary/agent**（release.yml:268 明写"由 ci.yml 发布"） |
| Release **正文**（取 `docs/release-notes.md` 本版小节 + generate_release_notes） | release.yml `github-release`(:387)→softprops `action-gh-release@v2`(:429) | 与 goreleaser 写同一个 Release |
| 发布物回核 | release.yml `verify-artifacts`(:444) `needs:[github-release]` | 校验含 ①②④⑤ 对 binary/agent `:<version>` 与 14/14 digest |

**竞态根因**：`verify-artifacts`（在 release.yml）要核对的 binary/agent 版本镜像由 **ci.yml 那条并发 run** 产出。release.yml 的 `needs` 只能引用本 workflow 的 job，**够不着 ci.yml** ⇒ 方向② 用 `wait-tag-ci.sh` 按 `head_sha` 轮询对方 run 到终态兜住（v0.15.0 首跑即绿）。这是"可判定的等"，但结构上仍是跨 workflow 依赖。

## 2. 必须先拆的隐藏纠缠：同一 tag 两个 Release 作者

- **goreleaser**（ci.yml `release`）`release.draft:false` → 建 Release 并上传二进制资产；
- **softprops**（release.yml `github-release`）`action-gh-release@v2`（Release 存在则更新，否则创建）→ 设正文。
- 二者谁先跑取决于两条并发 run 的调度，**不是编排保证**；GITHUB_TOKEN 下"谁覆盖谁"取决于到达顺序。这是设计里已存在、但没人显式钉住的脆弱点。

统一发布方时必须**单一 Release 作者**：
- **方案 (a)**：goreleaser 全权建 Release+资产；release.yml 不再调 softprops，改为在发布链末尾一步 `PATCH` 正文。
- **方案 (b)（推荐）**：`.goreleaser.yml` 置 `release.disable:true`（goreleaser 只 build 产物、不碰 Release）；Release 由 release.yml **一处**建（资产 = goreleaser 产出的 dist/ 文件 + chart 包），正文也在同处。发布形态集中、最可核对。

## 3. 目标架构：单一发布方 + 镜像管线复用（不复制 400 行）

- 把 image-leaf 的"build→scan→sign→retag→digest 自验（→可选 GitOps 写回）"抽成 **reusable workflow** `on: workflow_call`：`.github/workflows/_publish-image.yml`，入参 `image_leaf` / `promote_tags`（是否打 `:<version>`+`:latest`）/ `gitops_writeback`。**ci.yml 与 release.yml 都 `uses:` 它** ⇒ 单一实现、零复制。
- **tag 的对外发布整体归 release.yml 一条**：release.yml 调 `_publish-image.yml` 发 binary/agent（进同图）+ 现有 12 服务 build/promote + 按方案 (b) 建 Release + `verify-artifacts` `needs:` 全部发布 job。
- **ci.yml 在 tag push 不再产任何对外可变 tag/Release**：`image`/`image-agent`/`release` 三 job 加 `if: github.ref_type != 'tag'`（只保留 main/PR 质量门禁 + main 的 `:latest` 开发镜像）。ci.yml 回归纯 CI/门禁。
- `wait-tag-ci.sh` **保留**为"万一又跨 workflow"的兜底（不删），但从主判据降为 belt-and-suspenders。

## 4. 门禁 §17 重锚（`validate-deploy-assets.sh`）

现 §17 编码的是**分裂形态**（`required=["build-and-push","promote","github-release"]`、构建腿只推 `:<sha>` 的扫描面只解析 `build-and-push`、release.yml:268 那句"binary/agent 由 ci.yml 发布"的注释）。统一后须同步：
- `required` 换成统一后的发布 job 集（image-leaf 调用点 + 12 服务 build/promote + 单一 Release 步）。
- "构建腿只推不可变 `:<sha>`"断言把 binary/agent 纳入解析面（防有人在复用管线里又把 `:version` 写回构建步）。
- **新增结构断言**：ci.yml 的 `image`/`image-agent`/`release` 在 `ref_type=='tag'` 时必须不产 `:version`/`:latest`/Release（把"单一 owner"钉成判据，否则将来手滑加回 retag 又造双写者）。
- 保留：串行闸 `concurrency.cancel-in-progress:false`、`verify-artifacts` 注入 `GH_TOKEN`、`verify-release-artifacts`/`wait-tag-ci` 分诊自测真跑一次。

## 5. 验证策略（干跑为主，绝不碰真实版本序列）

- **本地**：actionlint（钉版）+ 对改到的 `run:` 块跑 shellcheck + `go test ./internal/gates/` + 三个相关 selftest（`verify-release-artifacts` / `wait-tag-ci` / `changelog-fold-gate`）+ 部署资产门禁整跑。
- **真环境（关键）**：给统一后的 release.yml 增一个 **`workflow_dispatch` 输入**（可传测试 ref 与版本 `v0.0.0-dryrun`），用 `if: github.event_name=='workflow_dispatch'` 与真实 tag 路径隔离，手动跑一遍"建 Release + 推镜像 + 回核"，跑完自动 `gh release delete --yes` + 删测试 tag（GHCR 测试镜像按需删）。**用一次性 tag 证明发布链，不进真实版本序列。**
- **交付判据（升级 TD-95）**：下一次**真实** tag，`verify-artifacts` **不依赖 wait-tag-ci 轮询**即绿——即图内已满足、wait-tag-ci 命中"无需等待"短路分支。

## 6. 分期与回滚（每段独立可回滚、逐批确认再推）

- **阶段 A**：抽 `_publish-image.yml` 复用，**ci.yml 先改用它、行为逐字不变**——可独立合与回滚。
- **阶段 B**：Release 作者归一（方案 b）——独立合。
- **阶段 C**：release.yml 接管 tag 全部发布 + ci.yml tag 侧退让 + §17 重锚——**原子一起合**（三者耦合，拆开必红）。
- **阶段 D**：加 dry-run 触发器 + 真环境干跑 + TD-95 判据更新 + CHANGELOG/board。
- 每段拆提交；任一段 CI 红就地回滚该段，不整体 revert。

## 7. 开放问题（请 release 链 / §17 归属方裁定）

1. Release 作者归一取 **(a) 还是 (b)**（我荐 b：发布形态集中一处）。
2. ci.yml 是否**完全退出 tag 发布**（推荐），还是保留 `image`/`image-agent` 仅打不可变 `:<sha>`（则回核仍要跨 run 判 digest==ci 产物，退不干净、方向①名不副实）。
3. 复用管线 `_publish-image.yml` 的 owner 与 §17 判据由哪条线维护（§17 你们刚在 `35c6213` 合过，形状改动大，需先对齐再动）。

> 本草稿不落任何代码。评审通过（尤其 §7 三问）后按 §6 分期开分支施工；推 main 前逐批确认。
