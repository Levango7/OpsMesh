# OpsMesh 多操作系统支持设计文档

> 版本：v1.0 · 编制日期：2026-08-17 · 状态：设计稿
>
> 适用范围：OpsMesh 控制面（controlplane）与 Agent（agent）双模式二进制在多操作系统、多 CPU 架构、多发行版上的构建、分发、纳管与运行支持。
>
> 关联文档：`docs/architecture.md`（整体架构）、`docs/deployment-guide.md`（部署指南）、`docs/operations.md`（运维手册）、`docs/product-roadmap.md`（产品路线图）、`docs/tech-selection.md`（技术选型）。

---

## 第1章 文档说明

### 1.1 目的与范围

本文档系统化描述 OpsMesh 在多操作系统（multi-OS）上的支持策略、平台抽象层设计、各系统详细适配方案、跨平台构建与 CI 矩阵、平台特定配置、已知限制与路线图。其目的在于：

1. **统一认知**：为研发、测试、运维、交付团队提供单一权威的多 OS 支持基线，避免"代码能编译 ≠ 平台能运行"的隐性偏差。
2. **指导实现**：为平台抽象层（Platform Abstraction Layer, PAL）的接口设计、文件布局、构建标签（build tag）使用提供可落地的规范。
3. **降低风险**：在扩展到 AIX/Solaris/FreeBSD/HarmonyOS/OpenWrt 等非主流平台前，提前识别 Go 运行时支持、第三方库兼容性、安全模块（SELinux/AppArmor/SMF）等已知风险。
4. **支撑合规**：国产化（openEuler/统信 UOS/中标麒麟/openKylin）与传统 Unix（AIX/Solaris）支持是企业级私有化交付的常见门槛，本文档为相关合规扫描提供可追溯的设计依据。

范围覆盖：

- 控制面与 Agent 二进制的目标 OS/架构矩阵；
- Agent 端平台相关能力（文件操作、进程管理、包管理、服务管理、用户管理、网络配置、防火墙、资源限制、日志收集、时间同步）的抽象与适配；
- 跨平台构建（交叉编译、CGO 处理、静态链接）、CI 矩阵、二进制分发；
- 平台特定配置差异（路径约定、默认值、配置项）；
- 已知限制、风险与渐进式路线图。

不在本文档范围：

- 控制面 Web 前端（Vue3）的浏览器兼容性（见 `docs/ui-design.md`）；
- K8s Operator 的多集群纳管细节（见 `docs/architecture.md` 第 4 章）；
- 应用层多租户隔离（见 `docs/security-mechanism.md`）。

### 1.2 设计原则

#### 1.2.1 最小侵入（Minimal Invasion）

平台差异应封装在 `internal/agent/platform/` 子包内，业务代码（任务执行、心跳、gRPC 通道）保持平台无关。判断标准：

- 业务包内不出现 `runtime.GOOS` / `runtime.GOARCH` 字面量；
- 平台差异经 `PlatformCapability` 接口注入，业务代码仅依赖接口；
- 已有平台判断点（见第 2 章）逐步收敛到 PAL，不在原位扩散。

#### 1.2.2 平台抽象（Platform Abstraction）

定义统一的 `PlatformCapability` 接口，覆盖十类平台相关能力。每个目标 OS 提供一个实现，经工厂函数按 `runtime.GOOS` + 发行版探测自动选择。抽象层要求：

- **接口稳定**：能力方法签名一旦发布，向后兼容；新增能力以扩展接口（capability mixin）形式提供；
- **降级友好**：能力不可用时返回 `ErrUnsupported` 而非 panic，调用方决定是否降级；
- **零开销**：未启用的能力（如未配置 rlimit）不产生系统调用与 goroutine 开销。

#### 1.2.3 渐进扩展（Progressive Extension）

按优先级 → P3 分阶段落地，每阶段须满足"构建通过 + CI 矩阵覆盖 + 至少一个发行版端到端验证"三条件方可进入下一阶段。不允许一次性合入多平台支持而无法验证。

#### 1.2.4 显式能力矩阵（Explicit Capability Matrix）

Agent 启动时显式打印本机能力矩阵（已在 `internal/agent/agent.go` `capabilityNote` 实现），避免"能注册但半数任务类型必挂"的错觉。每新增平台须同步更新能力矩阵输出与本文档第 2 章。

#### 1.2.5 一次编写、按平台编译（Write Once, Compile Per Platform）

优先使用 Go 标准库与 `golang.org/x/sys` 跨平台 API；仅在标准库缺失时使用 `//go:build` 标签提供平台特定实现（如 `exec_unix.go` / `exec_other.go`）。禁止在业务代码中用 `if runtime.GOOS == "x"` 做能力分支——这类分支应进入 PAL 实现。

### 1.3 术语对照

#### 表：术语对照表

| 术语 | 全称 | 含义 |
|---|---|---|
| OS | Operating System | 操作系统，如 Linux、Windows、AIX |
| 发行版 | Distribution | 操作系统的具体发行，如 CentOS、Ubuntu、Alpine |
| 架构 | Architecture | CPU 指令集架构，如 amd64、arm64、ppc64、sparc64 |
| PAL | Platform Abstraction Layer | 平台抽象层，本文档定义的 `internal/agent/platform/` 子包 |
| Capability | Platform Capability | 平台能力，PAL 接口的十类方法分组 |
| Agent | OpsMesh Agent | 部署在被纳管设备上的 OpsMesh 二进制（agent 模式） |
| 控制面 | Control Plane | OpsMesh 中心化管控二进制（controlplane 模式） |
| 交叉编译 | Cross Compile | 在某一 OS/架构上编译另一 OS/架构的二进制 |
| CGO | Cgo | Go 调用 C 代码的机制，影响交叉编译与静态链接 |
| systemd | System Daemon | Linux 系统与服务管理器，主流发行版默认 |
| OpenRC | Open Run Command | Gentoo/Alpine 使用的 init 系统 |
| SMF | Service Management Facility | Solaris 服务管理框架 |
| SRC | System Resource Controller | AIX 服务管理器 |
| rc.d | BSD rc.d | FreeBSD 服务管理脚本框架 |
| SELinux | Security-Enhanced Linux | Linux 强制访问控制安全模块 |
| AppArmor | Application Armor | Linux 强制访问控制安全模块（SUSE/Ubuntu 默认） |
| IPS | Image Packaging System | Solaris 包管理系统 |
| installp | AIX installp | AIX 软件安装命令 |
| hap/hsp | HarmonyOS Ability Package | 鸿蒙应用/共享包格式 |
| opkg | OpenWrt Package | OpenWrt 软件包管理器 |
| procd | OpenWrt Process Daemon | OpenWrt init 与服务管理守护进程 |
| musl | musl libc | Alpine 使用的轻量 C 库 |
| glibc | GNU C Library | 主流 Linux 发行版使用的 C 库 |
| 安全加固/P3 | Priority 0/1/2/3 | 支持优先级，最高（必须），P3 最低（尽力） |

---

## 第2章 当前支持状态

### 2.1 已支持平台矩阵

OpsMesh 当前（v1.0 基线）已支持的平台矩阵如下。能力列含义：✓ = 完整支持且经 CI 验证；△ = 部分支持（能力受限）；✗ = 不支持。

#### 表：当前已支持平台能力矩阵表

| 平台 | 架构 | Agent 注册/心跳 | Shell 任务 | Service 任务 | 文件传输 | 包管理 | 服务管理 | 资源限额（rlimit） | 进程组 kill | CMDB 采集 | 日志采集 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Linux | amd64 | ✓ | ✓ | ✓（systemctl） | ✓ | △（yum/apt 探测中） | ✓（systemctl） | ✓（unix.Setrlimit） | ✓（Setpgid+SIGTERM） | ✓ | ✓ |
| Linux | arm64 | ✓ | ✓ | ✓（systemctl） | ✓ | △ | ✓ | ✓ | ✓ | ✓ | ✓ |
| macOS | amd64 | ✓ | ✓ | ✗（冻结） | ✓ | ✗ | ✗ | ✓ | ✓ | ✓ | ✓ |
| macOS | arm64 | ✓ | ✓ | ✗ | ✓ | ✗ | ✗ | ✓ | ✓ | ✓ | ✓ |
| Windows | amd64 | ✓ | ✓（cmd /C） | ✗（冻结） | ✓ | ✗ | △（sc query 只读） | ✗（job object 未实现） | △（taskkill /T /F） | ✓ | ✓ |
| Windows | arm64 | ✓ | ✓ | ✗ | ✓ | ✗ | △ | ✗ | △ | ✓ | ✓ |

> 说明：
>
> - **Service 任务**：依赖 `systemctl`，当前显式拒绝非 Linux 平台（见 `internal/agent/agent.go:860`，Linux-only freeze）。Windows 的 `sc query` 仅用于 CMDB 服务状态采集（只读），不接受 service 类任务下发。
> - **资源限额**：Linux/Darwin 经 `golang.org/x/sys/unix.Setrlimit` 设置 NPROC/NOFILE/AS；Windows 无对应系统调用，跳过（等保纵深防御在控制面/IAM 侧兜底）。
> - **进程组 kill**：Linux/Darwin 经 `Setpgid=true` + `syscall.Kill(-pid, SIGTERM)`；Windows 经 `taskkill /T /F /PID` 杀进程树；其他非 POSIX 平台 noop。
> - **包管理**：当前未实现统一的包管理抽象，仅 `internal/deploy/` 在部署编排中按发行版探测 yum/apt，尚未暴露为 Agent 能力。

### 2.2 代码中的平台判断点

通过搜索 `runtime.GOOS` 与 `runtime.GOARCH`，当前平台判断点共 17 处，分布如下。这是 PAL 收敛的基线清单。

#### 表：runtime.GOOS / runtime.GOARCH 使用位置清单

| 文件 | 行号 | 判断式 | 用途 | PAL 收敛策略 |
|---|---|---|---|---|
| `internal/agent/agent.go` | 301 | `runtime.GOOS == "windows"` | CMDB 中间件探测选 tasklist vs ps aux | 迁入 `Platform.ProcessList()` |
| `internal/agent/agent.go` | 329 | `runtime.GOOS == "windows"` | ps 输出列解析（首列 vs 第 11 列） | 同上，结果经结构体返回 |
| `internal/agent/agent.go` | 390 | `capabilityNote(runtime.GOOS)` | 启动期能力矩阵日志 | 迁入 `Platform.CapabilityNote()` |
| `internal/agent/agent.go` | 519 | `runtime.GOOS` | 注册时上报 OS | 保留（元信息上报，非能力分支） |
| `internal/agent/agent.go` | 520 | `runtime.GOARCH` | 注册时上报 Arch | 保留（同上） |
| `internal/agent/agent.go` | 860 | `runtime.GOOS != "linux"` | service 任务前置拒绝 | 迁入 `Platform.Service().Available()` |
| `internal/agent/agent.go` | 1266 | `runtime.GOOS == "windows"` | shell 构造 cmd /C vs sh -c | 迁入 `Platform.ShellCommand()` |
| `internal/agent/exec_unix.go` | 1 | `//go:build linux \|\| darwin` | Setpgid + SIGTERM 进程组 | 保留（构建标签是 PAL 推荐形式） |
| `internal/agent/exec_other.go` | 30 | `runtime.GOOS == "windows"` | taskkill /T /F /PID | 保留（已隔离在 _other 文件） |
| `internal/agent/rlimit_unix.go` | 1 | `//go:build linux \|\| darwin` | unix.Setrlimit | 保留 |
| `internal/agent/rlimit_other.go` | 12 | `_ = runtime.GOOS` | noop 占位 | 保留 |
| `internal/agent/metrics_collect.go` | 161 | `runtime.GOARCH` | 上报 Arch 指标 | 保留（元信息） |
| `internal/agent/metrics_collect.go` | 341 | `runtime.GOOS == "windows"` | 服务状态查询 sc vs systemctl | 迁入 `Platform.Service().Status()` |
| `internal/controlplane/server_network.go` | 19 | 注释引用 | 控制面侧按 agent.OS 构造命令 | 保留（控制面侧，非 Agent PAL） |
| `internal/controlplane/e2e_test.go` | 179 | `runtime.GOOS == "windows"` | E2E 测试跳过 | 保留（测试代码） |
| `internal/agent/agent_extra_test.go` | 999 | `var _ = runtime.GOOS` | 测试引用占位 | 保留 |
| `internal/agent/metrics_collect_test.go` | 20/22 | 注释引用 | Arch 非空断言 | 保留 |

收敛目标：业务包内 `runtime.GOOS` 字面量比较从 8 处降至 2 处（注册上报 + 能力矩阵），其余进入 PAL。

### 2.3 平台特定实现

当前平台特定代码以 `//go:build` 标签隔离，文件命名遵循 `<base>_<suffix>.go` 约定：

#### 表：现有平台特定文件清单

| 文件 | 构建标签 | 平台 | 实现内容 |
|---|---|---|---|
| `internal/agent/exec_unix.go` | `linux \|\| darwin` | POSIX | `setProcessGroup`（Setpgid）、`killProcessGroup`（SIGTERM 进程组） |
| `internal/agent/exec_other.go` | `!linux && !darwin` | 非 POSIX | `setProcessGroup` noop、`killProcessGroup` 经 taskkill（Windows）或 noop |
| `internal/agent/rlimit_unix.go` | `linux \|\| darwin` | POSIX | `setRlimits` 经 `unix.Setrlimit` 设置 NPROC/NOFILE/AS |
| `internal/agent/rlimit_other.go` | `!linux && !darwin` | 非 POSIX | `setRlimits` noop（Windows 无 job object 等价实现） |
| `internal/events/kafka.go` | `kafka` | 自定义标签 | Kafka 事件总线实现 |
| `internal/events/kafka_stub.go` | `!kafka` | 默认 | Kafka stub（降级 NoopBus） |

构建标签策略：

