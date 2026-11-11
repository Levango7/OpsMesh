# opencode 本线教训流水（append-only）

> 本线（opencode 分支线）自记的操作教训，区别于 kilo/qoder/workbuddy 的板子。
> 每条：日期 + 事故事实 + 可检验判据。**写完即生效。**

## 2026-11-11 —— 删 base 分支害掉了基于它的 PR

**事故**：批次 12（PR #31）合并后，我直接 `git push origin --delete fix/b12-injection-sweep` 删了远端 base 分支。批次 13（PR #32）的 base 正是该分支——GitHub 把 #32 **自动关闭**；闭闭合的 PR **不能改 base、也不能重开**（`gh pr edit --base` / `gh pr reopen` 都报错），只能用同一分支重开为接替 PR #33（内容零损失，但平添一次往返、一次新 CI、以及关闭链的说明注释）。

**根本原因**：GitHub 只对 web 界面「Delete branch」这类路径自动 retarget 依赖该分支的 open PR；`git push --delete` 不会 retarget，直接连坐关闭。

**新判据（可执行）**：凡删除带有 open PR 依赖的远端分支前，必须按序执行：

1. `gh pr list --repo Levango7/OpsMesh --state open --base <branch>` 列出全部依赖该分支的 open PR；
2. 逐个 `gh pr edit <n> --repo Levango7/OpsMesh --base <new-base>` 显式 retarget（通常为 `main`）；
3. **再**删分支（`git push origin --delete <branch>`）；
4. 删完立即核对被 retarget 的 PR：`gh pr view <n> --repo Levango7/OpsMesh --json baseRefName,state,mergeable`；若仍 CLOSED，立即创建接替 PR 并注明 supersedes。

**附带事实**：批次 13 接替链 = #32（因 base 删除被关，已注释指向 #33）→ **#33**（`fix/b13-injection-sweep`，base=`main`，内容完全一致，唯一提交 `ddd2c62`）。
