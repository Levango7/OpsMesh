# k8s 生命周期 ①A/①C 裁决材料（2026-11-09 刷新）

- **基线**：main @ 42991b3f（#20 合入后首跑 run 42544909045 conclusion=success）
- **作者线**：qoder；**性质**：只读取证 + 裁决材料，未改任何生产代码
- **关系**：workbuddy《语义疑点裁决单（2026-10-30）》（docs/semantic-rulings-2026-10-30.md）① 项的刷新材料；原单正文未动
- **状态**：待 owner 裁决；裁决前不投施工、不动代码

## 0. 结论先行

自原单（基线 91cfcb2，10-30）后唯一变化 = 其建议动作 **B「先补鉴权」已闭环**（a3e5f87 + f88477d，10-30 02:56 合入 main）。占位语义（A/C 之争的对象）**未变**：executeUpgrade / executeRollback / executeRotation 仍只打日志 + 推进内存状态，UI 按钮仍无占位提示。原单「先 B 后 A/C」的前置已满足，owner 可直接裁 A 或 C。

## 1. 当前 main 事实（file:line，2026-11-09 实测）

**鉴权（B 项已闭环）**

- `internal/k8slifecycle/handler.go:637-655`：Authorize 回调字段 + authorize 包装（GET/HEAD/OPTIONS → `k8s:read`，其余 → `k8s:write`；失败时回调已写响应、调用方直接 return；nil 回调透传，供独立使用/单测）。
- `internal/k8slifecycle/handler.go:902-910`：九条路由全部经 authorize(...) 注册（upgrade / upgrade/plan / upgrade/status / rollback / cert-rotate / node-scale / etcd-backup / etcd-restore / etcd-backups）。
- a3e5f87（路由包装 + 方法映射 + 租户回填 + 9 路由逐条 401 断言，拆任一包装即转红）+ f88477d（controlplane NewServer 注入回调，匿名访问 upgrade/etcd-restore/node-scale/etcd-backups 一律 401，拆注入行即转红）。

**占位语义（A/C 对象，未变）**

- `handler.go:143` executeUpgrade：四子步骤各一包装函数（upgradeControlPlane / upgradeRemainingCP / upgradeWorkers / verifyCluster，:172-190 区段），体内各一行 logx.Info + return nil；状态经 updateStatus 推 10/40/60/90。
- `handler.go:274` executeRollback：五步只留注释，一行 logx.Info + updateStatus 50/80/100 + return nil。
- `handler.go:354` executeRotation：七步只留注释，updateStatus 10/50/80/100 + return nil。
- 状态存内存 map（handler.go:58 upgrades / :309 status，行号沿用原单——10-30 后该包零提交故未漂移），无持久化、无集群回读，重启即丢。
- 同包对照组仍为真执行：PlanUpgrade（SSH kubeadm upgrade plan）/ EtcdManager.Backup / Restore / NodeScaler.AddNodes·RemoveNodes（行号引原单 §1.1）。

**时间线证据**

- `git log --since=2026-10-30 -- internal/k8slifecycle/` 实测为空 ⇒ 原单除鉴权段外事实未过期。

**UI 面**

- `web/enterprise/src/views/K8sLifecycleView.vue:32` doUpgrade、`:82` doCertRotate 按钮均无「未实现/占位」提示（grep 未见占位字样；:disabled 仅 upgrading 态）。

## 2. 选项（估价沿用原单 10-30，未重新估）

| 选项 | 改动点 | 爆炸半径 | 测试面 | 估价 |
|---|---|---|---|---|
| A 显式 501 | 三 handler 入口判占位返 501（或去内存演示）；UI 按钮禁用 + 「未实现」提示 | 前端 toast/按钮态；对外契约 202→501 | 三路由 501 契约（拆 authorize 包装转红同范式）+ 前端断言 | 0.5d |
| C 真实现 | execute* 子步骤接 kubeadm/etcd/SSH（复用同包 PlanUpgrade/Backup/Restore/NodeScaler 真执行范式） | 大：真实集群操作、回滚语义、权限细分、状态持久化 | 真进程验证 + 四/五/七子步骤逐项 | 5-10d |