- **POSIX vs 非 POSIX**：以 `linux || darwin` 与 `!linux && !darwin` 对偶，未来 FreeBSD/Solaris/AIX 须扩展为 `linux || darwin || freebsd || solaris || aix`；
- **能力标签**：如 `kafka` 表示可选能力启用，默认 stub 降级；
- **文件命名**：`_unix` / `_other` / `_windows` 后缀，配合标签而非依赖文件名（Go 仅认标签）。

### 2.4 已支持平台的构建产物

当前 `.goreleaser.yml` 配置的构建矩阵：

```yaml
# 命令示例：goreleaser 跨平台构建配置
builds:
  - id: opsmesh
    main: ./cmd/opsmesh
    binary: opsmesh
    env:
      - CGO_ENABLED=0
    goos:
      - linux
    goarch:
      - amd64
      - arm64
```

> 现状：仅 `linux/amd64` + `linux/arm64` 经 goreleaser 产出发布产物。macOS/Windows 在开发者本机经 `make backend` 构建验证，未纳入发布矩阵。本文档第 6/7 章将扩展发布矩阵。

---

## 第3章 目标支持矩阵

### 3.1 完整目标支持矩阵

下表是 OpsMesh 计划支持的完整 OS/发行版/架构矩阵，按优先级排列。优先级含义：

- ****：必须支持，CI 矩阵覆盖 + 端到端验证 + 发布产物；
- ****：应支持，CI 矩阵覆盖 + 至少单元测试；
- ****：可支持，交叉编译通过 + 手工验证；
- **P3**：尽力支持，社区贡献或按需启用。

#### 表：完整目标支持矩阵表

| 系统 | 发行版 | 架构 | 优先级 | 场景 | 阶段 |
|---|---|---|---|---|---|
| Linux | CentOS 7/8/9 | amd64/arm64 | | 企业服务器 | Phase 1 |
| Linux | RHEL 7/8/9 | amd64/arm64 | | 企业服务器 | Phase 1 |
| Linux | Ubuntu 20.04/22.04/24.04 | amd64/arm64 | | 云服务器 | Phase 1 |
| Linux | Debian 11/12 | amd64/arm64 | | 通用服务器 | Phase 1 |
| Linux | SUSE/SLES 15 | amd64/arm64 | | 企业服务器 | Phase 2 |
| Linux | Amazon Linux 2/2023 | amd64/arm64 | | AWS | Phase 2 |
| Linux | Alpine 3.18+ | amd64/arm64 | | 容器 | Phase 2 |
| Linux | openEuler 22.03+ | amd64/arm64 | | 国产化 | Phase 3 |
| Linux | 统信 UOS | amd64/arm64 | | 国产化 | Phase 3 |
| Linux | 中标麒麟 | amd64/arm64 | | 国产化 | Phase 3 |
| Linux | openKylin | amd64/arm64 | | 国产化 | Phase 3 |
| AIX | 7.2/7.3 | ppc64 | | 传统企业 | Phase 4 |
| Solaris | 11.4 | sparc64/x86_64 | | 传统企业 | Phase 4 |
| FreeBSD | 13/14 | amd64/arm64 | | 网络/存储 | Phase 4 |
| macOS | 12+ | amd64/arm64 | | 开发/运维终端 | Phase 1 |
| Windows | Server 2016+ | amd64 | | 企业服务器 | Phase 1 |
| Windows | 10/11 | amd64 | | 运维终端 | Phase 1 |
| HarmonyOS | 5.0+ | arm64 | | IoT/边缘 | Phase 5 |
| OpenWrt | 21.02+ | mips/arm | P3 | 网络设备 | Phase 5 |

### 3.2 优先级与场景说明

#### 3.2.1 P0（必须支持）

平台是 OpsMesh 私有化交付的最小集，覆盖 90% 企业服务器与开发终端：

- **CentOS/RHEL 7/8/9**：企业 Linux 事实标准，systemd + SELinux，yum/dnf 包管理；
- **Ubuntu 20.04/22.04/24.04**：云服务器主流，systemd + apparmor，apt 包管理；
- **macOS 12+**：开发与运维终端，brew 包管理，launchd 服务管理（Agent 端 service 任务暂不要求）；
- **Windows Server 2016+**：企业 Windows 服务器，sc/PowerShell 服务管理。

#### 3.2.2 P1（应支持）

平台扩展企业覆盖面，CI 矩阵覆盖但允许部分能力降级：

- **Debian 11/12**：Ubuntu 上游，apt/dpkg，与 Ubuntu 共享实现；
- **SUSE/SLES 15**：欧洲企业主流，zypper + AppArmor；
- **Amazon Linux 2/2023**：AWS EC2 默认镜像，dnf/yum，需 AWS 元数据集成；
- **统信 UOS / 中标麒麟 / openEuler**：国产化合规场景，yum/dnf + systemd，需国产化安全模块适配；
- **Windows 10/11**：运维终端，与 Server 共享实现。

#### 3.2.3 P2（可支持）

平台为长尾场景，交叉编译通过 + 手工验证即可：

- **Alpine 3.18+**：容器场景，musl libc，apk + OpenRC，需注意 musl 与 glibc 差异；
- **openKylin**：国产化社区版，与麒麟商业版共享实现；
- **AIX 7.2/7.3**：Power 架构传统 Unix，installp + SRC，ppc64 大端序；
- **Solaris 11.4**：sparc64/x86_64，IPS + SMF，ZFS/Zones 集成；
- **FreeBSD 13/14**：网络/存储场景，pkg + rc.d，Jails/ZFS；
- **HarmonyOS 5.0+**：IoT/边缘，hap/hsp + Ability，ArkTS 适配层。

#### 3.2.4 P3（尽力支持）

- **OpenWrt 21.02+**：嵌入式网络设备，mips/arm，opkg + procd + BusyBox，资源受限严重。

### 3.3 Go 运行时支持对照

目标矩阵中的 OS/架构须先满足 Go 官方运行时支持。下表对照 Go 1.26（OpsMesh 当前 `go.mod` 基线）的支持状态。

#### 表：Go 1.26 运行时支持对照表

| OS/架构 | Go 官方支持 | 备注 |
|---|---|---|
| linux/amd64 | ✓ | 一等公民 |
| linux/arm64 | ✓ | 一等公民 |
| linux/ppc64le | ✓ | 小端 Power（AIX 是 ppc64 大端，不同） |
| linux/riscv64 | ✓ | 实验性稳定 |
| darwin/amd64 | ✓ | macOS 10.15+ |
| darwin/arm64 | ✓ | Apple Silicon |
| windows/amd64 | ✓ | Windows 10/Server 2016+ |
| windows/arm64 | ✓ | Windows on ARM |
| freebsd/amd64 | ✓ | FreeBSD 13+ |
| freebsd/arm64 | ✓ | FreeBSD 13+ |
| aix/ppc64 | ✓ | AIX 7.2+，大端 |
| solaris/amd64 | ✓ | Solaris 11.4+，illumos 兼容 |
| js/wasm | ✓ | 浏览器/Wasm runtime，非本场景 |
| wasip1/wasm | ✓ | WASI preview 1 |
| linux/sparc64 | ✗ | Go 不支持，Solaris sparc64 须用 x86_64 |
| harmonyos/arm64 | ✗ | Go 不直接支持，须用 OHOS SDK + ArkTS 适配 |
| openwrt/mips | △ | Go 支持 linux/mips 但 OpenWrt 自带 musl，需交叉编译验证 |

> 关键结论：
>
> - **Solaris sparc64** 与 **HarmonyOS arm64** 无 Go 原生支持，须分别用 x86_64 架构与 ArkTS 适配层；
> - **AIX ppc64** 是大端，与 Linux ppc64le 不同，须单独 CI 矩阵；
> - **OpenWrt mips** 须验证 musl + Go linux/mips 兼容性。

---

## 第4章 平台抽象层设计

### 4.1 平台能力接口

`PlatformCapability` 是 PAL 的核心接口，聚合十类平台相关能力。接口定义在 `internal/agent/platform/platform.go`，每个能力是一个 mixin 接口，OS 实现按需组合。

```go
// 代码示例：PlatformCapability 核心接口（Go）
package platform

import (
    "context"
    "io"
    "os/exec"
)

// Platform 是平台抽象层的根接口。
// 每个目标 OS 提供一个实现，经 Detect() 工厂按 runtime.GOOS + 发行版探测自动选择。
type Platform interface {
    // OS 返回平台标识（linux/darwin/windows/aix/solaris/freebsd/harmony/openwrt）。
    OS() string
    // Arch 返回架构标识（amd64/arm64/ppc64/sparc64/x86_64/mips/arm）。
    Arch() string
    // Distro 返回发行版信息（name + version），非发行版场景返回空。
    Distro() DistroInfo
    // CapabilityNote 返回人类可读的能力矩阵描述（启动期日志用）。
    CapabilityNote() string

    // 十类能力，按 mixin 组合（见下）。
    File() FileOps
    Process() ProcessOps
    Package() PackageOps
    Service() ServiceOps
    User() UserOps
    Network() NetworkOps
    Firewall() FirewallOps
    Rlimit() RlimitOps
    Log() LogOps
    Time() TimeOps
}

// DistroInfo 描述发行版信息。
type DistroInfo struct {
    Name    string // centos/rhel/ubuntu/debian/suse/amazon/alpine/openeuler/uos/kylin/...
    Version string // 9 / 22.04 / 3.18 / 22.03LTS
    Codename string // jammy / bookworm / ...
}

// ErrUnsupported 表示当前平台不支持该能力。调用方决定是否降级。
var ErrUnsupported = errors.New("platform: capability unsupported on this OS")
```

#### 4.1.1 文件操作（FileOps）

```go
// 代码示例：文件操作能力接口（Go）
type FileOps interface {
    // Transfer 上传/下载文件。src 为本地路径，dst 为目标路径。
    // mode 决定方向：upload（控制面→agent）/ download（agent→控制面）。
    Transfer(ctx context.Context, src, dst string, mode TransferMode) error
    // Stat 返回文件元信息（大小/权限/修改时间/是否符号链接）。
    Stat(path string) (FileInfo, error)
    // Chown 设置属主/属组。uid/gid 为 -1 时不变。
    Chown(path string, uid, gid int) error
    // Symlink 创建符号链接。非 POSIX 平台返回 ErrUnsupported。
    Symlink(target, link string) error
    // TempDir 返回平台约定的临时目录（Linux: /tmp，Windows: %TEMP%，AIX: /tmp）。
    TempDir() string
}
```

#### 4.1.2 进程管理（ProcessOps）

```go
// 代码示例：进程管理能力接口（Go）
type ProcessOps interface {
    // List 列出当前进程（用于 CMDB 中间件探测）。
    // 返回进程名 + PID + 命令行，跨平台经 ps/tasklist/ps -e 实现。
    List(ctx context.Context) ([]ProcessInfo, error)
    // Kill 杀进程。pid > 0 杀单进程，pid < 0 杀进程组（POSIX）。
    Kill(pid int) error
    // SetProcessGroup 在子进程上设置进程组（POSIX: Setpgid，Windows: noop）。
    SetProcessGroup(cmd *exec.Cmd)
    // ShellCommand 按平台选择 shell 构造子进程（POSIX: sh -c，Windows: cmd /C）。
    ShellCommand(command string) *exec.Cmd
}
```

#### 4.1.3 包管理（PackageOps）

```go
// 代码示例：包管理能力接口（Go）
type PackageOps interface {
    // Manager 返回包管理器标识（yum/dnf/apt/dpkg/zypper/apk/pkg/ips/installp/opkg/hap）。
    Manager() string
    // Install 安装包。name 为包名，version 为空表示最新。
    Install(ctx context.Context, name, version string) error
    // Remove 卸载包。
    Remove(ctx context.Context, name string) error
    // Query 查询包是否已安装及版本。
    Query(ctx context.Context, name string) (installed bool, version string, err error)
    // ListInstalled 列出所有已安装包（用于 CMDB 软件清单）。
    ListInstalled(ctx context.Context) ([]PackageInfo, error)
    // UpdateRepo 更新包仓库索引（apt update / yum makecache）。
    UpdateRepo(ctx context.Context) error
}
```

#### 4.1.4 服务管理（ServiceOps）

```go
// 代码示例：服务管理能力接口（Go）
type ServiceOps interface {
    // Available 返回服务管理是否可用（Linux systemd: true，macOS: false）。
    Available() bool
    // Manager 返回服务管理器标识（systemd/openrc/smf/src/rc.d/launchd/sc）。
    Manager() string
    // Status 查询服务状态（running/stopped）与是否开机自启。
    Status(ctx context.Context, name string) (status string, enabled bool, err error)
    // Start/Stop/Restart 启停服务。
    Start(ctx context.Context, name string) error
    Stop(ctx context.Context, name string) error
    Restart(ctx context.Context, name string) error
    // Enable/Disable 设置开机自启。
    Enable(ctx context.Context, name string) error
    Disable(ctx context.Context, name string) error
}
```

#### 4.1.5 用户与组管理（UserOps）

```go
// 代码示例：用户管理能力接口（Go）
type UserOps interface {
    // AddUser 添加用户。home 为空使用平台默认（/home/<user>）。
    AddUser(ctx context.Context, user UserSpec) error
    // DelUser 删除用户。removeHome=true 同时删除家目录。
    DelUser(ctx context.Context, name string, removeHome bool) error
    // ListUsers 列出所有用户（用于 CMDB）。
    ListUsers(ctx context.Context) ([]UserInfo, error)
    // AddGroup/DelGroup 组管理。
    AddGroup(ctx context.Context, name string, gid int) error
    DelGroup(ctx context.Context, name string) error
    // Sudo 配置 sudo/sudoers（POSIX）/ RunAs（Windows）。
    Sudo(ctx context.Context, user string, rules []SudoRule) error
}
```

#### 4.1.6 网络配置（NetworkOps）

