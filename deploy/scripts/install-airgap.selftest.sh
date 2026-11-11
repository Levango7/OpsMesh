#!/usr/bin/env bash
# install-airgap.selftest.sh —— 自测 install-airgap.sh 的判据（D1）。
#
# 钉四条：
#   A. 正常路径：校验和全绿 → 按 manifest image 行 docker load → compose 形态调
#      deploy.sh init + up --no-build -y（离线机不得编译/拉取）；
#   B. 校验和不符判红：篡改任一文件后立即退出 1，且 docker load 零调用（绝不带病装载）；
#   C. 目标目录非空判红（防覆盖既有栈）；
#   D. helm 形态退出 0 且打印 helm install 提示。
# 手法：假 docker 记录 load 调用；bundle 由自测手工组装（含 stub deploy.sh），
# CHECKSUMS.sha256 用真 sha256sum 生成，与 build 脚本同一形态。
# 退出码: 0 = 全部用例符合预期；1 = 任一不符。

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI="$HERE/install-airgap.sh"

if [ ! -r "$CLI" ]; then
    echo "  [FAIL] 找不到被测脚本 $CLI" >&2
    exit 1
fi

PASS=0; FAIL=0
ok()   { echo "  [PASS] $*"; PASS=$((PASS+1)); }
bad()  { echo "  [FAIL] $*"; FAIL=$((FAIL+1)); }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# ---- 夹具 ------------------------------------------------------------------
make_bundle() { # make_bundle <stage-dir>
    local s="$1"
    mkdir -p "$s/images" "$s/helm" "$s/deploy/scripts" "$s/dockertree/scripts"
    printf 'fake-image-layer\n' > "$s/images/nginx__1.27-alpine.tar"
    printf 'fake-chart\n' > "$s/helm/opsmesh-0.18.0.tgz"
    cat > "$s/dockertree/scripts/deploy.sh" <<'STUB'
#!/usr/bin/env bash
echo "deploy.sh $*" >> "$DEPLOY_LOG"
exit 0
STUB
    printf '#!/usr/bin/env bash\necho "verify-runtime called" >&2\nexit 0\n' > "$s/deploy/scripts/verify-runtime.sh"
    printf 'OpsMesh airgap bundle 0.18.0 (fixture)\n' > "$s/README.txt"
    tar czf "$s/deploy/docker-tree.tar.gz" -C "$s/dockertree" .
    {
        echo "# version=0.18.0"
        echo -e "type\tref\tpath\tsha256"
        printf 'image\tnginx:1.27-alpine\timages/nginx__1.27-alpine.tar\t%s\n' "$(sha256sum "$s/images/nginx__1.27-alpine.tar" | cut -d' ' -f1)"
        printf 'chart\topsmesh\thelm/opsmesh-0.18.0.tgz\t%s\n' "$(sha256sum "$s/helm/opsmesh-0.18.0.tgz" | cut -d' ' -f1)"
        printf 'deploy-tree\tcompose-prod\tdeploy/docker-tree.tar.gz\t%s\n' "$(sha256sum "$s/deploy/docker-tree.tar.gz" | cut -d' ' -f1)"
        printf 'verify-script\truntime\tdeploy/scripts/verify-runtime.sh\t%s\n' "$(sha256sum "$s/deploy/scripts/verify-runtime.sh" | cut -d' ' -f1)"
        printf 'readme\tbundle\tREADME.txt\t%s\n' "$(sha256sum "$s/README.txt" | cut -d' ' -f1)"
    } > "$s/manifest.tsv"
    # 两遍法：先物化文件列表再算哈希。边遍历边写 CHECKSUMS.sha256 有竞态
    # （find 可能把正在写的 CHECKSUMS.sha256 自己列进去 ⇒ 自指哈希，sha256sum -c 必红）；
    # 列表放 $s 之外（系统临时目录），find 遍历与列表写入无交集，竞态与静态告警双消。
    LIST="$(mktemp)"
    ( cd "$s" && find . -type f ! -name CHECKSUMS.sha256 -print0 | sort -z > "${LIST}" \
        && xargs -0 sha256sum < "${LIST}" > CHECKSUMS.sha256 )
    rm -f "${LIST}"
}

