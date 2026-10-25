# 多 agent 协作板（Coordination Board）v2

> 目的：多条 agent 线在同一仓库施工时，把"谁在动哪片 / 谁听谁指挥 / 谁在等谁"变成
> **可机械检查**的状态，而不是埋在散文日志里的叙事。
>
> 生效范围：`main` 分支。真源 = 远端仓库 `Levango7/OpsMesh`；
> 各线本地工作区只是施工面（kilo 线 `F:\Agent\kilo\workspace\OpsMesh`，
> ZCode 线 `F:\Nexus\OpsMesh`）。
>
> v2 变更（2026-10-26，ZCode 线执行）：双线协议扩为多线 + 层级分工；
> 队头约定从"互投对方 INBOX"改为"`CLAIMS.md` 单一追加日志 + INBOX 只收定向消息"。

---

## 线名册

| 线 | 角色 | 常态 |
|---|---|---|
| `kilo` | **主力线（lead）** | 只有 lead 可发 DISPATCH；merge 前门禁由它跑 |
| `workbuddy` | 机动线 | 接 DISPATCH 施工；未经派遣只做隔离面（文档/测试/脚本） |
| `qoder` | 机动线 | 同上 |
| `ZCode` | 机动线（本线） | 同上；归版/板面维护类事务默认归本线 |
| `trae` | —（遗留，见 §9 清理项） | 无 |

角色判定写死：**同一时间只有一个 lead**。lead 变更（lead 主动让位或 ttl 过期接管）
必须在 `CLAIMS.md` 留一行 `[ROLE] <ts> lead=<新线>`，不许口头默认。

---

## 文件布局

- `docs/CLAIMS.md` —— **唯一**的领地日志，append-only。CLAIM/RELEASE/DONE/
  CONFLICT/TAKEOVER/DISPATCH/STATUS/BLOCKED/ROLE 全部落这里。
- `docs/INBOX-<线名>.md` —— 只收**定向**消息（NOTE、问答、通报），线名=名册里的名字。
  例：给 kilo 的就发 `INBOX-KILO.md`。
- `scripts/coord-gate.py` —— 解析上述两类文件，回写下方「当前状态」块并出退出码。
  板面与脚本同时改时，先改脚本跑一遍，再提交两边。

历史文件 `docs/INBOX-ZCODE.md` / `docs/INBOX-LEVANGO.md` 保留不动（只读档案，
门禁仍解析——旧 CLAIM 都会重放，但既有 DONE/RELEASE 已闭环，不会误判）。

---

## 协议

### 1. 留言格式（所有类型共用）

**必须单行**，键值成对、顺序不限（`Add-Content` 会拆续行，解析器只认同行——踩过的坑）：

```
[CLAIM] 2026-10-26T02:00:00Z from=ZCode ttl=4h scope=internal/controlplane/svcproxy/
[DISPATCH] 2026-10-26T02:00:00Z from=kilo to=workbuddy task=T-012 scope=web/enterprise/ ddl=2026-10-28T00:00:00Z
[STATUS] 2026-10-26T06:00:00Z from=workbuddy task=T-012 note=building-ok
[BLOCKED] 2026-10-26T06:30:00Z from=workbuddy task=T-012 reason=runner-queued
[DONE] 2026-10-26T09:00:00Z from=workbuddy task=T-012 scope=web/enterprise/ commit=abc1234
```

### 2. 认领（CLAIM）与完成（DONE）

- `scope` 必须是**目录或单个文件**，不许 `internal/` 这种粒度；多 scope 就多行。
- `ttl` 到期无 DONE/RELEASE，任何一线可单方面 TAKEOVER（见 §6）。
- 同一 scope 只允许一个 CLAIM 存活；重复认领 = 撞车（§5）。
- DISPATCH 含 scope 时**等价于一次性 CLAIM**（有效期到 ddl），无需再补 CLAIM。

### 3. 层级：派遣（DISPATCH）与上报（BLOCKED）

- 只有 **lead** 可发 `[DISPATCH]`；`task=` 引用下方任务队列的行号。
- 执行线接活后开始施工；**中途自助重定向必须先回 `[BLOCKED]`**（不得绕道施工
  别的 scope——那正是本板要防的形态）。
- `BLOCKED` 由 lead 裁定：继续等 / 改派（新 DISPATCH）/ 挂起（`[NOTE]` 说明）。
- 机动线发现活没人派，不许私自领：在 `INBOX-<lead>.md` 留 `[NOTE]` 问。

### 4. 撞车（CONFLICT）

同 scope 两个存活 CLAIM，或某线在你已认领 scope 里有未提交改动：

```
[CONFLICT] 2026-10-26T02:05:00Z from=kilo scope=internal/controlplane/svcproxy/ other=ZCode
```

**裁定写死**：先 CLAIM（含 DISPATCH 隐含认领）者保留；后到者立即停手并回
`[CONFLICT]`。都称先时，`git log` 里留言提交时间早的赢。
**撞车不许互相 checkout / stash 对方文件**——那静默吞掉在途工作。

### 5. 禁区（FROZEN）

各线（含 lead）都不碰，除非用户明确开口：
- `.github/workflows/`（发布链，动前必须双线确认）
- `CHANGELOG.md` 顶部 `> 当前最新已发布版本` 引用块
- `docs/tech-debt.md` 的标记行（只能由跑门禁的一方改）

### 6. 接管（TAKEOVER）

CLAIM ttl 过期且无 DONE/RELEASE，或 DISPATCH 过了 ddl 无进展：

```
[TAKEOVER] 2026-10-26T06:00:00Z from=kilo scope=... reason=ttl-expired
```

接管前必须留痕本板；接管方承担该 scope 全部未提交改动。

### 7. 纪律（各线通用，写进各 harness 的指令文件）

1. **开工先 `git pull`**（含本板），有活先看「当前状态」块。
2. 动文件**之前**留 CLAIM / 接到 DISPATCH 才算开闸。
3. **收工 `[DONE] commit=<sha>`，并当天 `push`**——未提交状态不得过夜，
   共享工作区里"已提交"才是真相（10-14 两次清场事故的根因）。
4. 不读板不动禁区；不在他人未提交的 in-flight 面上施工。

---

## 当前状态

（由 `python scripts/coord-gate.py` 解析 CLAIMS.md + INBOX-*.md 回写）

```
LEAD: kilo
ACTIVE CLAIMS:
  docs/                                          <- ZCode  (dispatch by kilo, ddl 2026-10-27T00:00:00Z)

TASKS:
  T-001 <- ZCode  [open] ddl=2026-10-27T00:00:00Z note=协作板v2落地+v0.16.0归版+路线图状态修正
```

---

## 为什么不用协议互相对话

不同 harness 的会话互相隔离，进程级消息（MCP board 等）到不了对方 session——
**仓库是唯一双方都能读写的共享介质，所以板子必须是文件。**
A2A/ACP 这类协议服务的是"独立 agent 服务"，coding CLI 不在它的生态里；
本板就是为同一仓库多 CLI agent 的场景量身定做的最小公共介质。
