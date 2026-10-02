#!/usr/bin/env bash
# OpsMesh 部署资产一致性门禁
#
# 目的：把「部署清单与源码/版本/镜像契约漂移」这类问题挡在 CI，而不是留到真机部署。
# 本脚本只做静态校验（不启动任何容器/集群），可在本地与 CI 复用。
#
# 校验项：
#   1. 版本源一致性：Chart.yaml / values-production.yaml / gitops segment / compose 镜像 tag
#   2. 服务矩阵对齐：release.yml matrix ↔ services/ 目录 ↔ Helm values.services 键
#   3. Helm 渲染：helm lint + 多种 values 组合 template（含 ServiceMonitor 门禁）
#   4. Compose 渲染：两份 compose（+反代 overlay）可渲染，项目名为 opsmesh*，
#      且不存在「所连网络全为 internal + 声明宿主端口」的静默失效组合
#   5. K8s 样例清单：kubeconform 校验（未安装则跳过并提示）
#   6. 行尾一致性：部署资产（Dockerfile/脚本/Helm/compose）不得含 CR
#   7. 镜像发布名 ↔ chart 引用一致性（发布名集合来自 CI，单一事实源）
#   8. 构建上下文自洽：Dockerfile 字面 COPY 源必须存在且不被 .gitignore 排除
#   9. 提交内容不得含**行内**孤立 CR（会随 blob 推送、被渲染器当换行）
#  10. 镜像矩阵每个服务的构建目标必须存在（cmd/<svc> + package main），豁免表须带理由
#  11. 抓取配置 ↔ 服务能力一致性（prometheus.yml 的 job ↔ 源码 /metrics 注册行，双向）
#
# 用法：bash deploy/scripts/validate-deploy-assets.sh
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "$ROOT" || exit 1

PASS=0; FAIL=0; SKIP=0
ok()   { echo "  [PASS] $*"; PASS=$((PASS+1)); }
bad()  { echo "  [FAIL] $*"; FAIL=$((FAIL+1)); }
skip() { echo "  [SKIP] $*"; SKIP=$((SKIP+1)); }
sec()  { echo ""; echo "=== $* ==="; }

# YAML 解析依赖 Python：CI（ubuntu-latest）通常只有 python3，本地 mac/Windows 可能只有 python。
PY=""
for _c in python3 python; do
    if command -v "$_c" >/dev/null 2>&1; then PY="$_c"; break; fi
done

# helm 在本地/CI 多以「docker run -v "$PWD:/apps" -w /apps alpine/helm」包装提供。
# Git Bash（MSYS）会把该容器内路径改写成 Windows 路径（`-w /apps` → `-w C:/.../git/apps`），
# 导致 helm 渲染报 "working directory ... is invalid" 的**假失败**。
# 故仅对 helm 调用关闭路径改写——宿主侧路径（--env-file、python 读 $RENDER_BIN）必须保留
# 默认改写，否则本地 Windows 的原生 docker.exe / python.exe 会读不到 /tmp 下的临时文件。
run_helm() {
    MSYS_NO_PATHCONV=1 helm "$@"
}

# ---------------------------------------------------------------
sec "1. 版本源一致性（期望全部 = Chart.yaml version）"
# ---------------------------------------------------------------
CHART_VERSION="$(grep -E '^version:' deploy/helm/opsmesh/Chart.yaml | head -1 | awk '{print $2}' | tr -d '"'"'"' ')"
if [[ -z "$CHART_VERSION" ]]; then
    bad "无法从 deploy/helm/opsmesh/Chart.yaml 读取 version"
else
    ok "Chart.yaml version = ${CHART_VERSION}"
fi