```go
// 代码示例：网络配置能力接口（Go）
type NetworkOps interface {
    // ListInterfaces 列出网卡（复用 gopsutil/net，跨平台）。
    ListInterfaces() ([]InterfaceInfo, error)
    // SetIP 配置静态 IP（POSIX: netplan/network-scripts，Windows: netsh）。
    SetIP(ctx context.Context, iface string, cfg IPConfig) error
    // SetDNS 配置 DNS（POSIX: /etc/resolv.conf 或 systemd-resolved，Windows: netsh）。
    SetDNS(ctx context.Context, servers []string) error
    // SetHostname 设置主机名。
    SetHostname(ctx context.Context, name string) error
    // RouteAdd/RouteDel 路由管理。
    RouteAdd(ctx context.Context, route Route) error
    RouteDel(ctx context.Context, route Route) error
}
```

#### 4.1.7 防火墙（FirewallOps）

```go
// 代码示例：防火墙能力接口（Go）
type FirewallOps interface {
    // Manager 返回防火墙标识（firewalld/ufw/iptables/nftables/pf/ipf/Windows Firewall）。
    Manager() string
    // AddRule 添加放行规则。
    AddRule(ctx context.Context, rule FirewallRule) error
    // DelRule 删除规则。
    DelRule(ctx context.Context, rule FirewallRule) error
    // ListRules 列出所有规则。
    ListRules(ctx context.Context) ([]FirewallRule, error)
    // Reload 重载规则（firewalld: reload，ufw: reload，iptables: iptables-restore）。
    Reload(ctx context.Context) error
}
```

#### 4.1.8 资源限制（RlimitOps）

```go
// 代码示例：资源限制能力接口（Go）
type RlimitOps interface {
    // Available 返回 rlimit 是否可用（POSIX: true，Windows: false）。
    Available() bool
    // Set 设置进程资源限额（NPROC/NOFILE/AS/CPU/FSIZE）。
    Set(res RlimitResource, cur, max uint64) error
    // Get 查询当前限额。
    Get(res RlimitResource) (cur, max uint64, err error)
}
```

#### 4.1.9 日志收集（LogOps）

```go
// 代码示例：日志收集能力接口（Go）
type LogOps interface {
    // SystemLogPaths 返回系统日志路径约定（Linux: /var/log/messages,/var/log/syslog；Windows: EventLog）。
    SystemLogPaths() []string
    // Tail 尾随读取日志文件增量（复用现有 logCollectLoop 实现）。
    Tail(ctx context.Context, path string, offset int64) (content []byte, newOffset int64, err error)
    // QueryEventLog 查询 Windows EventLog / macOS unified log / AIX errpt。
    QueryEventLog(ctx context.Context, query EventLogQuery) ([]EventLogEntry, error)
}
```

#### 4.1.10 时间同步（TimeOps）

```go
// 代码示例：时间同步能力接口（Go）
type TimeOps interface {
    // Now 返回当前时间（跨平台，标准库 time.Now）。
    Now() time.Time
    // SetTime 设置系统时间（需 root/Administrator）。
    SetTime(ctx context.Context, t time.Time) error
    // NTPSync 配置 NTP 同步（Linux: chronyd/ntpd，Windows: w32tm，AIX: xntpd）。
    NTPSync(ctx context.Context, servers []string) error
    // Timezone 设置时区。
    Timezone(ctx context.Context, tz string) error
}
```

### 4.2 平台检测与适配策略

#### 4.2.1 检测流程

Agent 启动时按以下顺序探测平台，构造对应的 `Platform` 实现：

```text
图：平台检测与适配流程图

  Agent 启动
       │
       ▼
  读取 runtime.GOOS / runtime.GOARCH
       │
       ▼
  按 GOOS 分支 ─────────────────────────────────┐
       │                                         │
       ├─ linux ──► 读 /etc/os-release           │
       │              ├─ ID=centos/rhel  ──► RHELFamily{dnf/yum, systemd, selinux}
       │              ├─ ID=ubuntu/debian ──► DebianFamily{apt, systemd, apparmor}
       │              ├─ ID=suse/sles ────► SUSEFamily{zypper, systemd, apparmor}
       │              ├─ ID=amzn ────────► AmazonLinux{dnf, systemd, aws}
       │              ├─ ID=alpine ──────► AlpineLinux{apk, openrc, musl}
       │              ├─ ID=openeuler ───► OpenEuler{dnf, systemd, selinux}
       │              ├─ ID=uos ─────────► UOS{dnf, systemd, selinux}
       │              ├─ ID=kylin ───────► Kylin{dnf/yum, systemd, selinux}
       │              └─ 其他 ──────────► GenericLinux{systemd}
       │                                         │
       ├─ darwin ──► 读 /System/Library/...     │
       │              └─ macOS{brew, launchd}    │
       │                                         │
       ├─ windows ─► 读 HKLM\SOFTWARE\...        │
       │              └─ Windows{sc, wua, netsh}  │
       │                                         │
       ├─ aix ─────► AIX{installp, src, errpt}   │
       │                                         │
       ├─ solaris ─► Solaris{ips, smf, zfs}      │
       │                                         │
       ├─ freebsd ─► FreeBSD{pkg, rc.d, zfs}     │
       │                                         │
       └─ 其他 ────► UnknownPlatform（仅 shell） │
                                                 │
  构造 PlatformCapability ──────────────────────┘
       │
       ▼
  注入 Agent，启动主循环
```

#### 4.2.2 发行版探测实现

Linux 发行版经 `/etc/os-release`（LSB 标准化）探测，关键字段：

- `ID`：发行版标识（centos/rhel/ubuntu/debian/opensuse/sles/amzn/alpine/openEuler/uos/kylin/openkylin）；
- `VERSION_ID`：主版本号；
- `PRETTY_NAME`：人类可读全名（已用于 CMDB `os.version` 属性）。

非 Linux 平台：

- **macOS**：`/System/Library/CoreServices/SystemVersion.plist`；
- **Windows**：注册表 `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion`；
- **AIX**：`oslevel` 命令；
- **Solaris**：`/etc/release` 文件；
- **FreeBSD**：`freebsd-version` 命令。

#### 4.2.3 适配降级策略

当某能力不可用时，PAL 实现返回 `ErrUnsupported`，调用方按业务语义降级：

#### 表：能力降级策略表

| 能力 | 不可用场景 | 降级行为 |
|---|---|---|
| Service | macOS/Windows/Alpine（OpenRC） | service 任务前置拒绝（模式），返回明确错误 |
| Rlimit | Windows | 跳过进程限额，控制面/IAM 侧兜底 |
| Package | macOS/Windows | 包管理任务拒绝，提示用 brew/手动安装 |
| Firewall | 无 firewalld/ufw 的最小系统 | 跳过防火墙配置，记录警告 |
| Symlink | Windows | 改用快捷方式或返回 ErrUnsupported |
| NTPSync | 容器场景 | 跳过（容器与宿主共享时钟） |

### 4.3 代码架构

#### 4.3.1 目录设计

```text
图：internal/agent/platform/ 目录结构

internal/agent/platform/
├── platform.go              # 接口定义（Platform + 十类 mixin + ErrUnsupported）
├── detect.go                # Detect() 工厂：按 runtime.GOOS + 发行版探测构造实现
├── distro.go                # DistroInfo + 发行版探测辅助函数
├── errors.go                # ErrUnsupported + 错误类型
├── linux/
│   ├── linux.go             # Linux 根实现（OS/Arch/Distro/CapabilityNote）
│   ├── file.go              # FileOps（POSIX 通用）
│   ├── process.go           # ProcessOps（ps aux、Setpgid、SIGTERM）
│   ├── package_yum.go       # yum/dnf 实现（RHEL/CentOS/Amazon/openEuler/UOS/Kylin）
│   ├── package_apt.go       # apt/dpkg 实现（Ubuntu/Debian）
│   ├── package_zypper.go    # zypper 实现（SUSE/SLES）
│   ├── package_apk.go       # apk 实现（Alpine）
│   ├── service_systemd.go   # systemd 实现（主流 Linux）
│   ├── service_openrc.go    # OpenRC 实现（Alpine）
│   ├── user.go              # UserOps（useradd/userdel/groupadd）
│   ├── network.go           # NetworkOps（netplan/network-scripts）
│   ├── firewall_firewalld.go # firewalld 实现
│   ├── firewall_ufw.go      # ufw 实现
│   ├── firewall_iptables.go # iptables/nftables 实现
│   ├── rlimit.go            # RlimitOps（unix.Setrlimit）
│   ├── log.go               # LogOps（/var/log/*）
│   └── time.go              # TimeOps（chronyd/ntpd）
├── darwin/
│   ├── darwin.go            # macOS 根实现
│   ├── file.go              # FileOps（POSIX）
│   ├── process.go           # ProcessOps（ps aux）
│   ├── package_brew.go      # brew 实现（可选，默认 ErrUnsupported）
│   ├── service_launchd.go   # launchd 实现（可选）
│   ├── user.go              # UserOps（dscl）
│   ├── network.go           # NetworkOps（networksetup）
│   ├── firewall_pf.go       # pf 防火墙
│   ├── rlimit.go            # RlimitOps（unix.Setrlimit）
│   ├── log.go               # LogOps（/var/log/* + unified log）
│   └── time.go              # TimeOps（systemsetup -setusingnetworktime）
├── windows/
│   ├── windows.go           # Windows 根实现
│   ├── file.go              # FileOps（无 symlink）
│   ├── process.go           # ProcessOps（tasklist、taskkill /T /F）
│   ├── package.go           # PackageOps（wua/MSI，默认 ErrUnsupported）
│   ├── service.go           # ServiceOps（sc query 只读 + sc start/stop）
│   ├── user.go              # UserOps（net user / PowerShell）
│   ├── network.go           # NetworkOps（netsh）
│   ├── firewall.go          # FirewallOps（netsh advfirewall）
│   ├── rlimit.go            # RlimitOps（noop，返回 ErrUnsupported）
│   ├── log.go               # LogOps（EventLog）
│   └── time.go              # TimeOps（w32tm）
├── aix/
│   ├── aix.go               # AIX 根实现（ppc64 大端）
│   ├── package.go           # PackageOps（installp/rpm）
│   ├── service.go           # ServiceOps（SRC: startsrc/stopsrc）
│   ├── log.go               # LogOps（errpt）
│   └── ...
├── solaris/
│   ├── solaris.go           # Solaris 根实现
│   ├── package.go           # PackageOps（pkg/IPS）
│   ├── service.go           # ServiceOps（SMF: svcadm/svcs）
│   └── ...
├── freebsd/
│   ├── freebsd.go           # FreeBSD 根实现
│   ├── package.go           # PackageOps（pkg）
│   ├── service.go           # ServiceOps（rc.d: service <name> start）
│   └── ...
├── harmony/
│   ├── harmony.go           # HarmonyOS 根实现（经 ArkTS 适配层）
│   ├── package.go           # PackageOps（hap/hsp 安装）
│   └── ...
└── openwrt/
    ├── openwrt.go           # OpenWrt 根实现（BusyBox + musl）
    ├── package.go           # PackageOps（opkg）
    ├── service.go           # ServiceOps（procd: /etc/init.d/<name> start）
    └── ...
```

#### 4.3.2 构建标签策略

每个子目录的文件按 OS 隔离，配合 `//go:build` 标签：

```go
// 代码示例：Linux 实现的构建标签（Go）
//go:build linux

package linux

import (
    "runtime"
    _ "unsafe" // 防止 go vet 误报
)

// Linux 是 Linux 平台的 Platform 实现。
type Linux struct {
    distro   DistroInfo
    pkgMgr   PackageOps
    svcMgr   ServiceOps
    fwMgr    FirewallOps
}

func (l *Linux) OS() string   { return "linux" }
func (l *Linux) Arch() string { return runtime.GOARCH }
```

```go
// 代码示例：AIX 实现的构建标签（Go）
//go:build aix

package aix

// AIX 是 AIX 平台的 Platform 实现（ppc64 大端）。
type AIX struct {
    version string // 7.2 / 7.3
}

func (a *AIX) OS() string   { return "aix" }
func (a *AIX) Arch() string { return "ppc64" }
```

#### 4.3.3 工厂函数

```go
// 代码示例：Detect 工厂函数（Go）
//go:build linux || darwin || windows || aix || solaris || freebsd

package platform

import "runtime"

// Detect 按 runtime.GOOS + 发行版探测构造 Platform 实现。
// 未识别的发行版返回 GenericLinux/GenericPOSIX（仅 shell 可用）。
func Detect() (Platform, error) {
    switch runtime.GOOS {
    case "linux":
        return detectLinux()
    case "darwin":
        return detectDarwin(), nil
    case "windows":
        return detectWindows(), nil
    case "aix":
        return detectAIX(), nil
    case "solaris":
        return detectSolaris(), nil
    case "freebsd":
        return detectFreeBSD(), nil
    default:
        return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
    }
}

// detectLinux 读 /etc/os-release 按发行版构造对应实现。
func detectLinux() (Platform, error) {
    d, err := readOSRelease()
    if err != nil {
        // 降级为 GenericLinux（systemd 假定可用）。
        return linux.NewGeneric(), nil
    }
    switch d.Name {
    case "centos", "rhel", "fedora":
        return linux.NewRHELFamily(d), nil
    case "ubuntu", "debian":
        return linux.NewDebianFamily(d), nil
    case "opensuse-leap", "sles", "suse":
        return linux.NewSUSEFamily(d), nil
    case "amzn":
        return linux.NewAmazonLinux(d), nil
    case "alpine":
        return linux.NewAlpine(d), nil
    case "openEuler":
        return linux.NewOpenEuler(d), nil
    case "uos":
        return linux.NewUOS(d), nil
    case "kylin", "openkylin":
        return linux.NewKylin(d), nil
    default:
        return linux.NewGeneric(d), nil
    }
}
```

---

