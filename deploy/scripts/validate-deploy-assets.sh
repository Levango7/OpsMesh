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
    if kubeconform -strict -summary -ignore-missing-schemas $K8S_FILES >/dev/null 2>&1; then
        ok "kubeconform 校验通过（deploy/k8s/deployments）"
    else
        bad "kubeconform 校验失败："
        # shellcheck disable=SC2086
        kubeconform -strict -summary -ignore-missing-schemas $K8S_FILES 2>&1 | tail -10 | sed 's/^/         /'
    fi
elif command -v kubectl >/dev/null 2>&1; then
    out="$(kubectl apply --dry-run=client -f deploy/k8s/deployments/ 2>&1)"
    if [[ $? -eq 0 ]]; then
        ok "kubectl client dry-run 通过"
    else
        bad "kubectl client dry-run 失败："
        echo "$out" | head -10 | sed 's/^/         /'
    fi
else
    skip "未安装 kubeconform/kubectl，跳过 K8s 清单校验"
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

# ---------------------------------------------------------------
echo ""
echo "==================================================="
echo "  部署资产门禁：PASS=${PASS}  FAIL=${FAIL}  SKIP=${SKIP}"
echo "==================================================="
[[ "$FAIL" -eq 0 ]]