check_kv() {
    local file="$1" pattern="$2" label="$3"
    local v
    v="$(grep -E "$pattern" "$file" 2>/dev/null | head -1 | sed -E 's/.*"([^"]*)".*/\1/')"
    if [[ -z "$v" ]]; then
        bad "${label}：在 ${file} 未匹配到 ${pattern}"
    elif [[ "$v" = "$CHART_VERSION" ]]; then
        ok "${label} = ${v}"
    else
        bad "${label} = ${v}（期望 ${CHART_VERSION}）—— ${file}"
    fi
}
check_kv deploy/helm/opsmesh/Chart.yaml                    '^appVersion:'      "Chart.yaml appVersion"
check_kv deploy/helm/opsmesh/values-production.yaml        '^[[:space:]]*tag:[[:space:]]*"' "values-production controlplane 镜像 tag"
check_kv deploy/gitops/segments/production-segment.yaml    '^[[:space:]]*tag:[[:space:]]*"' "gitops production-segment 镜像 tag"

# compose 的镜像 tag 来自 .env 的 OPSMESH_VERSION，这里校验默认值/示例值不写死旧版本
if grep -qE 'image:[[:space:]]*opsmesh/[a-z-]+:latest' deploy/docker/docker-compose.prod.yml; then
    bad "docker-compose.prod.yml 仍使用 :latest 镜像 tag（不可追溯）"
else
    ok "docker-compose.prod.yml 未使用 :latest"
fi
if grep -qE 'OPSMESH_VERSION' deploy/docker/scripts/deploy.sh; then
    ok "deploy.sh 引用 OPSMESH_VERSION 作为镜像 tag 源"
else
    bad "deploy.sh 未引用 OPSMESH_VERSION（镜像 tag 来源不明）"
fi

# ---------------------------------------------------------------
sec "2. 服务矩阵对齐"
# ---------------------------------------------------------------
RELEASE_SVCS="$(grep -A40 'matrix:' .github/workflows/release.yml \
    | grep -E '^\s+-\s+\S+$' \
    | sed -E 's/^\s+-\s+//' | sort)"
DIR_SVCS="$(ls services/ 2>/dev/null | sort)"
# 豁免表：`services/` 下有模块、但**不是常驻服务**因而不出镜像的东西。必须带理由，
# 否则"豁免"就成了第二个静默漂移的入口。与 release.yml 矩阵注释同源。
NON_SERVICE="tf-provider|Terraform 插件（main.go 里 plugin.Serve）：装进容器会在启动瞬间打印 \"This binary is a plugin\" 并以码 1 退出 ⇒ 出镜像=发布一个必定 CrashLoop 的产物"
NS_NAMES="$(printf '%s\n' "$NON_SERVICE" | cut -d'|' -f1 | sed '/^$/d' | sort)"
CHART_SVCS="$(grep -E '^  [a-z_]+_?[a-z_]*:' deploy/helm/opsmesh/values.yaml \
    | sed -E 's/^  ([a-z_]+):.*/\1/' | grep -E '_svc$' | sort)"

n_rel="$(echo "$RELEASE_SVCS" | grep -c . || true)"
n_dir="$(echo "$DIR_SVCS" | grep -c . || true)"
n_cht="$(echo "$CHART_SVCS" | grep -c . || true)"
echo "  release.yml=${n_rel}  services/=${n_dir}  chart=${n_cht}  非服务豁免=$(echo "$NS_NAMES" | grep -c . || true)"

only_rel="$(comm -23 <(echo "$RELEASE_SVCS") <(echo "$DIR_SVCS") | tr '\n' ' ')"
# 比对的是「矩阵 ∪ 豁免表」而不是矩阵本身：这样"新增一个非服务模块"仍会被本节逼着表态
# （进矩阵或进豁免表），而不是靠放宽对齐规则来消红。
only_dir="$(comm -13 <(sort -u <(printf '%s\n' "$RELEASE_SVCS") <(printf '%s\n' "$NS_NAMES")) <(echo "$DIR_SVCS") | tr '\n' ' ')"
if [[ -z "${only_rel// }" && -z "${only_dir// }" ]]; then
    ok "release.yml matrix（∪ 豁免表）与 services/ 目录完全对齐（${n_rel} 个镜像 + $(echo "$NS_NAMES" | grep -c . || true) 个豁免）"
else
    [[ -n "${only_rel// }" ]] && bad "release.yml 有但 services/ 无目录：${only_rel}"
    [[ -n "${only_dir// }" ]] && bad "services/ 有目录，但既不在 release.yml 矩阵也不在豁免表：${only_dir}"
fi

# chart 未覆盖的服务是允许的（例如纯 CLI 工具），但必须显式声明为豁免，避免静默漂移
CHART_MISSING="$(comm -23 <(echo "$DIR_SVCS") <(sed 's/_/-/g' <<<"$CHART_SVCS") | tr '\n' ' ')"
if [[ -n "${CHART_MISSING// }" ]]; then
    echo "  [INFO] 未纳入 Helm chart 的服务：${CHART_MISSING}"
    echo "         （若非刻意豁免，请在 values.yaml 的 services 中补齐）"
fi

# ---------------------------------------------------------------
sec "3. Helm 渲染"
# ---------------------------------------------------------------
if command -v helm >/dev/null 2>&1; then
    if run_helm lint deploy/helm/opsmesh >/dev/null 2>&1; then
        ok "helm lint 通过"
    else
        bad "helm lint 失败："
        run_helm lint deploy/helm/opsmesh 2>&1 | tail -10 | sed 's/^/         /'
    fi

    # 3a 默认 values 必须可渲染
    if run_helm template t deploy/helm/opsmesh >/dev/null 2>&1; then
        ok "helm template（默认 values）渲染通过"
    else
        bad "helm template（默认 values）渲染失败："
        run_helm template t deploy/helm/opsmesh 2>&1 | tail -10 | sed 's/^/         /'
    fi

    # 3b 生产 values 必须可渲染
    if run_helm template t deploy/helm/opsmesh -f deploy/helm/opsmesh/values-production.yaml >/dev/null 2>&1; then
        ok "helm template（values-production）渲染通过"
    else
        bad "helm template（values-production）渲染失败："
        run_helm template t deploy/helm/opsmesh -f deploy/helm/opsmesh/values-production.yaml 2>&1 | tail -10 | sed 's/^/         /'
    fi

    # 3c ServiceMonitor 门禁：期望数量**由 values.yaml 派生**（控制面 + 所有 metrics: true 的服务）。
    # 此前把 4 硬编码在断言里（控制面 + device/task/alert），一旦有服务补上 /metrics 就必然漂移。
    # 现在"哪些服务暴露 /metrics"只有两个事实源：values 的 metrics 字段 + 服务源码的注册行，
    # 而后者的正确性由第 11 节（抓取配置 ↔ 服务能力）来核对。
    EXPECT_SM="$(( $(grep -cE '^    metrics: true' deploy/helm/opsmesh/values.yaml || true) + 1 ))"
    RENDER_BIN="$(mktemp)"
    if run_helm template t deploy/helm/opsmesh \
        --set observability.serviceMonitor.enabled=true \
        --set services.auth_svc.enabled=true \
        --set services.device_svc.enabled=true \
        --set services.task_svc.enabled=true \
        --set services.alert_svc.enabled=true \
        --set services.config_svc.enabled=true \
        --set services.log_svc.enabled=true \
        --set services.gpu_svc.enabled=true \
        --set services.portal_svc.enabled=true \
        --set services.aio_svc.enabled=true \
        --set services.autoscaler_svc.enabled=true \
        --set services.incident_svc.enabled=true \
        --set services.runbook_svc.enabled=true \
        > "$RENDER_BIN" 2>/dev/null; then
        sm_count="$(grep -c 'kind: ServiceMonitor' "$RENDER_BIN" || true)"
        if [[ "$sm_count" = "$EXPECT_SM" ]]; then
            ok "ServiceMonitor 数量 = ${sm_count}（控制面 + values.yaml 中 $((EXPECT_SM - 1)) 个 metrics:true 服务）"
        else
            bad "ServiceMonitor 数量 = ${sm_count}（期望 ${EXPECT_SM}：控制面 + values 中 $((EXPECT_SM - 1)) 个 metrics:true 服务）"
            echo "         若刚给某服务补了 /metrics：把 values.yaml 对应条目的 metrics 改为 true；反之改回 false"
        fi
        # 微服务 Service 不应再暴露 metrics 端口（控制面 Service 有独立 metrics 端口，属正常）
        ms_metrics=""
        if [[ -n "$PY" ]]; then
            ms_metrics="$("$PY" - "$RENDER_BIN" <<'PY'
import sys, yaml
path = sys.argv[1]
bad = []
for d in yaml.safe_load_all(open(path, encoding='utf-8')):
    if not d or d.get('kind') != 'Service':
        continue
    name = d['metadata']['name']
    if 'controlplane' in name:
        continue
    ports = [p['name'] for p in d['spec'].get('ports', [])]
    if 'metrics' in ports:
        bad.append(name)
print(' '.join(bad))
PY
)"
        fi
        if [[ -z "$PY" ]]; then
            skip "未找到 python3/python，跳过微服务 metrics 端口渲染校验"
        elif [[ -z "${ms_metrics// }" ]]; then
            ok "微服务 Service 未渲染 metrics 端口"
        else
            bad "微服务 Service 仍渲染 metrics 端口：${ms_metrics}（微服务无独立 metrics 监听）"
        fi
    else
        bad "helm template（ServiceMonitor 全开）渲染失败"
    fi
    rm -f "$RENDER_BIN"

    # 3d 禁止 chart 中出现指向不存在的 9091 微服务采集
    if grep -qE 'metricsPort' deploy/helm/opsmesh/values.yaml; then
        bad "values.yaml 仍含 metricsPort（微服务无该监听端口，属假配置）"
    else
        ok "values.yaml 无 metricsPort 残留"
    fi
else
    skip "未安装 helm，跳过 Helm 渲染校验"
fi

# ---------------------------------------------------------------
sec "4. Compose 渲染"
# ---------------------------------------------------------------
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    TMPENV="$(mktemp)"
    cat > "$TMPENV" <<'ENVEOF'
OPSMESH_VERSION=0.11.0
OPSMESH_ADVERTISE_ADDR=https://opsmesh.ci.local:8080
MYSQL_ROOT_PASSWORD=CITestRootPw1
MYSQL_PASSWORD=CITestUserPw1
REDIS_PASSWORD=CITestRedisPw1
ADMIN_PASSWORD=CITestAdminPw1
JWT_SECRET=citest-jwt-secret-not-for-production-use-0123456789
ENCRYPTION_KEY=citest-encryption-key-not-for-production-0123456789
CONFIG_ENCRYPTION_KEY=citest-config-encryption-key-not-for-prod-0123456789
POSTGRES_PASSWORD=CITestPgPw1
GRAFANA_ADMIN_PASSWORD=CITestGrafanaPw1
TLS_CN=opsmesh.ci.local
ENVEOF
    # 单文件不用 for：shellcheck SC2043（"loop will only ever run once"）正是提醒这里
    # 曾经有两个文件、现在只剩一个——写成循环会让人以为还漏了一个 compose。
    f=deploy/docker/docker-compose.prod.yml
    out="$(docker compose --env-file "$TMPENV" -f "$f" config 2>&1 >/dev/null)"
    if [[ $? -eq 0 ]]; then
        ok "compose 渲染通过：${f}"
    else
        bad "compose 渲染失败：${f}"
        echo "$out" | head -12 | sed 's/^/         /'
    fi
    out="$(docker compose --env-file "$TMPENV" \
        -f deploy/docker/docker-compose.prod.yml \
        -f deploy/docker/docker-compose.prod-proxy.yml config 2>&1 >/dev/null)"
    if [[ $? -eq 0 ]]; then
        ok "compose 渲染通过：prod + prod-proxy overlay"
    else
        bad "compose 渲染失败：prod + prod-proxy overlay"
        echo "$out" | head -12 | sed 's/^/         /'
    fi

    # 项目名隔离硬门禁：项目名必须是 opsmesh*（否则 down --remove-orphans 会误删同项目名其他栈）
    proj="$(docker compose --env-file "$TMPENV" -f deploy/docker/docker-compose.prod.yml config 2>/dev/null \
        | grep -m1 -E '^name:' | awk '{print $2}')"
    if [[ -z "$proj" ]]; then
        skip "无法从 compose config 输出读取项目名"
    elif [[ "$proj" == opsmesh* ]]; then
        ok "compose 项目名 = ${proj}（隔离正确）"
    else
        bad "compose 项目名 = ${proj}（期望 opsmesh*；否则存在跨项目误删风险）"
    fi

    # 端口可达性硬门禁（2026-09-25 实测踩坑）：若某服务所连网络【全部】是 internal: true，
    # Docker Desktop(WSL2) 会把该服务的宿主端口 publish 静默丢弃（容器照常 Up，但
    # NetworkSettings.Ports 为空、宿主 curl 连不上，compose up 不报错）；
    # 同时 internal 网络无网关，出站 DNS/联网也不通（alert-svc 连不上 PagerDuty）。
    # 因此：凡是声明了 ports 的服务，至少要连一个非 internal 网络。
    RENDER_BIN="$(mktemp)"
    if docker compose --env-file "$TMPENV" -f deploy/docker/docker-compose.prod.yml config >"$RENDER_BIN" 2>/dev/null; then
        bad_ports=""
        if [[ -n "$PY" ]]; then
            bad_ports="$("$PY" - "$RENDER_BIN" <<'PY'
import sys
import yaml

d = yaml.safe_load(open(sys.argv[1], encoding="utf-8")) or {}
nets = d.get("networks") or {}
internal = {n for n, v in nets.items() if isinstance(v, dict) and v.get("internal")}
bad = []
for name, svc in (d.get("services") or {}).items():
    if not svc.get("ports"):
        continue
    joined = set(svc.get("networks") or [])
    if joined and joined <= internal:
        bad.append(f"{name}(ports={len(svc['ports'])},nets={','.join(sorted(joined))})")
print("\n".join(bad))
PY
)"
        fi
        if [[ -z "$PY" ]]; then
            skip "未找到 python3/python，跳过 internal/端口冲突校验"
        elif [[ -z "$bad_ports" ]]; then
            ok "compose 无「internal 网络 + 宿主端口发布」冲突（该组合端口静默不可达）"
        else
            bad "以下服务所连网络全是 internal: true，宿主端口将被静默丢弃："
            echo "$bad_ports" | sed 's/^/         /'
        fi
    else
        skip "无法渲染 compose，跳过 internal/端口冲突校验"
    fi
    rm -f "$RENDER_BIN"
    rm -f "$TMPENV"