## 第5章 各系统详细支持方案

### 5.1 CentOS / RHEL

#### 5.1.1 概述

CentOS 与 RHEL 同属 Red Hat 家族，共享 yum/dnf 包管理、systemd 服务管理、SELinux 安全模块。CentOS 7 用 yum，CentOS 8/9 与 RHEL 8/9 用 dnf（yum 为 dnf 软链接）。

#### 5.1.2 包管理

- **管理器**：yum（7）/ dnf（8/9）；
- **安装**：`dnf install -y <package>` 或 `yum install -y <package>`；
- **卸载**：`dnf remove -y <package>`；
- **查询**：`rpm -q <package>`；
- **列已装**：`rpm -qa`；
- **更新索引**：`dnf makecache` 或 `yum makecache fast`；
- **仓库管理**：`dnf config-manager --add-repo <url>`（启用 EPEL/PowerTools 等）。

#### 5.1.3 服务管理

- **管理器**：systemd（CentOS 7 起默认，CentOS 6 的 upstart 不支持）；
- **状态**：`systemctl is-active <name>` + `systemctl is-enabled <name>`；
- **启停**：`systemctl start/stop/restart <name>`；
- **开机自启**：`systemctl enable/disable <name>`；
- **重载**：`systemctl daemon-reload`（unit 文件变更后）。

#### 5.1.4 特殊处理：SELinux

SELinux 是 RHEL 系默认强制访问控制模块，影响：

- **文件上下文**：复制文件到非默认目录须 `restorecon -Rv <path>` 恢复上下文；
- **端口绑定**：Agent 监听非标准端口须 `semanage port -a -t http_port_t -p tcp 9090`；
- **布尔开关**：`setsebool -P httpd_can_network_connect 1` 允许 httpd 联网；
- **模式切换**：`setenforce 0` 临时切 Permissive（不推荐生产），`/etc/selinux/config` 永久配置。

PAL 的 `FirewallOps` 与 `FileOps` 须感知 SELinux 模式（`getenforce`），在 Enforcing 模式下自动追加 `restorecon` 与 `semanage` 步骤。

#### 5.1.5 网络管理

- **CentOS 7**：network-scripts（`/etc/sysconfig/network-scripts/ifcfg-<iface>`）；
- **CentOS 8/9 / RHEL 8/9**：NetworkManager（`nmcli`）默认，network-scripts 已弃用；
- **DNS**：`/etc/resolv.conf` 或 systemd-resolved（`/etc/systemd/resolved.conf`）；
- **主机名**：`hostnamectl set-hostname <name>`。

#### 5.1.6 开发需求

- **构建**：linux/amd64 + linux/arm64 交叉编译，CGO_ENABLED=0；
- **依赖**：gopsutil/v3 已支持，无额外 C 依赖；
- **测试**：CI 矩阵须覆盖 CentOS 7（glibc 2.17，最老）与 CentOS 9（glibc 2.34）两端，验证 glibc 版本兼容。

### 5.2 Ubuntu / Debian

#### 5.2.1 概述

Ubuntu 是 Debian 的衍生发行版，共享 apt/dpkg 包管理、systemd 服务管理。Ubuntu 20.04/22.04/24.04 是 LTS 版本，是企业云服务器主流。Debian 11（bullseye）/12（bookworm）是 Ubuntu 的上游。

#### 5.2.2 包管理

- **管理器**：apt（前端）+ dpkg（底层）；
- **安装**：`apt-get install -y <package>`；
- **卸载**：`apt-get remove -y <package>` 或 `apt-get purge -y <package>`（含配置）；
- **查询**：`dpkg -l <package>`；
- **列已装**：`dpkg -l`；
- **更新索引**：`apt-get update`；
- **仓库管理**：`add-apt-repository ppa:<ppa>`（Ubuntu）或编辑 `/etc/apt/sources.list`。

#### 5.2.3 服务管理

- **管理器**：systemd（Ubuntu 16.04/Debian 8 起默认）；
- **命令**：与 RHEL 系完全一致（`systemctl ...`）；
- **特有**：`service <name> <action>` 兼容脚本（经 systemd 转发）。

#### 5.2.4 特殊处理：AppArmor

Ubuntu 默认启用 AppArmor（Debian 可选），影响：

- **配置文件**：`/etc/apparmor.d/<profile>`；
- **模式**：`aa-status` 查询，`aa-complain <profile>` 切抱怨模式，`aa-enforce <profile>` 切强制；
- **Agent 适配**：若 Agent 二进制无 AppArmor profile，AppArmor 默认放行，但建议提供 profile 限制其能力。

#### 5.2.5 网络管理

- **Ubuntu 20.04+**：netplan（`/etc/netplan/*.yaml`），经 `netplan apply` 生效；
- **Debian**：ifupdown（`/etc/network/interfaces`）或 NetworkManager；
- **DNS**：systemd-resolved（`/etc/systemd/resolved.conf`）或 `/etc/resolv.conf`；
- **主机名**：`hostnamectl set-hostname <name>`。

netplan 配置示例：

```yaml
# netplan.yaml
network:
  version: 2
  ethernets:
    eth0:
      dhcp4: false
      addresses:
        - 192.168.1.10/24
      gateway4: 192.168.1.1
      nameservers:
        addresses: [8.8.8.8, 1.1.1.1]
```

#### 5.2.6 开发需求

- **构建**：linux/amd64 + linux/arm64，CGO_ENABLED=0；
- **测试**：CI 矩阵覆盖 Ubuntu 22.04（glibc 2.35）+ Debian 11（glibc 2.31）；
- **PPA**：若分发 deb 包，须提供 PPA 或经 apt 仓库。

### 5.3 SUSE / SLES

#### 5.3.1 概述

SUSE Linux Enterprise Server（SLES）是欧洲企业主流，openSUSE Leap 是其社区版。SLES 15 SP4/SP5 是当前 LTS。共享 systemd 服务管理，使用 zypper 包管理器，AppArmor 默认启用（与 SELinux 互斥）。

#### 5.3.2 包管理

- **管理器**：zypper（前端）+ rpm（底层）；
- **安装**：`zypper install -y <package>`；
- **卸载**：`zypper remove -y <package>`；
- **查询**：`zypper search --installed-only <package>` 或 `rpm -q <package>`；
- **列已装**：`rpm -qa`；
- **更新索引**：`zypper refresh`；
- **仓库管理**：`zypper ar <url> <alias>` 添加仓库，`zypper rr <alias>` 删除。

#### 5.3.3 服务管理

- **管理器**：systemd（SLES 12 起默认）；
- **命令**：与 RHEL/Ubuntu 一致（`systemctl ...`）；
- **特有**：SLES 15 起 `systemctl` 完全替代 `init.d` 脚本。

#### 5.3.4 特殊处理

- **AppArmor**：SUSE 默认启用，配置同 Ubuntu；
- **SELinux**：SUSE 可选启用（默认禁用），与 AppArmor 互斥；
- **SUSE Connect**：SLES 须 `SUSEConnect` 注册订阅方可使用官方仓库，PAL 须检测注册状态。

#### 5.3.5 网络管理

- **SLES 15**：Wicked（`/etc/wicked/ifconfig/<iface>.xml`）默认，NetworkManager 可选；
- **命令**：`wicked ifup <iface>` / `wicked ifdown <iface>`；
- **DNS**：`/etc/resolv.conf` 或 netconfig；
- **主机名**：`hostnamectl set-hostname <name>`。

#### 5.3.6 开发需求

- **构建**：linux/amd64 + linux/arm64，CGO_ENABLED=0；
- **测试**：CI 矩阵覆盖 openSUSE Leap 15.5（社区版，免费）作为 SLES 代理；
- **订阅**：SLES 须 `SUSEConnect -r <code>` 注册，CI 用 openSUSE 规避。

### 5.4 Amazon Linux

#### 5.4.1 概述

Amazon Linux 是 AWS 维护的 EC2 默认镜像，Amazon Linux 2（AL2）基于 CentOS 7，Amazon Linux 2023（AL2023）基于 Fedora 34。共享 systemd 服务管理，使用 dnf/yum 包管理器。

#### 5.4.2 包管理

- **管理器**：yum（AL2）/ dnf（AL2023）；
- **安装**：`dnf install -y <package>`；
- **仓库**：默认启用 `amazon-linux-extras`（AL2）或 `dnf repositories`（AL2023）；
- **特有**：AL2023 仓库锁定到特定版本（`/etc/dnf/vars/releasever`），确定性更新。

#### 5.4.3 服务管理

- **管理器**：systemd（AL2/AL2023 默认）；
- **命令**：与 RHEL 一致。

#### 5.4.4 特殊处理：AWS 集成

- **元数据服务**：Agent 须能查询 IMDSv2（`http://169.254.169.254/latest/meta-data/`）获取 instance-id/region/availability-zone，填充 CMDB；
- **IAM 角色**：EC2 instance profile 提供临时凭证，Agent 可经此访问 S3/SSM；
- **SSM Agent**：Amazon SSM Agent 默认预装，OpsMesh Agent 须与之共存（端口/资源不冲突）；
- **CloudWatch**：日志可推送到 CloudWatch Logs（经 `aws logs put-log-events`）。

#### 5.4.5 网络管理

- **默认**：DHCP，由 EC2 网络元数据驱动；
- **静态 IP**：network-scripts（AL2）或 NetworkManager（AL2023）；
- **DNS**：`/etc/resolv.conf` 由 DHCP 自动管理。

#### 5.4.6 开发需求

- **构建**：linux/amd64 + linux/arm64（Graviton），CGO_ENABLED=0；
- **测试**：CI 矩阵覆盖 AL2 + AL2023，arm64 经 Graviton 实例验证；
- **IMDSv2**：须使用 IMDSv2（强制 token），IMDSv1 默认禁用。

### 5.5 Alpine Linux

#### 5.5.1 概述

Alpine Linux 是容器场景主流镜像基础（< 5MB），使用 musl libc（非 glibc）、apk 包管理器、OpenRC init 系统（非 systemd）。Alpine 3.18+ 是当前稳定版。

#### 5.5.2 包管理

- **管理器**：apk；
- **安装**：`apk add --no-cache <package>`；
- **卸载**：`apk del <package>`；
- **查询**：`apk info <package>`；
- **列已装**：`apk info`；
- **更新索引**：`apk update`；
- **仓库**：`/etc/apk/repositories`。

#### 5.5.3 服务管理

- **管理器**：OpenRC（非 systemd）；
- **状态**：`rc-service <name> status`；
- **启停**：`rc-service <name> start/stop/restart`；
- **开机自启**：`rc-update add/delete <name>`；
- **特有**：Alpine 容器通常不运行 init 系统，service 任务支持降级（仅 shell 可用）。

#### 5.5.4 特殊处理：musl libc

musl 与 glibc 差异影响 Go 二进制：

- **CGO**：默认 CGO_ENABLED=0 时纯 Go 二进制不依赖 libc，musl/glibc 兼容；
- **CGO 依赖**：若引入 C 依赖（如 sqlite3），须用 musl-gcc 交叉编译或 `apk add build-base` 在 Alpine 内构建；
- **DNS 解析**：musl 的 DNS 解析器与 glibc 不同（无 `nsswitch.conf`），纯 Go 经 `net` 包不受影响；
- **时区**：Alpine 默认无 `tzdata`，须 `apk add tzdata` 否则 `time.LoadLocation("Asia/Shanghai")` 失败。

#### 5.5.5 网络管理

- **默认**：DHCP（`udhcpc`）；
- **静态**：`/etc/network/interfaces`（ifupdown）；
- **DNS**：`/etc/resolv.conf`；
- **特有**：容器场景通常由 Docker/K8s 管理网络，Agent 须检测是否在容器内（`/.dockerenv` 或 `/proc/1/cgroup` 含 docker/kubepods）。

#### 5.5.6 开发需求

- **构建**：linux/amd64 + linux/arm64，CGO_ENABLED=0（避免 musl/glibc 兼容问题）；
- **测试**：CI 矩阵覆盖 Alpine 3.18 + 3.19，须验证 musl 下 gopsutil/Go 标准库行为；
- **镜像**：提供 `opsmesh:alpine` 变体（< 20MB），与默认 `opsmesh:latest`（distroless）并行。

### 5.6 openEuler / 统信 UOS / 中标麒麟

#### 5.6.1 概述

国产化 Linux 发行版基于 RHEL/CentOS 系 fork，共享 yum/dnf 包管理、systemd 服务管理、SELinux 安全模块。差异主要在内核补丁、安全模块增强、国产硬件支持（鲲鹏 arm64、飞腾 arm64、龙芯 mips64）。

- **openEuler 22.03 LTS**：华为开源，社区版，支持 amd64/arm64；
- **统信 UOS**：统信软件商业版，基于 Debian/Deepin，支持 amd64/arm64/mips64/loongarch64；
- **中标麒麟**：中标软件商业版，基于 CentOS/RHEL，支持 amd64/arm64；
- **openKylin**：麒麟社区版，基于 Ubuntu，支持 amd64/arm64。

#### 5.6.2 包管理

- **openEuler / 中标麒麟**：dnf/yum（RHEL 系）；
- **统信 UOS / openKylin**：apt/dpkg（Debian 系）；
- **命令**：与对应上游一致。

#### 5.6.3 服务管理

- **管理器**：systemd（全部）；
- **命令**：与上游一致。

#### 5.6.4 特殊处理：国产化安全

- **SELinux**：openEuler/麒麟默认 Enforcing，须提供 Agent 的 SELinux policy 模块；
- **三员分立**：国产化系统常启用三员分立（系统管理员/安全管理员/安全审计员），Agent 须支持以不同角色运行；
- **密评**：商用密码评估要求，Agent 通信须支持国密算法（SM2/SM3/SM4），TLS 须支持国密套件；
- **等保 2.0**：三级等保要求审计日志不可篡改，Agent 须将审计日志写到只追加文件或经 syslog 转发到审计服务器。

