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
#  12. 微服务健康路径与端口键统一（TD-77：规范 /health+/ready、<N>_SVC_HTTP_PORT，
#      compose ↔ Helm 探针路径逐服务一致）
#  13. 引导脚本只建库+授权（建表职责归代码）
#  14. 出厂 PromQL 结构检查（CI 跑不了 promtool，钉住最常犯的四类写法错）
#  15. 告警送达链路接通性（规则 → Alertmanager → 外发通道）
#  16. CHANGELOG 归版账目（[Unreleased] 不得早于已发布版本日期）
#  17. 发布链顺序（构建腿只推不可变 :<sha>；promote 必须是提权唯一入口且在 Release 之前）
#  18. 入站边界（出厂 compose 发布的宿主端口除白名单外必须只绑环回；含条目数与独立正则对账，防判定面静默缩小）
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

# pyyaml 缺失探测（2026-10-05 实测加的）。
#
# 为什么必须单独探：多处判据是 `"$PY" - <<'PY' ... import yaml ... PY" || bad(...)`。
# 依赖缺失时 Python 抛 ModuleNotFoundError，退出码非 0，被 `||` 兜底吞成**业务判红**——
# 实测本机未装 pyyaml 时，alertmanager 判据报的是「出厂默认 AM 配置不合规」，
# 而配置本身完全合规（装上 pyyaml 后同段判据输出「出厂默认 AM 配置合规」）。
# 「环境缺依赖」被说成「资产不合规」会把排查带偏：去查配置，其实要装包。
#
# 这里只提示、不阻断：判据 ①②③④ 不依赖 yaml，缺包时它们仍有效。
if [[ -n "$PY" ]] && ! "$PY" -c 'import yaml' >/dev/null 2>&1; then
    echo "  [提示] 当前 ${PY} 缺 PyYAML：依赖 YAML 解析的判据（告警链路 ⑤、微服务 metrics 端口等）"
    echo "         会因 ModuleNotFoundError 被兜底成业务判红。装法：${PY} -m pip install pyyaml"
fi

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

# check_kv 只看**第一条**匹配（head -1）。这条在只有一个 pin 时够用，
# 但 values-production 从 2026-10-05 起给 12 个微服务各钉了一个 tag——
# 只核对第一条就等于"11 个 pin 可以静默落后一个版本"，而那正是 §32.9 记过的那一类
# （声明与产物脱节）。所以再加一层：**全部**匹配都必须等于 appVersion。
# 计数为 0 判红：模式失配时本节在空转，"绿"没有意义。
check_all_kv() {
    local file="$1" pattern="$2" label="$3"
    local hits v n=0 m=0 bad_vals="" lineno content
    hits="$(grep -nE "$pattern" "$file" 2>/dev/null || true)"
    if [[ -z "$hits" ]]; then
        bad "${label}：在 ${file} 没匹配到任何 ${pattern}（扫描面塌了，判红）"
        return
    fi
    while IFS= read -r line; do
        [[ -z "$line" ]] && continue
        n=$((n + 1))
        # grep -n 的前缀是 "行号:"；带上行号报点，否则判红只说"有一处不对"，
        # 下一次 bump 的人还得自己去找是哪一行——门禁的输出也是交付物。
        lineno="${line%%:*}"
        content="${line#*:}"
        # 只剥掉第一对引号之间的值：不 eval、不用 sed 反向引用拼命令，避免值里有特殊字符时炸开。
        v="${content#*\"}"; v="${v%%\"*}"
        if [[ "$v" != "$CHART_VERSION" ]]; then
            m=$((m + 1)); bad_vals="${bad_vals} ${file}:${lineno}=${v}"
        fi
    done <<< "$hits"
    if [[ "$m" -eq 0 ]]; then
        ok "${label}：${n} 处 tag 全部 = ${CHART_VERSION}"
    else
        bad "${label}：${n} 处里有 ${m} 处不等于 ${CHART_VERSION}:${bad_vals}"
    fi
}
check_all_kv deploy/helm/opsmesh/values-production.yaml     '^[[:space:]]*tag:[[:space:]]*"' "values-production 全部镜像 tag"
check_all_kv deploy/gitops/segments/production-segment.yaml '^[[:space:]]*tag:[[:space:]]*"' "gitops production-segment 全部镜像 tag"
# internal/version 的默认值也在版本源之列：`opsmesh --version` 与 GET /version 在
# **源码直构**（无 -ldflags 注入）时回的就是它，2026-10-04 实测它比 Chart.yaml 落后一整版
# （0.11.0 vs 0.12.0）而门禁毫无反应——第 1 节当时只比对清单，不知道二进制里也写着一个版本。
check_kv internal/version/version.go                       '^var Version'      "internal/version.Version 默认值"

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
# 服务清单在 release.yml 里**存在两份**（build-and-push 与 promote 各一个 matrix）：
# GitHub Actions 的 workflow 解析器不支持 YAML 锚点/别名（2026-10-05 实测：用
# &releaseServices / *releaseServices 做单一来源，整条 workflow 在 GitHub 端 0 秒挂，
# actionlint 同时判红），所以清单只能两处各抄一份。
# 于是本节不能再用 `grep -A40 'matrix:'`：那种抓法把两份清单叠成 24 行，
# 还会越界把 matrix 后面的普通 `- xxx` 行算进来。改成**按 matrix 块逐个解析**，
# 并顺带把"两处必须一致"从口头约定变成断言——漏改一处就是"发出去的"与"提权的"不是同一批服务。
if [[ -z "$PY" ]]; then
    skip "未找到 python3/python，跳过 matrix 分块解析（回退到旧的全量 grep，第 2 节可能误判）"
    RELEASE_SVCS="$(grep -A40 'matrix:' .github/workflows/release.yml \
        | grep -E '^\s+-\s+\S+$' | sed -E 's/^\s+-\s+//' | sort -u)"
    MATRIX_COUNT=0
    MATRIX_SAME=1
else
    MATRIX_JSON="$("$PY" - .github/workflows/release.yml <<'PY'
import json
import re
import sys

lines = open(sys.argv[1], encoding='utf-8').read().split('\n')
blocks = []
cur = None
for line in lines:
    if re.match(r'^      matrix:\s*$', line):
        cur = []
        blocks.append(cur)
        continue
    if cur is None:
        continue
    m = re.match(r'^          -\s+([A-Za-z0-9][A-Za-z0-9_-]*)\s*$', line)
    if m:
        cur.append(m.group(1))
        continue
    # 缩回到 job 层级即认为本 matrix 块结束（避免把 steps 里的 "- xxx" 收进来）
    if re.match(r'^    \S', line):
        cur = None
