#!/usr/bin/env bash
# deploy-verify.sh — OpsMesh 部署冒烟（第七批·实际部署验证，2026-11-05）
#
# 与 deploy/scripts/validate-deploy-assets.sh（2501 行静态门禁 PASS=87）的分工：
#   validate-deploy-assets.sh —— 纯静态：values/模板/组合文件一致性，不碰集群；
#   本脚本                    —— **真集群真渲染**：helm install 进 kind、等 rollout、
#                                对 k8s 探针与端点做活断言，最后 uninstall 清理。
# 两者互补：静态门禁回答「资产自洽吗」，本脚本回答「真装得上、真起来吗」。
#
# 冒烟范围（刻意保持最小闭环）：
#   默认 values 只拉起 controlplane + mysql + redis（12 个微服务默认 enabled:false，
#   存量部署零行为变化的既有设计）。本脚本**沿用默认形态**——冒烟的是「装得上、跑得起来、
#   探针真 200」，不是把全部微服务也拉一遍（那属全量 E2E 的事）。
#
# 用法：
#   bash deploy/scripts/deploy-verify.sh            # 完整冒烟（模板渲染→建集群→install→断言→卸载）
#   bash deploy/scripts/deploy-verify.sh --keep     # 保留集群与 release，便于事后排查
#   bash deploy/scripts/deploy-verify.sh --skip-install   # 只做 helm template 静态渲染断言
#   bash deploy/scripts/deploy-verify.sh --cluster NAME   # 指定 kind 集群名（默认 opsmesh-smoke）
#
# 前提：kind、kubectl、helm、docker 在 PATH；ghcr.io 可达（镜像公开）。
# 幂等：重复运行会先删旧集群（同名）再建；--skip-install 不动集群。
set -euo pipefail

CLUSTER_NAME="${OPSMESH_SMOKE_CLUSTER:-opsmesh-smoke}"
NS="opsmesh"
RELEASE="opsmesh"
KEEP=0
SKIP_INSTALL=0
ARTIFACT_DIR="${OPSMESH_SMOKE_ARTIFACT_DIR:-$(mktemp -d)}"
KUBECTL_TIMEOUT="${OPSMESH_SMOKE_TIMEOUT:-300}"

for arg in "$@"; do
  case "$arg" in
    --keep) KEEP=1 ;;
    --skip-install) SKIP_INSTALL=1 ;;
    --cluster) shift; CLUSTER_NAME="${1:-$CLUSTER_NAME}" ;;
    --cluster=*) CLUSTER_NAME="${arg#*=}" ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

CHART_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../helm/opsmesh" && pwd)"
PASS=0; FAIL=0