#### 5.6.5 网络管理

- **openEuler**：NetworkManager（`nmcli`）默认；
- **统信 UOS / openKylin**：NetworkManager 或 netplan；
- **命令**：与上游一致。

#### 5.6.6 开发需求

- **构建**：linux/amd64 + linux/arm64（鲲鹏/飞腾），CGO_ENABLED=0；
- **国密**：须引入 `github.com/tjfoc/gmsm` 或等价库，经构建标签 `gm` 启用；
- **测试**：CI 矩阵覆盖 openEuler 22.03 LTS（社区版免费），UOS/麒麟须手工验证或经合作伙伴实验室；
- **硬件**：arm64 须在鲲鹏 920/飞腾 S2500 上验证，QEMU 仿真不够（性能计数器/NUMA 差异）。

### 5.7 AIX

#### 5.7.1 概述

AIX 是 IBM Power 架构专有 Unix，当前主流版本 7.2（TL5）/7.3。使用 installp/rpm 包管理器、SRC 服务管理器、errpt 错误报告。Power 架构是 ppc64 大端（与 Linux ppc64le 不同）。

#### 5.7.2 包管理

- **管理器**：installp（AIX 原生）+ rpm（Linux 互操作，AIX Toolbox 提供）；
- **安装**：`installp -aXgd <device> <fileset>` 或 `rpm -i <package>`；
- **卸载**：`installp -e <fileset>` 或 `rpm -e <package>`；
- **查询**：`lslpp -l <fileset>` 或 `rpm -q <package>`；
- **列已装**：`lslpp -l all`；
- **仓库**：AIX Toolbox（yum on AIX 7.1+ 可用）或 NIM。

#### 5.7.3 服务管理

- **管理器**：SRC（System Resource Controller）；
- **状态**：`lssrc -s <subsystem>`；
- **启停**：`startsrc -s <subsystem>` / `stopsrc -s <subsystem>`；
- **开机自启**：编辑 `/etc/inittab` 或经 SRC 注册；
- **特有**：AIX 7.2 起部分服务可经 systemd 管理（AIX 7.3 增强），但 SRC 仍是主流。

#### 5.7.4 特殊处理：Power 架构

- **大端**：ppc64 大端，与 Linux ppc64le 不同，Go 须 `GOARCH=ppc64`（Go 1.26 支持）；
- **字节序**：网络协议/文件格式须显式处理字节序（`encoding/binary.BigEndian`）；
- **性能**：Power SMT8 线程/核，gopsutil 须正确识别；
- **WPAR**：Workload Partition 是 AIX 容器化，Agent 须检测是否在 WPAR 内（`/proc/sys/wpar`）。

#### 5.7.5 网络管理

- **配置**：`/etc/rc.net` 或 `smit tcpip`；
- **命令**：`ifconfig`、`route`（与 BSD 类似，非 iproute2）；
- **DNS**：`/etc/resolv.conf`；
- **主机名**：`hostname <name>` 或 `chdev -l inet0 -a hostname=<name>`。

#### 5.7.6 开发需求

- **构建**：linux/amd64 交叉编译 `GOOS=aix GOARCH=ppc64`，CGO_ENABLED=0；
- **依赖**：gopsutil/v3 对 AIX 支持有限，须验证或补充；
- **测试**：CI 须用 AIX 7.2/7.3 真机或 PowerVM LPAR，无免费 QEMU 仿真；
- **二进制**：AIX XCOFF 格式，Go 产出 ELF，须 `ar` 封装为 XCOFF 或经 AIX 原生 Go 构建。

### 5.8 Solaris

#### 5.8.1 概述

Solaris 11.4 是 Oracle 商业 Unix，使用 IPS（Image Packaging System）包管理器、SMF（Service Management Facility）服务管理器、ZFS 文件系统、Zones 容器化。支持 sparc64 与 x86_64 架构，但 Go 仅支持 solaris/amd64（即 x86_64）。

#### 5.8.2 包管理

- **管理器**：pkg（IPS）；
- **安装**：`pkg install <package>`；
- **卸载**：`pkg uninstall <package>`；
- **查询**：`pkg list <package>`；
- **列已装**：`pkg list`；
- **仓库**：`pkg set-publisher` 配置 IPS 仓库；
- **特有**：IPS 是镜像化包管理，所有包从 BE（Boot Environment）原子安装。

#### 5.8.3 服务管理

- **管理器**：SMF（Service Management Facility）；
- **状态**：`svcs <service>`（state: online/offline/degraded/maintenance）；
- **启停**：`svcadm enable <service>` / `svcadm disable <service>`；
- **重启**：`svcadm restart <service>`；
- **开机自启**：`svcadm enable <service>` 即开机自启；
- **特有**：SMF 自动重启失败服务（经 restarton-fault 配置），日志经 `svcprop` 查询。

#### 5.8.4 特殊处理：ZFS / Zones

- **ZFS**：Solaris 原生文件系统，支持快照/克隆/去重，Agent 文件操作须感知 ZFS（如 `zfs snapshot` 用于备份）；
- **Zones**：Solaris 容器化（非 K8s），Agent 须检测是否在 non-global zone 内（`zonename` 命令），non-global zone 能力受限；
- **DTrace**：动态追踪，Agent 性能采集可经 DTrace 脚本补充（gopsutil 不覆盖）。

#### 5.8.5 网络管理

- **配置**：`ipadm create-addr` / `dladm`；
- **命令**：`ipadm`、`route`（非 iproute2）；
- **DNS**：`/etc/resolv.conf` 或 SMF `dns/client` 服务；
- **主机名**：`hostname <name>`。

#### 5.8.6 开发需求

- **构建**：linux/amd64 交叉编译 `GOOS=solaris GOARCH=amd64`，CGO_ENABLED=0；
- **sparc64**：Go 不支持 solaris/sparc64，须用 x86_64 架构；
- **依赖**：gopsutil/v3 对 Solaris 支持有限，须验证或补充；
- **测试**：CI 须用 Solaris 11.4 x86_64 真机或 VM，无免费仿真；
- **二进制**：Solaris ELF，Go 直接产出。

### 5.9 FreeBSD

#### 5.9.1 概述

FreeBSD 13/14 是 BSD 系主流，用于网络/存储设备（pfSense/TrueNAS 基于 FreeBSD）。使用 pkg 包管理器、rc.d 服务管理脚本、ZFS 文件系统、Jails 容器化。支持 amd64/arm64。

#### 5.9.2 包管理

- **管理器**：pkg（pkgng）；
- **安装**：`pkg install -y <package>`；
- **卸载**：`pkg delete -y <package>`；
- **查询**：`pkg info <package>`；
- **列已装**：`pkg info`；
- **更新索引**：`pkg update`；
- **仓库**：`/etc/pkg/FreeBSD.conf`。

#### 5.9.3 服务管理

- **管理器**：rc.d（BSD init 脚本）；
- **状态**：`service <name> status`；
- **启停**：`service <name> start/stop/restart`；
- **开机自启**：编辑 `/etc/rc.conf` 添加 `<name>_enable="YES"`；
- **特有**：rc.d 脚本支持 `rcorder` 依赖排序，无 systemd 的 socket activation 等价。

#### 5.9.4 特殊处理：Jails / ZFS

- **Jails**：FreeBSD 容器化（chroot 增强），Agent 须检测是否在 Jail 内（`sysctl security.jail.jailed`），Jail 内能力受限（无 mount/ifconfig）；
- **ZFS**：FreeBSD 原生支持 ZFS，Agent 文件操作须感知 ZFS；
- **pf**：FreeBSD 防火墙（pf），与 OpenBSD 共享，PAL 的 `FirewallOps` 须实现 `pfctl`。

#### 5.9.5 网络管理

- **配置**：`/etc/rc.conf`；
- **命令**：`ifconfig`、`route`（非 iproute2）；
- **DNS**：`/etc/resolv.conf`；
- **主机名**：`hostname <name>` 或 `/etc/rc.conf`。

#### 5.9.6 开发需求

- **构建**：linux/amd64 交叉编译 `GOOS=freebsd GOARCH=amd64`，CGO_ENABLED=0；
- **依赖**：gopsutil/v3 对 FreeBSD 支持良好；
- **测试**：CI 须用 FreeBSD 14 真机或 VM（Bhyve/QEMU），GitHub Actions 无 FreeBSD runner，须自建或用 Cirrus CI；
- **二进制**：FreeBSD ELF，Go 直接产出。

### 5.10 HarmonyOS

#### 5.10.1 概述

HarmonyOS 5.0+ 是华为分布式操作系统，用于 IoT/边缘/终端。使用 hap/hsp 应用包格式、Ability 生命周期框架、ArkTS 开发语言。Go 不直接支持 HarmonyOS，须经 OHOS NDK + ArkTS 适配层。

#### 5.10.2 包管理

- **管理器**：hap（Harmony Ability Package）/ hsp（Harmony Shared Package）；
- **安装**：`hdc install <path.hap>`（hdc 是 HarmonyOS Device Connector）；
- **卸载**：`hdc uninstall <bundle>`；
- **查询**：`hdc shell bm dump -n <bundle>`（Bundle Manager）；
- **特有**：hap/hsp 须签名后安装，调试签名经 DevEco Studio 生成。

#### 5.10.3 服务管理

- **管理器**：Ability 框架（非 systemd/init）；
- **状态**：`hdc shell aa dump -a`（Ability Assistant）；
- **启停**：`hdc shell aa start -a <ability> -b <bundle>` / `hdc shell aa force-stop <bundle>`；
- **开机自启**：经 `module.json5` 配置 `abilities[].skills[].uris[]` 与 `extensionAbilities[]`；
- **特有**：Ability 生命周期由系统管理，Agent 须以 ServiceAbility 模式常驻。

#### 5.10.4 特殊处理：ArkTS 适配

- **Go 不支持**：HarmonyOS 无 Go 运行时，须用 ArkTS 重写 Agent 核心或经 NDK + C++ 封装；
- **适配层**：建议用 ArkTS 实现 Agent UI + NAPI 调用 C++ 核心，C++ 核心经 OHOS NDK 编译；
- **gRPC**：HarmonyOS 无原生 gRPC，须用 HTTP/WebSocket 或 NAPI 封装 gRPC-C；
- **文件系统**：HarmonyOS 沙箱化，Agent 仅能访问自身 `getFilesDir()` / `getCacheDir()`；
- **权限**：经 `module.json5` `requestPermissions` 申请 ohos.permission.* 权限。

#### 5.10.5 网络管理

- **配置**：经系统 Settings，Agent 无直接配置能力；
- **命令**：`hdc shell ifconfig`（受限）；
- **DNS**：系统管理；
- **主机名**：不可修改。

#### 5.10.6 开发需求

- **构建**：OHOS NDK + ArkTS，非 Go 交叉编译；
- **依赖**：gopsutil/gRPC 不可用，须用 OHOS API 重写；
- **测试**：须 HarmonyOS 5.0+ 真机或 DevEco Studio 模拟器；
- **分发**：hap 包经华为应用市场或企业分发，非 goreleaser。

### 5.11 OpenWrt

#### 5.11.1 概述

OpenWrt 21.02+ 是嵌入式网络设备主流固件（路由器/网关），使用 opkg 包管理器、procd init 守护进程、BusyBox 用户空间、musl libc。资源受限严重（典型 32MB RAM/8MB Flash）。

#### 5.11.2 包管理

- **管理器**：opkg；
- **安装**：`opkg install <package>`；
- **卸载**：`opkg remove <package>`；
- **查询**：`opkg list-installed | grep <package>`；
- **列已装**：`opkg list-installed`；
- **更新索引**：`opkg update`；
- **仓库**：`/etc/opkg/distfeeds.conf`。

#### 5.11.3 服务管理

- **管理器**：procd + init.d 脚本；
- **状态**：`/etc/init.d/<name> status` 或 `ubus call service list`；
- **启停**：`/etc/init.d/<name> start/stop/restart`；
- **开机自启**：`/etc/init.d/<name> enable/disable`；
- **特有**：procd 是 OpenWrt 专用 init，支持 respawn（自动重启）与 ubus 集成。

#### 5.11.4 特殊处理：BusyBox + musl

- **BusyBox**：所有命令（ps/ls/ash/...）是 BusyBox 多调用二进制，选项与 GNU coreutils 不同（如 `ps` 无 `aux` 选项，用 `ps w`）；
- **musl**：与 Alpine 同，CGO_ENABLED=0 避免兼容问题；
- **资源受限**：Agent 二进制须 < 5MB，禁用调试符号（`-ldflags "-s -w"`）；
- **存储**：Flash 空间紧张，Agent 须支持安装到外置存储（USB/SD）或 overlayfs。

#### 5.11.5 网络管理

- **配置**：UCI（Unified Configuration Interface，`/etc/config/network`）；
- **命令**：`uci set network.<section>.<option>=<value>` + `uci commit network` + `/etc/init.d/network reload`；
- **DNS**：dnsmasq 默认，经 UCI 配置；
- **防火墙**：fw3（基于 iptables/nftables），经 UCI `/etc/config/firewall` 配置。

#### 5.11.6 开发需求

- **构建**：linux/mips + linux/arm 交叉编译，CGO_ENABLED=0，`-ldflags "-s -w"` 最小化；
- **依赖**：gopsutil/v3 须验证 mips 支持，可能需裁剪；
- **测试**：须 OpenWrt 真机或 QEMU mips 仿真；
- **二进制**：须 < 5MB，可能需 UPX 压缩；
- **分发**：opkg 包格式，非 tar.gz。

---

## 第6章 跨平台 CI 矩阵

### 6.1 CI 构建矩阵设计

CI 矩阵须覆盖所有 /平台的构建 + 单元测试，平台的交叉编译验证。当前 `.github/workflows/ci.yml` 仅在 `ubuntu-latest` 单 runner 上构建，须扩展为多 runner 矩阵。