blocks = [b for b in blocks if b]
union = sorted({s for b in blocks for s in b})
same = len(blocks) >= 1 and all(b == blocks[0] for b in blocks)
print(json.dumps({'count': len(blocks), 'same': same, 'services': union,
                  'blocks': [sorted(b) for b in blocks]}))
PY
)"
    # Windows 的 python print 走文本模式 ⇒ 行尾 CRLF。comm 会把 "aio-svc\r" 当成
    # 与 "aio-svc" 不同的名字，于是同一批服务同时被报成"只在矩阵、不在目录"和反向。
    # 在捕获处统一剥掉 \r（Linux/CI 上无行可剥，判据不变）。
    MATRIX_JSON="$(printf '%s' "$MATRIX_JSON" | tr -d '\r')"
    # 必须再过一道 shell 的 sort：comm 要求两侧**同一种**排序，而 python 的 sorted() 是字节序、
    # shell sort 在 MSYS 下按语言序（会忽略连字符，"aio-svc" 排在 "alert-svc" 之后）。
    # 两边各排各的，comm 就会把同一批名字同时报成"只在左/只在右"。
    RELEASE_SVCS="$(printf '%s' "$MATRIX_JSON" | "$PY" -c 'import json,sys;print("\n".join(json.load(sys.stdin)["services"]))' | tr -d '\r' | sort)"
    MATRIX_COUNT="$(printf '%s' "$MATRIX_JSON" | "$PY" -c 'import json,sys;print(json.load(sys.stdin)["count"])')"
    MATRIX_SAME="$(printf '%s' "$MATRIX_JSON" | "$PY" -c 'import json,sys;print(1 if json.load(sys.stdin)["same"] else 0)')"
fi
# 不用 `ls services/ | sort`：SC2012 之外更要紧的是，管道左端换成 find 后，
# 目录名里的空格/前缀路径都不会污染结果（这里比对的是集合差，多一个字符就是误判）。
DIR_SVCS="$(find services -mindepth 1 -maxdepth 1 -type d 2>/dev/null \
    | sed 's|^services/||' | sort)"
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

# 两份 matrix 必须逐字一致（锚点不可用 ⇒ 一致性只能靠这里守）。
# 漂移的后果不是"数字难看"：build-and-push 少一个服务 ⇒ 那个服务没有 :<sha> 可提权，
# promote 会对着不存在的源 tag 失败；promote 少一个服务 ⇒ 该服务发得出 :<sha> 却永远
# 拿不到 :<版本>/:latest，客户按 chart 装就是 ErrImagePull。
if [[ "$MATRIX_COUNT" -eq 0 ]]; then
    skip "没解析出任何 matrix 块（release.yml 结构变了？本节其余比对仍按 union 走）"
elif [[ "$MATRIX_COUNT" -eq 1 ]]; then
    bad "release.yml 只解析出 1 个 matrix.service 清单——build-and-push 与 promote 应当各有一份"
    echo "         ⇒ 少一份通常意味着 promote job 的矩阵被删/改名，提权会漏发或整批不启动"
elif [[ "$MATRIX_SAME" -eq 1 ]]; then
    ok "release.yml 的 ${MATRIX_COUNT} 份 matrix.service 清单逐字一致（${n_rel} 个服务，各份都含全集）"
else
    bad "release.yml 的多份 matrix.service 清单**已经漂移**（${MATRIX_COUNT} 份，去重后 ${n_rel} 个）"
    echo "         ⇒ 各份内容：$(printf '%s' "$MATRIX_JSON" | "$PY" -c 'import json,sys;print(" | ".join(",".join(b) for b in json.load(sys.stdin)["blocks"]))')"
    echo "           发出去的镜像集合与被提权的集合不是同一批，必须同步（本项目在孪生清单上吃过亏）"
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
    if grep -qE 'failed (downloading|parsing) schema' <<<"$kc_out" ; then
        kc_schema_msg=1
    fi
    kc_parse_err=0
    if grep -qE 'error unmarshalling resource' <<<"$kc_out" ; then
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
# 检测必须按字节看 CR，不能用 grep/awk：
# 2026-09-26 实测 Git-Bash 的 `grep -c $'\r'` 与 `awk '/\r/'` 对确实含 CRLF 的文件都返回 0
# （MSYS 文本模式在读时吞 CR）。也就是说"用 grep 写的 CRLF 门禁在 Windows 上空转"——
# 而 Windows 正是它唯一要防的平台。Python 的二进制读同样能看见 CR，且不依赖 MSYS 文本模式。
#
# 为什么改成单次 Python 遍历（2026-10-05）：原实现对每个文件起 2 个管道进程
# （wc + tr）做字节数比对，41 个文件 = 82 次进程创建。**Windows 上实测这一节跑到
# 17 分钟仍未结束**，门禁事实等于不存在。改为单次遍历后同一范围 2.7 秒（对账：
# 两种实现命中文件数均为 41）。
if [[ -z "$PY" ]]; then
    skip "未找到 python3/python，跳过 CRLF 检查（本节需要字节级读取，grep/awk 在 MSYS 下不可靠）"
else
    CRLF_BAD="$("$PY" - <<'PY'
import os, fnmatch, sys

# 模式集严格对齐原 find：'Dockerfile*' '.dockerignore' 'docker-compose*.yml'
# '*.sh' '*.tpl' 'Chart.yaml' 'values*.yaml'。改这里等于改扫描范围——
# 变异验证时正是靠"少扫一个"来证明本断言不是恒绿。
EXACT = {".dockerignore", "Chart.yaml"}

def hit(fn):
    return (fn in EXACT
            or fn.startswith("Dockerfile")
            or fnmatch.fnmatch(fn, "docker-compose*.yml")
            or fn.endswith(".sh")
            or fn.endswith(".tpl")
            or fnmatch.fnmatch(fn, "values*.yaml"))

bad = []
for root, dirs, files in os.walk(".", topdown=True):
    dirs[:] = [d for d in dirs if d != ".git"]
    for fn in files:
        if not hit(fn):
            continue
        p = os.path.join(root, fn)
        if not os.path.isfile(p):
            continue
        try:
            with open(p, "rb") as fh:
                if b"\r" in fh.read():
                    bad.append(p[2:] if p.startswith("./") else p)
        except OSError as e:
            # 读不到就报出来，不能静默跳过——否则「没扫到」会被当成「没问题」。
            bad.append(f"{p} (读取失败: {e})")

