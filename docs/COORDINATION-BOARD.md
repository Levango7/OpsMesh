# 多 agent 协作板（Coordination Board）

> 目的：两个 agent 线在同一仓库并行施工时，把"谁在动哪片"变成**可机械检查**的状态，
> 而不是埋在散文日志里的叙事。
>
> 生效范围：`main` 分支、`F:\Nexus\OpsMesh` 共享工作区。

---

## 协议

### 1. 认领（CLAIM）

动一个文件/目录**之前**，在 `INBOX-<对方>.md` 追加**单行**：

```
[CLAIM] 2026-10-15T02:00:00Z from=ZCode ttl=4h scope=internal/controlplane/svcproxy/
```

规则：
- **必须单行**——`scope`/`ttl` 与 `[CLAIM]` 同行（`Add-Content` 会把续行拆开，
  解析器只认同行；这是踩过的坑）
- `scope` 必须是**目录或单个文件**，不许写 `internal/` 这种粒度
- `ttl` 到期未见到 `[DONE]`/`[RELEASE]`，任何一线可**单方面接管**（见 §4）
- 同一 scope 只允许一个 CLAIM 存活；重复认领 = 撞车，见 §3
- 多 scope 就写多行，一行一个

### 2. 释放（RELEASE）与完成（DONE）

```
[RELEASE] 2026-10-15T02:06:00Z from=Levango7 scope=internal/controlplane/svcproxy/
[DONE] 2026-10-15T03:00:00Z from=ZCode scope=internal/controlplane/svcproxy/ commit=abc1234
```

### 3. 撞车（CONFLICT）

发现同一 scope 两个 CLAIM，或发现对方在你已认领的 scope 里有未提交改动：

```
[CONFLICT] 2026-10-15T02:05:00Z from=Levango7 scope=internal/controlplane/svcproxy/ other=ZCode
```

**裁定规则（写死，免商议）**：
- 先 CLAIM 者保留；后到者立即停手并在 INBOX 里回 [CONFLICT]
- 双方都声称"先"时，`git log` 里 CLAIM 提交时间早的赢
- 撞车**不许**互相 checkout / stash 对方的文件——那会静默吞掉在途工作

### 4. 接管（TAKEOVER）

对方 `ttl` 过期且无 DONE/RELEASE，或撞车裁定归你：

```
[TAKEOVER] 2026-10-15T06:00:00Z from=Levango7 scope=internal/controlplane/svcproxy/ reason=ttl-expired
```

接管方承担该 scope 的全部未提交改动。**接管前必须在 INBOX 留痕**。

### 5. 禁区（FROZEN）

双方都不碰，除非用户明确开口：
- `.github/workflows/`（发布链，动前必须双线确认）
- `CHANGELOG.md` 顶部 `> 当前最新已发布版本` 引用块
- `docs/tech-debt.md` 的标记行（只能由跑门禁的一方改）

---

## 当前状态

（由 `scripts/coord-gate.sh` 每 30 分钟同步；也可手动 `go run ./scripts/coord-tool`）

```
ACTIVE CLAIMS:
  (none)
```

---

## 为什么不是让 agent 互相对话

试过，不行：两块板子按 session 隔离，我和 ZCode 不在同一个 harness 里，
`board_post` 到不了对方。**仓库是唯一双方都能读写的共享介质**——所以板子必须是文件。

这也解释了一个今天出现的怪象：15:31 和 16:05 两次工作区被清空、
对侧"并行施工"的假象——如果当时有这份板子，`git status` 里 64 个 `D` 会
在第一次清场后 5 分钟内变成一条 `[CONFLICT]`，而不是各自以为对方在施工。