#### 表：CI 构建矩阵设计表

| Job | Runner | GOOS | GOARCH | CGO | 用途 | 触发 |
|---|---|---|---|---|---|---|
| build-linux-amd64 | ubuntu-latest | linux | amd64 | 0 | 主构建 + 单元测试 + 覆盖率 | push/PR |
| build-linux-arm64 | ubuntu-latest（arm64 自托管或 Graviton） | linux | arm64 | 0 | arm64 构建 + 测试 | push/PR |
| build-darwin-amd64 | macos-13 | darwin | amd64 | 0 | macOS Intel 构建 | push/PR |
| build-darwin-arm64 | macos-14 | darwin | arm64 | 0 | macOS Apple Silicon 构建 | push/PR |
| build-windows-amd64 | windows-latest | windows | amd64 | 0 | Windows 构建 + 测试 | push/PR |
| build-windows-arm64 | windows-latest | windows | arm64 | 0 | Windows ARM 构建 | push/PR |
| cross-compile-p2 | ubuntu-latest | aix/solaris/freebsd | ppc64/amd64/arm64 | 0 | 平台交叉编译验证 | push/PR |
| integration-linux | ubuntu-latest + mysql + redis | linux | amd64 | 0 | SQL 集成测试 | push/PR |
| e2e-linux | ubuntu-latest | linux | amd64 | 0 | 端到端测试（docker-compose） | push/PR |
| e2e-windows | windows-latest | windows | amd64 | 0 | Windows 端到端 | release |
| release | ubuntu-latest | linux/darwin/windows | amd64/arm64 | 0 | goreleaser 多平台发布 | tag v* |

### 6.2 交叉编译策略

/P3 平台无原生 CI runner，须交叉编译验证构建通过：

```yaml
# 命令示例：交叉编译验证矩阵（GitHub Actions）
cross-compile-p2:
  runs-on: ubuntu-latest
  strategy:
    matrix:
      include:
        - { goos: aix,     goarch: ppc64  }
        - { goos: solaris, goarch: amd64 }
        - { goos: freebsd, goarch: amd64 }
        - { goos: freebsd, goarch: arm64 }
        - { goos: linux,   goarch: ppc64le }
        - { goos: linux,   goarch: mips64 }
  steps:
    - uses: actions/checkout@v5
    - uses: actions/setup-go@v6
      with:
        go-version: "1.26.6"
    - name: Cross compile
      env:
        GOOS: ${{ matrix.goos }}
        GOARCH: ${{ matrix.goarch }}
        CGO_ENABLED: 0
      run: go build -trimpath -ldflags "-s -w" ./cmd/opsmesh
```

### 6.3 平台特定测试策略

#### 6.3.1 单元测试

单元测试在所有 平台 runner 上执行，须覆盖：

- 平台无关逻辑：在任一 runner 上跑全量；
- 平台特定逻辑：经 `//go:build` 标签隔离，仅在对应平台 runner 上跑；
- 跨平台行为：用接口 mock，不依赖真实平台。

#### 6.3.2 集成测试

集成测试（SQL 后端）仅在 `integration-linux` job 上执行（mysql + redis services），其他平台跳过：

```yaml
# 命令示例：集成测试平台门禁（GitHub Actions）
integration:
  runs-on: ubuntu-latest
  services:
    mysql: { image: mysql:8, ... }
    redis: { image: redis:7, ... }
  steps:
    - run: go test -tags integration -timeout 300s ./internal/store/...
```

#### 6.3.3 端到端测试

端到端测试在 Linux + Windows 上执行，覆盖控制面 + Agent 完整闭环：

- **Linux**：docker-compose 启动控制面 + Agent，验证注册/心跳/任务执行；
- **Windows**：Windows runner 上直接启动 controlplane + agent 二进制，验证同上；
- **macOS**：开发者本机手工验证，不纳入 CI（macOS runner 资源紧张）。

### 6.4 二进制分发策略

#### 6.4.1 发布产物矩阵

```yaml
# 命令示例：goreleaser 多平台发布配置（goreleaser.yml）
builds:
  - id: opsmesh
    main: ./cmd/opsmesh
    binary: opsmesh
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
      - freebsd
    goarch:
      - amd64
      - arm64
      - ppc64
    ignore:
      - { goos: darwin,   goarch: arm64, goarm: "" }  # macOS ARM 不需 goarm
      - { goos: windows,  goarch: ppc64 }              # Windows 无 ppc64
      - { goos: darwin,   goarch: ppc64 }              # macOS 无 ppc64
      - { goos: freebsd,  goarch: ppc64 }              # FreeBSD 无 ppc64
    ldflags:
      - -s -w -trimpath
      - -X opsmesh/internal/version.Version={{.Version}}
      - -X opsmesh/internal/version.Commit={{.ShortCommit}}
      - -X opsmesh/internal/version.Date={{.Date}}
```

#### 6.4.2 产物命名与归档

| 平台 | 归档格式 | 命名模板 |
|---|---|---|
| Linux/FreeBSD | tar.gz | `opsmesh-<version>-<os>-<arch>.tar.gz` |
| macOS | tar.gz | `opsmesh-<version>-darwin-<arch>.tar.gz` |
| Windows | zip | `opsmesh-<version>-windows-<arch>.zip` |
| AIX/Solaris | tar.gz | `opsmesh-<version>-<os>-<arch>.tar.gz` |

#### 6.4.3 校验与 SBOM

- **checksums.txt**：所有产物的 SHA256 校验和（goreleaser `checksum` 段）；
- **SBOM**：每个归档的 Software Bill of Materials（经 syft 生成）；
- **签名**：经 cosign 签名（Sigstore），可选验证。

---

## 第7章 Agent 跨平台构建

### 7.1 交叉编译命令矩阵

#### 表：交叉编译命令矩阵表

| 目标平台 | 命令 | 备注 |
|---|---|---|
| linux/amd64 | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` | 主构建 |
| linux/arm64 | `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build` | ARM 服务器 |
| linux/ppc64le | `GOOS=linux GOARCH=ppc64le CGO_ENABLED=0 go build` | Power Little Endian |
| linux/mips64 | `GOOS=linux GOARCH=mips64 CGO_ENABLED=0 go build` | 龙芯 |
| darwin/amd64 | `GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build` | macOS Intel |
| darwin/arm64 | `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build` | Apple Silicon |
| windows/amd64 | `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build` | Windows 服务器 |
| windows/arm64 | `GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build` | Windows ARM |
| aix/ppc64 | `GOOS=aix GOARCH=ppc64 CGO_ENABLED=0 go build` | AIX Power |
| solaris/amd64 | `GOOS=solaris GOARCH=amd64 CGO_ENABLED=0 go build` | Solaris x86_64 |
| freebsd/amd64 | `GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 go build` | FreeBSD |
| freebsd/arm64 | `GOOS=freebsd GOARCH=arm64 CGO_ENABLED=0 go build` | FreeBSD ARM |

### 7.2 CGO 依赖处理

OpsMesh 当前 `go.mod` 依赖中，CGO 相关：

- **纯 Go 依赖**（CGO_ENABLED=0 兼容）：grpc、mysql driver（纯 Go）、redis（纯 Go）、gopsutil（部分 CGO）、k8s client-go（纯 Go）；
- **潜在 CGO 依赖**：
  - `github.com/go-sql-driver/mysql`：纯 Go，无 CGO；
  - `github.com/shirou/gopsutil/v3`：部分平台用 CGO（如 Linux CPU 型号），但纯 Go 路径可用；
  - `github.com/hashicorp/vault/api`：纯 Go HTTP 客户端；
  - 国密库 `github.com/tjfoc/gmsm`：纯 Go。

**结论**：OpsMesh 全程 `CGO_ENABLED=0` 可行，所有目标平台纯 Go 静态链接，无 C 依赖。若未来引入 sqlite3（CGO），须：

- 交叉编译时用对应平台的 C 工具链（如 `musl-gcc` for Alpine）；
- 或用 `modernc.org/sqlite`（纯 Go sqlite 实现）规避 CGO。

### 7.3 静态链接策略

`CGO_ENABLED=0` 时 Go 产出纯静态二进制，无 libc 依赖。验证：

```bash
# 命令示例：验证二进制静态链接
file opsmesh-linux-amd64
# 输出: ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked, ...

ldd opsmesh-linux-amd64
# 输出: not a dynamic executable
```

ldflags 优化：

- `-s`：去除符号表；
- `-w`：去除 DWARF 调试信息；
- `-trimpath`：去除构建机路径（可复现构建）。

### 7.4 二进制大小优化

#### 表：二进制大小优化措施表

| 措施 | 节省 | 副作用 |
|---|---|---|
| `-ldflags "-s -w"` | ~30% | 丢失符号/调试信息，panic stack trace 无行号 |
| `-trimpath` | < 1% | 可复现构建，路径不可追溯 |
| `CGO_ENABLED=0` | 0 | 静态链接，无 libc 依赖 |
| UPX 压缩 | ~50% | 启动慢 ~100ms，反病毒误报 |
| 裁剪不依赖模块 | 视情况 | 须持续监控 go.mod |

典型大小（`opsmesh` 单二进制，含控制面 + Agent）：

| 平台 | 优化前 | `-s -w` 后 | UPX 后 |
|---|---|---|---|
| linux/amd64 | ~45 MB | ~32 MB | ~16 MB |
| linux/arm64 | ~42 MB | ~30 MB | ~15 MB |
| windows/amd64 | ~46 MB | ~33 MB | ~17 MB |

> OpenWrt 场景须 UPX 压缩至 < 5MB，或裁剪控制面代码仅保留 Agent。

### 7.5 构建脚本设计

```bash
# 命令示例：多平台构建脚本（build.sh）
#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-dev}"
LDFLAGS="-s -w -trimpath -X opsmesh/internal/version.Version=${VERSION}"
LDFLAGS="${LDFLAGS} -X opsmesh/internal/version.Commit=$(git rev-parse --short HEAD)"
LDFLAGS="${LDFLAGS} -X opsmesh/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"

PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
  "linux/ppc64le"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/arm64"
  "aix/ppc64"
  "solaris/amd64"
  "freebsd/amd64"
  "freebsd/arm64"
)

for platform in "${PLATFORMS[@]}"; do
  IFS="/" read -r goos goarch <<< "$platform"
  output="dist/opsmesh-${VERSION}-${goos}-${goarch}"
  if [ "$goos" = "windows" ]; then
    output="${output}.exe"
  fi
  echo "构建 ${platform} -> ${output}"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -ldflags "$LDFLAGS" -o "$output" ./cmd/opsmesh
done

echo "生成 checksums"
cd dist && sha256sum opsmesh-* > checksums.txt
```

---

## 第8章 平台特定配置

### 8.1 配置项的平台差异

#### 表：配置项平台差异表

| 配置项 | Linux | macOS | Windows | AIX | Solaris | FreeBSD |
|---|---|---|---|---|---|---|
| `--data-dir` 默认 | `/var/lib/opsmesh` | `~/Library/Application Support/opsmesh` | `%PROGRAMDATA%\opsmesh` | `/var/opt/opsmesh` | `/var/opt/opsmesh` | `/var/db/opsmesh` |
| `--config` 默认 | `/etc/opsmesh/config.yaml` | `~/Library/Preferences/opsmesh/config.yaml` | `%PROGRAMDATA%\opsmesh\config.yaml` | `/etc/opsmesh/config.yaml` | `/etc/opsmesh/config.yaml` | `/usr/local/etc/opsmesh/config.yaml` |
| `--log-file` 默认 | `/var/log/opsmesh/agent.log` | `~/Library/Logs/opsmesh/agent.log` | `%PROGRAMDATA%\opsmesh\logs\agent.log` | `/var/adm/opsmesh/agent.log` | `/var/log/opsmesh/agent.log` | `/var/log/opsmesh/agent.log` |
| `--pid-file` 默认 | `/var/run/opsmesh/agent.pid` | `/tmp/opsmesh-agent.pid` | N/A（经服务管理） | `/var/run/opsmesh-agent.pid` | `/var/run/opsmesh-agent.pid` | `/var/run/opsmesh-agent.pid` |
| `--user` 默认 | `opsmesh` | `opsmesh` | `LocalService` | `opsmesh` | `opsmesh` | `opsmesh` |
| `--shell` 默认 | `/bin/sh` | `/bin/sh` | `cmd.exe` | `/usr/bin/sh` | `/usr/bin/sh` | `/bin/sh` |

### 8.2 路径约定的平台差异

#### 8.2.1 配置文件路径

```text
图：配置文件路径约定

Linux:        /etc/opsmesh/config.yaml
macOS:        ~/Library/Preferences/opsmesh/config.yaml
Windows:      %PROGRAMDATA%\opsmesh\config.yaml
AIX:          /etc/opsmesh/config.yaml
Solaris:      /etc/opsmesh/config.yaml
FreeBSD:      /usr/local/etc/opsmesh/config.yaml
```

#### 8.2.2 数据目录

```text
图：数据目录约定

Linux:        /var/lib/opsmesh/
macOS:        ~/Library/Application Support/opsmesh/
Windows:      %PROGRAMDATA%\opsmesh\
AIX:          /var/opt/opsmesh/
Solaris:      /var/opt/opsmesh/
FreeBSD:      /var/db/opsmesh/
```

#### 8.2.3 日志目录

```text
图：日志目录约定