print(" ".join(bad))
PY
)"
    if [[ -n "${CRLF_BAD// }" ]]; then
        bad "以下部署资产含 CRLF（Windows 检出后 docker/helm/bash 会坏）：$CRLF_BAD"
        echo "         修法：git add --renormalize <file>，并确认 .gitattributes 覆盖该文件类型"
    else
        ok "Dockerfile/compose/脚本/Helm 模板均为 LF（无 CRLF 破坏风险）"
    fi
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
    grep -qx "$d" <<<"$RELEASE_SVCS" && continue
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
    grep -qw "$svc" <<<"$NOT_SCRAPED_EXEMPT" && continue
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
    grep -qw "$svc" <<<"$NOT_SCRAPED_EXEMPT" || continue
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
elif am_alert="$(grep -A 6 '^alerting:' "$AM_PROM" 2>/dev/null)"; ! grep -qE 'alertmanager:[0-9]+' <<<"$am_alert"; then
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

# ⑥ 用**真 Alertmanager 的 schema** 校验渲染结果：YAML 合法 ≠ AM 认这些字段。
#    本机实测（2026-10-02，#54 端到端投递验证）：deploy.sh 渲染出的 http_headers / headers /
#    timeout 三个键 YAML 全合法、上面 ⑤ 的引用检查也全过，但 prom/alertmanager:v0.27.0
#    逐个报 "field X not found in type config.plain" 并进入崩溃重启循环——填了
#    ALERT_WEBHOOK_URL 的客户会摊上一个起不来的告警组件，而 ⑤ 和 CI 当时都毫无感觉。
#    amtool 就在那个镜像里，是同一份 schema 的权威实现，比查文档可靠（timeout 是否存在
#    这件事，实测结论就和我的预期相反）。
AM_IMG="$(grep -oE 'image: prom/alertmanager:[^ ]+' "$AM_COMPOSE" | head -1 | sed 's/^image: //')"
if [ -z "$AM_IMG" ]; then
    AM_HITS="${AM_HITS} 无法从 $AM_COMPOSE 读出 alertmanager 镜像 tag（第 ⑥ 项无从下手）"
elif ! command -v docker >/dev/null 2>&1; then
    skip "无 docker：跳过真 amtool 校验 AM 配置（第 ⑤ 项的 YAML/引用检查仍在）"
else
    if ! docker image inspect "$AM_IMG" >/dev/null 2>&1; then
        if ! docker pull "$AM_IMG" >/dev/null 2>&1; then
            skip "取不到镜像 $AM_IMG（离线或限流）：真 amtool 校验跳过，第 ⑤ 项仍在"
            AM_IMG=""
        fi
    fi
    if [ -n "$AM_IMG" ]; then
        AM_TMP="$(mktemp -d)"
        printf 'ALERT_WEBHOOK_URL=http://sink.invalid:9919/alert\nALERT_WEBHOOK_BEARER=schema-probe-token\n' \
            > "${AM_TMP}/.env.probe"
        # 探针产物必须放开读权限——这条是 CI 教我的（2026-10-03）：
        # render_alertmanager_config() 结尾对**真实生成物**做 `chmod 600`（里面有 bearer），
        # 而 prom/alertmanager 镜像的默认用户是 `nobody`(65534)；`mktemp -d` 又是 0700。
        # 于是在 Linux 上 amtool 直接 `stat /c/rendered.yml: permission denied`，
        # 门禁报的是"配置不合法"这种假红；本机 Windows 上 NTFS 不强制权限，同一条命令全绿
        # ⇒ 这类缺陷只在 CI 暴露，别拿本机结果当数。
        # 探针文件里只有合成值（sink.invalid + schema-probe-token），放开读没有泄露面；
        # **绝不要把这里的 .env.probe 换成真 .env**，那等于把 bearer 变成全局可读。
        chmod 755 "$AM_TMP" 2>/dev/null || true
        # 调产品自己的渲染函数（子 shell 里 source：deploy.sh 顶部有 set -euo pipefail，
        # 直接在主进程 source 会把 -e 带进本门禁，之后任何非零返回都会让整脚本中途退出）。
        # ENV_FILE / ALERTMANAGER_OUT 在 source 之后覆盖：合成外发键、且绝不碰真 .env 与生成物。
        (
            # shellcheck disable=SC1091  # 被 source 的脚本由仓库提供，非固定路径可静态解析
            source deploy/docker/scripts/deploy.sh >/dev/null 2>&1 || exit 91
            # 这两个变量是被 source 进来的 render_alertmanager_config 读的；shellcheck 看不穿
            # source，才报 SC2034"未使用"。禁用精确到 SC2034，不做整文件/整脚本的 -e。
            # shellcheck disable=SC2034
            ENV_FILE="${AM_TMP}/.env.probe"
            # shellcheck disable=SC2034
            ALERTMANAGER_OUT="${AM_TMP}/rendered.yml"
            render_alertmanager_config >/dev/null 2>&1
        )
        if [ ! -s "${AM_TMP}/rendered.yml" ]; then
            AM_HITS="${AM_HITS} 用合成 ALERT_WEBHOOK_URL 渲染失败（deploy.sh 的渲染函数返回非零或产出空文件）"
        elif ! grep -q 'webhook_configs' "${AM_TMP}/rendered.yml"; then
            # 空转检测：渲染结果里根本没有外发段，那"amtool 接受"就只证明了默认配置能起。
            AM_HITS="${AM_HITS} 渲染产物里没有 webhook_configs 段——第 ⑥ 项无从判定外发形状"
        else
            # Git Bash 下 docker.exe 需要宿主形态路径；MSYS_NO_PATHCONV 保证容器内 /c 不被改写。
            # `pwd -W` 只有 MSYS/Git Bash 有；把它做成显式的"前者不成则后者"分组，
            # 而不是 `cd && pwd -W || pwd`——后者在 cd 失败时也会跑 fallback，那才是真语义错。
            AM_WIN="$(cd "${AM_TMP}" && { pwd -W 2>/dev/null || pwd; })"
            chmod 644 "${AM_TMP}/rendered.yml" 2>/dev/null || true
            amtool_check() {
                MSYS_NO_PATHCONV=1 docker run --rm -v "${AM_WIN}:/c:ro" --entrypoint amtool \
                    "$AM_IMG" check-config "/c/$1" 2>&1
            }
            if GOOD_OUT="$(amtool_check rendered.yml)"; then
                ok "真 amtool（${AM_IMG##*:}）接受渲染出的外发配置"
            else
                AM_HITS="${AM_HITS} 真 amtool 拒绝渲染出的外发配置：$(printf '%s' "$GOOD_OUT" | tr '\n' ' ')"
            fi
            # 变异样本：证明上面那句真的会红。用本次实测到的**历史缺陷形状**（http_headers），
            # 它必须被拒；amtool 若接受，说明第 ⑥ 项在空转。
            cat > "${AM_TMP}/mutant.yml" <<EOF
route:
  receiver: "default"
receivers:
  - name: "default"
    webhook_configs:
      - url: "http://sink.invalid:9919/alert"
        send_resolved: true
        http_headers:
          Authorization: "Bearer x"
