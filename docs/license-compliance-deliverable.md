# 第三方许可与源码获取说明（客户交付用）

> 本文件把 OpsMesh 交付物里**第三方组件的许可事实**、**我们履行义务的位置**、以及**客户获取源码的途径**
> 摆在一起，供客户的合规/供应链审查使用。
>
> 它是**事实汇总与机制说明，不是法律意见**；最终许可结论由商务 + 法务签署
> （决策材料见 `docs/license-decision-2026-10-03.md`，企业版授权边界见 `LICENSE` 与
> `internal/config/license.go`）。任何一条数字都可由下列命令当场复算。

## 1. 许可分层

| 层 | 范围 | 许可 |
|---|---|---|
| 内核 | `internal/`、`cmd/`、`pkg/`、`operator/` 等 | **Apache-2.0**（仓库根 `LICENSE`） |
| 企业版增值 | 企业版前端交付、企业级能力（以授权凭据里的 `features` 为准） | **商业授权**（随合同签发的授权凭据） |
| 服务 | 升级、故障响应、私有化实施 | 商业合同 |

许可证只约束**代码**；上表第二、三行是商业安排，与开源许可并行不冲突。

## 2. 交付物里的四条外部供应链

| 供应链 | 覆盖什么 | 证据文件 | 怎么生成 |
|---|---|---|---|
| Go 模块 | 服务端二进制静态链接的依赖 | `docs/third-party-licenses.md` | `bash deploy/scripts/gen-third-party-licenses.sh` |
| npm 包 | 企业版前端构建/运行时依赖 | `docs/third-party-licenses-npm.md` | 同上（同一命令，两份清单一起写） |
| 容器操作系统包 | 镜像内发行版包（busybox/coreutils/bash 等） | 本文件 §4 的实测表 + 按需重跑 | `bash deploy/scripts/collect-os-package-licenses.sh <镜像>` |
| 上游中间件镜像 | MySQL / Redis / Prometheus / Grafana / Loki / OTel 等 | 随各自官方仓库 | 由客户从上游仓库拉取（我们不再分发其内容） |

CI 里有一条门禁（`.github/workflows/ci.yml` 的「第三方依赖许可证清单门禁」）对**前两条**做
`--check`：Go 侧对账 `go.sum`，npm 侧对账 `web/enterprise/package-lock.json`（锁文件版本与
`license` 字段是入库产物，任何机器结果一致）。后两条依赖镜像内容，属**发布期快照**，见 §4/§8。

## 3. 义务 → 落实位置

| 义务 | 触发条件 | 我们怎么落实 | 可复核点 |
|---|---|---|---|
| Apache-2.0 §4(d)：保留上游 NOTICE 署名 | 再分发含上游 NOTICE 的组件 | 仓库根 `NOTICE` 由 `--emit-notice` 逐个**逐字**汇编 | `bash deploy/scripts/gen-third-party-licenses.sh --emit-notice` |
| Apache-2.0 §4(c)：随附许可证文本 | 再分发 | 仓库根 `LICENSE`（内核）+ 各依赖清单文件 | 交付包内含 `LICENSE`、`NOTICE`、两份清单 |
| MPL-2.0：文件级 copyleft | **修改**了 MPL 文件才需公开该文件 | 未修改任何 MPL 模块（`go.sum` 钉版本、无 vendor、无 replace 指向它们）；清单单列 24 个 MPL 模块供审查 | `docs/third-party-licenses.md` 的「需法务确认」段 |
| GPL/LGPL（OS 包）：二进制再分发 | 镜像内含 GPL/LGPL 程序 | 提供**书面要约**与源码获取途径（§5）；枚举出实际包与声明（§4） | `collect-os-package-licenses.sh` 的 `SUMMARY` 行 |
| npm 宽松许可（MIT/ISC/BSD）：保留版权与许可声明 | 前端产物分发 | 完整清单随交付物提供；产物内的声明保留策略以构建配置为准（见 §7 边界 3） | `docs/third-party-licenses-npm.md` |

**纪律**：不要 patch `go-sql-driver/mysql` 或 `hashicorp/vault/api` 这类 MPL 模块的任何文件——
一旦本地改动，被改的文件即产生源码提供义务（当前无此类改动，见清单里的直接依赖标记）。