Linux:        /var/log/opsmesh/
macOS:        ~/Library/Logs/opsmesh/
Windows:      %PROGRAMDATA%\opsmesh\logs\
AIX:          /var/adm/opsmesh/        # AIX 习惯用 /var/adm
Solaris:      /var/log/opsmesh/
FreeBSD:      /var/log/opsmesh/
```

### 8.3 默认值的平台差异

#### 表：默认值平台差异表

| 配置项 | Linux | macOS | Windows | 备注 |
|---|---|---|---|---|
| `--workers` | 4 | 4 | 4 | 与 CPU 核心数无关 |
| `--task-timeout` | 30m | 30m | 30m | 单任务超时 |
| `--max-output-bytes` | 10MB | 10MB | 10MB | stdout/stderr 截断 |
| `--grpc-port` | 9090 | 9090 | 9090 | gRPC 通道端口 |
| `--metrics-port` | 9091 | 9091 | 9091 | Prometheus 指标端口 |
| `--http-port` | 8080 | 8080 | 8080 | 控制面 HTTP 端口 |
| `--max-procs` | 0（不限制） | 0 | 0（无效） | RLIMIT_NPROC |
| `--max-files` | 0 | 0 | 0（无效） | RLIMIT_NOFILE |
| `--max-memory-mb` | 0 | 0 | 0（无效） | RLIMIT_AS |
| `--log-collect-paths` | `/var/log/messages,/var/log/syslog` | `/var/log/system.log` | `EventLog` | 系统日志路径 |

### 8.4 示例配置文件

#### 8.4.1 Linux 示例

```yaml
# opsmesh-agent.yaml（Linux）
mode: agent
control-addrs: "https://control.opsmesh.internal:9090"
data-dir: /var/lib/opsmesh
log-file: /var/log/opsmesh/agent.log
workers: 8
task-timeout: 30m
max-output-bytes: 10485760
tls-cert: /etc/opsmesh/tls/agent.crt
tls-key: /etc/opsmesh/tls/agent.key
client-ca: /etc/opsmesh/tls/ca.crt
grpc-signature-key: ${OPSMESH_GRPC_SIGNATURE_KEY}
log-collect-paths: "/var/log/messages,/var/log/syslog,/var/log/audit/audit.log"
log-collect-interval: 30s
otel-endpoint: "otel.opsmesh.internal:4317"
otel-service-name: "opsmesh-agent"
```

#### 8.4.2 Windows 示例

```yaml
# opsmesh-agent.yaml（Windows）
mode: agent
control-addrs: "https://control.opsmesh.internal:9090"
data-dir: 'C:\ProgramData\opsmesh'
log-file: 'C:\ProgramData\opsmesh\logs\agent.log'
workers: 8
task-timeout: 30m
max-output-bytes: 10485760
tls-cert: 'C:\ProgramData\opsmesh\tls\agent.crt'
tls-key: 'C:\ProgramData\opsmesh\tls\agent.key'
client-ca: 'C:\ProgramData\opsmesh\tls\ca.crt'
grpc-signature-key: ${OPSMESH_GRPC_SIGNATURE_KEY}
log-collect-paths: "EventLog"
log-collect-interval: 30s
otel-endpoint: "otel.opsmesh.internal:4317"
otel-service-name: "opsmesh-agent"
```

#### 8.4.3 macOS 示例

```yaml
# opsmesh-agent.yaml（macOS）
mode: agent
control-addrs: "https://control.opsmesh.internal:9090"
data-dir: /Users/opsmesh/Library/Application Support/opsmesh
log-file: /Users/opsmesh/Library/Logs/opsmesh/agent.log
workers: 4
task-timeout: 30m
tls-cert: /Users/opsmesh/Library/Application Support/opsmesh/tls/agent.crt
tls-key: /Users/opsmesh/Library/Application Support/opsmesh/tls/agent.key
client-ca: /Users/opsmesh/Library/Application Support/opsmesh/tls/ca.crt
grpc-signature-key: ${OPSMESH_GRPC_SIGNATURE_KEY}
log-collect-paths: "/var/log/system.log"
```

---

## 第9章 已知限制与风险

### 9.1 各平台的已知限制

#### 表：各平台已知限制表

| 平台 | 已知限制 | 影响 | 缓解 |
|---|---|---|---|
| Linux（所有发行版） | service 任务依赖 systemctl，非 systemd 发行版（如 Alpine）不可用 | service 任务前置拒绝 | PAL 检测 init 系统，OpenRC 降级 |
| Linux（CentOS 7） | glibc 2.17，部分新 Go API 可能不兼容 | 编译失败 | CI 矩阵覆盖 CentOS 7 |
| Linux（Alpine） | musl libc，DNS 解析行为差异 | 网络相关测试失败 | CGO_ENABLED=0 + tzdata 包 |
| macOS | service 任务不支持（launchd 模型不同） | service 任务前置拒绝 | 文档明示，引导用 launchctl |
| macOS | rlimit RLIMIT_AS 设置过低致进程被杀 | Agent 崩溃 | 默认不开启 RLIMIT_AS |
| Windows | 无 rlimit 等价物 | 进程限额不可用 | 控制面/IAM 侧兜底 |
| Windows | 无 symlink（需开发者模式或管理员） | 文件操作受限 | PAL 检测能力，降级 |
| Windows | taskkill /T /F 不保证杀孙进程 | 孤儿后台进程 | 文档明示，建议用 Job Object |
| AIX | ppc64 大端，与 Linux ppc64le 不同 | 字节序相关代码须显式处理 | 编码规范要求显式字节序 |
| AIX | gopsutil 对 AIX 支持有限 | 部分指标采集失败 | 降级，缺失指标为零值 |
| AIX | Go 产出 ELF，AIX 期望 XCOFF | 二进制可能无法执行 | 须 AIX 原生 Go 构建 |
| Solaris | Go 不支持 sparc64 | sparc64 平台不支持 | 须用 x86_64 架构 |
| Solaris | gopsutil 对 Solaris 支持有限 | 同 AIX | 同 AIX |
| FreeBSD | GitHub Actions 无 FreeBSD runner | CI 矩阵覆盖难 | 用 Cirrus CI 或自建 runner |
| FreeBSD | rc.d 无 socket activation | 服务管理能力弱 | 文档明示 |
| HarmonyOS | Go 不支持 | 须重写 Agent | 用 ArkTS + NAPI |
| HarmonyOS | 无原生 gRPC | 通信须替代 | 用 HTTP/WebSocket |
| OpenWrt | 资源受限（32MB RAM） | Agent 须极简 | UPX 压缩 + 裁剪 |
| OpenWrt | BusyBox 命令选项与 GNU 不同 | shell 任务行为差异 | 文档明示，提供兼容脚本 |

### 9.2 Go 运行时支持状态

#### 表：Go 1.26 运行时支持状态表

| OS/架构 | Go 1.26 支持 | 状态 | 备注 |
|---|---|---|---|
| linux/amd64 | ✓ | 一等公民 | 主构建平台 |
| linux/arm64 | ✓ | 一等公民 | ARM 服务器 |
| linux/ppc64le | ✓ | 二等公民 | Power Little Endian |
| linux/riscv64 | ✓ | 实验性 | RISC-V |
| darwin/amd64 | ✓ | 一等公民 | macOS Intel |
| darwin/arm64 | ✓ | 一等公民 | Apple Silicon |
| windows/amd64 | ✓ | 一等公民 | Windows |
| windows/arm64 | ✓ | 二等公民 | Windows ARM |
| aix/ppc64 | ✓ | 二等公民 | AIX 7.2+ |
| solaris/amd64 | ✓ | 二等公民 | Solaris 11.4+ |
| freebsd/amd64 | ✓ | 二等公民 | FreeBSD 13+ |
| freebsd/arm64 | ✓ | 实验性 | FreeBSD 13+ ARM |
| linux/sparc64 | ✗ | 不支持 | 须用 x86_64 |
| linux/mips64 | ✓ | 实验性 | 龙芯 |
| harmonyos/* | ✗ | 不支持 | 须用 ArkTS |
| openwrt/mips | △ | 间接 | 经 linux/mips + musl |

### 9.3 第三方库兼容性矩阵

#### 表：第三方库兼容性矩阵表

| 库 | linux | darwin | windows | aix | solaris | freebsd | 备注 |
|---|---|---|---|---|---|---|---|
| google.golang.org/grpc | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 纯 Go |
| github.com/go-sql-driver/mysql | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 纯 Go |
| github.com/redis/go-redis/v9 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 纯 Go |
| github.com/shirou/gopsutil/v3 | ✓ | ✓ | ✓ | △ | △ | ✓ | AIX/Solaris 部分缺失 |
| go.opentelemetry.io/otel | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 纯 Go |
| golang.org/x/sys | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 官方扩展 |
| k8s.io/client-go | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 纯 Go |
| github.com/hashicorp/vault/api | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 纯 Go |
| github.com/segmentio/kafka-go | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 纯 Go |
| github.com/tjfoc/gmsm（国密） | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 纯 Go，按需引入 |

> 结论：除 gopsutil 在 AIX/Solaris 上部分指标缺失外，所有依赖纯 Go，全平台兼容。gopsutil 缺失指标经降级处理（零值 + 警告日志）。

### 9.4 安全模块兼容性

#### 表：安全模块兼容性表

| 安全模块 | 平台 | 影响 | Agent 适配 |
|---|---|---|---|
| SELinux | RHEL/CentOS/Fedora/openEuler/麒麟 | 文件上下文/端口/布尔 | PAL 感知 Enforcing 模式，自动 restorecon/semanage |
| AppArmor | Ubuntu/Debian/SUSE | 进程能力限制 | 提供 AppArmor profile 模板 |
| firewalld | RHEL/CentOS/Fedora | 端口放行 | PAL FirewallOps 实现 firewalld |
| ufw | Ubuntu/Debian | 端口放行 | PAL FirewallOps 实现 ufw |
| iptables/nftables | 所有 Linux | 端口放行 | PAL FirewallOps 实现 iptables/nftables |
| pf | macOS/FreeBSD/OpenBSD | 端口放行 | PAL FirewallOps 实现 pfctl |
| Windows Firewall | Windows | 端口放行 | PAL FirewallOps 实现 netsh advfirewall |
| ipf | Solaris | 端口放行 | PAL FirewallOps 实现 ipf |
| 三员分立 | 国产化 | 角色权限 | Agent 支持以不同角色运行 |
| 国密（SM2/SM3/SM4） | 国产化 | TLS/签名 | 经构建标签 `gm` 启用国密库 |
| 等保 2.0 | 国产化 | 审计不可篡改 | 审计日志只追加 + syslog 转发 |

---

## 第10章 路线图

### 10.1 Phase 1：Linux / macOS / Windows（已完成）

**时间**：v1.0 基线（2026-08-17）

**交付物**：

- Linux（CentOS/Ubuntu/Debian）amd64/arm64 完整支持；
- macOS amd64/arm64 shell + 文件 + rlimit 支持（service 不支持）；
- Windows amd64 shell + 文件 + sc query 只读支持（service/rlimit 不支持）；
- goreleaser 发布 linux/amd64 + linux/arm64 产物；
- CI 矩阵：ubuntu-latest 单 runner，build + test + vet + lint + coverage。

**已实现代码**：

- `internal/agent/exec_unix.go` + `exec_other.go`：进程组管理跨平台隔离；
- `internal/agent/rlimit_unix.go` + `rlimit_other.go`：资源限额跨平台隔离；
- `internal/agent/agent.go` `capabilityNote`：启动期能力矩阵显式输出；
- `internal/agent/metrics_collect.go` `queryService`：服务状态跨平台查询。

**遗留**：

- 平台判断点散落在业务代码（8 处 `runtime.GOOS` 字面量比较），未收敛到 PAL；
- macOS/Windows 未纳入 goreleaser 发布矩阵；
- 包管理未抽象为 Agent 能力。

### 10.2 Phase 2：SUSE / Amazon Linux / Alpine

**时间**：v1.1（计划 2026-09）

**目标**：

- SUSE/SLES 15 amd64/arm64 完整支持（zypper + AppArmor）；
- Amazon Linux 2/2023 amd64/arm64 完整支持（dnf + AWS IMDSv2 集成）；
- Alpine 3.18+ amd64/arm64 支持（apk + OpenRC，service 降级）；
- 引入 `internal/agent/platform/` PAL 骨架，收敛现有平台判断点。

**关键工作**：

1. 设计并实现 `PlatformCapability` 接口与 `Detect()` 工厂；
2. 实现 `linux/` 子包，按发行版分支（RHELFamily/DebianFamily/SUSEFamily/AmazonLinux/Alpine）；
3. 实现 `darwin/`、`windows/` 子包，迁移现有平台特定代码；
4. 扩展 `.goreleaser.yml` 至 linux/darwin/windows × amd64/arm64；
5. 扩展 CI 矩阵至多 runner（ubuntu/macos/windows）；
6. 端到端验证：SUSE Leap 15.5、Amazon Linux 2023、Alpine 3.19。

**验收**：

- 业务包内 `runtime.GOOS` 字面量比较 ≤ 2 处；
- CI 矩阵覆盖 6 个平台/架构组合；
- 端到端测试在 SUSE/Amazon/Alpine 各通过一轮。

### 10.3 Phase 3：国产化系统验证

**时间**：v1.2（计划 2026-10）

**目标**：

- openEuler 22.03 LTS amd64/arm64 完整支持；
- 统信 UOS / 中标麒麟 / openKylin amd64/arm64 完整支持；
- 国密（SM2/SM3/SM4）TLS 支持（经构建标签 `gm`）；
- 等保 2.0 三级审计日志不可篡改。

**关键工作**：

1. 实现 `linux/` 子包的 openEuler/UOS/Kylin 分支；
2. 引入 `github.com/tjfoc/gmsm` 国密库，经 `//go:build gm` 标签启用；
3. 实现 TLS 国密套件（SM2 ECDHE-SM4-SM3）；
4. 审计日志只追加模式 + syslog 转发到审计服务器；
5. SELinux policy 模块（Agent 的 .te/.fc 文件）；
6. 端到端验证：openEuler 22.03 LTS（社区版免费），UOS/麒麟经合作伙伴实验室。

**验收**：

- openEuler 22.03 LTS CI 矩阵覆盖；
- 国密 TLS 经商用密码检测中心检测；
- 等保 2.0 三级测评通过（或经合作伙伴互认）。