EOF
            chmod 644 "${AM_TMP}/mutant.yml" 2>/dev/null || true
            if BAD_OUT="$(amtool_check mutant.yml)"; then
                AM_HITS="${AM_HITS} 变异样本 http_headers 被 amtool 接受——第 ⑥ 项在空转：$(printf '%s' "$BAD_OUT" | tr '\n' ' ')"
            else
                ok "变异样本 http_headers 被真 amtool 拒绝（第 ⑥ 项确实在判定）"
            fi
        fi
        rm -rf "${AM_TMP}"
    fi
fi
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

# M7 业务告警通道必须在**出厂 compose** 里有入口：它过去只有代码里的旗标
# （--alert-webhook-url / OPSMESH_ALERT_WEBHOOK_URL），Docker 客户不改 compose 就配不了，
# 于是"业务告警可以外发"这句话对 compose 部署形态其实不成立。
M7_HITS=""
if ! grep -q 'OPSMESH_ALERT_WEBHOOK_URL' "$AM_COMPOSE"; then
    M7_HITS="${M7_HITS} compose 的 controlplane 没暴露 OPSMESH_ALERT_WEBHOOK_URL"
fi
if ! grep -q 'OPSMESH_WEBHOOK_ALLOW_PRIVATE' "$AM_COMPOSE"; then
    M7_HITS="${M7_HITS} compose 没暴露 OPSMESH_WEBHOOK_ALLOW_PRIVATE（内网收件端无法显式放行）"
fi
# compose 引用了就必须出现在 .env 模板里，否则新装时是一个没人能填的空引用。
if ! grep -q 'CONTROLPLANE_ALERT_WEBHOOK_URL=' deploy/docker/scripts/deploy.sh; then
    M7_HITS="${M7_HITS} deploy.sh 的 .env 模板缺 CONTROLPLANE_ALERT_WEBHOOK_URL"
fi
if ! grep -q 'ALERT_WEBHOOK_ALLOW_PRIVATE=' deploy/docker/scripts/deploy.sh; then
    M7_HITS="${M7_HITS} deploy.sh 的 .env 模板缺 ALERT_WEBHOOK_ALLOW_PRIVATE"
fi
if [ -z "${M7_HITS// }" ]; then
    ok "M7 业务告警通道在 compose 与 .env 模板两侧都有入口（变量对齐，不会渲染出没人能填的空引用）"
else
    bad "M7 业务告警通道入口不全："
    printf '%s\n' "$M7_HITS" | sed 's/^/         /'
fi

# ---------------------------------------------------------------------------
# TD-77：微服务健康路径与端口环境变量名的统一口径门禁
#
# 背景：此前健康端点三套并存（/health、/healthz、/api/v1/health），端口键也三套
# （<N>_SVC_HTTP_PORT、AIO_SVC_PORT、LOG_SVC_HEALTH_ADDR）。这类分叉的代价不是
# "起不来"——出厂栈能跑纯粹因为 compose 把两侧写死成同一个数——而是"改不动"：
# 改 .env 的 *_HTTP_PORT 只动宿主侧、不动容器内监听。本门禁把三条不变量钉住，
# 让分叉不能再无声长回来。
#
# 为什么用追加式而不是并进已有节：本脚本第 1 节的 check_all_kv 由另一条工作线
# 维护（见 docs/COORDINATION.md 通告），改动他人正在维护的节会制造无谓冲突。
# ---------------------------------------------------------------------------

# P0 前置：被断言的文件必须真的读得到。
#
# 这不是形式主义。首版 TD-77 段没有这道前置，结果在「工作目录不对」时
# grep 与 python 双双报 "No such file or directory"，而 P2–P5 因为
# 「没匹配到任何违规」而**全部判绿**——门禁在最需要它的时候（部署资产真的
# 出了问题）恰恰给出假信号。一条永远绿的检查比没有检查更坏：
# 它让人以为这块有网。
#
# 判红而不是跳过：读不到 compose 意味着后面五条断言的结论全部无意义。
if [ ! -r "$AM_COMPOSE" ]; then
    bad "TD-77-P0 读不到 ${AM_COMPOSE}（工作目录或文件权限不对）——后续 TD-77 断言全部无意义，已判红"
    echo "         （当前工作目录：$(pwd)）"
elif [ ! -r deploy/helm/opsmesh/values.yaml ]; then
    bad "TD-77-P0 读不到 deploy/helm/opsmesh/values.yaml（工作目录或文件权限不对）——跨资产比对无法进行"
else
    ok "TD-77-P0 部署资产可读（compose 与 Helm values 均在位）"
fi

# P1 规范存活路径必须存在。
#
# 这里刻意**不禁止**旧路径（/healthz、/api/v1/health）：过渡期它们是合法的别名，
# 目的是外部探针不断。真正的收口动作是下个版本摘别名，届时把本断言反过来写成
# "旧路径不得再出现在部署资产里"即可。只禁新路径的话，别名消失会导致探针 404，
# 而那正是本门禁要防的事故。
#
# 这里只断言 /health，**不断言 /ready**：compose healthcheck 只有一个 test 字段，
# 语义是存活（liveness）；/ready 是 Helm/K8s 侧的 readiness 探针，compose 里本就不该有。
# 早先这里把两个路径一起断言，结果 /ready 恒缺——门禁自己变成假失败。
# /ready 的存在性由 P5 与各服务的 Go 契约测试覆盖，不在此处重复。
HEALTH_HITS=""
if ! grep -q '"http://localhost:[0-9]\{1,5\}/health"' "$AM_COMPOSE" 2>/dev/null; then
    HEALTH_HITS="${HEALTH_HITS} compose 无任何服务使用规范存活探针路径 /health"
fi
if [ -z "${HEALTH_HITS// }" ]; then
    ok "TD-77-P1 compose 存活探针使用规范路径 /health"
else
    bad "TD-77-P1 规范健康路径缺失："
    printf '%s\n' "$HEALTH_HITS" | sed 's/^/         /'
fi

# P2 compose 里不得再出现已废弃的探针路径。
# 容器 healthcheck 一旦指向不存在的路径，容器永远 unhealthy——症状是
# "部署成功但服务永远不起来"，且日志里只有 wget 失败，很容易误判成镜像问题。
DEPRECATED_PROBE=""
if grep -qE '"http://localhost:[0-9]{1,5}/(healthz|readyz|api/v1/health)"' "$AM_COMPOSE" 2>/dev/null; then
    DEPRECATED_PROBE="$(grep -nE '"http://localhost:[0-9]{1,5}/(healthz|readyz|api/v1/health)"' "$AM_COMPOSE" 2>/dev/null | head -5)"
