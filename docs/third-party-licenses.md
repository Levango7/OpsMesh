# 第三方依赖许可证清单（自动生成，勿手改）

> 生成命令：`bash deploy/scripts/gen-third-party-licenses.sh`（CI 用 `--check` 阻止文档与 go.sum 漂移）  
> 生成时间：2026-10-09T07:04:51Z  
> 依赖来源：根模块 + `services/*/go.mod` + `operator/go.mod` 的全部 `go.sum`（162 个 module@version）  
> 许可证识别：读本地 Go 模块缓存里各模块自带的 LICENSE 文件，按签名表分类为常见 SPDX 名。

## 本清单**不做**的事（必须人判）

- 不判定「某个许可证能否随 OpsMesh 商用分发」——那是商务 + 法务决定（P1-7）。
- 下面「需法务确认」段只是**把风险面摊开**：copyleft（GPL/LGPL/AGPL/MPL/CDDL/EPL）与识别失败项。
- 静态链接/动态插件、是否随镜像分发、源码提供义务等边界，都改变结论，脚本不猜。

## 汇总

| 许可证 | 模块数 |
|---|---|
| `Apache-2.0` | 50 |
| `MIT` | 47 |
| `BSD-3-Clause` | 31 |
| `MPL-2.0` | 24 |
| `BSD-2-Clause` | 9 |
| `ISC` | 1 |

## 需法务确认（24 个）

| 许可证 | 模块 | 版本 | 依赖类型 | 为什么要看它 |
|---|---|---|---|---|
| `MPL-2.0` | `github.com/go-sql-driver/mysql` | v1.10.0 | 直接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/errwrap` | v1.1.0 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/go-cleanhttp` | v0.5.2 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/go-multierror` | v1.1.1 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/go-plugin` | v1.6.0 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/go-retryablehttp` | v0.7.8 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/go-rootcerts` | v1.0.2 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/go-secure-stdlib/parseutil` | v0.2.0 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/go-secure-stdlib/strutil` | v0.1.2 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/go-sockaddr` | v1.0.7 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/go-uuid` | v1.0.3 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/go-version` | v1.6.0 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/hcl` | v1.0.1-vault-7 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/hcl/v2` | v2.20.1 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/logutils` | v1.0.0 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/terraform-plugin-go` | v0.23.0 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/terraform-plugin-log` | v0.9.0 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/terraform-plugin-sdk/v2` | v2.34.0 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/terraform-registry-address` | v0.2.3 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/terraform-svchost` | v0.1.1 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/vault/api` | v1.23.0 | 直接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/hashicorp/yamux` | v0.1.1 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/shoenig/go-m1cpu` | v0.1.6 | 间接 | copyleft：可能有衍生/源码提供义务 |
| `MPL-2.0` | `github.com/shoenig/test` | v0.6.4 | 间接 | copyleft：可能有衍生/源码提供义务 |

## 上游自带 NOTICE 的模块（7 个）

Apache-2.0 §4(d) 是**可机械核实**的义务（不是判断题）：再分发的产物含上游 NOTICE 时，
必须把其中的署名收进自己的 NOTICE。汇编命令：
`bash deploy/scripts/gen-third-party-licenses.sh --emit-notice`（写仓库根 `NOTICE`，逐个逐字保留）。

| 模块 | 版本 | 上游 NOTICE 文件名 |
|---|---|---|
| `github.com/agext/levenshtein` | v1.2.2 | `NOTICE` |
| `github.com/prometheus/client_golang` | v1.19.1 | `NOTICE` |
| `github.com/prometheus/client_model` | v0.6.1 | `NOTICE` |
| `github.com/prometheus/common` | v0.55.0 | `NOTICE` |
| `github.com/prometheus/procfs` | v0.15.1 | `NOTICE` |
| `google.golang.org/grpc` | v1.83.2 | `NOTICE.txt` |
| `gopkg.in/yaml.v3` | v3.0.1 | `NOTICE` |