else
    skip "未安装 docker compose，跳过 Compose 渲染校验"
fi

# ---------------------------------------------------------------
sec "5. K8s 样例清单校验"
# ---------------------------------------------------------------
K8S_FILES="$(ls deploy/k8s/deployments/*.yaml 2>/dev/null)"
if [[ -z "$K8S_FILES" ]]; then
    bad "deploy/k8s/deployments/ 下无清单文件"
elif command -v kubeconform >/dev/null 2>&1; then
    # shellcheck disable=SC2086
    kc_out="$(kubeconform -strict -summary -ignore-missing-schemas $K8S_FILES 2>&1)"
    kc_summary="$(printf '%s\n' "$kc_out" | grep -E '^Summary:' | tail -1)"
    kc_invalid="$(printf '%s' "$kc_summary" | grep -oE 'Invalid: [0-9]+' | grep -oE '[0-9]+')"
    kc_errors="$(printf '%s' "$kc_summary" | grep -oE 'Errors: [0-9]+' | grep -oE '[0-9]+')"
    # 分支判定（按「资产缺陷必须 FAIL、环境问题才 SKIP」分层，避免假绿/假红）：
    #   Invalid>0                                    → 清单不合规，FAIL
    #   Errors>0 且全部是 failed (downloading|parsing) schema → 拉不到 JSON schema
    #     （默认 schema 源 raw.githubusercontent.com，离线/受限网络全额失败），
    #     属环境问题而非资产缺陷，SKIP；这是 CI 上唯一可能出现的 Errors 成因。
    #   Errors>0 含 error unmarshalling resource     → YAML 语法/类型错误，资产缺陷，FAIL。
    #     kubeconform 把这类也计入 Errors（实测：坏缩进 → Valid: 0, Invalid: 0, Errors: 1），
    #     若无条件按 Errors SKIP，等于给坏清单开后门。
    #   无 Summary 行                                → kubeconform 异常退出，FAIL（不得假绿）。
    kc_schema_msg=0
    if printf '%s' "$kc_out" | grep -qE 'failed (downloading|parsing) schema'; then
        kc_schema_msg=1
    fi
    kc_parse_err=0
    if printf '%s' "$kc_out" | grep -qE 'error unmarshalling resource'; then
        kc_parse_err=1
    fi
    if [[ -z "$kc_summary" ]]; then
        bad "kubeconform 未输出 Summary（异常退出）："
        printf '%s\n' "$kc_out" | head -10 | sed 's/^/         /'
    elif [[ "${kc_invalid:-0}" != "0" ]]; then
        bad "kubeconform 校验失败：清单 schema 不合规（Invalid=${kc_invalid}）"
        printf '%s\n' "$kc_out" | grep -vE '^Summary:' | head -10 | sed 's/^/         /'
    elif [[ "${kc_errors:-0}" != "0" && "$kc_parse_err" = "0" && "$kc_schema_msg" = "1" ]]; then
        skip "kubeconform 拉取 JSON schema 失败（Errors=${kc_errors}，离线/受限网络），本次跳过 K8s schema 校验"
    elif [[ "${kc_errors:-0}" != "0" ]]; then
        bad "kubeconform 校验失败：资源无法解析（Errors=${kc_errors}）"
        printf '%s\n' "$kc_out" | grep -vE '^Summary:' | head -10 | sed 's/^/         /'
    else
        ok "kubeconform 校验通过（deploy/k8s/deployments，Invalid=0）"
    fi
elif command -v kubectl >/dev/null 2>&1 && kubectl cluster-info --request-timeout=5s >/dev/null 2>&1; then
    # 仅在有可用集群时走 kubectl：client dry-run 的 schema 校验需要从服务端拉
    # OpenAPI（kubectl 1.12+ 的客户端校验事实上依赖服务端 schema），无集群时
    # 报 "failed to download openapi: ... connection refused" 并以非 0 退出——
    # 那会让一份正确的清单被误判为失败（实测：CI 的 security job 从未装 kubeconform，
    # 走本分支即假失败；而本机有 kind 集群故掩盖了该缺陷）。
    # 无集群而装了 kubeconform 时走上一分支（离线 schema 校验，不依赖集群）。
    out="$(kubectl apply --dry-run=client -f deploy/k8s/deployments/ 2>&1)"
    if [[ $? -eq 0 ]]; then
        ok "kubectl client dry-run 通过"
    else
        bad "kubectl client dry-run 失败："
        echo "$out" | head -10 | sed 's/^/         /'
    fi
elif command -v kubectl >/dev/null 2>&1; then
    skip "kubectl 存在但无可用集群（client dry-run 需服务端 OpenAPI schema），跳过 K8s 清单校验；装 kubeconform 可做离线校验"
else
    skip "未安装 kubeconform/kubectl，跳过 K8s 清单校验（装 kubeconform 可离线校验 schema）"
fi

# 样例边界硬门禁：过权 RBAC 不得回归
if grep -rqE '^\s*kind:\s*(ClusterRole|ClusterRoleBinding)\b' deploy/k8s/ 2>/dev/null; then
    bad "deploy/k8s 出现 ClusterRole/ClusterRoleBinding（样例 Pod 不需 K8s API 权限，属过权）"
else
    ok "deploy/k8s 无 ClusterRole/ClusterRoleBinding"
fi
if grep -rq 'serviceAccountName' deploy/k8s/deployments/ 2>/dev/null; then
    bad "deploy/k8s 清单仍引用 serviceAccountName（配套 RBAC 已移除）"
else
    ok "deploy/k8s 清单未引用 serviceAccountName"
fi