elif [ ! -r "$AM_COMPOSE" ]; then
    # 文件读不到时"没匹配到违规"是假绿，必须显式判红（见 P0 说明）。
    DEPRECATED_PROBE="compose 不可读，无法判定是否残留历史探针路径"
fi
if [ -z "${DEPRECATED_PROBE// }" ]; then
    ok "TD-77-P2 compose 探针无历史路径残留（/healthz、/readyz、/api/v1/health 已全部切走）"
else
    bad "TD-77-P2 compose 仍有服务在探历史路径（应为规范路径 /health）："
    printf '%s\n' "$DEPRECATED_PROBE" | sed 's/^/         /'
fi

# P3 端口键口径：compose 不得再注入已废弃的键。
# 容器注入了 AIO_SVC_PORT 而宿主映射读 AIO_SVC_HTTP_PORT 时，改 .env 只会改到
# 宿主侧——服务仍在旧端口上监听，表现为"改了配置没生效"，是最难自查的一类。
DEPRECATED_ENV=""
for _k in AIO_SVC_PORT LOG_SVC_HEALTH_ADDR; do
    if grep -qE "^      $_k:" "$AM_COMPOSE" 2>/dev/null; then
        DEPRECATED_ENV="${DEPRECATED_ENV} $_k"
    fi
done
if [ -z "${DEPRECATED_ENV// }" ]; then
    ok "TD-77-P3 compose 无废弃端口键注入（AIO_SVC_PORT、LOG_SVC_HEALTH_ADDR 已切规范键）"
else
    bad "TD-77-P3 compose 仍注入已废弃的端口键：${DEPRECATED_ENV}"
    echo "         （宿主映射与容器注入必须用同一个 *_SVC_HTTP_PORT，否则改 .env 只动一侧）"
fi

# P4 端口键宿主侧 ↔ 容器侧必须同名。
# 断言的是「同一个键出现在 ports 映射与 environment 注入两侧」，
# 而不是端口数值相等——数值相等是巧合，同名才是可维护性。
# 逐服务检查，任何一个服务两侧键名不同即报出服务名与两个键。
if [ -z "$PY" ]; then
    skip "未找到 python3/python，跳过 TD-77-P4 端口键宿主/容器同名校验"
else
    PORTKEY_OUT="$("$PY" - "$AM_COMPOSE" <<'PY'
import re, sys, io

path = sys.argv[1]
text = io.open(path, encoding="utf-8", newline="").read().replace("\r\n", "\n")

# 按顶层服务名切块（缩进 2 空格的 "<name>:"）。
blocks = {}
cur = None
for line in text.split("\n"):
    m = re.match(r"^  ([A-Za-z0-9_.-]+):\s*$", line)
    if m:
        cur = m.group(1)
        blocks[cur] = []
        continue
    if cur is not None:
        blocks[cur].append(line)

bad = []
for name, lines in blocks.items():
    body = "\n".join(lines)
    # 宿主映射："127.0.0.1:${KEY:-NNNN}:NNNN"
    host_keys = set(re.findall(r'\$\{([A-Z0-9_]*SVC_HTTP_PORT)[:-]', body))
    # 容器注入："      KEY: NNNN"
    env_keys = set(re.findall(r"^      ([A-Z0-9_]*SVC_HTTP_PORT):\s*\d+", body, re.M))
    if not host_keys and not env_keys:
        continue  # 该服务不经 *_SVC_HTTP_PORT 暴露（如 mysql/loki/prometheus），不在本门禁范围
    for k in sorted(host_keys | env_keys):
        in_host = k in host_keys
        in_env = k in env_keys
        if in_host and not in_env:
            bad.append(f"{name}: 宿主映射用 {k}，但容器未注入该键（容器监听端口将回落到代码默认值）")
        if in_env and not in_host:
            bad.append(f"{name}: 容器注入 {k}，但宿主映射未引用该键（改 .env 不会影响已注入的容器）")

print("\n".join(bad))
PY
)" || PORTKEY_OUT="${PORTKEY_OUT:-compose 或脚本执行失败，无法判定端口键一致性}"
    if [ -z "${PORTKEY_OUT// }" ]; then
        ok "TD-77-P4 compose 每个微服务的端口键宿主映射与容器注入同名（改一次 .env 两侧都生效）"
    else
        bad "TD-77-P4 端口键宿主/容器不同名："
        printf '%s\n' "$PORTKEY_OUT" | sed 's/^/         /'
    fi
fi

# P5 12 个微服务的健康路径在 compose 与 Helm values 两侧必须逐字一致。
# 跨资产漂移的症状极隐蔽：helm 部署探针 404，容器照常跑，只是永远 not ready。
if [ -z "$PY" ]; then
    skip "未找到 python3/python，跳过 TD-77-P5 compose ↔ Helm 探针路径一致性校验"
else
    PROBEPATH_OUT="$("$PY" - "$AM_COMPOSE" deploy/helm/opsmesh/values.yaml <<'PY'
import re, sys, io

compose_path, values_path = sys.argv[1], sys.argv[2]
ctext = io.open(compose_path, encoding="utf-8", newline="").read().replace("\r\n", "\n")
vtext = io.open(values_path, encoding="utf-8", newline="").read().replace("\r\n", "\n")

# compose：服务名 -> 探针路径
cur = None
compose_probe = {}
for line in ctext.split("\n"):
    m = re.match(r"^  ([A-Za-z0-9_.-]+):\s*$", line)
    if m:
        cur = m.group(1)
        continue
    m = re.search(r'wget.*?-q",\s*"http://localhost:\d{1,5}(/[\w/-]*)"', line)
    if m and cur:
        compose_probe[cur] = m.group(1)

# values.yaml：<svc>_svc 段 -> probe.path
values_probe = {}
cur = None
for line in vtext.split("\n"):
    m = re.match(r"^  ([a-z0-9_]+):\s*$", line)
    if m:
        cur = m.group(1)
        continue
    m = re.match(r"^      path:\s*(/\S*)\s*$", line)
    if m and cur:
        values_probe[cur] = m.group(1)

bad = []
for svc, path in sorted(compose_probe.items()):
    if not svc.endswith("-svc"):
        continue
    key = svc.replace("-", "_")
    if key not in values_probe:
        continue  # helm 未纳管该服务（如 tf-provider），不在比对范围
    if values_probe[key] != path:
        bad.append(f"{svc}: compose 探针 {path}，Helm 探针 {values_probe[key]}")

# 反向：helm 纳管但 compose 没有对应服务的键，报出来避免"只改了一边"。
for key, path in sorted(values_probe.items()):
    svc = key.replace("_", "-")
    if svc.endswith("-svc") and svc not in compose_probe:
        bad.append(f"{key}: Helm 探针 {path}，但 compose 无该服务（两侧服务矩阵应一致）")