log()  { printf '[deploy-verify] %s\n' "$*"; }
ok()   { PASS=$((PASS+1)); printf '  PASS %s\n' "$*"; }
bad()   { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$*" >&2; }
section() { printf '\n== %s ==\n' "$*"; }

# ---------- 依赖前置检查（缺工具即红，不跑到一半才炸） ----------
# 本脚本要能在三种环境跑：Git Bash（本机）、CI runner、WSL（exec 层恰在 WSL 时）。
# WSL 调 Windows 侧的可执行文件需要 .exe 后缀（interop），且 PATH 常不含安装目录，
# 因此这里做「显式解析 + .exe 回退」。
find_tool() {
  local t="$1"
  if command -v "$t" >/dev/null 2>&1; then command -v "$t"; return 0; fi
  if command -v "${t}.exe" >/dev/null 2>&1; then command -v "${t}.exe"; return 0; fi
  # 常见安装目录回退（Windows 侧：Docker Desktop 的 bin、用户 bin 的 WSL 视图）
  for d in "/mnt/c/Program Files/Docker/Docker/resources/bin" \
           "/mnt/c/Users/${USER}/bin" "/mnt/c/Users/${USERNAME}/bin" \
           "$HOME/bin" /usr/local/bin /usr/bin; do
    if [[ -x "$d/$t" ]]; then echo "$d/$t"; return 0; fi
    if [[ -x "$d/${t}.exe" ]]; then echo "$d/${t}.exe"; return 0; fi
  done
  return 1
}
# 静态可见性：动态 export 的 TOOL_ 变量对扫描器不可见（SC2154），
# 故先显式声明再循环赋值（声明即「已赋值」的证明）。
TOOL_kind=""
TOOL_kubectl=""
TOOL_helm=""
TOOL_docker=""
missing=()
for tool in kind kubectl helm docker; do
  path="$(find_tool "$tool")" || { missing+=("$tool"); continue; }
  export "TOOL_${tool}=$path"
done
if [[ ${#missing[@]} -gt 0 ]]; then
  log "缺少依赖：${missing[*]}（PATH=$PATH）"
  exit 1
fi
if ! "$TOOL_docker" info >/dev/null 2>&1; then
  log "docker daemon 不可达——启动 Docker Desktop 后重试"
  exit 1
fi

PF_PID=""
# shellcheck disable=SC2317 # trap 注册的 EXIT 钩子：静态分析看不见调用面故判「不可达」；实为退出必经
cleanup() {
  if [[ -n "$PF_PID" ]]; then kill "$PF_PID" 2>/dev/null || true; fi
  if [[ $KEEP -eq 1 ]]; then
    log "--keep：集群/命名空间/release 保留（集群=$CLUSTER_NAME ns=$NS）"
    return
  fi
  if [[ $SKIP_INSTALL -eq 0 ]]; then
    log "卸载 release 并删除 kind 集群 $CLUSTER_NAME"
    "$TOOL_helm" uninstall "$RELEASE" -n "$NS" >/dev/null 2>&1 || true
    "$TOOL_kind" delete cluster --name "$CLUSTER_NAME" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

# ---------- 判据 1：helm template 静态渲染断言（不依赖集群） ----------
# 抓 values schema 错配、模板语法错、渲染后引用空值。这是 CI 静态门禁之外的
# 第二道保险：validate-deploy-assets.sh 校验资产文本，这里让 helm 真渲染一遍。
section "helm template 渲染"
RENDERED="$ARTIFACT_DIR/rendered.yaml"
if "$TOOL_helm" template "$RELEASE" "$CHART_DIR" -n "$NS" >"$RENDERED" 2>"$ARTIFACT_DIR/render.err"; then
  ok "helm template 渲染成功（$(wc -l <"$RENDERED") 行）"
else
  bad "helm template 渲染失败：$(head -5 "$ARTIFACT_DIR/render.err")"
  log "渲染失败属内容问题，无需继续"
  exit 1
fi
# 渲染面自证（防 template 成功但内容空泛）。默认形态的组件集**由 values 决定**：
# controlplane.store=memory 时 mysql/redis 的 StatefulSet 不渲染（有意设计——内置存储），
# 因此 Deployment/Service 是任何形态都必须存在的基线，StatefulSet 仅在 store=mysql 时要求。
DEFAULT_STORE=$(grep -m1 'store:' "$CHART_DIR/values.yaml" | awk '{print $2}' | tr -d '"')
if [[ "$DEFAULT_STORE" == "mysql" ]]; then NEED_STS=1; else NEED_STS=0; fi
if [[ $NEED_STS -eq 1 ]]; then STS_HINT='要求'; else STS_HINT='不要求（内置存储形态）'; fi
log "默认 store=$DEFAULT_STORE ⇒ StatefulSet 断言: $STS_HINT"
for must in "kind: Deployment" "kind: Service"; do
  if grep -q "$must" "$RENDERED"; then ok "渲染产物含 $must"; else bad "渲染产物缺 $must"; fi
done
if [[ $NEED_STS -eq 1 ]]; then
  if grep -q "kind: StatefulSet" "$RENDERED"; then ok "渲染产物含 StatefulSet（store=mysql）"; else bad "渲染产物缺 StatefulSet（store=mysql 必须渲染）"; fi
fi
# 部署面自证：默认形态至少拉起 controlplane Deployment 与探针（否则冒烟无对象）。
if grep -q "livenessProbe" "$RENDERED"; then ok "渲染产物含 livenessProbe"; else bad "渲染产物无 livenessProbe——探针口径丢失"; fi

# 生产 values 也要渲染一遍（values-production.yaml 若存在）。
if [[ -f "$(dirname "$CHART_DIR")/values-production.yaml" ]]; then
  if "$TOOL_helm" template "$RELEASE" "$CHART_DIR" -n "$NS" \
      -f "$(dirname "$CHART_DIR")/values-production.yaml" >"$ARTIFACT_DIR/rendered-prod.yaml" 2>&1; then
    ok "生产 values 渲染成功（$(wc -l <"$ARTIFACT_DIR/rendered-prod.yaml") 行）"
  else
    bad "生产 values 渲染失败：$(head -3 "$ARTIFACT_DIR/rendered-prod.yaml")"
  fi
fi

if [[ $SKIP_INSTALL -eq 1 ]]; then
  section "汇总"; log "--skip-install：仅完成渲染断言"
  printf '\n[deploy-verify] PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
  [[ $FAIL -eq 0 ]]
  exit $?
fi

# ---------- 判据 2：kind 集群 ----------
section "kind 集群"
EXISTING_CLUSTERS="$("$TOOL_kind" get clusters 2>/dev/null || true)"
if grep -qx "$CLUSTER_NAME" <<<"$EXISTING_CLUSTERS"; then
  log "同名集群已存在，先删后建（幂等）"
  "$TOOL_kind" delete cluster --name "$CLUSTER_NAME" >/dev/null 2>&1 || true
fi
if kind create cluster --name "$CLUSTER_NAME" --wait 120s >"$ARTIFACT_DIR/kind.log" 2>&1; then
  ok "kind 集群 $CLUSTER_NAME 就绪"
else
  bad "kind create cluster 失败：$(tail -5 "$ARTIFACT_DIR/kind.log")"
  exit 1
fi

# ---------- 判据 3：helm install ----------
section "helm install"
if "$TOOL_helm" install "$RELEASE" "$CHART_DIR" -n "$NS" --create-namespace \
    --wait --timeout "${KUBECTL_TIMEOUT}s" >"$ARTIFACT_DIR/install.log" 2>&1; then
  ok "helm install --wait 成功（rollout 全部就绪）"
else
  bad "helm install 失败：$(tail -8 "$ARTIFACT_DIR/install.log")"
  # ---- 失败诊断工件：无论原因，先把现场固化 ----
  "$TOOL_kubectl" get pods -n "$NS" >"$ARTIFACT_DIR/pods-after-fail.txt" 2>&1 || true
  "$TOOL_kubectl" get events -n "$NS" >>"$ARTIFACT_DIR/pods-after-fail.txt" 2>&1 || true
  "$TOOL_kubectl" describe pods -n "$NS" >>"$ARTIFACT_DIR/pods-after-fail.txt" 2>&1 || true
  # 关键容器日志（CrashLoopBackOff 的根因只在 logs 里；install --wait 超时多半是它）
  for c in controlplane agent; do
    pod="$("$TOOL_kubectl" get pods -n "$NS" -l "app.kubernetes.io/component=$c" \
      -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
    if [[ -n "$pod" ]]; then
      "$TOOL_kubectl" logs -n "$NS" "$pod" --tail=80 \
        >"$ARTIFACT_DIR/logs-$c.txt" 2>&1 || true
    fi
  done
  # ---- 失败分类：内容问题 vs 环境/吞吐 ----
  if grep -qE 'rate limiter|context deadline|Client\.Timeout|timed out waiting|connection refused|TLS handshake' "$ARTIFACT_DIR/install.log"; then
    log "失败定性：**环境/吞吐类**（API 限流或连接超时）——非资产内容问题；重跑或换干净集群。"
    log "本机实测过此类：同机已有多个 kind 集群 + docker 容器时，单节点 API server QPS 打满即出现。"
  else
    log "失败定性：**内容类**（渲染/配置/镜像拉取）——见 install.log 尾部。"
  fi
  log "诊断工件：pods-after-fail.txt（pods+events+describe）+ logs-controlplane.txt/logs-agent.txt（容器日志）"
  exit 1
fi

# ---------- 判据 4：pod 与就绪断言 ----------
section "rollout 状态"
# 与渲染段同口径：默认 store=memory 时只有 controlplane Deployment 一个工作负载。
# 资源名动态取（helm fullname 前缀），不写死 deploy/controlplane。
CP_DEPLOY="$("$TOOL_kubectl" get deploy -n "$NS" -l "app.kubernetes.io/component=controlplane" \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -z "$CP_DEPLOY" ]]; then
  bad "找不到 controlplane Deployment——安装形态与预期不符"
else
  if "$TOOL_kubectl" rollout status "deploy/$CP_DEPLOY" -n "$NS" \
      --timeout="${KUBECTL_TIMEOUT}s" >/dev/null 2>&1; then
    ok "rollout status deploy/$CP_DEPLOY"
  else
    bad "rollout status deploy/$CP_DEPLOY"
  fi
fi

POD_SNAPSHOT="$("$TOOL_kubectl" get pods -n "$NS" --no-headers 2>/dev/null || true)"
TOTAL_PODS=$(grep -c . <<<"$POD_SNAPSHOT" || true)
# READY 列形态 "1/1"：列 3（NAME READY STATUS RESTARTS AGE）——按 x==x 判全就绪。
READY_PODS=$(awk '{split($2,a,"/"); if (a[1]==a[2] && a[1]!="") n++} END {print n+0}' <<<"$POD_SNAPSHOT")
log "pod 快照：总计 $TOTAL_PODS / Running 且全容器就绪 $READY_PODS"
"$TOOL_kubectl" get pods -n "$NS" >"$ARTIFACT_DIR/pods.txt" 2>&1 || true
if [[ "$TOTAL_PODS" -ge 1 && "$TOTAL_PODS" -eq "$READY_PODS" ]]; then
  ok "全部 pod Running 且容器就绪（$READY_PODS/$TOTAL_PODS）"
else
  bad "存在未就绪 pod（$READY_PODS/$TOTAL_PODS）"
fi

# ---------- 判据 5：控制面健康端点活断言（端口转发） ----------
section "端点活断言"
CP_SVC="${RELEASE}-controlplane"
PF_LOCAL="${OPSMESH_SMOKE_PF_PORT:-18080}"
"$TOOL_kubectl" port-forward "svc/$CP_SVC" "${PF_LOCAL}:8080" -n "$NS" >"$ARTIFACT_DIR/pf.log" 2>&1 &
PF_PID=$!
sleep 3

assert_http() {
  local path="$1" want="$2"
  local code
  code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "http://127.0.0.1:${PF_LOCAL}${path}" || echo 000)
  if [[ "$code" == "$want" ]]; then
    ok "GET $path → $code"
  else
    bad "GET $path → $code（期望 $want）"
  fi
}
# /healthz = controlplane liveness（TD-77 通告：控制面不在微服务 /health 口径内）
assert_http "/healthz" 200
# /readyz = readiness（server_lifecycle.go 注册）
assert_http "/readyz" 200
# /version = 构建信息端点（无鉴权，P1-6 可支撑性）
assert_http "/version" 200
# SPA 外壳：企业版前端未内置构建产物时返回「未内置」说明页（非 404）
assert_http "/enterprise/" 200

section "汇总"
printf '[deploy-verify] PASS=%d FAIL=%d  工件目录：%s\n' "$PASS" "$FAIL" "$ARTIFACT_DIR"
if [[ $FAIL -gt 0 ]]; then
  log "失败详情见上方 FAIL 行；集群可用 --keep 保留排查"
  exit 1
fi
exit 0