sec "6. 行尾一致性（CRLF 会让 Dockerfile / Helm / bash 开箱即坏）"
# 为什么要单列一节：CI 全跑在 Linux，checkout 永远是 LF——CRLF 缺陷 CI 不可见，
# 只在 Windows 本地构建/部署时爆。2026-09-26 实测对照（同一 Dockerfile 仅行尾不同）：
#   LF   → docker build 成功
#   CRLF → ERROR: failed to solve: dockerfile parse error on line 3: unknown instruction: &&
# 原因：RUN 续行的行尾变成 CR+LF，Docker 解析器识别不到续行。
# 检测手段必须走 tr 做字节比对，不能用 grep/awk：
# 2026-09-26 实测 Git-Bash 的 `grep -c $'\r'` 与 `awk '/\r/'` 对确实含 CRLF 的文件都返回 0
# （MSYS 文本模式在读时吞 CR，而 `tr -d '\r'` 能看到：32 → 31 字节）。
# 也就是说"用 grep 写的 CRLF 门禁在 Windows 上空转"——而 Windows 正是它唯一要防的平台。
has_crlf() { # $1=文件；含 CR 则返回 0
    local raw stripped
    raw="$(wc -c < "$1" | tr -d ' ')"
    stripped="$(tr -d '\r' < "$1" | wc -c | tr -d ' ')"
    [[ "$raw" != "$stripped" ]]
}
CRLF_BAD=""
while IFS= read -r f; do
    [[ -n "$f" && -f "$f" ]] || continue
    if has_crlf "$f"; then
        CRLF_BAD="${CRLF_BAD} ${f#./}"
    fi
done < <(find . -path ./.git -prune -o -type f \
            \( -name 'Dockerfile*' -o -name '.dockerignore' -o -name 'docker-compose*.yml' \
               -o -name '*.sh' -o -name '*.tpl' -o -name 'Chart.yaml' -o -name 'values*.yaml' \) -print 2>/dev/null)
if [[ -n "$CRLF_BAD" ]]; then
    bad "以下部署资产含 CRLF（Windows 检出后 docker/helm/bash 会坏）：$CRLF_BAD"
    echo "         修法：git add --renormalize <file>，并确认 .gitattributes 覆盖该文件类型"
else
    ok "Dockerfile/compose/脚本/Helm 模板均为 LF（无 CRLF 破坏风险）"
fi
# 根因防护：问 git 本身而不是解析 .gitattributes——check-attr 覆盖显式规则与继承，
# 不会因为规则写法不同而误判通过。
if [[ "$(git check-attr eol -- Dockerfile 2>/dev/null)" == *"eol: lf"* ]]; then
    ok "git 判定 Dockerfile 为 eol=lf（Windows 检出不会变 CRLF）"
else
    bad "git check-attr 未把 Dockerfile 判为 eol=lf（core.autocrlf=true 的机器检出即 CRLF → docker build 失败）"
fi
# ---------------------------------------------------------------
sec "7. 镜像发布名 ↔ chart 引用一致性（Helm 开箱即 ErrImagePull 的那次教训）"
# ---------------------------------------------------------------
# 为什么要这道门禁：CI 的 image job 推 ghcr.io/levango7/opsmesh-binary，而仓库内 chart 的
# 默认值写的是 opsmesh/opsmesh:latest —— 两边各说各话，默认装完两个核心 workload 必
# ErrImagePull。更糟的是它长期不可见：CI 全绿（镜像真的推上去了、chart 也真的渲染成功），
# 只是两件"成功的事"从不互相引用。2026-09-26 实测记录见报告 §19.4。
# 这道门禁把「chart 引用的名字必须有人推送」变成机器判定，而不是靠注释与记忆。
PUBLISHED_FILE="$(mktemp)"
trap 'rm -f "$PUBLISHED_FILE"' EXIT

# 发布名集合 = release.yml 的 microservice 矩阵 + ci.yml 的两个 IMAGE_LEAF
# （核心镜像刻意不在矩阵里：它们由 ci.yml 的 image / image-agent job 构建，用 Dockerfile 与
#  Dockerfile.agent，而非服务模板 Dockerfile.service。）
if [ -f .github/workflows/release.yml ]; then
    sed -n '/^      matrix:/,/^    steps:/p' .github/workflows/release.yml \
        | grep -oE '^[[:space:]]+- [a-z0-9-]+[[:space:]]*$' \
        | sed 's/^[[:space:]]*-[[:space:]]*//; s/[[:space:]]*$//' >> "$PUBLISHED_FILE"
    # 坑（实测踩过）：这里不能用 `tr -d ' -'` 去同时删空格和连字符 —— tr 把 ' -' 解释成
    # **0x20~0x2D 的字符区间**（空格到连字符，含 '-' 与全部数字），于是 auth-svc 被洗成
    # authsvc、任何版本号也被洗残；两边集合都变形后，门禁会以"对不上"的方式长期误判。
fi
grep -hoE 'IMAGE_LEAF: [a-z0-9-]+' .github/workflows/ci.yml 2>/dev/null \
    | awk '{print $2}' >> "$PUBLISHED_FILE"
sort -u -o "$PUBLISHED_FILE" "$PUBLISHED_FILE"

PUB_N=$(wc -l < "$PUBLISHED_FILE" | tr -d ' ')
if [ "${PUB_N:-0}" -lt 3 ]; then
    # 解析不出集合就直接判红：workflow 的 YAML 写法一变，这道门禁就会"瞎"，
    # 而"瞎了的门禁"必须以红的形态被发现，不能静默退化成永远 PASS（教训 11）。
    bad "未能从 workflow 解析出 CI 发布名集合（只得到 ${PUB_N} 条）——请同步本脚本的解析规则"
else
    ok "解析到 CI 发布名 ${PUB_N} 个（release.yml 矩阵 + ci.yml IMAGE_LEAF）"
fi

# chart 侧：values.yaml / values-production.yaml 里每个 image.repository 的叶子名都必须有人推
CHART_REPOS="$(grep -hoE '^[[:space:]]*repository:[[:space:]]*[^ ]+' \
    deploy/helm/opsmesh/values.yaml deploy/helm/opsmesh/values-production.yaml 2>/dev/null \
    | awk '{print $2}' | sort -u)"
if [ -z "$CHART_REPOS" ]; then
    bad "chart 里一个 image.repository 都没解析到（values 结构变了？本脚本需同步）"
else
    MISSING=""
    N=0
    while IFS= read -r repo; do
        [ -n "$repo" ] || continue
        N=$((N + 1))
        leaf="${repo##*/}"
        grep -qxF "$leaf" "$PUBLISHED_FILE" || MISSING="$MISSING $repo"
    done <<< "$CHART_REPOS"
    if [ -n "$MISSING" ]; then
        bad "chart 引用了 CI 从不推送的镜像名：$MISSING"
        echo "         修法：把 chart 的 repository 改成 CI 实际推送名，或在 workflow 中补上该发布项"
    else
        ok "chart 的 $N 个 image.repository 叶子名全部落在 CI 发布名集合内"
    fi
fi

# 默认值必须"自带 registry 主机"：否则 helm install 不填任何 values 时会去 Docker Hub 找
# opsmesh/*（本项目不在那儿发布）——这是"名字对得上但仍然拉不到"的另一半根因。
#
# 判定口径与 chart helper 严格一致（templates/_helpers.tpl）：**首段含 "." 或 ":"（或等于
# localhost）才算 registry 主机**。所以 `opsmesh/opsmesh` 这种 Bitnami 风格名字**不是**合格的
# 默认值 —— 它带斜杠但指向 Docker Hub。早期版本这里用 `grep -v '/'` 只挑"完全没斜杠"的，
# 于是恰好把本次真缺陷（opsmesh/opsmesh）放过去了：门禁与自己要防的形态错位。
UNQUALIFIED="$(grep -hoE '^[[:space:]]*repository:[[:space:]]*[^ ]+' deploy/helm/opsmesh/values.yaml 2>/dev/null \
    | awk '{print $2}' | awk -F/ '{print $1}' | grep -vE '[.:]' | grep -v '^localhost$' | tr '\n' ' ')"