## 同一模块存在多个版本（6 个）

上表**按模块取语义最高版本**判定许可证。以下是 `go.sum` 里同时出现多个版本的模块——
低版本若许可证不同，本清单不会体现，故逐个列出供核对。
另需注意：`go.sum` 记录的是**曾参与解析**的版本集合，它是实际构建清单（`go list -m all`）的超集，
不等于「这些代码都进了产物」。要精确到产物级依赖，用镜像 SBOM（release.yml 的 syft 产物）。

| 模块 | go.sum 内全部版本 | 本清单判定用 |
|---|---|---|
| `golang.org/x/net` | v0.58.0, v0.59.0, v0.60.0 | `v0.60.0` |
| `golang.org/x/sync` | v0.22.0, v0.23.0 | `v0.23.0` |
| `golang.org/x/sys` | v0.47.0, v0.48.0 | `v0.48.0` |
| `golang.org/x/term` | v0.45.0, v0.46.0 | `v0.46.0` |
| `golang.org/x/text` | v0.41.0, v0.42.0 | `v0.42.0` |
| `golang.org/x/tools` | v0.49.0, v0.50.0 | `v0.50.0` |

## 全量清单

### `Apache-2.0`（50）

| 模块 | 版本 | 依赖类型 |
|---|---|---|
| `github.com/agext/levenshtein` | v1.2.2 | 间接 |
| `github.com/bufbuild/protocompile` | v0.4.0 | 间接 |
| `github.com/go-jose/go-jose/v4` | v4.1.4 | 间接 |
| `github.com/go-logr/logr` | v1.4.4 | 间接 |
| `github.com/go-logr/stdr` | v1.2.2 | 间接 |
| `github.com/go-logr/zapr` | v1.3.0 | 间接 |
| `github.com/go-openapi/jsonpointer` | v0.21.0 | 间接 |
| `github.com/go-openapi/jsonreference` | v0.20.2 | 间接 |
| `github.com/go-openapi/swag` | v0.23.0 | 间接 |
| `github.com/google/btree` | v1.1.3 | 间接 |
| `github.com/google/gnostic-models` | v0.6.8 | 间接 |
| `github.com/google/gofuzz` | v1.2.0 | 间接 |
| `github.com/google/pprof` | v0.0.0-20241029153458-d1b30febd7db | 间接 |
| `github.com/jhump/protoreflect` | v1.15.1 | 间接 |
| `github.com/modern-go/concurrent` | v0.0.0-20180306012644-bacd9c7ef1dd | 间接 |
| `github.com/modern-go/reflect2` | v1.0.2 | 间接 |
| `github.com/oklog/run` | v1.0.0 | 间接 |
| `github.com/prometheus/client_golang` | v1.19.1 | 间接 |
| `github.com/prometheus/client_model` | v0.6.1 | 间接 |
| `github.com/prometheus/common` | v0.55.0 | 间接 |
| `github.com/prometheus/procfs` | v0.15.1 | 间接 |
| `github.com/tklauser/numcpus` | v0.6.1 | 间接 |
| `github.com/xdg-go/pbkdf2` | v1.0.0 | 间接 |
| `github.com/xdg-go/scram` | v1.1.2 | 间接 |
| `github.com/xdg-go/stringprep` | v1.0.4 | 间接 |
| `go.opentelemetry.io/auto/sdk` | v1.2.1 | 间接 |
| `go.opentelemetry.io/otel` | v1.45.0 | 直接 |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace` | v1.45.0 | 间接 |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | v1.45.0 | 直接 |
| `go.opentelemetry.io/otel/exporters/stdout/stdouttrace` | v1.45.0 | 直接 |
| `go.opentelemetry.io/otel/metric` | v1.45.0 | 间接 |
| `go.opentelemetry.io/otel/sdk` | v1.45.0 | 直接 |
| `go.opentelemetry.io/otel/sdk/metric` | v1.45.0 | 间接 |
| `go.opentelemetry.io/otel/trace` | v1.45.0 | 直接 |
| `go.opentelemetry.io/proto/otlp` | v1.11.0 | 间接 |
| `gomodules.xyz/jsonpatch/v2` | v2.4.0 | 间接 |
| `google.golang.org/appengine` | v1.6.8 | 间接 |
| `google.golang.org/genproto/googleapis/api` | v0.0.0-20260803160001-6ac0973c030d | 间接 |
| `google.golang.org/genproto/googleapis/rpc` | v0.0.0-20260818201246-1b0934165a6f | 间接 |
| `google.golang.org/grpc` | v1.83.2 | 直接 |
| `k8s.io/api` | v0.32.0 | 直接 |
| `k8s.io/apiextensions-apiserver` | v0.32.0 | 间接 |
| `k8s.io/apimachinery` | v0.32.0 | 直接 |
| `k8s.io/client-go` | v0.32.0 | 直接 |
| `k8s.io/klog/v2` | v2.130.1 | 间接 |
| `k8s.io/kube-openapi` | v0.0.0-20241105132330-32ad38e42d3f | 间接 |
| `k8s.io/utils` | v0.0.0-20241104100929-3ea5e8cea738 | 间接 |
| `sigs.k8s.io/controller-runtime` | v0.20.0 | 直接 |
| `sigs.k8s.io/json` | v0.0.0-20241010143419-9aa6b5e7a4b3 | 间接 |
| `sigs.k8s.io/structured-merge-diff/v4` | v4.4.2 | 间接 |

### `BSD-2-Clause`（9）

| 模块 | 版本 | 依赖类型 |
|---|---|---|
| `github.com/DATA-DOG/go-sqlmock` | v1.5.0 | 直接 |
| `github.com/pkg/errors` | v0.9.1 | 间接 |
| `github.com/pmezard/go-difflib` | v1.0.1-0.20181226105442-5d4384ee4fb2 | 间接 |
| `github.com/redis/go-redis/v9` | v9.22.0 | 直接 |
| `github.com/vmihailenco/msgpack` | v4.0.4+incompatible | 间接 |
| `github.com/vmihailenco/msgpack/v5` | v5.4.1 | 间接 |
| `github.com/vmihailenco/tagparser/v2` | v2.0.0 | 间接 |
| `github.com/zeebo/xxh3` | v1.1.0 | 间接 |
| `gopkg.in/check.v1` | v1.0.0-20201130134442-10cb98267c6c | 间接 |

### `BSD-3-Clause`（31）

| 模块 | 版本 | 依赖类型 |
|---|---|---|
| `filippo.io/edwards25519` | v1.2.0 | 间接 |
| `github.com/evanphx/json-patch` | v4.12.0+incompatible | 间接 |
| `github.com/evanphx/json-patch/v5` | v5.9.0 | 间接 |
| `github.com/fsnotify/fsnotify` | v1.10.1 | 直接 |
| `github.com/gogo/protobuf` | v1.3.2 | 间接 |
| `github.com/golang/protobuf` | v1.5.4 | 间接 |
| `github.com/google/go-cmp` | v0.7.0 | 间接 |
| `github.com/google/uuid` | v1.6.0 | 直接 |
| `github.com/grpc-ecosystem/grpc-gateway/v2` | v2.29.0 | 间接 |
| `github.com/klauspost/compress` | v1.15.9 | 间接 |
| `github.com/lufia/plan9stats` | v0.0.0-20211012122336-39d0f177ccd0 | 间接 |
| `github.com/munnerz/goautoneg` | v0.0.0-20191010083416-a7dc8b61c822 | 间接 |
| `github.com/pierrec/lz4/v4` | v4.1.15 | 间接 |
| `github.com/rogpeppe/go-internal` | v1.14.1 | 间接 |
| `github.com/shirou/gopsutil/v3` | v3.24.5 | 直接 |
| `github.com/spf13/pflag` | v1.0.5 | 间接 |
| `github.com/tklauser/go-sysconf` | v0.3.12 | 间接 |
| `golang.org/x/crypto` | v0.57.0 | 直接 |
| `golang.org/x/mod` | v0.41.0 | 间接 |
| `golang.org/x/net` | v0.60.0 | 间接 |
| `golang.org/x/oauth2` | v0.36.0 | 间接 |
| `golang.org/x/sync` | v0.23.0 | 间接 |
| `golang.org/x/sys` | v0.48.0 | 直接 |
| `golang.org/x/term` | v0.46.0 | 间接 |
| `golang.org/x/text` | v0.42.0 | 间接 |
| `golang.org/x/time` | v0.12.0 | 直接 |
| `golang.org/x/tools` | v0.50.0 | 间接 |
| `gonum.org/v1/gonum` | v0.17.0 | 间接 |
| `google.golang.org/protobuf` | v1.36.11 | 直接 |
| `gopkg.in/evanphx/json-patch.v4` | v4.12.0 | 间接 |
| `gopkg.in/inf.v0` | v0.9.1 | 间接 |

### `ISC`（1）

| 模块 | 版本 | 依赖类型 |
|---|---|---|
| `github.com/davecgh/go-spew` | v1.1.2-0.20180830191138-d8f796af33cc | 间接 |

### `MIT`（47）

| 模块 | 版本 | 依赖类型 |
|---|---|---|
| `github.com/andybalholm/brotli` | v1.1.0 | 直接 |
| `github.com/apparentlymart/go-textseg/v15` | v15.0.0 | 间接 |
| `github.com/beorn7/perks` | v1.0.1 | 间接 |
| `github.com/bsm/ginkgo/v2` | v2.12.0 | 间接 |
| `github.com/bsm/gomega` | v1.27.10 | 间接 |
| `github.com/cenkalti/backoff/v4` | v4.3.0 | 间接 |
| `github.com/cenkalti/backoff/v5` | v5.0.3 | 间接 |
| `github.com/cespare/xxhash/v2` | v2.3.0 | 间接 |
| `github.com/emicklei/go-restful/v3` | v3.11.0 | 间接 |
| `github.com/fatih/color` | v1.18.0 | 间接 |
| `github.com/fxamacker/cbor/v2` | v2.7.0 | 间接 |
| `github.com/go-ole/go-ole` | v1.2.6 | 间接 |
| `github.com/go-task/slim-sprig/v3` | v3.0.0 | 间接 |
| `github.com/go-test/deep` | v1.1.1 | 间接 |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | 直接 |
| `github.com/hashicorp/go-cty` | v1.4.1-0.20200414143053-d3edf31b6320 | 间接 |
| `github.com/hashicorp/go-hclog` | v1.6.3 | 间接 |
| `github.com/josharian/intern` | v1.0.0 | 间接 |
| `github.com/json-iterator/go` | v1.1.12 | 间接 |
| `github.com/klauspost/cpuid/v2` | v2.2.10 | 间接 |
| `github.com/kr/pretty` | v0.3.1 | 间接 |
| `github.com/kr/text` | v0.2.0 | 间接 |
| `github.com/mailru/easyjson` | v0.7.7 | 间接 |
| `github.com/mattn/go-colorable` | v0.1.14 | 间接 |
| `github.com/mattn/go-isatty` | v0.0.20 | 间接 |
| `github.com/mitchellh/copystructure` | v1.2.0 | 间接 |
| `github.com/mitchellh/go-homedir` | v1.1.0 | 间接 |
| `github.com/mitchellh/go-testing-interface` | v1.14.1 | 间接 |
| `github.com/mitchellh/go-wordwrap` | v1.0.1 | 间接 |
| `github.com/mitchellh/mapstructure` | v1.5.0 | 间接 |
| `github.com/mitchellh/reflectwalk` | v1.0.2 | 间接 |
| `github.com/onsi/ginkgo/v2` | v2.21.0 | 间接 |
| `github.com/onsi/gomega` | v1.35.1 | 间接 |
| `github.com/power-devops/perfstat` | v0.0.0-20210106213030-5aafc221ea8c | 间接 |
| `github.com/ryanuber/go-glob` | v1.0.0 | 间接 |
| `github.com/segmentio/kafka-go` | v0.4.51 | 直接 |
| `github.com/stretchr/testify` | v1.11.1 | 间接 |
| `github.com/x448/float16` | v0.8.4 | 间接 |
| `github.com/yusufpapurcu/wmi` | v1.2.4 | 间接 |
| `github.com/zclconf/go-cty` | v1.14.4 | 间接 |
| `github.com/zclconf/go-cty-debug` | v0.0.0-20191215020915-b22d67c1ba0b | 间接 |
| `go.uber.org/atomic` | v1.11.0 | 间接 |
| `go.uber.org/goleak` | v1.3.0 | 间接 |
| `go.uber.org/multierr` | v1.11.0 | 间接 |
| `go.uber.org/zap` | v1.27.0 | 间接 |
| `gopkg.in/yaml.v3` | v3.0.1 | 间接 |
| `sigs.k8s.io/yaml` | v1.4.0 | 间接 |

### `MPL-2.0`（24）

| 模块 | 版本 | 依赖类型 |
|---|---|---|
| `github.com/go-sql-driver/mysql` | v1.10.0 | 直接 |
| `github.com/hashicorp/errwrap` | v1.1.0 | 间接 |
| `github.com/hashicorp/go-cleanhttp` | v0.5.2 | 间接 |
| `github.com/hashicorp/go-multierror` | v1.1.1 | 间接 |
| `github.com/hashicorp/go-plugin` | v1.6.0 | 间接 |
| `github.com/hashicorp/go-retryablehttp` | v0.7.8 | 间接 |
| `github.com/hashicorp/go-rootcerts` | v1.0.2 | 间接 |
| `github.com/hashicorp/go-secure-stdlib/parseutil` | v0.2.0 | 间接 |
| `github.com/hashicorp/go-secure-stdlib/strutil` | v0.1.2 | 间接 |
| `github.com/hashicorp/go-sockaddr` | v1.0.7 | 间接 |
| `github.com/hashicorp/go-uuid` | v1.0.3 | 间接 |
| `github.com/hashicorp/go-version` | v1.6.0 | 间接 |
| `github.com/hashicorp/hcl` | v1.0.1-vault-7 | 间接 |
| `github.com/hashicorp/hcl/v2` | v2.20.1 | 间接 |
| `github.com/hashicorp/logutils` | v1.0.0 | 间接 |
| `github.com/hashicorp/terraform-plugin-go` | v0.23.0 | 间接 |
| `github.com/hashicorp/terraform-plugin-log` | v0.9.0 | 间接 |
| `github.com/hashicorp/terraform-plugin-sdk/v2` | v2.34.0 | 间接 |
| `github.com/hashicorp/terraform-registry-address` | v0.2.3 | 间接 |
| `github.com/hashicorp/terraform-svchost` | v0.1.1 | 间接 |
| `github.com/hashicorp/vault/api` | v1.23.0 | 直接 |
| `github.com/hashicorp/yamux` | v0.1.1 | 间接 |
| `github.com/shoenig/go-m1cpu` | v0.1.6 | 间接 |
| `github.com/shoenig/test` | v0.6.4 | 间接 |

## 基础镜像（另一条供应链线，不在此清单）

Go 依赖不等于镜像内容：镜像里还有 `FROM` 的操作系统包。见
`Dockerfile` / `Dockerfile.agent` / `Dockerfile.service` / `deploy/docker/Dockerfile.*` 的 `FROM`
（已钉 digest，由 Renovate 维护），以及 Trivy 扫描结果——两侧的许可证义务不同（镜像内系统包
通常触发 GPL/LGPL 的**二进制再分发**条款，需单独结论）。

