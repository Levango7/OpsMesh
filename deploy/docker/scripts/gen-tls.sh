#!/usr/bin/env bash
# OpsMesh 自签 TLS 证书生成（生产部署 bootstrap）
#
# 用法：
#   ./scripts/gen-tls.sh [选项] [HOST_OR_IP ...]
#   ./scripts/gen-tls.sh --dir ./certs --advertise https://ops.example.com:8080 ops.example.com 10.0.0.5
#
# 产物（--dir 目录下）：
#   tls.crt  自签证书（SAN 列出全部可用主机名/IP）
#   tls.key  私钥（0600，未加密——容器无人值守启动需要；切勿外传或提交仓库）
#   ca.crt   信任锚副本（自签场景即证书本身），供 agent 以 --client-ca 使用
#
# 为什么必须配 SAN：TLS 客户端按 SAN 校验对端名称，CN 已不再参与校验。
# 若 SAN 中缺少 agent/浏览器实际使用的主机名，握手直接失败——表现为 agent 注册失败、
# gRPC 报 x509 name mismatch、控制台报证书不匹配。因此控制面对外地址（--advertise）
# 里的主机名会被自动并入 SAN。
#
# 默认 SAN：localhost / 127.0.0.1 / ::1 / controlplane（compose 服务名，供网内互访）。
#
# 信任模型：单张自签证书既做服务端证书，也做 agent 侧信任锚（CA:TRUE + EKU serverAuth,clientAuth）。
# 不生成独立 CA 私钥——挂载到容器的是同一份 tls.key，少一个可被读取的长期 CA 密钥面。
# 生产对外服务建议替换为受信 CA 签发的证书（覆盖同名文件后重启 controlplane 即可）。
set -euo pipefail

DIR="./certs"
DAYS=825
FORCE=false
CN=""
ADVERTISE="${OPSMESH_ADVERTISE_ADDR:-}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
log_ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

usage() {
    cat <<'EOF'
用法: gen-tls.sh [选项] [HOST_OR_IP ...]

选项:
  --dir DIR         输出目录（默认 ./certs，须与 .env 的 OPSMESH_TLS_DIR 一致）
  --days N          有效期天数（默认 825，浏览器对自签无此上限，仅作轮换周期）
  --cn NAME         Common Name（默认取首个非本机 SAN；仅供人工辨识，不参与校验）
  --advertise URL   从控制面对外地址提取主机名并入 SAN（默认读 $OPSMESH_ADVERTISE_ADDR）
  --force           已存在时覆盖重建
  -h, --help        显示本帮助

位置参数为额外 SAN，可写域名或 IP（例: gen-tls.sh ops.example.com 10.0.0.5）。
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --dir)       DIR="${2:?--dir 需要参数}"; shift 2 ;;
        --days)      DAYS="${2:?--days 需要参数}"; shift 2 ;;
        --cn)        CN="${2:?--cn 需要参数}"; shift 2 ;;
        --advertise) ADVERTISE="${2:?--advertise 需要参数}"; shift 2 ;;
        --force)     FORCE=true; shift ;;
        -h|--help)   usage; exit 0 ;;
        -*)          log_error "未知选项: $1"; usage >&2; exit 2 ;;
        *)           break ;;
    esac
done

if ! command -v openssl &>/dev/null; then
    log_error "缺少 openssl。安装：apt-get install -y openssl / yum install -y openssl / apk add openssl"
    exit 1
fi

if ! [[ "$DAYS" =~ ^[0-9]+$ ]] || [ "$DAYS" -lt 1 ]; then
    log_error "--days 须为正整数，当前: $DAYS"
    exit 2
fi

# ============================================================
# SAN 收集（DNS 与 IP 分开登记，配置节里必须分开写 DNS.n / IP.n）
# ============================================================
DNS_LIST=""   # 形如 "|localhost|controlplane"
IP_LIST=""    # 形如 "|127.0.0.1"

san_seen() { case "$1" in *"|$2|"*) return 0 ;; *) return 1 ;; esac; }

add_san() {
    local h="$1"
    [ -n "$h" ] || return 0
    # 仅接受域名/IP 合法字符，避免把引号、空格、换行等注入 openssl 配置。
    if ! [[ "$h" =~ ^[A-Za-z0-9._:-]+$ ]]; then
        log_warn "跳过非法 SAN（含不支持字符）: $h"
        return 0
    fi
    if [[ "$h" =~ ^[0-9]+(\.[0-9]+){3}$ ]] || [[ "$h" == *:* ]]; then
        san_seen "$IP_LIST" "$h" && return 0
        IP_LIST="${IP_LIST}|${h}"
    else
        san_seen "$DNS_LIST" "$h" && return 0
        DNS_LIST="${DNS_LIST}|${h}"
    fi
}

advertise_host() {
    local url="$1"
    url="${url#*://}"      # 去 scheme
    url="${url%%/*}"       # 去 path
    url="${url##*@}"       # 去 userinfo
    if [[ "$url" == \[*\]* ]]; then
        printf '%s' "${url#[}" | sed 's/\].*//'   # IPv6 字面量 [::1]:8080
    else
        printf '%s' "${url%%:*}"
    fi
}

add_san localhost
add_san 127.0.0.1
add_san ::1
add_san controlplane

ADV_HOST=""
if [ -n "$ADVERTISE" ]; then
    ADV_HOST="$(advertise_host "$ADVERTISE")"
    add_san "$ADV_HOST"