GLOBAL_REG="$(grep -A4 '^global:' deploy/helm/opsmesh/values.yaml | grep -E '^[[:space:]]*imageRegistry:[[:space:]]*"[^"]+"' || true)"
if [ -n "${UNQUALIFIED// /}" ] && [ -z "$GLOBAL_REG" ]; then
    bad "values.yaml 里这些 repository 既无 registry 主机、global.imageRegistry 又留空：$UNQUALIFIED（默认装必拉不到）"
else
    ok "默认 image 引用可解析（自带 registry 主机，或 global.imageRegistry 已给前缀）"
fi

# 核心镜像名固定断言：上面两条的语义都建立在"CI 确实发布这两个叶子名"之上，
# 一旦被改回去，集合会变小、chart 也会跟着改，两条断言可能同时"自洽地错"。
for want in opsmesh-binary opsmesh-agent; do
    if grep -qxF "$want" "$PUBLISHED_FILE"; then
        ok "CI 发布名包含 $want"
    else
        bad "CI 不再发布 $want —— chart / GitOps / 文档的消费方需同步改名"
    fi
done

sec "8. 构建上下文自洽（Dockerfile 的 COPY 源必须存在于干净检出）"
# ---------------------------------------------------------------
# 起因（报告 §19.1）：Dockerfile.service 写 `COPY go.work go.work.sum ./`，而 .gitignore
# 明确排除 go.work.sum ⇒ 干净检出里没有该文件 ⇒ buildx 在 compute cache key 阶段就死：
#   ERROR: failed to build: failed to solve: failed to compute cache key: "/go.work.sum": not found
# 代价已经付过一次：tag v0.9.1 的 release 矩阵 18 条全灭，GHCR 至今没有 0.9.1 的微服务镜像，
# 而同期源码侧 CI 全绿 —— 因为没有任何常规检查会去构建这些镜像（现由 release-dryrun 补上）。
# 这道静态门禁是第二层：在"改 Dockerfile 的那一刻"就报警，不必等构建。
MISSING_COPY=""
IGNORED_COPY=""
CHECKED=0
for df in Dockerfile Dockerfile.agent Dockerfile.service deploy/docker/Dockerfile.controlplane deploy/docker/Dockerfile.micro; do
    [ -f "$df" ] || continue
    dfdir="$(dirname "$df")"
    CHECKED=$((CHECKED + 1))
    # 取每条 COPY 的源（第 2..NF-1 个字段；最后一个字段是目标路径；以 - 开头的是 --from/--chown 等旗标）
    while IFS= read -r src; do
        [ -n "$src" ] || continue
        case "$src" in
            -*|*'*'*|*'?'*) continue ;;          # 旗标与通配不适用"路径必须存在"
        esac
        if [ ! -e "$src" ] && [ ! -e "$dfdir/$src" ]; then
            MISSING_COPY="$MISSING_COPY ${df}:${src}"
        fi
        # 干净检出里没有的东西 = 被 .gitignore 排除的东西（go.work.sum 正是这一类）
        if git check-ignore -q "$src" 2>/dev/null; then
            IGNORED_COPY="$IGNORED_COPY ${df}:${src}"
        fi
    # **含 --from= 的 COPY 整条跳过**：它的源来自上一个构建阶段（跨阶段拷贝），不是构建上下文；
    # 拿上下文里的文件去要求它存在是错的（首版误报了 `COPY --from=build /svc /usr/local/bin/svc` 里的 /svc）。
    done < <(awk '/^COPY[ \t]/{ if ($0 ~ /--from=/) next; for (i = 2; i < NF; i++) { if ($i ~ /^-/) continue; print $i } }' "$df")

done
if [ "$CHECKED" -eq 0 ]; then
    bad "一个 Dockerfile 都没扫到（路径变了？本脚本需同步）"
else
    if [ -n "${MISSING_COPY// /}" ]; then
        bad "Dockerfile 的 COPY 源在仓库里不存在：${MISSING_COPY}"
        echo "         构建会在 compute cache key 阶段直接失败（不是运行期问题，现场改不回来）"
    else
        ok "${CHECKED} 个 Dockerfile 的字面 COPY 源都存在于仓库中"
    fi
    if [ -n "${IGNORED_COPY// /}" ]; then
        bad "Dockerfile 的 COPY 源同时被 .gitignore 排除：${IGNORED_COPY}"
        echo "         本机可构建（工作区里有该文件）、CI/干净检出必失败 —— 正是 v0.9.1 镜像全灭的形态"
    else
        ok "没有 COPY 源被 .gitignore 排除（干净检出与本工作区在这一点上等价）"
    fi
fi

# ---------------------------------------------------------------
sec "9. 提交内容里的『行内孤立 CR』（§6 抓不到、grep 也看不见的那一类）"
# ---------------------------------------------------------------
# 为什么 §6 不够，要单列一节：
#   ① 范围：§6 只扫部署资产（Dockerfile/*.sh/*.yml/…），文档与代码不在其内；
#      而且它判的是"含任何 CR"——放到全仓会把 Windows 检出的正常 CRLF 行尾全判成缺陷，
#      所以它只能窄覆盖，这是刻意的。
#   ② 危害形态不同：危险的是**行内**孤立 CR（不是行尾）。git 的 CRLF 归一化（* text=auto）
#      只处理行尾，行内 CR 会**原样进入 blob 并被推送**；markdown 渲染器把它当换行，
#      于是一条 bullet 从句子中间断开——2026-09-26 在 CHANGELOG.md 实测抓到一处已推送的。
#   ③ 检测口径：必须问 git 自己而不是读工作区文件。--cached 读**暂存 blob**（clean filter
#      已应用），行尾 CRLF 归一化掉了，剩下的任何 CR 都必然是真杂质。实测对照：
#      同一仓库工作区 335 个文件带正常 CRLF → 本门禁 0 误报；CHANGELOG.md 那 1 个行内 CR → 点名。
#      反过来若在 Windows 工作区用 grep/awk 找 CR，MSYS 文本模式会吞 CR 而返回 0（见 §6 注释）。
# 门禁后端缺席必须**判红而不是判绿**（本轮 actionlint/shellcheck 的教训）：
# 因此下面显式区分 rc=1（无匹配，正常）与 rc≥2（PCRE 不可用/命令失败 = 门禁失明）。
LONE_CR_OUT="$(git grep -IP --cached -e '\r(?!\n)' -- . 2>&1)"
case $? in
    0)
        bad "以下文件的**已提交内容**含行内孤立 CR（会被 markdown/渲染器当换行，且 grep 在 Windows 上看不见）："
        echo "$LONE_CR_OUT" | head -20 | sed 's/^/         /'
        echo "         修法：按字节删掉该 CR（勿用 sed/perl 的文本模式，它会连行尾一起改写）"
        ;;
    1)
        ok "全部暂存 blob 无行内孤立 CR（含 .md/.go/.yml 等所有文本文件）"
        ;;
    *)
        bad "行内 CR 门禁**失明**：git grep -P 返回非 0/1 状态，输出：$(echo "$LONE_CR_OUT" | head -3 | tr '\n' ' ')"
        echo "         这不是「内容干净」，而是「没检查成」：请确认该 git 构建带 PCRE（git grep -P 可用）"
        ;;
esac

# ---------------------------------------------------------------
sec "10. 镜像矩阵的构建目标必须存在（Dockerfile.service 假设 cmd/<svc> 布局）"
# ---------------------------------------------------------------
# 这是「只在发版那一刻才执行」那一类假绿的镜像（§19 第 5 类）：Dockerfile.service 硬编码
# `-o /svc ./cmd/${SERVICE}`，而 2026-09-26 v0.9.2 第一次真发版就红在
#   stat /src/services/tf-provider/cmd/tf-provider: directory not found
#   → 矩阵 fail-fast：1 红 + 16 cancel + github-release skip ⇒ Release 依旧 0 资产
# 常规 CI 看不见它的原因有两层：`services` job 跑的是 `go build ./...`（不假设 cmd 布局），
# 而 `release-dryrun` 只构建 auth-svc 一个样本。⇒ 「布局约定」与「矩阵内容」之间从没有人核对。
BAD_TARGET=""; NO_MAIN_PKG=""; FORGOTTEN=""
while IFS= read -r svc; do
    [[ -n "$svc" ]] || continue
    if [ ! -d "services/${svc}/cmd/${svc}" ]; then
        BAD_TARGET="${BAD_TARGET} ${svc}"
        continue
    fi
    if ! grep -qs '^package main' "services/${svc}/cmd/${svc}/"*.go; then
        NO_MAIN_PKG="${NO_MAIN_PKG} ${svc}"
    fi
