# AGENTS.md — agent 线必读（kilo / qoder / workbuddy / ZCode / …）

> 本仓有多条 AI agent 线并行施工。**开工前三步，缺一不可：**
>
> 1. `git pull`（本地状态以已提交为准，未提交不得过夜）；
> 2. 读 `docs/COORDINATION-BOARD.md`（线名册 + 当前状态块 + 协议）；
> 3. 读 `docs/CLAIMS.md` 末尾（谁认领了哪片、有什么定向任务给你）。

## 铁律（写死，免商议）

1. **动文件之前先 CLAIM**：在 `docs/CLAIMS.md` 追加单行
   `[CLAIM] <UTC时间> from=<你的线名> ttl=<Nh> scope=<目录或单文件>`。
   同 scope 只许一个存活 CLAIM；先到先得，撞车立即停手回 `[CONFLICT]`。
2. **只动自己认领的 scope**。绝不 checkout/stash/删除其他线的未提交改动——
   那会静默吞掉在途工作（10-14 两次清场事故的根因）。
3. **收工 `[DONE] ... commit=<sha>` 并当天 push**。
4. **只 lead（当前=kilo）可发 `[DISPATCH]`**；机动线没有派遣时只做隔离面
   （docs / tests / scripts / deploy 文档），不进主干敏感区。
5. **禁区（谁都不碰，除非用户明确开口）**：
   `.github/workflows/`、`CHANGELOG.md` 顶部版本引用块、`docs/tech-debt.md` 标记行。
6. **版本与发布**：归版/打 tag 由协调线（当前=ZCode）在 main CI 全绿后执行；
   不要自行 bump 版本号（`internal/version`、Chart、deploy 资产是联动面）。
7. 改完跑 `python scripts/coord-gate.py`（仓库根）确认板面干净再提交。

## 线名册与工作区

| 线 | 角色 | 本地工作区 |
|---|---|---|
| `kilo` | **lead / 主力** | `F:\Agent\kilo\workspace\OpsMesh` |
| `qoder` | 机动 | `F:\Agent\Qoder CN\workspace\.opsmesh-work`（⚠️ 当前状态见 `docs/INBOX-QODER.md`，先按其指示处理再开工） |
| `ZCode` | 机动 / 协调·发布 | `F:\Nexus\OpsMesh` |
| `workbuddy` | 备用 | 无固定（开工前先在 CLAIMS.md 留 NOTE 报备） |

## 留言在哪看

- 领地与任务动作（CLAIM/DISPATCH/DONE/…）：`docs/CLAIMS.md`（唯一日志，append-only）
- 发给你的定向消息：`docs/INBOX-<你的线名>.md`
- 板面状态块由 `scripts/coord-gate.py` 自动重算，不要手改。

历史背景与设计取舍见 `docs/COORDINATION-BOARD.md` 底部「为什么不用协议互相对话」。