fi

for h in "$@"; do
    add_san "$h"
done

first_dns() {
    local s="${1#|}"
    printf '%s' "${s%%|*}"
}
if [ -z "$CN" ]; then
    if [ -n "${ADV_HOST:-}" ]; then
        CN="$ADV_HOST"
    else
        CN="$(first_dns "$DNS_LIST")"
    fi
fi

# ============================================================
# 幂等：已存在且证书/私钥配对时跳过（除非 --force）
# ============================================================
cert_key_match() {
    local crt="$1" key="$2" a b
    a="$(openssl x509 -in "$crt" -noout -pubkey 2>/dev/null | openssl sha256 2>/dev/null || true)"
    b="$(openssl pkey -in "$key" -pubout 2>/dev/null | openssl sha256 2>/dev/null || true)"
    [ -n "$a" ] && [ "$a" = "$b" ]
}

if [ -f "$DIR/tls.crt" ] && [ -f "$DIR/tls.key" ] && [ "$FORCE" != true ]; then
    if cert_key_match "$DIR/tls.crt" "$DIR/tls.key"; then
        log_ok "证书已存在且配对，跳过生成: $DIR/tls.crt（--force 可覆盖重建）"
        exit 0
    fi
    log_warn "已存在证书与私钥不配对，将重建: $DIR"
fi

mkdir -p "$DIR"

# ============================================================
# 生成（配置写临时文件；不用 -addext，兼容 OpenSSL 1.1.1 与 3.x）
# ============================================================
CNF="${DIR}/.openssl.cnf.$$"
trap 'rm -f "$CNF"' EXIT

{
    echo "[req]"
    echo "default_bits       = 2048"
    echo "prompt             = no"
    echo "distinguished_name = dn"
    echo "x509_extensions    = v3_req"
    echo ""
    echo "[dn]"
    echo "CN = ${CN}"
    echo "O  = OpsMesh"
    echo ""
    echo "[v3_req]"
    # CA:TRUE + EKU serverAuth,clientAuth：同一张证书既作服务端证书，也作 agent 的信任锚。
    echo "basicConstraints     = critical, CA:TRUE"
    echo "keyUsage             = critical, digitalSignature, keyEncipherment, keyCertSign"
    echo "extendedKeyUsage     = serverAuth, clientAuth"
    echo "subjectKeyIdentifier = hash"
    echo "subjectAltName       = @alt_names"
    echo ""
    echo "[alt_names]"
    idx=1
    IFS='|' read -r -a dns_arr <<< "${DNS_LIST#|}"
    for d in "${dns_arr[@]:-}"; do
        [ -n "$d" ] || continue
        echo "DNS.${idx} = ${d}"
        idx=$((idx + 1))
    done
    idx=1
    IFS='|' read -r -a ip_arr <<< "${IP_LIST#|}"
    for i in "${ip_arr[@]:-}"; do
        [ -n "$i" ] || continue
        echo "IP.${idx} = ${i}"
        idx=$((idx + 1))
    done
} > "$CNF"

log_info "生成自签证书（CN=${CN}, ${DAYS} 天）→ $DIR"
openssl req -x509 -nodes -newkey rsa:2048 -sha256 -days "$DAYS" \
    -keyout "$DIR/tls.key" -out "$DIR/tls.crt" \
    -config "$CNF" -extensions v3_req >/dev/null 2>&1

rm -f "$CNF"
trap - EXIT

chmod 600 "$DIR/tls.key"
chmod 644 "$DIR/tls.crt"
# agent 侧信任锚（自签场景 = 证书本身）。刻意不生成 ca.key：挂载到容器里的私钥面越小越好。
cp -f "$DIR/tls.crt" "$DIR/ca.crt"
chmod 644 "$DIR/ca.crt"

if ! cert_key_match "$DIR/tls.crt" "$DIR/tls.key"; then
    log_error "生成后的证书与私钥不配对，请检查 openssl 版本与磁盘状态"
    exit 1
fi

if ! openssl x509 -in "$DIR/tls.crt" -noout -text 2>/dev/null | grep -q "DNS:localhost"; then
    log_error "证书 SAN 异常（未包含 localhost），请检查 openssl 版本与配置模板"
    exit 1
fi

NOT_AFTER="$(openssl x509 -in "$DIR/tls.crt" -noout -enddate | sed 's/^notAfter=//')"
log_ok "证书已生成: $DIR/tls.crt（到期 $NOT_AFTER）"
log_info "SAN: $(printf '%s' "${DNS_LIST#|}" | tr '|' ' ')$(printf '%s' "${IP_LIST#|}" | tr '|' ' ' | sed 's/^/ /')"

if [ -z "${ADV_HOST:-}" ]; then
    log_warn "未提供 --advertise，SAN 仅含本机/服务名。若 agent 通过其他主机名或 IP 连接，握手将因名称不匹配失败。"
elif [ "$ADV_HOST" = "localhost" ] || [ "$ADV_HOST" = "127.0.0.1" ]; then
    log_warn "控制面对外地址为 ${ADV_HOST}——仅适合同机访问。远程 agent 需改用可达 IP/域名重新生成。"
fi

echo ""
echo "  下一步："
echo "    agent 侧信任该证书: ./opsmesh-agent --client-ca=$DIR/ca.crt ..."
echo "    查看证书详情:       openssl x509 -in $DIR/tls.crt -noout -text"