done <<< "$RELEASE_SVCS"
# 反方向同样要判：有 cmd/<name> 布局却不在矩阵里 ⇒ 一个忘发镜像的真服务
while IFS= read -r d; do
    [[ -n "$d" ]] || continue
    printf '%s\n' "$RELEASE_SVCS" | grep -qx "$d" && continue
    [ -d "services/${d}/cmd/${d}" ] && FORGOTTEN="${FORGOTTEN} ${d}"
done <<< "$DIR_SVCS"
if [[ -n "${BAD_TARGET// }" ]]; then
    bad "矩阵里这些服务没有 Dockerfile.service 要求的 cmd/<svc> 目录：${BAD_TARGET}"
    echo "         后果是在发版那一刻才失败并连带取消整批；要么补齐 cmd/<svc>/main.go，要么从矩阵移除"
else
    ok "矩阵 ${n_rel} 个服务全部具备 cmd/<svc> 构建目标"
fi
if [[ -n "${NO_MAIN_PKG// }" ]]; then
    bad "这些 cmd/<svc> 目录里没有 package main：${NO_MAIN_PKG}"
else
    ok "每个构建目标都含 package main"
fi
if [[ -n "${FORGOTTEN// }" ]]; then
    bad "有 cmd/<name> 布局（看起来是可发布服务）却不在矩阵也不在豁免表：${FORGOTTEN}"
else
    ok "没有「具备服务布局却被漏掉」的模块"
fi
# 豁免表本身也要自证：每条必须有理由，否则"豁免"就是新的静默漂移入口。
NO_REASON=""
while IFS='|' read -r nm rs; do
    [[ -n "$nm" ]] || continue
    [[ -n "${rs// }" ]] || NO_REASON="${NO_REASON} ${nm}"
    grep -qx "$nm" <<< "$DIR_SVCS" || NO_REASON="${NO_REASON} ${nm}(目录不存在)"
done <<< "$NON_SERVICE"
if [[ -n "${NO_REASON// }" ]]; then
    bad "豁免表条目缺理由或指向不存在的模块：${NO_REASON}"
else
    ok "豁免表 $(printf '%s\n' "$NON_SERVICE" | grep -c . || true) 条：均有理由且模块真实存在"
fi

# ---------------------------------------------------------------
sec "11. 抓取配置 ↔ 服务能力一致性（prometheus.yml 的微服务 job ↔ 源码注册行）"
# ---------------------------------------------------------------
# 为什么单列（§21.6 实测教训）：prometheus.yml 曾为 9 个服务配了 :9091 job，而 services/** 里
# 没有任何 9091 监听 ⇒ 9 个 target 恒 DOWN（"配置看起来齐全，其实全是坏目标"）。
# 反方向同样有害：服务暴露了 /metrics 却没配 job = 面板永远空白。两个方向都要静态兜住。
# 判定口径：job 的 target 主机名若对应 services/<name>/ 目录，则该服务 main.go 必须有 GetHandler() 注册行。
PROM="deploy/monitoring/prometheus.yml"
# 豁免表（带理由）：暴露了 /metrics 但【不在出厂 compose 栈里】、因此不进 prometheus.yml 的服务。
# tf-provider 是 Terraform 插件，没有 HTTP 面。
# 曾经这张表里有 autoscaler-svc / incident-svc / runbook-svc，理由是「不在 compose 生产栈里」——
# 该理由在 2026-09-29 三域转正后就过期了，而门禁只看表不看理由，于是三个在跑的服务的指标
# 长期无人抓取（规则写了也恒 no data）。故下面 11c 把「在出厂栈里」变成判定而不是注释。
# （bot-svc / deploy-svc / plugin-svc / workflow-svc / grafana-bridge 已于 2026-09-29 删除。）
NOT_SCRAPED_EXEMPT="tf-provider"
JOB_MISS=""
while IFS= read -r tgt; do
    [[ -n "$tgt" ]] || continue
    svc="${tgt%%:*}"
    [ -d "services/$svc" ] || continue   # 非服务目标（controlplane/mysql/redis/otel/blackbox）跳过
    if ! grep -qs 'GetHandler()' "services/$svc/cmd/$svc/main.go"; then
        JOB_MISS="${JOB_MISS} ${svc}"
    fi
done <<< "$(grep -oE 'targets: \["[a-zA-Z0-9._-]+:[0-9]+"\]' "$PROM" | sed -E 's/.*\["([^"]+)"\]/\1/' | sort -u)"
NOT_SCRAPED=""
while IFS= read -r svc; do
    [[ -n "$svc" ]] || continue
    grep -qs 'GetHandler()' "services/$svc/cmd/$svc/main.go" || continue
    grep -qE "targets: \[\"${svc}:" "$PROM" && continue
    printf '%s\n' "$NOT_SCRAPED_EXEMPT" | grep -qw "$svc" && continue
    NOT_SCRAPED="${NOT_SCRAPED} ${svc}"
done <<< "$(ls services/)"
if [[ -n "${JOB_MISS// }" ]]; then
    bad "prometheus.yml 配了 job 但源码没有 /metrics 注册：${JOB_MISS}（这些 target 会恒 DOWN）"
else
    ok "prometheus.yml 的每个微服务 job 都有对应的 /metrics 注册（无坏目标）"
fi
if [[ -n "${NOT_SCRAPED// }" ]]; then
    bad "已暴露 /metrics 却没有任何抓取配置、也不在豁免表：${NOT_SCRAPED}"
    echo "         要么在 prometheus.yml 加 job（compose 栈）或 chart 的 values.services.<x>.metrics=true（k8s），要么进豁免表并写明理由"
else
    ok "已暴露 /metrics 的服务都有抓取配置，或落在带理由的豁免表里"
fi

# 11c：出厂 compose 栈里的每个微服务都必须被抓取——豁免表对在栈里的服务一律不适用。
# 为什么还要再加一层：上面那条只在「不在豁免表」时报红，而豁免表的理由是**注释**；
# 注释会过期（三域转正后「不在 compose 栈里」即为假），过期注释让门禁继续放行，
# 症状却是某个正在运行的服务整块指标没人抓——规则、面板、告警全都恒 no data。
# 「在不在栈里」是可静态核验的事实，所以核验事实，不相信理由。
COMPOSE_IN_STACK=""
while IFS= read -r svc; do
    [[ -n "$svc" ]] || continue
    [ -d "services/$svc" ] || continue
    COMPOSE_IN_STACK="${COMPOSE_IN_STACK}${svc}"$'\n'
done <<< "$(grep -oE '^  [a-z][a-z0-9_-]+:[[:space:]]*$' deploy/docker/docker-compose.prod.yml \
        | sed -E 's/^  //; s/:.*$//')"
STACK_NO_JOB=""
while IFS= read -r svc; do
    [[ -n "$svc" ]] || continue
    grep -qs 'GetHandler()' "services/$svc/cmd/$svc/main.go" || continue   # 无 /metrics 的服务由 §11 前两条管
    grep -qE "targets: \[\"${svc}:" "$PROM" && continue
    STACK_NO_JOB="${STACK_NO_JOB} ${svc}"
done <<< "$COMPOSE_IN_STACK"
EXEMPT_CONTRADICTION=""
while IFS= read -r svc; do
    [[ -n "$svc" ]] || continue
    printf '%s\n' "$NOT_SCRAPED_EXEMPT" | grep -qw "$svc" || continue
    EXEMPT_CONTRADICTION="${EXEMPT_CONTRADICTION} ${svc}"
done <<< "$COMPOSE_IN_STACK"
if [[ -n "${STACK_NO_JOB// }" ]]; then
    bad "在出厂 compose 栈里、暴露了 /metrics，却没有抓取任务：${STACK_NO_JOB}"
    echo "         后果：这些服务的指标从不进入 Prometheus，任何引用它们的出厂告警恒为 no data"
else
    ok "出厂栈内 $(printf '%s\n' "$COMPOSE_IN_STACK" | grep -c . || true) 个服务键与 services/ 交集里，凡暴露 /metrics 者均有抓取任务"
fi
if [[ -n "${EXEMPT_CONTRADICTION// }" ]]; then
    bad "豁免表与出厂栈冲突（豁免理由已失效）：${EXEMPT_CONTRADICTION}"
else
    ok "豁免表没有覆盖任何在出厂栈里的服务"
fi

echo ""
echo "=== 12. 业务指标写法（实体 ID 当标签 / counter 名字重复 _total）==="
# 依据：pkg/metrics 以前没有基数上限，而 device-svc / task-svc 把 device_id / agent_id /
# task_id 直接当标签 ⇒ 序列数随设备数线性增长（P1-5 同族，只是暴露在微服务侧）。
# 上限现在补上了，但**上限是兜底不是许可**：新代码再写实体 ID 标签就该判红。
# tenant_id 例外——租户数量由商务决定，是有界维度。
# 另一条：counter 家族名已带 _total，指标名再带后缀会渲染成 business_metrics_total{name="x_total"}。
BIZ_HITS="$("$PY" - "$ROOT" <<'PY'
import pathlib, re, sys
root = sys.argv[1]
hits = []
# 扫描面含 cmd/：task-svc 的 reclaim/fire 计数就写在 cmd/task-svc/main.go，
# 只看 internal/ 会让 cmd/ 里的调用完全不受这条规则约束。
files = [p for pat in ("*/internal/**/*.go", "*/cmd/**/*.go", "*/pkg/**/*.go")
         for p in sorted(pathlib.Path(root, "services").glob(pat))]
for p in files:
    if p.name.endswith("_test.go"):
        continue
    rel = str(p.relative_to(root)).replace("\\", "/")
    lines = p.read_text(encoding="utf-8", errors="replace").splitlines()
    for i, l in enumerate(lines):
        if "BusinessMetric(" not in l:
            continue
        window = "\n".join(lines[i:i + 6])
        for k in sorted(set(re.findall(r'"([a-z0-9_]+_id)"\s*:', window))):
            if k != "tenant_id":
                hits.append(f"{rel}:{i + 1} 用实体 ID 当标签（{k}）⇒ 序列数随实体数增长")
        nm = re.search(r'BusinessMetric\("([^"]+)"', l)
        if nm and nm.group(1).endswith("_total") and "Add" in l:
            hits.append(f"{rel}:{i + 1} counter 指标名重复 _total 后缀（{nm.group(1)}）")
print("\n".join(hits))
PY
)"
if [[ -z "${BIZ_HITS// }" ]]; then
    ok "业务指标调用无实体 ID 标签、counter 命名合规"