print("\n".join(bad))
PY
)" || PROBEPATH_OUT="${PROBEPATH_OUT:-compose 或 values 读取失败，无法判定跨资产探针一致性}"
    if [ -z "${PROBEPATH_OUT// }" ]; then
        ok "TD-77-P5 compose ↔ Helm 探针路径逐服务一致（两侧不会各自漂移）"
    else
        bad "TD-77-P5 探针路径跨资产漂移："
        printf '%s\n' "$PROBEPATH_OUT" | sed 's/^/         /'
    fi
fi

echo ""
echo "=== 16. CHANGELOG 归版账目（[Unreleased] 不得早于已发布版本）==="
# 为什么必须钉这一条（2026-10-06 实测）：CHANGELOG 里有 68 个 `## [Unreleased]` 块，其中 54 个
# 的正文早已出现在 v0.8.0…v0.11.0 的**发布正文**里。取证例：CHANGELOG.md:1885「刷新 401 自等待
# 死锁」所属块由提交 0f77e0d（2026-08-30）写入，`git tag --contains 0f77e0d` 含 v0.8.0——
# 也就是随 0.8.0 就发出去了，标题却一直写着未发布。历次切版只往文件顶部加 `## [0.x.y]` 摘要，
# 从未重命名过这些历史标题（各 tag 上的计数：v0.10.0=44、v0.11.0=54、v0.12.0=54，净变更 0）。
# 客户按 Keep-a-Changelog 读这份文件，会把 41 项已交付的东西当成欠账。
#
# 判据为什么是"日期比较"而不是"查 git tag"：本门禁所在的 CI job 是浅检出（未设 fetch-depth: 0），
# 拿不到 tag 列表；真要查 tag 会让这道门禁在 CI 里静默空转——那是比没门禁更糟的形态。
# "早于最新发布版本日期却仍标 Unreleased"只用文件自身就能判红，且能抓住本次这一整类错标。
if [[ -f CHANGELOG.md ]]; then
    if [[ -z "$PY" ]]; then
        bad "本机/CI 没有 python，第 16 节无法执行（判红而不是跳过）"
    else
        CL_OUT="$("$PY" - CHANGELOG.md <<'PY'
import re, sys
lines = open(sys.argv[1], encoding="utf-8").read().split("\n")
# 标题行的严格定义：`## [x.y.z]` / `## [Unreleased]` 独占一行，或后跟 " — 描述"。
# 为什么这么严：正文里存在以 `## [Unreleased]` 开头的**散文行**（例如"把 `## [Unreleased]`
# 一次性改成 `## [0.12.0]`"），变异验证时它被当成了无日期标题 → 假阳性判红。
# 判据一旦会因散文误报，下一次真错标就会被当成"又是老毛病"而被忽略。
def is_unreleased(l):
    return l == "## [Unreleased]" or l.startswith("## [Unreleased] — ")

def is_version(l):
    return bool(re.match(r"^## \[\d+\.\d+\.\d+\]( — |$)", l))

date = re.compile(r" — (\d{4}-\d{2}-\d{2})")
rel_dates = [date.search(l).group(1) for l in lines if is_version(l) and date.search(l)]
if not rel_dates:
    print("NO_RELEASED_VERSION")
    sys.exit(0)
cut = max(rel_dates)
n_ver = sum(1 for l in lines if is_version(l))
bad, n_un = [], 0
for i, l in enumerate(lines, 1):
    if not is_unreleased(l):
        continue
    n_un += 1
    m = date.search(l)
    d = m.group(1) if m else None
    if not d:
        bad.append(f"L{i}: [Unreleased] 标题没有日期，无法判它是否该归版：{l[:56]}")
    elif d < cut:
        bad.append(f"L{i}: 日期 {d} 早于最新发布版本日期 {cut}，却仍标 [Unreleased]：{l[:56]}")
print(f"SUMMARY released_max={cut} released_blocks={n_ver} unreleased={n_un} offenders={len(bad)}")
print("\n".join(bad))
PY
)"
        if grep -qF 'NO_RELEASED_VERSION' <<<"$CL_OUT"; then
            bad "CHANGELOG.md 里一个已发布版本标题（## [x.y.z] — 日期）都没有——本节失去基准，判红而不是放行"
        else
            CL_SUMMARY="$(grep -F 'SUMMARY released_max=' <<<"$CL_OUT" | head -1)"
            offenders="$(printf '%s' "$CL_SUMMARY" | sed -n 's/.*offenders=\([0-9][0-9]*\).*/\1/p')"
            offenders="${offenders:-}"
            if [[ -z "$offenders" ]]; then
                bad "第 16 节没解析出 SUMMARY（判据在空转，判红）：${CL_SUMMARY:-空}"
            elif [[ "$offenders" -gt 0 ]]; then
                bad "CHANGELOG 归版账目错标 ${offenders} 处（已发货却仍标 [Unreleased]）：${CL_SUMMARY#SUMMARY }"
                grep -vE '^(SUMMARY |NO_RELEASED_VERSION$)' <<<"$CL_OUT" | grep -vE '^[[:space:]]*$' | sed 's/^/         /'
            else
                ok "CHANGELOG 归版账目自洽（${CL_SUMMARY#SUMMARY }）"
            fi
        fi
    fi
else
    bad "找不到 CHANGELOG.md，第 16 节无从核对（判红）"
fi

echo ""
echo "=== 17. 发布链顺序（gate-before-promote：构建腿只推不可变 :<sha>，浮动 tag 必须在门禁之后）==="
# 为什么钉这一节：`9b23b90` 把 release.yml 改成"构建只推 :<sha>，:版本 与 :latest 由 promote job
# 在 Trivy/SBOM/签名全绿后改标"。这条性质**只在打 tag 那一刻才第一次执行**（release.yml 只在 v* 触发），
# 而本项目已经在"只在发版时才执行的路径"上栽过两次（v0.9.1 有 tag 无产物、v0.12.0 归版把 pin 指到
# 从未发布的版本）。谁日后把 needs 顺序调回去、或在构建腿里顺手加回 `--tag …:latest`，
# 常规 CI 完全看不见——直到下一次真实发布把未过扫描的镜像推成客户默认拉到的那一份。
# 实测依据（2026-10-06）：run `37379548900` 就是 main 推送误触发 release.yml，job 数为 0，
# 靠 needs 链才没有让 promote 启动；那次侥幸不能当下一次保障。
if [[ -f .github/workflows/release.yml ]]; then
    if [[ -z "$PY" ]]; then
        bad "本机/CI 没有 python，第 17 节无法执行（判红而不是跳过）"
    else
        CHAIN_OUT="$("$PY" - .github/workflows/release.yml <<'PY'
import re, sys
try:
    import yaml