B 不再列（已闭环）。

## 3. 需 owner 回答

1. A 还是 C？
2. 若 A：内存状态推进与 202「假成功」是否一并去掉（按钮改显式不可用 + 「未实现」提示）？
3. 若 C：三动作实现优先级；是否先补状态持久化（现重启即丢）？

## 4. 未核清单

- K8sLifecycleView.vue 全文（本次仅 grep 关键段；zcode 领地，只读核实）。
- 若裁 A：前端 i18n key 现状（k8sLifecycle.* 是否已有可用文案）。
- 原单 ②③④ 三项不在本材料范围（本单只刷新 ①）。

## 5. 边界

- 只读核实零代码改动；workbuddy 原裁决单正文未动；本文落 docs/ 隔离面。
- 行号以 main @ 42991b3f 为准；k8slifecycle 包 10-30 后零提交，原单行号未漂移。
- 估价未重估（沿用原单）。

## 6. 裁决记录（2026-11-09）

- 授权：用户令「kilo的 交给你裁了吧！你来接管」＝用户级授权进 kilo 领地并裁决 semantic-rulings ①（板面 CLAIM 07:18:19Z）。
- 裁决 = **A（显式 501）**：HandleUpgrade / HandleRollback / HandleCertRotation 三占位入口在参数校验后返 501 + 错误体（upgrade 错误体含 `POST /api/v1/k8s/lifecycle/upgrade/plan` dry-run 指引）；真执行路由不动；前端不动（`K8sLifecycleView.vue` doUpgrade :172/:180、doCertRotate :207/:212 已有 catch→loadErr 通用错误路径，501 自然落该路径）；C 留产品立项。
- 施工：handler.go 三处插入 501 块 + 删其后的 Manager 调用 / 500 分支 / 202 WriteJSON 死代码；Manager 方法 Upgrade/Rollback/RotateCertificates 保留作 C 基座。handler_test.go：新增 TestUpgradeRejectedAsNotImplemented（断言 501 + 错误体全串）；TestRollbackAndCertRotationAccepted 改名 …RejectedAsNotImplemented（202/phase 断言→501/错误体）；TestRoutesPassThroughWithoutAuthorize 断言 202→501（nil authorize 下请求仍到达 handler 并按其语义响应）。
- 验证：go build ./...、go test ./internal/k8slifecycle/ + ./internal/controlplane/、gofmt、golangci-lint（0 issues）全绿；变异验证：拆 upgrade 501 ⇒ TestUpgradeRejectedAsNotImplemented 判红（HandleUpgrade=200 空体），恢复后全绿。lint 另报 3 处 S1023 冗余 return（删死代码后 501 成函数末句），已删。
- 状态：代码本地提交（未推——推送烧 CI 额度，须用户单独授权）；docs 账目（CLAIMS/INBOX-KILO/本文）留本地。

## 7. 注销记录（2026-11-09T08:45:19Z）

- 本文档 §1–§6 基于本地陈旧基线（7e39d62，施工前未 fetch origin）写成，前提「k8slifecycle 包 10-30 后零提交」错误：origin/main 已有 a554697（2026-10-31，板面 :298-305/:338，PR #1 合入 3dc3162）同题闭环且更彻底——删 execute* 子步骤与内存假推进状态机、前端按钮 disabled + i18n 收口、charter 立项、TestUnimplementedActionsReturn501 + 三条窄变异判红。①A 已交付、C 已立项，本文档不再具备裁决待办效力，留档作重复施工案例取证。
- 本线 07:18 CLAIM → d70c283 为重复施工；用户授权推送前取证发现该事实，授权前提已不存在 ⇒ 未推送；本地 main 已 reset --hard origin/main，d70c283 由 backup/qoder-501-d70c283 分支留档。同坑先例：opencode 11-04（板面 :543）。
- 教训：施工前先 fetch origin 并对目标包跑 `git log origin/main -- <path>` 核对，不以本地陈旧视图判「零提交」。