else
    bad "业务指标写法不合规："
    printf '%s\n' "$BIZ_HITS" | sed 's/^/         /'
fi

# ---------------------------------------------------------------
sec "13. 引导脚本只建库+授权（建表职责归代码；同名表静默让位是已证伪的高危形态）"
# ---------------------------------------------------------------
# 判据依据（2026-10-01 实测）：deploy/docker/scripts/init-mysql.sql 原是「单库版合并脚本」，
# 含 43 张表 DDL，其中 7 张与代码定义漂移（agents / devices / ci_items / permissions /
# roles / users / tasks 存在「只由代码 CREATE 定义、且无 ALTER 兜底」的列）。
# 一旦被挂载执行，CREATE TABLE IF NOT EXISTS 会让先落地的那份**静默胜出**、后到者不报错，
# 随后控制面的 INSERT 就是 Unknown column——排查成本极高。故这里钉两条硬断言：
#   ① deploy/docker/scripts/*.sql 不得出现 CREATE TABLE（建表归迁移与服务 initSchema）；
#   ② compose 每个服务 DSN 指向的库，必须由引导脚本建库，或等于 MYSQL_DATABASE（自动建库）。
BOOT_HITS=$( "$PY" - <<'PY'
import glob, os, re
root = os.getcwd()
hits = []

def strip_comments(src):
    # SQL 行注释 `--` 要剥掉：这两个脚本的头部注释里**故意**引用了 CREATE TABLE（讲清为什么
    # 禁止它），不剥会把「解释」当成「违规」——门禁假阳性比漏报更耗人。
    out = []
    for ln in src.split("\n"):
        i = ln.find("--")
        out.append(ln[:i] if i >= 0 else ln)
    return "\n".join(out)

scripts = sorted(glob.glob(os.path.join(root, "deploy/docker/scripts/*.sql")))
created = set()
for p in scripts:
    raw = open(p, encoding="utf-8", errors="replace").read()
    src = strip_comments(raw)
    for m in re.finditer(r"CREATE\s+TABLE\b", src, re.I):
        line = src[:m.start()].count("\n") + 1
        hits.append(f"{os.path.relpath(p, root)}:{line} 含 CREATE TABLE（建表职责应归代码）")
    created |= {m.group(1).lower() for m in re.finditer(
        r"CREATE\s+DATABASE\s+IF\s+NOT\s+EXISTS\s+`?(\w+)`?", src, re.I)}
compose = os.path.join(root, "deploy/docker/docker-compose.prod.yml")
if os.path.exists(compose):
    cs = open(compose, encoding="utf-8", errors="replace").read()
    auto = re.search(r"MYSQL_DATABASE:\s*([\w.-]+)", cs)
    auto_db = auto.group(1).lower() if auto else ""
    dsn_dbs = {m.group(1).lower() for m in re.finditer(r"\w*_DSN:\s*\"[^\"]*/([\w-]+)\?", cs)}
    for db in sorted(dsn_dbs):
        if db not in created and db != auto_db:
            hits.append(f"compose DSN 指向 `{db}`，但引导脚本没建它、也不等于 MYSQL_DATABASE ⇒ 连库即 Access denied（库不存在或无授权）")
    if not dsn_dbs:
        hits.append("未从 compose 解析到任何 *_DSN（正则失配，本节在空转）")
print("\n".join(hits))
PY
)
if [[ -z "${BOOT_HITS// }" ]]; then
    ok "引导脚本只建库+授权（无 CREATE TABLE），compose 每个 DSN 的库都有来源"
else
    bad "引导脚本/库集合不合规："
    printf '%s\n' "$BOOT_HITS" | sed 's/^/         /'
fi

# ---------------------------------------------------------------
sec "14. 出厂 PromQL 结构检查（CI 里跑不了 promtool，所以钉住最常犯的四类写法错）"
# ---------------------------------------------------------------
# 为什么单列：规则文件与面板里的表达式**只有到运行期才会被发现是错的**——
# Prometheus 加载坏规则会整份文件拒绝加载（于是一条告警都没有），Grafana 坏面板显示 parse error。
# 2026-10-02 就是靠这套判据抓到一条**已发布**的坏面板：opsmesh-overview.json 的 Error Rate
# 写成 rate({__name__=~"…"}{status=~"5.."}) ——两个花括号相邻不是合法 PromQL（应合成一个 {…} 用逗号分隔），
# 出厂面板里那条错误率曲线从来没画出来过。
# 这里不假装是 promtool：只做四类确定性的结构判定（相邻选择器 / 括号配平 /
# rate|increase 缺区间 / histogram_quantile 作用在非 _bucket 序列），
# 语义级校验（函数名拼错、标签是否存在）仍由 §11 与 metrics_contract_*_test.go 负责。
PROMQL_HITS="$("$PY" - <<'PY'
import glob, json, os, re

LOGQL_HINTS = ("| json", "|~", "| line_format", "| unwrap")

def check(expr, where, hits):
    if not expr or any(h in expr for h in LOGQL_HINTS):
        return  # Loki 面板的 expr 是 LogQL，不适用 PromQL 判据
    if "}{" in expr.replace(" ", ""):
        hits.append(f"{where}: 选择器后紧跟另一个 {{…}}（非法 PromQL；应合进同一个花括号用逗号分隔）")
    for a, b in (("(", ")"), ("[", "]"), ("{", "}")):
        if expr.count(a) != expr.count(b):
            hits.append(f"{where}: {a}{b} 不配平（{expr.count(a)} vs {expr.count(b)}）")
    for fn in ("rate", "irate", "increase", "delta"):
        for m in re.finditer(r"\b" + fn + r"\(", expr):
            depth, i, end = 0, m.end() - 1, None
            while i < len(expr):
                if expr[i] == "(":
                    depth += 1
                elif expr[i] == ")":
                    depth -= 1
                    if depth == 0:
                        end = i
                        break
                i += 1
            if end is None:
                break  # 不配平已经报过，不重复报
            if "[" not in expr[m.end():end]:
                hits.append(f"{where}: {fn}() 的参数没有区间选择 [..]（传即时向量在运行期才报错）")
    if "histogram_quantile" in expr and "_bucket" not in expr:
        hits.append(f"{where}: histogram_quantile 只作用于 …_bucket，缺该后缀等于查询必然为空")