except ImportError:
    print("NO_YAML")
    sys.exit(0)

doc = yaml.safe_load(open(sys.argv[1], encoding="utf-8"))
jobs = (doc or {}).get("jobs") or {}
if not jobs:
    print("NO_JOBS")
    sys.exit(0)

def needs(name):
    j = jobs.get(name)
    if j is None:
        return None
    n = j.get("needs")
    if n is None:
        return []
    return [n] if isinstance(n, str) else list(n)

errs = []
required = ["build-and-push", "promote", "github-release"]
for j in required:
    if j not in jobs:
        errs.append(f"缺少 job：{j}（发布链结构变了，本节失去比较对象）")

if "promote" in jobs and "build-and-push" not in needs("promote"):
    errs.append("promote 不再 needs build-and-push ⇒ 未过扫描的镜像可能被提权成 :<版本>/:latest")
gr = needs("github-release")
if gr is not None and "promote" not in gr:
    errs.append("github-release 不再 needs promote ⇒ 提权失败也会对外发布 Release")

# 串行闸：发布链必须是"同一时刻只跑一条"。两个 tag 短间隔推送时，promote 会并发重打 :latest，
# 最终 :latest 由最后完成者而不是版本最新者决定（与 §20 里"feature 分支刻意不打 latest"同源）。
# cancel-in-progress 必须显式为 false：true 会掐断正在发布的 run——镜像推一半、Release 建一半，
# 比排队等它跑完危险得多。
conc = (doc or {}).get("concurrency")
if conc is None:
    conc_desc = "缺失"
    errs.append("release.yml 没有顶层 concurrency ⇒ 两个 tag 短间隔推送会并发跑两条发布链，:latest 由最后完成者决定")
elif isinstance(conc, str):
    conc_desc = f"{conc}(cancel 未显式设置)"
    errs.append("concurrency 只给了 group 字符串、没显式写 cancel-in-progress: false ⇒ 形态漂移时容易误开取消")
elif isinstance(conc, dict):
    grp = conc.get("group")
    cip = conc.get("cancel-in-progress")
    if not grp:
        errs.append("concurrency 缺 group（每条 run 自成一组 = 串行效果为零）")
    elif "${{" in str(grp) or "github." in str(grp) or "run_id" in str(grp) or "run_number" in str(grp):
        # 关键一条：group 带任何"每条 run / 每个 ref 唯一"的量，跨 tag 就不同组 ⇒ 并发照旧，
        # 而判据若只看"有没有 concurrency"就会给这种写法开绿灯（实测漏过一次，见报告 §41）。
        errs.append(f"concurrency.group 含动态量（{grp}）⇒ 每条 run 自成一组或按 ref 分组，跨 tag 的串行效果为零；group 必须是字面量")
    if cip is not False:
        errs.append(f"concurrency.cancel-in-progress 必须显式为 false（当前={cip!r}）⇒ true 会掐断正在发布的 run")
    conc_desc = f"group={grp} cancel={cip}"
else:
    conc_desc = str(type(conc))
    errs.append(f"concurrency 形态不认识（既不是映射也不是字符串）：{type(conc)}")

# 构建腿的 tag 集合：docker buildx 的 --tag 实参，以及 build-push-action 的 with.tags
steps = (jobs.get("build-and-push") or {}).get("steps") or []
refs = []
for st in steps:
    run = st.get("run") or ""
    # --tag 的实参里带 `${{ … }}`（内含空格），所以必须**按行取到行尾**再去掉续行反斜杠，
    # 不能按"空白分隔的单 token"取——那样只会截到 `${{`，判据整体失真。
    for line in run.splitlines():
        m = re.match(r"\s*--tag\s+(.+?)\s*\\?\s*$", line)
        if m:
            refs.append(m.group(1).strip().strip('"').strip("'"))
    for k, v in (st.get("with") or {}).items():
        if str(st.get("uses", "")).startswith("docker/build-push-action") and k == "tags":
            refs += [x.strip() for x in str(v).split(",") if x.strip()]

if not refs:
    errs.append("build-and-push 里一个 --tag/tags 都没解析出来（本节在空转，判红）")
for r in refs:
    if r.endswith(":latest"):
        errs.append(f"构建腿直接推送 :latest ⇒ 扫描判红的镜像会立刻成为客户默认拉到的那份：{r}")
    elif "github.sha" not in r and "GITHUB_SHA" not in r:
        errs.append(f"构建腿推送了非 :<sha> 的可变 tag（版本 tag 应交由 promote 改标）：{r}")

print(f"SUMMARY build_tags={len(refs)} promote_needs={needs('promote')} gr_needs={gr} concurrency={conc_desc}")
print("\n".join(errs))
PY
)"
        if grep -qE '^(NO_YAML|NO_JOBS)$' <<<"$CHAIN_OUT"; then
            bad "第 17 节无法解析 release.yml（缺 pyyaml 或 jobs 为空）——判红而不是静默跳过"
        else
            CHAIN_SUMMARY="$(grep -F 'SUMMARY build_tags=' <<<"$CHAIN_OUT" | head -1)"
            nbad="$(grep -cvE '^(SUMMARY |$)' <<<"$CHAIN_OUT")"
            if [[ -z "$CHAIN_SUMMARY" ]]; then
                bad "第 17 节没解析出 SUMMARY（判据在空转，判红）"
            elif [[ "$nbad" -gt 0 ]]; then
                bad "发布链顺序被破坏（${nbad} 处）："
                grep -vE '^SUMMARY ' <<<"$CHAIN_OUT" | grep -vE '^[[:space:]]*$' | sed 's/^/         /'
            else
                ok "发布链顺序成立（${CHAIN_SUMMARY#SUMMARY }）"
            fi
        fi
    fi
else
    bad "找不到 .github/workflows/release.yml，第 17 节无从核对（判红）"
fi

sec "18. 入站边界（出厂 compose 发布的宿主端口除白名单外必须只绑环回）"
# 为什么要钉这一节：P0-2 之后产品的入站模型是"容器网络内部互通，宿主侧只暴露控制面两个口 +
# 反代 overlay 的 80/443"。未鉴权的微服务 gRPC（如 alert-svc 50053）与 MySQL/Redis/Loki 等
# 后端，**全靠"端口只绑 127.0.0.1"这一层兜住**——而这一层此前没有任何检查：
# 谁在 compose 里把 "127.0.0.1:50053:50053" 写成 "50053:50053"（或删掉绑定前缀），
# CI 不会红、真机验收也不会红，因为 verify-runtime 只断言"该可达的确实可达"，
# 从不测"不该可达的是否也可达"。这是典型的"声明了但没人验证"的边界。
# 实测基线（2026-10-06，两种独立算法互证：pyyaml 解析 vs 本节的文本解析）：
# docker-compose.prod.yml 27 个发布端口 / 25 个绑环回，例外只有 controlplane 的 8080 与 9090；
# docker-compose.prod-proxy.yml 4 个 / 1 个绑环回，例外是 controlplane 9090 与 gateway 80/443；
# sim 与 nullfix 两份不发布任何宿主端口（"没有 ports 段"不等于空转，见下方判定）。
COMPOSE_INBOUND=(
    deploy/docker/docker-compose.prod.yml
    deploy/docker/docker-compose.prod-proxy.yml
    deploy/docker/docker-compose.sim.yml
    deploy/docker/docker-compose.nullfix.yml
)
if [[ -z "$PY" ]]; then
    bad "本机/CI 没有 python，第 18 节无法判定入站边界（判红而不是跳过）"