### 10.4 Phase 4：AIX / Solaris / FreeBSD

**时间**：v1.3（计划 2026-12）

**目标**：

- AIX 7.2/7.3 ppc64 支持（installp + SRC + errpt）；
- Solaris 11.4 amd64 支持（IPS + SMF + ZFS）；
- FreeBSD 13/14 amd64/arm64 支持（pkg + rc.d + Jails）；
- 平台交叉编译 CI 验证。

**关键工作**：

1. 实现 `aix/`、`solaris/`、`freebsd/` 子包；
2. 扩展 gopsutil 补充 AIX/Solaris 缺失指标（或降级）；
3. AIX ppc64 大端字节序处理规范；
4. Solaris SMF + Zones 集成；
5. FreeBSD Jails 检测与降级；
6. CI 矩阵增加 cross-compile-p2 job；
7. 端到端验证：AIX 7.3（PowerVM LPAR）、Solaris 11.4（VM）、FreeBSD 14（Bhyve）。

**验收**：

- 交叉编译在 aix/ppc64、solaris/amd64、freebsd/amd64+arm64 全部通过；
- 端到端测试在 AIX/Solaris/FreeBSD 各通过一轮（shell + 文件 + 心跳）；
- gopsutil 缺失指标降级文档化。

### 10.5 Phase 5：HarmonyOS / OpenWrt

**时间**：v2.0（计划 2027-Q1）

**目标**：

- HarmonyOS 5.0+ arm64 支持（hap + Ability + ArkTS 适配层）；
- OpenWrt 21.02+ mips/arm 支持（opkg + procd + BusyBox）；
- IoT/边缘场景 Agent 极简化（< 5MB）。

**关键工作**：

1. HarmonyOS Agent 用 ArkTS 重写核心 + NAPI 调用 C++ 共享库；
2. HarmonyOS 通信改用 HTTP/WebSocket（无 gRPC）；
3. OpenWrt Agent 极简构建（裁剪控制面 + UPX 压缩）；
4. OpenWrt BusyBox 兼容性测试；
5. 端到端验证：HarmonyOS 5.0 真机、OpenWrt 22.03 真机。

**验收**：

- HarmonyOS hap 包可安装并常驻；
- OpenWrt Agent 二进制 < 5MB；
- 端到端测试在 HarmonyOS/OpenWrt 各通过一轮（shell + 心跳）。

### 10.6 路线图总览

```text
图：多 OS 支持路线图

  Phase 1 (v1.0, 已完成)          Phase 2 (v1.1, 2026-09)
  ┌─────────────────────┐         ┌─────────────────────┐
  │ Linux  CentOS/Ubuntu│         │ SUSE/SLES 15        │
  │        Debian       │ ──────► │ Amazon Linux 2/2023 │
  │ macOS  12+          │         │ Alpine 3.18+        │
  │ Windows Server 2016+│         │ PAL 骨架 + 收敛     │
  └─────────────────────┘         └─────────────────────┘
                                          │
                                          ▼
  Phase 3 (v1.2, 2026-10)         Phase 4 (v1.3, 2026-12)
  ┌─────────────────────┐         ┌─────────────────────┐
  │ openEuler 22.03+    │         │ AIX 7.2/7.3 ppc64   │
  │ 统信 UOS            │ ──────► │ Solaris 11.4 amd64  │
  │ 中标麒麟/openKylin  │         │ FreeBSD 13/14       │
  │ 国密 + 等保 2.0     │         │ 交叉编译 CI         │
  └─────────────────────┘         └─────────────────────┘
                                          │
                                          ▼
                                 Phase 5 (v2.0, 2027-Q1)
                                 ┌─────────────────────┐
                                 │ HarmonyOS 5.0+ arm64│
                                 │ OpenWrt 21.02+ mips │
                                 │ IoT/边缘极简化      │
                                 └─────────────────────┘
```

---

## 附录

### 附录 A：平台检测代码示例

#### A.1 发行版探测（Linux）

```go
// 代码示例：发行版探测函数（Go）
package platform

import (
    "bufio"
    "os"
    "strings"
)

// readOSRelease 读取 /etc/os-release（LSB 标准化）返回发行版信息。
// 文件不存在或解析失败返回 ErrUnknownDistro，调用方降级为 GenericLinux。
func readOSRelease() (DistroInfo, error) {
    f, err := os.Open("/etc/os-release")
    if err != nil {
        return DistroInfo{}, err
    }
    defer f.Close()

    var d DistroInfo
    scanner := bufio.NewScanner(f)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if strings.HasPrefix(line, "#") || line == "" {
            continue
        }
        parts := strings.SplitN(line, "=", 2)
        if len(parts) != 2 {
            continue
        }
        key := parts[0]
        val := strings.Trim(parts[1], `"`)
        switch key {
        case "ID":
            d.Name = val
        case "VERSION_ID":
            d.Version = val
        case "VERSION_CODENAME":
            d.Codename = val
        }
    }
    return d, scanner.Err()
}
```

#### A.2 平台检测（macOS）

```go
// 代码示例：macOS 平台检测（Go）
//go:build darwin

package platform

import (
    "os/exec"
    "strings"
)

func detectDarwin() Platform {
    version := ""
    if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
        version = strings.TrimSpace(string(out))
    }
    return &darwin.Darwin{Version: version}
}
```

#### A.3 平台检测（Windows）

```go
// 代码示例：Windows 平台检测（Go）
//go:build windows

package platform

import (
    "os/exec"
    "strings"
)

func detectWindows() Platform {
    version := ""
    if out, err := exec.Command("cmd", "/C", "ver").Output(); err == nil {
        version = strings.TrimSpace(string(out))
    }
    return &windows.Windows{Version: version}
}
```

#### A.4 平台检测（AIX）

```go
// 代码示例：AIX 平台检测（Go）
//go:build aix

package platform

import (
    "os/exec"
    "strings"
)

func detectAIX() Platform {
    version := ""
    if out, err := exec.Command("oslevel").Output(); err == nil {
        version = strings.TrimSpace(string(out))
    }
    return &aix.AIX{Version: version}
}
```

### 附录 B：交叉编译脚本模板

#### B.1 完整多平台构建脚本

```bash
#!/usr/bin/env bash
# 代码示例：多平台构建脚本（Bash）
set -euo pipefail

VERSION="${1:-dev}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w -trimpath"
LDFLAGS="${LDFLAGS} -X opsmesh/internal/version.Version=${VERSION}"
LDFLAGS="${LDFLAGS} -X opsmesh/internal/version.Commit=${COMMIT}"
LDFLAGS="${LDFLAGS} -X opsmesh/internal/version.Date=${DATE}"

MAIN="./cmd/opsmesh"
DIST="dist"
mkdir -p "$DIST"

# 平台矩阵：GOOS/GOARCH/GOARM（GOARM 仅 arm64 用）
declare -a PLATFORMS=(
  "linux/amd64/"
  "linux/arm64/"
  "linux/ppc64le/"
  "linux/mips64/"
  "darwin/amd64/"
  "darwin/arm64/"
  "windows/amd64/"
  "windows/arm64/"
  "aix/ppc64/"
  "solaris/amd64/"
  "freebsd/amd64/"
  "freebsd/arm64/"
)

for entry in "${PLATFORMS[@]}"; do
  IFS="/" read -r goos goarch goarm <<< "$entry"
  ext=""
  if [ "$goos" = "windows" ]; then ext=".exe"; fi
  output="${DIST}/opsmesh-${VERSION}-${goos}-${goarch}${ext}"
  echo "==> 构建 ${goos}/${goarch} -> ${output}"
  env GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" CGO_ENABLED=0 \
    go build -ldflags "$LDFLAGS" -o "$output" "$MAIN"
done

echo "==> 生成 checksums"
( cd "$DIST" && sha256sum opsmesh-* > checksums.txt )

echo "==> 完成"
ls -lh "$DIST"
```

#### B.2 单平台验证脚本

```bash
#!/usr/bin/env bash
# 代码示例：单平台构建并验证（Bash）
set -euo pipefail
GOOS="${1:?usage: $0 <goos> <goarch>}"
GOARCH="${2:?usage: $0 <goos> <goarch>}"

echo "==> 交叉编译 ${GOOS}/${GOARCH}"
env GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 \
  go build -o "/tmp/opsmesh-${GOOS}-${GOARCH}" ./cmd/opsmesh

echo "==> 验证文件类型"
file "/tmp/opsmesh-${GOOS}-${GOARCH}"

echo "==> 验证静态链接"
if [ "$GOOS" = "windows" ]; then
  echo "(Windows PE，跳过 ldd)"
else
  ldd "/tmp/opsmesh-${GOOS}-${GOARCH}" || true
fi

echo "==> 验证大小"
ls -lh "/tmp/opsmesh-${GOOS}-${GOARCH}"
```

### 附录 C：各平台包管理命令对照表

#### 表：各平台包管理命令对照表

| 平台 | 管理器 | 安装 | 卸载 | 查询 | 列已装 | 更新索引 |
|---|---|---|---|---|---|---|
| CentOS/RHEL 7 | yum | `yum install -y <p>` | `yum remove -y <p>` | `rpm -q <p>` | `rpm -qa` | `yum makecache fast` |
| CentOS/RHEL 8/9 | dnf | `dnf install -y <p>` | `dnf remove -y <p>` | `rpm -q <p>` | `rpm -qa` | `dnf makecache` |
| Ubuntu/Debian | apt | `apt-get install -y <p>` | `apt-get remove -y <p>` | `dpkg -l <p>` | `dpkg -l` | `apt-get update` |
| SUSE/SLES | zypper | `zypper install -y <p>` | `zypper remove -y <p>` | `zypper search -i <p>` | `rpm -qa` | `zypper refresh` |
| Amazon Linux 2 | yum | `yum install -y <p>` | `yum remove -y <p>` | `rpm -q <p>` | `rpm -qa` | `yum makecache` |
| Amazon Linux 2023 | dnf | `dnf install -y <p>` | `dnf remove -y <p>` | `rpm -q <p>` | `rpm -qa` | `dnf makecache` |
| Alpine | apk | `apk add --no-cache <p>` | `apk del <p>` | `apk info <p>` | `apk info` | `apk update` |
| openEuler/UOS/麒麟 | dnf/yum | `dnf install -y <p>` | `dnf remove -y <p>` | `rpm -q <p>` | `rpm -qa` | `dnf makecache` |
| AIX | installp/rpm | `installp -aXgd <d> <f>` | `installp -e <f>` | `lslpp -l <f>` | `lslpp -l all` | N/A |
| Solaris | pkg (IPS) | `pkg install <p>` | `pkg uninstall <p>` | `pkg list <p>` | `pkg list` | `pkg refresh` |
| FreeBSD | pkg | `pkg install -y <p>` | `pkg delete -y <p>` | `pkg info <p>` | `pkg info` | `pkg update` |
| macOS | brew | `brew install <p>` | `brew uninstall <p>` | `brew list <p>` | `brew list` | `brew update` |
| Windows | wua/MSI | `msiexec /i <p>` | `msiexec /x <p>` | `wmic product get` | `wmic product list` | N/A |
| HarmonyOS | hap | `hdc install <p>.hap` | `hdc uninstall <b>` | `hdc shell bm dump -n <b>` | `hdc shell bm dump -a` | N/A |
| OpenWrt | opkg | `opkg install <p>` | `opkg remove <p>` | `opkg list-installed \| grep <p>` | `opkg list-installed` | `opkg update` |

### 附录 D：各平台服务管理命令对照表

#### 表：各平台服务管理命令对照表

| 平台 | 管理器 | 状态 | 启动 | 停止 | 重启 | 开机自启 | 取消自启 |
|---|---|---|---|---|---|---|---|
| RHEL/CentOS/Ubuntu/Debian/SUSE/Amazon/openEuler/UOS/麒麟 | systemd | `systemctl is-active <s>` | `systemctl start <s>` | `systemctl stop <s>` | `systemctl restart <s>` | `systemctl enable <s>` | `systemctl disable <s>` |
| Alpine | OpenRC | `rc-service <s> status` | `rc-service <s> start` | `rc-service <s> stop` | `rc-service <s> restart` | `rc-update add <s>` | `rc-update del <s>` |
| macOS | launchd | `launchctl list <s>` | `launchctl start <s>` | `launchctl stop <s>` | `launchctl kickstart -k <s>` | `launchctl load -w <plist>` | `launchctl unload -w <plist>` |
| Windows | sc | `sc query <s>` | `sc start <s>` | `sc stop <s>` | `sc stop <s> && sc start <s>` | `sc config <s> start= auto` | `sc config <s> start= demand` |
| AIX | SRC | `lssrc -s <s>` | `startsrc -s <s>` | `stopsrc -s <s>` | `stopsrc -s <s> && startsrc -s <s>` | 编辑 `/etc/inittab` | 编辑 `/etc/inittab` |
| Solaris | SMF | `svcs <s>` | `svcadm enable <s>` | `svcadm disable <s>` | `svcadm restart <s>` | `svcadm enable <s>` | `svcadm disable <s>` |
| FreeBSD | rc.d | `service <s> status` | `service <s> start` | `service <s> stop` | `service <s> restart` | `sysrc <s>_enable=YES` | `sysrc <s>_enable=NO` |
| HarmonyOS | Ability | `hdc shell aa dump -a` | `hdc shell aa start -a <a> -b <b>` | `hdc shell aa force-stop <b>` | `hdc shell aa force-stop <b> && hdc shell aa start ...` | `module.json5` 配置 | `module.json5` 配置 |
| OpenWrt | procd | `/etc/init.d/<s> status` | `/etc/init.d/<s> start` | `/etc/init.d/<s> stop` | `/etc/init.d/<s> restart` | `/etc/init.d/<s> enable` | `/etc/init.d/<s> disable` |

---

## 变更记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-17 | 初版：完整 10 章 + 4 附录，覆盖 Phase 1-5 全部目标平台 |