hits, seen = [], 0
for rel in ["deploy/monitoring/prometheus-alerts.yml",
            "deploy/monitoring/prometheus-alerts.host.example.yml",
            "deploy/helm/opsmesh/templates/prometheusrule.yaml"]:
    if not os.path.exists(rel):
        hits.append(f"{rel}: 文件不存在（出厂规则丢了）")
        continue
    src = open(rel, encoding="utf-8", errors="replace").read()
    lines = src.split("\n")
    exprs = re.findall(r"(?m)^[ \t]*(?:-[ \t]*)?\{?[ \t]*[\"']?(?:expr|expression)[\"']?[ \t]*:[ \t]*(.*)$", src)
    seen += len(exprs)
    for n, e in enumerate(exprs, 1):
        check(e.replace('\\"', '"').strip().rstrip('",'), f"{rel}#{n}", hits)
    if not exprs:
        hits.append(f"{rel}: 没解析到任何 expr（本节对它空转）")

for rel in sorted(glob.glob("deploy/monitoring/grafana/dashboards/*.json")):
    try:
        doc = json.load(open(rel, encoding="utf-8"))
    except Exception as exc:
        hits.append(f"{rel}: JSON 解析失败 {exc}")
        continue
    n = 0
    for p in doc.get("panels", []):
        title = p.get("title", "?")
        for t in p.get("targets", []):
            e = t.get("expr") or t.get("query")
            if not e:
                continue
            n += 1
            ds = (t.get("datasource") or p.get("datasource") or {}).get("type", "")
            check(e if ds == "loki" else e.replace('\\"', '"'), f"{rel}[{title}]", hits)
    seen += n
    if n == 0:
        hits.append(f"{rel}: 面板里没解析到任何表达式（本节对它空转）")

if seen < 20:
    hits.append(f"出厂资产合计只解析到 {seen} 条表达式（阈值 20）——抽取正则失配，本节在空转")
print("\n".join(hits))
PY
)"
if [[ -z "${PROMQL_HITS// }" ]]; then
    ok "出厂规则与面板的 PromQL 结构合规（无相邻选择器 / 配平错 / 缺区间 / quantile 用错家族）"
else
    bad "出厂 PromQL 结构问题："
    printf '%s\n' "$PROMQL_HITS" | sed 's/^/         /'
fi

# ---------------------------------------------------------------
sec "15. 告警送达链路（规则 → Alertmanager → 外发通道）接通性"
# ---------------------------------------------------------------
# 为什么必须有这一节：出厂规则一直在增加（现在 33 条），而 `prometheus.yml` 的 `alerting:` 段
# 长期是**注释状态**、栈里也没有 alertmanager 容器——后果不是报错，是静默：规则照常评估、
# 照常进 firing，但没有任何人收到。"有告警状态"与"有人被叫醒"之间就是这一段，
# 它必须以机器判定的形式钉住，否则任何一次"顺手注释掉"都会让整套告警变成装饰。
AM_PROM="deploy/monitoring/prometheus.yml"
AM_COMPOSE="deploy/docker/docker-compose.prod.yml"
AM_TPL="deploy/monitoring/alertmanager.yml.template"
AM_HITS=""
# ① prometheus.yml 的 alerting 段必须存在且未被注释，且指向 alertmanager 服务。
if ! grep -qE '^alerting:' "$AM_PROM"; then
    AM_HITS="${AM_HITS} prometheus.yml 没有生效的 alerting 段（缺失或被注释＝规则不送达）"
elif ! grep -A 6 '^alerting:' "$AM_PROM" | grep -qE 'alertmanager:[0-9]+'; then
    AM_HITS="${AM_HITS} alerting 段里没有 alertmanager 目标"
fi
# ② compose 必须真的起 alertmanager，且挂载的是渲染出的配置（不是仓里一份死配置）。
if ! grep -qE '^  alertmanager:' "$AM_COMPOSE"; then
    AM_HITS="${AM_HITS} docker-compose.prod.yml 里没有 alertmanager 服务"
fi
if ! grep -q 'ALERTMANAGER_CONFIG' "$AM_COMPOSE"; then
    AM_HITS="${AM_HITS} compose 没有用 \${ALERTMANAGER_CONFIG} 挂载 AM 配置（外发地址将无法由操作者决定）"
fi
# ③ 模板与渲染步骤必须成对存在（只加模板不加渲染 = 挂进去的仍是死配置）。
if [ ! -f "$AM_TPL" ]; then
    AM_HITS="${AM_HITS} 缺少 $AM_TPL"
elif ! grep -q '__ALERT_WEBHOOK_RECEIVER__' "$AM_TPL"; then
    AM_HITS="${AM_HITS} 模板里没有 __ALERT_WEBHOOK_RECEIVER__ 占位（渲染步骤会替换不到东西）"
fi
if ! grep -q 'render_alertmanager_config' deploy/docker/scripts/deploy.sh; then
    AM_HITS="${AM_HITS} deploy.sh 未调用 render_alertmanager_config"
elif ! grep -q 'render_alertmanager_config ||' deploy/docker/scripts/deploy.sh; then
    AM_HITS="${AM_HITS} deploy.sh 的 do_up 里渲染失败没有拦住部署（会带着坏配置起 AM）"
fi
# ④ 生成物含 bearer token，必须不入库。
if ! grep -q 'deploy/docker/generated' .gitignore; then
    AM_HITS="${AM_HITS} .gitignore 没有排除 deploy/docker/generated/（渲染出的 bearer 会入库）"
fi
# ⑤ 用空外发段渲染一遍模板，确认出厂默认配置本身是合法 YAML 且路由指得到 receiver。
AM_RENDER_CHECK="$("$PY" - "$AM_TPL" <<'PY'
import sys, yaml
raw = open(sys.argv[1], encoding="utf-8").read()
d = yaml.safe_load(raw.replace("__ALERT_WEBHOOK_RECEIVER__", "    # 未配置外发通道"))
names = {r.get("name") for r in d.get("receivers", [])}
route = d.get("route", {})
need = [route.get("receiver")] + [r.get("receiver") for r in route.get("routes", [])]
bad = [x for x in need if x not in names]
if bad:
    print("route 引用了不存在的 receiver: %s" % bad); sys.exit(1)
if not d.get("inhibit_rules"):
    print("没有 inhibit 规则：ServiceDown 会带着成堆的下游 warning 一起刷屏"); sys.exit(1)
PY
)" || AM_HITS="${AM_HITS} 出厂默认 AM 配置不合规：${AM_RENDER_CHECK}"
if [ -z "${AM_HITS// }" ]; then
    ok "告警送达链路接通：alerting 段生效 + compose 起 AM + 配置由 .env 渲染 + 默认配置合法"
    if grep -q 'ALERT_WEBHOOK_URL=' deploy/docker/scripts/deploy.sh; then
        ok "deploy.sh 的 .env 模板含 ALERT_WEBHOOK_URL/BEARER 键（外发地址由操作者决定，不留死值）"
    else
        bad "deploy.sh 的 .env 模板缺 ALERT_WEBHOOK_URL 键——操作者没有可控的外发入口"
    fi
else
    bad "告警送达链路有问题："
    printf '%s\n' "$AM_HITS" | sed 's/^/         /'
fi

echo ""
echo "==================================================="
echo "  部署资产门禁：PASS=${PASS}  FAIL=${FAIL}  SKIP=${SKIP}"
echo "==================================================="
[[ "$FAIL" -eq 0 ]]