make_fake_docker() { # make_fake_docker <bin-dir>
    local bin="$1"
    mkdir -p "$bin"
    cat > "$bin/docker" <<'EOF'
#!/usr/bin/env bash
echo "docker $*" >> "$DOCKER_LOG"
if [ "$1" = "load" ]; then exit 0; fi
exit 0
EOF
    chmod +x "$bin/docker"
}

# ---- A. 正常路径 ---------------------------------------------------------------
make_fake_docker "$WORK/bin-a"
STAGE="$WORK/stage-a"; mkdir -p "$STAGE"; make_bundle "$STAGE"
( cd "$WORK" && tar czf bundle-a.tar.gz -C "$STAGE" . )
export PATH="$WORK/bin-a:$PATH" DOCKER_LOG="$WORK/docker-a.log" DEPLOY_LOG="$WORK/deploy-a.log"
: > "$DOCKER_LOG"; : > "$DEPLOY_LOG"
if bash "$CLI" "$WORK/bundle-a.tar.gz" --target "$WORK/target-a" >"$WORK/a.out" 2>&1; then
    ok "A1 正常路径退出 0"
else
    bad "A1 正常路径退出非 0：$(tail -3 "$WORK/a.out")"
fi
if [ "$(grep -c '^docker load' "$DOCKER_LOG")" = "1" ]; then ok "A2 load 调用=图数(1)"; else bad "A2 load 次数=$(grep -c '^docker load' "$DOCKER_LOG" 2>/dev/null || echo 0)"; fi
if grep -q 'deploy.sh init$' "$DEPLOY_LOG"; then ok "A3 deploy.sh init"; else bad "A3 未调 init"; fi
if grep -q 'deploy.sh up --no-build -y' "$DEPLOY_LOG"; then ok "A4 up --no-build -y（离线不编译不拉取）"; else bad "A4 未调 up --no-build -y"; fi
if grep -q 'bash .*verify-runtime.sh' "$WORK/a.out"; then ok "A5 末尾提示验收脚本"; else bad "A5 未提示 verify-runtime"; fi

# ---- B. 校验和篡改判红 -----------------------------------------------------------
STAGE="$WORK/stage-b"; mkdir -p "$STAGE"; make_bundle "$STAGE"
printf 'tampered\n' >> "$STAGE/images/nginx__1.27-alpine.tar"
( cd "$WORK" && tar czf bundle-b.tar.gz -C "$STAGE" . )
: > "$DOCKER_LOG"
if bash "$CLI" "$WORK/bundle-b.tar.gz" --target "$WORK/target-b" >"$WORK/b.out" 2>&1; then
    bad "B1 篡改后未判红"
else
    ok "B1 校验和不符判红退出"
fi
if [ ! -s "$DOCKER_LOG" ]; then ok "B2 判红后零 load（不带病装载）"; else bad "B2 判红仍发生 load"; fi

# ---- C. 目标非空判红 ---------------------------------------------------------------
mkdir -p "$WORK/target-c" && printf 'x\n' > "$WORK/target-c/keep.txt"
if bash "$CLI" "$WORK/bundle-a.tar.gz" --target "$WORK/target-c" >"$WORK/c.out" 2>&1; then
    bad "C1 非空目标未判红"
else
    ok "C1 目标非空判红"
fi

# ---- D. helm 形态 --------------------------------------------------------------------
if bash "$CLI" "$WORK/bundle-a.tar.gz" --target "$WORK/target-d" --mode helm >"$WORK/d.out" 2>&1; then
    if grep -q 'helm install' "$WORK/d.out"; then ok "D1 helm 形态退出 0 且打印 install 提示"; else bad "D1 未打印 helm install 提示"; fi
else
    bad "D1 helm 形态退出非 0"
fi

echo "install-airgap.selftest: PASS=${PASS} FAIL=${FAIL}"
[ "${FAIL}" = 0 ]
