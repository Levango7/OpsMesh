#!/usr/bin/env bash
# build-airgap-bundle.selftest.sh —— 自测 build-airgap-bundle.sh 的判据（D1）。
#
# 钉五条，失效方式都是静默反着来：
#   A. 派生：镜像清单从 compose image 行机械派生（${OPSMESH_VERSION} 两种变体都代入），
#      去重后与夹具期望集逐字一致；
#   B. 缺图判红：AIRGAP_SKIP_PULL=1 下缺图立即退出 1，不产出 bundle；
#   C. 守卫图缺失判红：清单里没有 opsmesh/controlplane（解析错文件/漂移）即红；
#   D. 用法错：无 --version 退出 2；
#   E. 版本值语义：latest 退出 1（不可追溯，同 deploy.sh:239-242 口径），非 x.y.z 退出 2。
# 手法与 docker-pull-retry.selftest.sh 同：假 docker/helm 放 PATH 最前，调用计数即证据。
# 退出码: 0 = 全部用例符合预期；1 = 任一不符。

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI="$HERE/build-airgap-bundle.sh"

if [ ! -r "$CLI" ]; then
    echo "  [FAIL] 找不到被测脚本 $CLI" >&2
    exit 1
fi

PASS=0; FAIL=0
ok()   { echo "  [PASS] $*"; PASS=$((PASS+1)); }
bad()  { echo "  [FAIL] $*"; FAIL=$((FAIL+1)); }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# ---- 夹具：假 docker / 假 helm ----------------------------------------------
make_fakes() { # make_fakes <dir>
    local bin="$1"
    mkdir -p "$bin"
    cat > "$bin/docker" <<EOF
#!/usr/bin/env bash
LOG="\$DOCKER_LOG"
echo "docker \$*" >> "\$LOG"
if [ "\$1" = "image" ] && [ "\$2" = "inspect" ]; then
    ref="\$3"; shift 3
    [ "\$ref" = "\$MISSING" ] && { echo "Error: No such image" >&2; exit 1; }
    if [ "\$1" = "--format" ]; then echo "sha256:fakeid_\${ref//[\/:]/_}"; fi
    exit 0
fi
if [ "\$1" = "save" ]; then
    out="\$3"; img="\$4"
    printf 'fake-tar %s\n' "\$img" > "\$out"
    exit 0
fi
if [ "\$1" = "version" ]; then echo "29.9.2"; exit 0; fi
if [ "\$1" = "pull" ]; then exit 0; fi
exit 0
EOF
    cat > "$bin/helm" <<'EOF'
#!/usr/bin/env bash
LOG="${HELM_LOG}"
echo "helm $*" >> "$LOG"
if [ "$1" = "package" ]; then
    # args: package CHART -d DEST
    dest=""
    while [ $# -gt 0 ]; do [ "$1" = "-d" ] && dest="$2"; shift; done
    printf 'fake-chart\n' > "${dest}/opsmesh-0.18.0.tgz"
fi
if [ "$1" = "version" ]; then echo "v0.18.2"; exit 0; fi
exit 0
EOF
    chmod +x "$bin/docker" "$bin/helm"
    echo "${bin}"
}

# 夹具 compose：2 服务 + 1 overlay 图，覆盖两种 ${OPSMESH_VERSION} 变体
make_fixture() {
    local d="$1"
    mkdir -p "$d"
    cat > "$d/docker-compose.prod.yml" <<'EOF'
services:
  cp:
    image: opsmesh/controlplane:${OPSMESH_VERSION:?请在 .env 设置 OPSMESH_VERSION（如 0.18.0，禁止 latest）}
  svc:
    image: opsmesh/auth-svc:${OPSMESH_VERSION:?请在 .env 设置 OPSMESH_VERSION}
  db:
    image: mysql:8.0
EOF
    cat > "$d/docker-compose.prod-proxy.yml" <<'EOF'
services:
  proxy:
    image: nginx:1.27-alpine
EOF
}

# ---- A. 派生正确 ---------------------------------------------------------------
BIN="$(make_fakes "$WORK/bin-a")"
export PATH="$BIN:$PATH" DOCKER_LOG="$WORK/docker-a.log" HELM_LOG="$WORK/helm-a.log" MISSING="__none__"
make_fixture "$WORK/fixture-a"
OUT="$WORK/out-a"
if bash "$CLI" --version 0.18.0 --deploy-dir "$WORK/fixture-a" --out "$OUT" >"$WORK/a.out" 2>&1; then
    if [ -s "$OUT.tar.gz" ]; then ok "A6 单一 tar 产出"; else bad "A6 产物 tar 缺失/空"; fi
    mkdir -p "$WORK/verify-a"
    tar xzf "$OUT.tar.gz" -C "$WORK/verify-a"
    BD="$WORK/verify-a/opsmesh-airgap-0.18.0"
    imgs="$(grep -c $'^image\t' "$BD/manifest.tsv" 2>/dev/null || echo 0)"
    if [ "${imgs}" = "4" ]; then ok "A1 派生 4 图（prod 3 + overlay 1）"; else bad "A1 派生数量=${imgs}，期望 4"; fi
    if grep -q $'opsmesh/controlplane:0.18.0\t' "$BD/manifest.tsv"; then ok "A2 :?长变体代入版本号"; else bad "A2 controlplane 长变体未代入"; fi
    if grep -q $'opsmesh/auth-svc:0.18.0\t' "$BD/manifest.tsv"; then ok "A3 短变体代入版本号"; else bad "A3 auth-svc 短变体未代入"; fi
    if grep -q $'nginx:1.27-alpine\t' "$BD/manifest.tsv"; then ok "A4 overlay 图入清单"; else bad "A4 nginx 缺失"; fi
    if ( cd "$BD" && sha256sum -c CHECKSUMS.sha256 >/dev/null 2>&1 ); then ok "A5 CHECKSUMS 全绿"; else bad "A5 CHECKSUMS 校验失败"; fi
    if [ "$(grep -c '^docker save' "$DOCKER_LOG")" = "4" ]; then ok "A7 save 调用=图数"; else bad "A7 save 次数不符"; fi
else
    bad "A 构建整体退出非 0"
fi

# ---- B. 缺图判红 ---------------------------------------------------------------
BIN="$(make_fakes "$WORK/bin-b")"
export PATH="$BIN:$PATH" DOCKER_LOG="$WORK/docker-b.log" HELM_LOG="$WORK/helm-b.log" MISSING="mysql:8.0"
if AIRGAP_SKIP_PULL=1 bash "$CLI" --version 0.18.0 --deploy-dir "$WORK/fixture-a" --out "$WORK/out-b" >"$WORK/b.out" 2>&1; then
    bad "B1 缺图未判红（退出 0）"
else
    rc=$?
    if [ "${rc}" = 1 ]; then ok "B1 缺图 + SKIP_PULL 判红退出 1"; else bad "B1 退出码非 1（实得 ${rc}）"; fi
fi
if [ ! -e "$WORK/out-b.tar.gz" ]; then ok "B2 判红不产出 tar"; else bad "B2 判红仍产出 tar"; fi

# ---- C. 守卫图缺失判红 ----------------------------------------------------------
mkdir -p "$WORK/fixture-c"
cat > "$WORK/fixture-c/docker-compose.prod.yml" <<'EOF'
services:
  db:
    image: mysql:8.0
EOF
: > "$WORK/fixture-c/docker-compose.prod-proxy.yml"
BIN="$(make_fakes "$WORK/bin-c")"
export PATH="$BIN:$PATH" DOCKER_LOG="$WORK/docker-c.log" HELM_LOG="$WORK/helm-c.log" MISSING="__none__"
if bash "$CLI" --version 0.18.0 --deploy-dir "$WORK/fixture-c" --out "$WORK/out-c" >"$WORK/c.out" 2>&1; then
    bad "C1 无守卫图未判红"
else
    ok "C1 清单缺 opsmesh/controlplane 判红"
fi

# ---- D. 用法错 --------------------------------------------------------------------
BIN="$(make_fakes "$WORK/bin-d")"
export PATH="$BIN:$PATH" DOCKER_LOG="$WORK/docker-d.log" HELM_LOG="$WORK/helm-d.log" MISSING="__none__"
if bash "$CLI" --out "$WORK/out-d" >"$WORK/d.out" 2>&1; then
    bad "D1 无 --version 未判红"
else
    rc=$?
    if [ "${rc}" = 2 ]; then ok "D1 无 --version 退出 2（用法错）"; else bad "D1 退出码非 2（实得 ${rc}）"; fi
fi

# ---- E. 版本值语义（latest 与非法值） ----------------------------------------------
BIN="$(make_fakes "$WORK/bin-e")"
export PATH="$BIN:$PATH" DOCKER_LOG="$WORK/docker-e.log" HELM_LOG="$WORK/helm-e.log" MISSING="__none__"
if AIRGAP_SKIP_PULL=1 bash "$CLI" --version latest --deploy-dir "$WORK/fixture-a" --out "$WORK/out-e" >"$WORK/e.out" 2>&1; then
    bad "E1 latest 未判红"
else
    rc=$?
    if [ "${rc}" = 1 ]; then ok "E1 latest 退出 1（不可追溯，同 deploy.sh 口径）"; else bad "E1 退出码非 1（实得 ${rc}）"; fi
fi
if AIRGAP_SKIP_PULL=1 bash "$CLI" --version abc --deploy-dir "$WORK/fixture-a" --out "$WORK/out-e2" >"$WORK/e2.out" 2>&1; then
    bad "E2 非法版本值未判红"
else
    rc=$?
    if [ "${rc}" = 2 ]; then ok "E2 非 x.y.z 退出 2（用法错）"; else bad "E2 退出码非 2（实得 ${rc}）"; fi
fi

echo "build-airgap-bundle.selftest: PASS=${PASS} FAIL=${FAIL}"
[ "${FAIL}" = 0 ]
