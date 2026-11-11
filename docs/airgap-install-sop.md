# OpsMesh 全离线安装 SOP（airgap bundle）

> D1（P0 准入五项·方案甲并行轨），2026-11-11 由 `deploy/scripts/build-airgap-bundle.sh`
> 与 `deploy/scripts/install-airgap.sh` 承载。本 SOP 的命令与判据以脚本内注释与退出码
> 为准；脚本升级以脚本为准、不以本 SOP 为准。

## 0. 形态与边界

- 一台**联网**构建机把「出厂 compose 形态的全部镜像 + Helm chart + 生产 values +
  部署树 + 验收脚本」打成**单一 tar**；断网机解包后校验和全绿才装载镜像并起栈，
  全程不需要 docker pull / helm repo / 任何外网。
- 镜像清单的单一事实源 = compose 文件本身的 `image:` 行（prod + prod-proxy overlay），
  `${OPSMESH_VERSION}`（含 `:?…` 报错变体）由 `--version` 代入后去重。未来加服务不漏图；
  清单与 compose 漂移会在构建时立即暴露（守卫图 `opsmesh/controlplane` 必须出现）。
- 只含**构建机架构**的镜像层（龙芯/飞腾属 A1 多架构项，不在本次范围）。

## 1. 构建机（联网）

前置：`docker` / `helm` / `sha256sum` / `tar`；`opsmesh/*` 镜像已在本地镜像库
（构建机先跑过 `deploy/docker/scripts/deploy.sh up`，或自行 `docker build`）；
第三方图本地缺时自动拉（走同目录 `docker-pull-retry.sh`，限流/抖动有界重试）。

```bash
bash deploy/scripts/build-airgap-bundle.sh --version <x.y.z> [--out DIR] [--deploy-dir DIR]
```

- `--version` 必填 `x.y.z`；`latest` 禁止（不可追溯，exit 1，同 deploy.sh:239-242 口径），
  其它非法值 exit 2。
- 环境变量 `AIRGAP_SKIP_PULL=1`：缺失镜像不自动拉取，直接判红。
- 缺图一律判红，不静默跳过。
- 产出 `opsmesh-airgap-<version>.tar.gz`（路径打印到 stdout 末行）。

包内结构（`manifest.tsv` 四列 `type/ref/path/sha256`，image 行附 `# image_id` 注释）：

```
images/<镜像名转写>.tar        # 逐图 docker save 产物
helm/opsmesh-<v>.tgz           # helm package 产出
helm/values-production.yaml    # 生产 values
deploy/docker-tree.tar.gz      # deploy/docker 全树（含 compose prod + overlay + deploy.sh）
deploy/scripts/verify-runtime.sh
manifest.tsv / CHECKSUMS.sha256 / README.txt
```

## 2. 搬运

单一 tar 走 U 盘/内网拷贝。**勿改包内任何字节**：安装时 `sha256sum -c` 全量比对，
任何一位不符即判红、绝不继续装载。

## 3. 断网机安装

### 3.1 compose 形态（默认）

```bash
bash install-airgap.sh <bundle.tar.gz> [--target DIR] [--skip-verify-runtime]
```

- `--target` 默认 `opsmesh-airgap`；目标目录必须不存在或是空目录（非空 exit 2，防覆盖既有栈）。
- 顺序即安全边界：解包 → `sha256sum -c CHECKSUMS.sha256` 全绿 → 按 manifest `image` 行
  `docker load` → 部署树解包后 `scripts/deploy.sh init` + `scripts/deploy.sh up --no-build -y`
  （`--no-build` = 离线机不编译、不拉取，只用刚 load 的镜像；同 deploy.sh:837 口径）。
- 末尾打印黑盒验收提示（只读断言）：`bash <target>/deploy/scripts/verify-runtime.sh`；
  `--skip-verify-runtime` 跳过该提示。

### 3.2 helm 形态

```bash
bash install-airgap.sh <bundle.tar.gz> --mode helm
```

- 脚本只负责到「解包 + 校验和 + docker load + chart 就位」；`helm install` 由你在断网机
  集群上下文执行（命名空间等本脚本不代为假设）：

```bash
helm install opsmesh helm/opsmesh-<v>.tgz -f helm/values-production.yaml --wait
```

## 4. 退出码

| 码 | 含义 |
|---|---|
| 0 | bundle 产出 / 安装完成 |
| 1 | 缺图、校验和不符、装载或部署失败（判红） |
| 2 | 用法错（版本格式、目标目录非空、未知参数等） |

## 5. 判据（selftest 钉死的行为）

- 校验和任何一位不符即红，且红在 `docker load` 之前（不带病装载）。
- manifest 无 `image` 行判红（bundle 损坏或形态不符）。
- 装载顺序与 manifest `image` 行顺序一致。

## 6. 未覆盖（如实记账）

- 龙芯/飞腾架构（A1 项）；本 bundle 只含构建机架构的镜像层。
- 断网机上的 mysql 首次初始化耗时与磁盘水位不在安装脚本判据内，由 `deploy.sh` 自带
  预检（`MIN_DISK_GB=10`，deploy.sh:44）与 `verify-runtime.sh` 黑盒断言兜底。
- **断网机实跑验收为用户侧步骤**：本 SOP §3 的命令即用户侧执行面；本地验证到
  「构建 / 校验和 / docker load 冒烟 / selftest 全绿」为止（2026-11-11 已实跑通过）。