## 4. 实测基线（2026-10-26，含镜像 digest）

| 对象 | 命令 | 结果 |
|---|---|---|
| 运行时基础镜像 | 见 `docs/third-party-licenses.md`「基础镜像」段（由脚本从各 Dockerfile 的 `FROM` 推导） | 控制面/operator=`distroless`；微服务=`alpine:3.23`；agent=`debian:bookworm-slim` |
| `alpine:3.23` | `collect-os-package-licenses.sh alpine:3.23` | 16 个包，**9 个**含 GPL 声明（busybox、apk-tools、libapk、ssl_client、scanelf、alpine-baselayout 等）；digest `sha256:ff7f0c72…` |
| `debian:bookworm-slim` | `collect-os-package-licenses.sh debian:bookworm-slim` | 88 个包，**64 个**含 GPL/LGPL/AGPL 声明，14 个无可读声明；digest `sha256:7ff6ba31…` |
| npm（`web/enterprise`） | `gen-third-party-licenses.sh` | 342 个唯一 `包@版本`（生产 54 / 构建 288），**copyleft = 0** |
| Go 模块 | 同上 | 162 个 `module@version`，许可证 6 类；MPL-2.0 **24 个**（需法务确认段） |

> digest 是**基础镜像 tag 在本次枚举时**解析到的值；我们自己构建的镜像 digest 以发布物为准
> （`deploy/scripts/verify-release-artifacts.sh <版本>` 会逐个核对）。

## 5. 源码获取途径（GPL-2.0 §3 / GPL-3.0 §6 / LGPL 对应条款）

镜像内的 GPL/LGPL 组件**未经我们修改**，均为发行版原样打包。源码获取方式：

1. **Alpine**：软件包源码在 [alpine/aports](https://gitlab.alpinelinux.org/alpine/aports) 的
   `3.23` 分支；也可在装有该版本的机器上 `apk fetch --source <包>`。
2. **Debian**：`deb-src` 源 + `apt-get source <包>`，或按版本查 [snapshot.debian.org](https://snapshot.debian.org/)。
3. **书面要约**：如需我们代为提供上述源码副本，可在交付合同中约定（有效期与联系方式以合同为准）。
   我们不自建源码镜像——发行版官方源是权威且可校验的来源。

## 6. 客户侧建议动作

- 交付包内的 `LICENSE`、`NOTICE`、两份第三方清单**随产品保留**；对外再分发时一并附上本文件。
- 若自行构建镜像（`docker-compose.prod.yml` 的 build 段），基础镜像的许可证义务随之转移到构建产物，
  请以本文件 §4 的命令重新枚举后归档。

## 7. 已知边界（如实列出）

1. **未经法务签署**：本文件是工程侧事实汇总；对外报价/合同附件需法务确认（尤其 MPL 的 24 个模块）。
2. **企业版 EULA 文本尚未定稿**：当前以「授权凭据 + 商业合同」承载，`LICENSE` 只覆盖 Apache-2.0 内核。
3. **前端产物内的声明保留策略未单独审计**：清单给到包级事实，但 bundler 可能合并/去除注释；
   产物级权威证据是镜像 SBOM（`release.yml` 的 syft 产物）与构建产物分析。
4. **`dev` 依赖不进交付产物**：npm 清单把「生产（进 bundle）」与「构建（不进产物）」分栏列出——
   两者法律意义不同，混在一张表里会让义务清单失真。
5. **快照会过期**：§4 的数字对应上述 digest；基础镜像或锁文件变更后须重跑（§8），CI 会拦住前两条的漂移。

## 8. 维护方式

```bash
# 1) Go + npm 两份清单（CI 用 --check 阻止漂移；改依赖后必须重跑并提交）
bash deploy/scripts/gen-third-party-licenses.sh

# 2) 镜像内 OS 包（发布期跑一次，把输出粘进本文件 §4 或作为附件归档）
bash deploy/scripts/collect-os-package-licenses.sh alpine:3.23
bash deploy/scripts/collect-os-package-licenses.sh debian:bookworm-slim

# 3) 上游 NOTICE 汇编（依赖新增带 NOTICE 的模块后重跑）
bash deploy/scripts/gen-third-party-licenses.sh --emit-notice
```