else
    INBOUND_OUT="$("$PY" - "${COMPOSE_INBOUND[@]}" <<'PY'
import re, sys

# 刻意外部可达的入口：控制面 B/S 与 agent gRPC；反代 overlay 的 HTTP/HTTPS 终止点。
# 加新例外必须同时在这里点名——这条清单本身就是入站攻击面的账本。
EXEMPT = {("controlplane", "8080"), ("controlplane", "9090"), ("gateway", "80"), ("gateway", "443")}
LOOP = ("127.0.0.1", "localhost", "::1")


def mask(s):
    """把 ${VAR:-default} 里的冒号换成占位符。
    不这么做的话 split(':') 会把一个端口项拆成 4 段，绑定/宿端口/容器端口全错位（本节的原型踩过）。"""
    out, i = [], 0
    for m in re.finditer(r"\$\{[^}]*\}", s):
        out.append(s[i:m.start()])
        out.append(m.group(0).replace(":", "\x00"))
        i = m.end()
    out.append(s[i:])
    return "".join(out)


def parse(text):
    """返回 [(service, bind, container_port, indent)]；只认 compose 短语法（实测四份出厂文件都是短语法）。"""
    items, svc, in_ports = [], None, False
    for raw in text.split("\n"):
        line = raw.rstrip("\r")
        if re.match(r"^  [A-Za-z0-9_.-]+:\s*$", line):
            svc, in_ports = line.strip().rstrip(":"), False
            continue
        if re.match(r"^    ports:\s*(!\S+)?\s*$", line):
            in_ports = True
            continue
        if not in_ports:
            continue
        st = line.strip()
        if st == "" or st.startswith("#"):
            # 出厂文件在 ports: 下写了带安全理由的注释行；把它们当成块结束会静默丢掉后续条目
            # （原型就是这样少算了 5 个端口 ⇒ 判定面凭空缩小）。
            continue
        m = re.match(r"^( +)-\s*(.+)$", line)
        if not m:
            in_ports = False
            continue
        indent = len(m.group(1))
        s = mask(m.group(2).strip().strip('"').strip("'"))
        parts = [p.replace("\x00", ":") for p in s.split(":")]
        if len(parts) >= 3:
            items.append((svc, parts[0], parts[2].split("/")[0], indent))
        elif len(parts) == 2:
            items.append((svc, "", parts[1].split("/")[0], indent))
        else:
            items.append((svc, "", parts[0].split("/")[0], indent))
    return items


errs, per_file = [], []
tot = loop_n = 0
for f in sys.argv[1:]:
    try:
        text = open(f, encoding="utf-8").read()
    except OSError as exc:
        errs.append(f"读不到 {f}：{exc}")
        continue
    items = parse(text)
    declares = bool(re.search(r"^    ports:", text, re.M))
    if declares and not items:
        errs.append(f"{f} 有 ports: 段却一个条目都没解析出来（本节在空转，判红）")
        continue
    if not declares:
        per_file.append(f"{f.split('/')[-1]}=无ports段")
        continue
    # 独立对照：用一条与状态机无关的正则，把"看起来就是端口映射"的条目数出来。
    # 为什么需要：状态机若在块中途被一行不合约定的写法打断，后续条目会**静默消失**
    # （既不触发上面的空转判据，也不触发缩进判据），判定面凭空缩小而输出照样是绿的。
    oracle = len(re.findall(r'^\s{6,}-\s+"?[\w:.${}/\-]*:\s*\d{2,5}(?:/\w+)?\s*"?$', text, re.M))
    if oracle != len(items):
        errs.append(f"{f} 状态机解析到 {len(items)} 个端口条目，独立正则数到 {oracle} 个"
                    f"⇒ 有条目被静默丢弃，本节的判定面已缩小（判红）")
        continue
    lp = sum(1 for _, b, _, _ in items if b in LOOP)
    tot += len(items)
    loop_n += lp
    for svc, b, cp, indent in items:
        if indent != 6:
            # 丢一个条目不会触发上面的"空转"判据（只要有其它项就仍 >0），所以缩进偏离约定就判红：
            # 这是"判定面静默缩小"唯一的可见信号（变异验证实测过：改坏一条的缩进，本节点位不报）。
            errs.append(f"{f}:{svc} 端口条目缩进为 {indent} 空格（约定 6）⇒ 本节可能漏项，判红")
        if b in LOOP:
            continue
        if (svc, cp) not in EXEMPT:
            errs.append(f"{f}:{svc} 把容器端口 {cp} 发布到所有网卡（绑定={b or '<空=0.0.0.0>'}），不在入站白名单")
    per_file.append(f"{f.split('/')[-1]}={len(items)}/{lp}")

print(f"SUMMARY published={tot} loopback={loop_n} exceptions={len(EXEMPT)} detail={per_file}")
print("\n".join(errs))
PY
)"
        if grep -qF 'SUMMARY published=' <<<"$INBOUND_OUT"; then
            INBOUND_SUMMARY="$(grep -F 'SUMMARY published=' <<<"$INBOUND_OUT" | head -1)"
            inboundbad="$(grep -cvE '^(SUMMARY |$)' <<<"$INBOUND_OUT")"
            if [[ "$inboundbad" -gt 0 ]]; then
                bad "入站边界被破坏（${inboundbad} 处）："
                grep -vE '^SUMMARY ' <<<"$INBOUND_OUT" | grep -vE '^[[:space:]]*$' | sed 's/^/         /'
            else
                ok "入站边界成立：发布的宿主端口除白名单外全部只绑环回（${INBOUND_SUMMARY#SUMMARY }）"
            fi
        else
            bad "第 18 节没解析出 SUMMARY（判据在空转，判红而不是跳过）"
        fi
fi

echo ""
echo "==================================================="
echo "  部署资产门禁：PASS=${PASS}  FAIL=${FAIL}  SKIP=${SKIP}"
echo "==================================================="
[[ "$FAIL" -eq 0 ]]
