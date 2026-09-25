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
CHART_SVCS="$(grep -E '^  [a-z_]+_?[a-z_]*:' deploy/helm/opsmesh/values.yaml \
    | sed -E 's/^  ([a-z_]+):.*/\1/' | grep -E '_svc$|^grafana_bridge$' | sort)"

n_rel="$(echo "$RELEASE_SVCS" | grep -c . || true)"
n_dir="$(echo "$DIR_SVCS" | grep -c . || true)"
n_cht="$(echo "$CHART_SVCS" | grep -c . || true)"
echo "  release.yml=${n_rel}  services/=${n_dir}  chart=${n_cht}"

only_rel="$(comm -23 <(echo "$RELEASE_SVCS") <(echo "$DIR_SVCS") | tr '\n' ' ')"
only_dir="$(comm -13 <(echo "$RELEASE_SVCS") <(echo "$DIR_SVCS") | tr '\n' ' ')"
if [[ -z "${only_rel// }" && -z "${only_dir// }" ]]; then
    ok "release.yml matrix 与 services/ 目录完全对齐（${n_rel} 个）"
else
    [[ -n "${only_rel// }" ]] && bad "release.yml 有但 services/ 无目录：${only_rel}"
    [[ -n "${only_dir// }" ]] && bad "services/ 有目录但 release.yml 未构建：${only_dir}"
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

    # 3c ServiceMonitor 门禁：只允许为「真实暴露 /metrics」的服务生成采集项。
    # 源码依据：仅 device-svc / task-svc / alert-svc 在自身 HTTP 端口注册 /metrics，
    # 控制面为独立 metrics 端口 9091。
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
        --set services.bot_svc.enabled=true \
        --set services.deploy_svc.enabled=true \
        --set services.incident_svc.enabled=true \
        --set services.plugin_svc.enabled=true \
        --set services.runbook_svc.enabled=true \
        --set services.grafana_bridge.enabled=true \
        > "$RENDER_BIN" 2>/dev/null; then
        sm_count="$(grep -c 'kind: ServiceMonitor' "$RENDER_BIN" || true)"
        if [[ "$sm_count" = "4" ]]; then
            ok "ServiceMonitor 数量 = 4（控制面 + device/task/alert，符合源码）"
        else
            bad "ServiceMonitor 数量 = ${sm_count}（期望 4：控制面 + device/task/alert）"
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
OPSMESH_VERSION=0.9.0
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
    for f in deploy/docker/docker-compose.prod.yml; do
        out="$(docker compose --env-file "$TMPENV" -f "$f" config 2>&1 >/dev/null)"
        if [[ $? -eq 0 ]]; then
            ok "compose 渲染通过：${f}"
        else
            bad "compose 渲染失败：${f}"
            echo "$out" | head -12 | sed 's/^/         /'
        fi
    done
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

echo ""
echo "==================================================="
echo "  部署资产门禁：PASS=${PASS}  FAIL=${FAIL}  SKIP=${SKIP}"
echo "==================================================="
[[ "$FAIL" -eq 0 ]]
