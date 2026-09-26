#!/usr/bin/env bash
# OpsMesh 生产部署脚本
#
# 用法：
#   ./scripts/deploy.sh init      # 生成 .env（随机口令/密钥）+ 自签 TLS 证书（幂等）
#   ./scripts/deploy.sh up        # 预检 → 构建 → 基础设施 → 可观测 → 服务 → 冒烟
#   ./scripts/deploy.sh up --proxy        # 叠加反代 overlay（Nginx 终止 TLS）
#   ./scripts/deploy.sh down [--volumes]  # 停止并移除容器（--volumes 连数据卷一起删）
#   ./scripts/deploy.sh smoke             # 只跑冒烟测试
#   ./scripts/deploy.sh config            # 只做 docker compose 渲染校验
#   ./scripts/deploy.sh help
#
# 通用参数：--proxy / -y|--yes（非交互，端口占用等询问一律视为同意）/ --no-build / --force
#
# 设计要点（每条都对应一个真实踩过的坑）：
#   1. 所有口令/密钥由 openssl 随机生成（无预置弱口令）。生产模式下控制面要求
#      ADMIN_PASSWORD 非空、JWT_SECRET ≥32 字节、ENCRYPTION_KEY 为 base64 的 32 字节，
#      缺失即拒绝启动——本脚本在预检阶段就给出可执行的报错，而不是让容器反复重启。
#   2. 证书必须生成：compose 把 ${OPSMESH_TLS_DIR} 挂进 controlplane（gRPC TLS 用），
#      TLS 目录不存在时 Docker 会把宿主路径「当成目录创建」，得到空挂载 + 启动失败。
#   3. 端口检查区分「对外发布」与「仅绑 127.0.0.1」——后者同样会占用宿主端口
#      （如宿主自带的 3306/6379），必须在启动前发现。
#   4. 非交互环境（CI/远程 ssh 无 tty）不做 read 询问，改为直接失败并给出 -y 提示，
#      避免部署卡在无人应答的提示上。
#   5. OPSMESH_ADVERTISE_ADDR 按「浏览器 Origin 形态」强校验（默认端口不写、非默认端口必写）：
#      该值既是 bootstrap 安装命令里的下载地址，也是控制面 CSRF Origin 校验的基准
#      （server_middleware.go 做 host 字符串比对），填错表现为「浏览器写操作 403」，
#      且启动期与日志里都没有明显错误，属最难排查的一类故障。
#   6. 项目名硬门禁：compose 顶部 name: opsmesh 必须保留。不写 name 时 Compose 以目录名
#      为项目名（本目录名 docker），任何同名目录下的 compose 项目会共享项目名，
#      一次 down --remove-orphans 就能删掉对方的容器（实测曾列出 17 个 Dify 容器）。

set -euo pipefail

# ============================================================
# Configuration
# ============================================================
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
REPO_ROOT="$(cd "${PROJECT_DIR}/../.." && pwd)"
ENV_FILE="${PROJECT_DIR}/.env"
GEN_TLS="${SCRIPT_DIR}/gen-tls.sh"
LOG_FILE="${PROJECT_DIR}/deploy.log"
MIN_DISK_GB=10

COMPOSE_BASE="docker-compose.prod.yml"
COMPOSE_PROXY="docker-compose.prod-proxy.yml"

# 运行时解析的变量
PROXY=false
ASSUME_YES=false
NO_BUILD=false
FORCE=false

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'
BOLD='\033[1m'

# ============================================================
# Helper Functions
# ============================================================
log_info()  { echo -e "${BLUE}[INFO]${NC}  $(date '+%H:%M:%S') $*"; }
log_ok()    { echo -e "${GREEN}[OK]${NC}    $(date '+%H:%M:%S') $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $(date '+%H:%M:%S') $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $(date '+%H:%M:%S') $*" >&2; }
log_section() { echo -e "\n${BOLD}${CYAN}═══ $* ═══${NC}\n"; }

check_command() {
    if ! command -v "$1" &>/dev/null; then
        log_error "'$1' 未安装。"
        return 1
    fi
}

compose() {
    local files=(-f "$COMPOSE_BASE")
    if [ "$PROXY" = true ]; then
        files+=(-f "$COMPOSE_PROXY")
    fi
    docker compose --env-file .env "${files[@]}" "$@"
}

# env_val KEY [DEFAULT]：从 .env 取值（不 source，避免值里的元字符被执行）
env_val() {
    local key="$1" def="${2:-}" v=""
    if [ -f "$ENV_FILE" ]; then
        v="$(grep -E "^[[:space:]]*${key}=" "$ENV_FILE" 2>/dev/null | tail -1 | cut -d= -f2- | tr -d '\r' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')" || true
    fi
    if [ -n "$v" ]; then printf '%s' "$v"; else printf '%s' "$def"; fi
}

rand_b64() { openssl rand -base64 "$1" | tr -d '\n'; }
rand_hex() { openssl rand -hex "$1" | tr -d '\n'; }

# 口令字符集约束：口令会流经 .env、compose 插值、YAML stringData、shell 双引号与 DSN，
# 故限定在安全集合内，避免 '@'（破坏 MySQL DSN）、'#'（env/注释歧义）、'$'/'"'/'\'/反引号（插值）
# 与空白字符。auth-svc 只要求「非字母数字」，本集合是其子集。
PASSWORD_SPECIAL_CHARS='!%^*-_+=.'

# rand_pw：长度 $1 的口令，字符集严格 ⊆ [0-9a-f] ∪ PASSWORD_SPECIAL_CHARS，且末位恒为特殊字符
# （满足「含特殊字符」类策略）。刻意不复用 rand_b64：base64 字母表含 '/'，不在上面的集合内——
# 早先该常量从未被任何代码引用，等于「声明了字符集、生成器却在字符集外」。
# 也不用 `tr -dc … /dev/urandom | head -c N` 的常见写法：head 提前关管道会让 tr 吃 SIGPIPE，
# 在 pipefail 下变成随机失败。熵：长度 32 ⇒ 31 位 hex（124 bit）+ 1 位固定位置的特殊字符。
rand_pw() {
    local n="$1" body idx
    body="$(openssl rand -hex "$n")"
    idx="$(( 0x$(openssl rand -hex 1) % ${#PASSWORD_SPECIAL_CHARS} ))"
    printf '%s%s' "${body:0:$((n - 1))}" "${PASSWORD_SPECIAL_CHARS:idx:1}"
}

confirm() {
    local prompt="$1"
    if [ "$ASSUME_YES" = true ]; then
        log_warn "${prompt} → 已由 -y/--yes 自动确认"
        return 0
    fi
    if [ ! -t 0 ]; then
        # 问题本身不该印成 [ERROR]（运维读日志时会以为已经出错）：这里出错的是"无法确认"。
        log_error "需要确认「${prompt}」，但当前不是交互终端（stdin 非 tty）——已按拒绝处理。请加 -y/--yes 显式承担风险。"
        return 1
    fi
    local answer=""
    read -rp "${prompt} [y/N] " answer
    [[ "$answer" =~ ^[Yy]$ ]]
}

# ============================================================
# Compose 版本
# ============================================================
compose_version() {
    local raw
    raw="$(docker compose version --short 2>/dev/null || true)"
    printf '%s' "$(printf '%s' "$raw" | grep -oE '[0-9]+(\.[0-9]+){1,2}' | head -1)"
}

# ver_ge A B → A ≥ B
ver_ge() {
    [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -1)" = "$2" ]
}

check_compose_version() {
    local v min
    v="$(compose_version)"
    if [ -z "$v" ]; then
        log_error "未检测到 docker compose v2（docker compose version 无输出）。"
        log_error "安装：https://docs.docker.com/compose/install/linux/"
        exit 1
    fi
    # 统一要求 ≥ 2.24.4：反代 overlay 的 !override 标签需要该版本（低版本会静默忽略该标签，
    # 端口列表变成「追加」而非「替换」，控制面明文 8080 仍对外暴露）。
    # 两种形态统一设此下限，避免出现「直连能部署、加 --proxy 才炸」的差异化门槛。
    min="2.24.4"
    if ! ver_ge "$v" "$min"; then
        log_error "docker compose 版本过低：当前 ${v}，需要 ≥ ${min}"
        log_error "（!override 标签在低版本被静默忽略 → 反代形态端口覆盖失效，明文 8080 仍对外暴露）"
        exit 1
    fi
    log_ok "Docker Compose ${v}"
}

# 项目名必须显式且唯一：不写 name 时 Compose 以「目录名」为项目名，
# 任何同名目录下的 compose 项目会共享项目名，down --remove-orphans 会跨项目删容器。
compose_project_name() {
    compose config 2>/dev/null | sed -n 's/^name: *//p' | head -1
}

# ============================================================
# 环境变量校验
# ============================================================
REQUIRED_VARS=(
    OPSMESH_VERSION
    OPSMESH_ADVERTISE_ADDR
    OPSMESH_TLS_DIR
    JWT_SECRET
    ENCRYPTION_KEY
    ADMIN_PASSWORD
    MYSQL_ROOT_PASSWORD
    MYSQL_PASSWORD
    REDIS_PASSWORD
    CONFIG_ENCRYPTION_KEY
    GRAFANA_ADMIN_PASSWORD
)

check_env_vars() {
    local missing=() empty=() key v

    for key in "${REQUIRED_VARS[@]}"; do
        if ! grep -qE "^[[:space:]]*${key}=" "$ENV_FILE" 2>/dev/null; then
            missing+=("$key")
            continue
        fi
        v="$(env_val "$key")"
        [ -z "$v" ] && empty+=("$key")
    done

    if [ ${#missing[@]} -gt 0 ]; then
        log_error ".env 缺少必需变量：${missing[*]}"
        log_error "执行 ./scripts/deploy.sh init 生成完整 .env（已存在的 .env 不会被覆盖）。"
        exit 1
    fi
    if [ ${#empty[@]} -gt 0 ]; then
        log_error ".env 中以下变量为空：${empty[*]}"
        exit 1
    fi

    # 逐项校验「控制面启动期会拒绝」的值，避免容器起来后才反复重启。
    local jwt enc admin ver adv
    jwt="$(env_val JWT_SECRET)"
    if [ "$(printf '%s' "$jwt" | wc -c)" -lt 32 ]; then
        log_error "JWT_SECRET 长度不足（$(printf '%s' "$jwt" | wc -c) 字节 < 32）：生产模式控制面拒绝启动。"
        log_error "生成：openssl rand -base64 48"
        exit 1
    fi

    enc="$(env_val ENCRYPTION_KEY)"
    local enc_bytes
    enc_bytes="$(printf '%s' "$enc" | base64 -d 2>/dev/null | wc -c || echo 0)"
    if [ "$enc_bytes" -ne 32 ]; then
        log_error "ENCRYPTION_KEY 不是 base64 编码的 32 字节（解码后 ${enc_bytes} 字节）：kubeconfig 静态加密要求 AES-256 密钥。"
        log_error "生成：openssl rand -base64 32"
        exit 1
    fi

    admin="$(env_val ADMIN_PASSWORD)"
    if ! password_is_strong "$admin"; then
        log_error "ADMIN_PASSWORD 不满足强口令要求：至少 12 位且含大写字母、小写字母、数字与特殊字符。"
        log_error "（该规则取控制面与 auth-svc 的并集，任一侧不满足都会导致对应服务拒绝启动）"
        log_error "生成：openssl rand -hex 16（再按要求补大小写/数字/特殊字符，或用 deploy.sh init 重新生成）"
        exit 1
    fi

    ver="$(env_val OPSMESH_VERSION)"
    if [ "$ver" = "latest" ]; then
        log_error "OPSMESH_VERSION=latest 禁止用于生产：镜像内容不可追溯，回滚无法定位版本。"
        log_error "改为具体版本号（如 0.9.0）。"
        exit 1
    fi

    adv="$(env_val OPSMESH_ADVERTISE_ADDR)"
    if [[ ! "$adv" =~ ^https?:// ]]; then
        log_error "OPSMESH_ADVERTISE_ADDR 必须以 http:// 或 https:// 开头，当前: $adv"
        exit 1
    fi

    # DSN 拼接安全：compose 用 "user:pass@tcp(host:port)/db" 形式，go-sql-driver 取最后一个 '@'
    # 作为 userinfo 分隔符，口令含 '@' 会被截断成错误的口令（连接认证失败且看不出原因）。
    local mp
    mp="$(env_val MYSQL_PASSWORD)"
    case "$mp" in
        *@*) log_error "MYSQL_PASSWORD 不能包含 '@'（会破坏 user:pass@tcp(...) 形式的 DSN 解析）。"; exit 1 ;;
        *[[:space:]]*) log_error "MYSQL_PASSWORD 不能包含空白字符。"; exit 1 ;;
    esac
    local rp
    rp="$(env_val MYSQL_ROOT_PASSWORD)"
    case "$rp" in
        *\'*) log_error "MYSQL_ROOT_PASSWORD 不能包含单引号（MySQL 初始化脚本以单引号包裹口令）。"; exit 1 ;;
        *[[:space:]]*) log_error "MYSQL_ROOT_PASSWORD 不能包含空白字符。"; exit 1 ;;
    esac

    log_ok "环境变量校验通过（${#REQUIRED_VARS[@]} 项必需变量齐备）"
}

# 强口令判定：必须与两个「认证权威」的规则取并集，否则同一口令会在其中一个服务被拒。
#   - 控制面 internal/controlplane/auth_password.go（validateStrongPassword）
#   - auth-svc services/auth-svc/internal/http/password.go（defaultPasswordPolicy）
# auth-svc 默认更严（≥12 位 + 大小写 + 数字 + 特殊字符 + 弱口令黑名单），故此处按最严基线校验。
# 不满足时历史症状是 auth-svc 启动即 unhealthy（初始 admin 引导失败），而控制面却是 healthy——
# 部署看起来「起了一半」，排查成本很高，因此在预检阶段就拦下。
password_is_strong() {
    local pw="$1"
    [ "${#pw}" -ge 12 ] || return 1
    [[ "$pw" == *[A-Z]* ]] || return 1
    [[ "$pw" == *[a-z]* ]] || return 1
    [[ "$pw" == *[0-9]* ]] || return 1
    # 特殊字符：非字母数字即可（与 auth-svc 的 hasSpecial 判定一致）
    [[ "$pw" == *[^A-Za-z0-9]* ]] || return 1
    return 0
}

# 这里只判定「含非字母数字」；生成侧更严，用文件头的 PASSWORD_SPECIAL_CHARS（rand_pw 强制执行，是本判定的子集）。

# ============================================================
# TLS 证书校验
# ============================================================
tls_dir_abs() {
    local d
    d="$(env_val OPSMESH_TLS_DIR ./certs)"
    case "$d" in
        /*|[A-Za-z]:*) printf '%s' "$d" ;;
        *) printf '%s' "${PROJECT_DIR}/${d#./}" ;;
    esac
}

check_tls_certs() {
    local dir; dir="$(tls_dir_abs)"
    local crt="${dir}/tls.crt" key="${dir}/tls.key"

    if [ ! -f "$crt" ] || [ ! -f "$key" ]; then
        log_error "TLS 证书缺失：$crt 或 $key 不存在。"
        log_error "生成：./scripts/deploy.sh init（或 ./scripts/gen-tls.sh --dir $dir --advertise $(env_val OPSMESH_ADVERTISE_ADDR)）"
        log_error "注意：不要让 Docker 代替你创建该目录——宿主路径不存在时会被自动创建为空目录，"
        log_error "      容器挂载后看不到证书，控制面在启动期以「无 TLS 证书」拒绝启动。"
        exit 1
    fi

    if command -v openssl &>/dev/null; then
        if ! openssl x509 -in "$crt" -noout -checkend 604800 &>/dev/null; then
            log_warn "证书将在 7 天内到期，请及时轮换：$crt"
        fi
        # -noout -text 在各 openssl 版本均可用（-ext 需 ≥1.1.1，缺失时会误报无 SAN）
        local san
        if san="$(openssl x509 -in "$crt" -noout -text 2>/dev/null)"; then
            if ! printf '%s' "$san" | grep -q "DNS:"; then
                log_warn "证书未包含任何 DNS SAN——agent 通过主机名连接时会因名称不匹配握手失败。"
            fi
        fi
    fi
    log_ok "TLS 证书就绪：$crt"
}

is_self_signed() {
    local crt="$1" iss subj
    iss="$(openssl x509 -in "$crt" -noout -issuer 2>/dev/null | sed 's/^issuer=//')" || true
    subj="$(openssl x509 -in "$crt" -noout -subject 2>/dev/null | sed 's/^subject=//')" || true
    [ -n "$iss" ] && [ "$iss" = "$subj" ]
}

# ============================================================
# 预检
# ============================================================
port_in_use() {
    local port="$1" ports
    if ports="$(listening_ports)"; then
        printf '%s\n' "$ports" | grep -qx "$port" && return 0
        return 1
    fi
    # 无可用工具（或非 Linux/macOS 的 netstat）：退化为连接测试——能连上即认为被占用
    (exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null && return 0
    return 1
}

# listening_ports 输出当前 LISTEN 状态的端口号（每行一个）。
# 返回非 0 表示本机没有可用的探测工具，调用方需退化为连接测试。
# 注意：Windows 自带 netstat 不支持 -tln（会打印帮助文本），因此这里按输出列位置
# 同时兼容 Linux/macOS 的 Local Address 列（$4）与 Windows 的 $2，而不是信任退出码。
listening_ports() {
    local raw="" ports=""
    if command -v ss &>/dev/null; then
        raw="$(ss -tln 2>/dev/null || true)"
        if printf '%s\n' "$raw" | grep -q "LISTEN"; then
            printf '%s\n' "$raw" | awk 'NR>1 {print $4}' | sed -n 's/.*[:.]\([0-9]\{1,5\}\)$/\1/p'
            return 0
        fi
    fi
    if command -v netstat &>/dev/null; then
        raw="$(netstat -an 2>/dev/null || true)"
        ports="$(printf '%s\n' "$raw" | awk 'toupper($0) ~ /LISTEN/ {print $2; print $4}' | sed -n 's/.*[:.]\([0-9]\{1,5\}\)$/\1/p')"
        if [ -n "$ports" ]; then
            printf '%s\n' "$ports"
            return 0
        fi
    fi
    return 1
}

check_disk_space() {
    local avail
    avail="$(df -Pk "$PROJECT_DIR" 2>/dev/null | awk 'NR==2 {print int($4/1024/1024)}')" || true
    if [[ "$avail" =~ ^[0-9]+$ ]] && [ "$avail" -gt 0 ]; then
        if [ "$avail" -lt "$MIN_DISK_GB" ]; then
            log_error "磁盘可用空间不足：${avail}GB < ${MIN_DISK_GB}GB"
            exit 1
        fi
        log_ok "磁盘可用空间：${avail}GB"
    else
        log_warn "无法读取磁盘可用空间，跳过检查"
    fi
}

check_ports() {
    local ports=() var def
    # 对外发布端口
    if [ "$PROXY" = true ]; then
        ports+=("$(env_val GATEWAY_HTTP_PORT 80)" "$(env_val GATEWAY_HTTPS_PORT 443)")
    else
        ports+=("$(env_val CONTROLPLANE_HTTP_PORT 8080)")
    fi
    ports+=("$(env_val CONTROLPLANE_GRPC_PORT 9090)")
    # 仅绑 127.0.0.1 的端口同样占用宿主端口（宿主自带 MySQL/Redis 是最常见的冲突源）
    for spec in \
        CONTROLPLANE_METRICS_PORT:9091 MYSQL_PORT:3306 REDIS_PORT:6379 \
        PROMETHEUS_PORT:9092 GRAFANA_PORT:3000 LOKI_PORT:3100 \
        OTEL_GRPC_PORT:4317 OTEL_HTTP_PORT:4318 OTEL_METRICS_PORT:8888 \
        OTEL_HEALTH_PORT:13134 \
        AUTH_SVC_HTTP_PORT:8100 DEVICE_SVC_HTTP_PORT:8101 TASK_SVC_HTTP_PORT:8102 \
        ALERT_SVC_HTTP_PORT:8103 LOG_SVC_HTTP_PORT:8105 LOG_SVC_GRPC_PORT:9095 \
        CONFIG_SVC_HTTP_PORT:8106 GPU_SVC_HTTP_PORT:8107 AIO_SVC_HTTP_PORT:8108 \
        PORTAL_SVC_HTTP_PORT:8109
    do
        var="${spec%%:*}"; def="${spec##*:}"
        ports+=("$(env_val "$var" "$def")")
    done

    local used=() p
    for p in "${ports[@]}"; do
        port_in_use "$p" && used+=("$p")
    done

    if [ ${#used[@]} -gt 0 ]; then
        log_warn "以下端口已被占用：${used[*]}"
        log_warn "占用 3306/6379 通常是宿主已装 MySQL/Redis；可在 .env 改 MYSQL_PORT/REDIS_PORT。"
        confirm "继续部署（占用方若与本栈冲突，相关容器会启动失败）？" || exit 1
    else
        log_ok "全部 ${#ports[@]} 个宿主端口可用"
    fi
}

check_proxy_consistency() {
    # 控制面 CSRF 校验是 Origin 与 AdvertiseAddr 的 host:port 字符串比对
    # （internal/controlplane/server_middleware.go:112），而浏览器在 Origin 里
    # 会省略协议默认端口（https 的 443 / http 的 80）——因此 AdvertiseAddr 必须与
    # 浏览器地址栏实际形态逐字符一致：默认端口就不能写，非默认端口就必须写。
    # 不一致的后果是「浏览器写操作全部 403」，且启动期与日志里都没有明显错误。
    local adv scheme rest host_part port_part expected_port default_port files_note

    adv="$(env_val OPSMESH_ADVERTISE_ADDR)"
    scheme="${adv%%://*}"

    if [ "$PROXY" = true ]; then
        if [ "$scheme" != "https" ]; then
            log_error "反代形态下 OPSMESH_ADVERTISE_ADDR 必须是 https://（网关终止 TLS），当前: $adv"
            exit 1
        fi
        expected_port="$(env_val GATEWAY_HTTPS_PORT 443)"
        files_note="GATEWAY_HTTPS_PORT"
    else
        if [ "$scheme" != "https" ]; then
            log_warn "直连形态建议 OPSMESH_ADVERTISE_ADDR 使用 https://（证书已挂载且 --http-tls=auto）"
        fi
        expected_port="$(env_val CONTROLPLANE_HTTP_PORT 8080)"
        files_note="CONTROLPLANE_HTTP_PORT"
    fi

    case "$scheme" in
        https) default_port="443" ;;
        http)  default_port="80" ;;
        *)     log_error "OPSMESH_ADVERTISE_ADDR 协议无法识别: $adv"; exit 1 ;;
    esac

    rest="${adv#*://}"; rest="${rest%%/*}"; rest="${rest##*@}"
    host_part="$rest"
    port_part=""
    if [[ "$rest" == \[*\]* ]]; then
        host_part="${rest%%]*}]"
        port_part="${rest#*]}"; port_part="${port_part#:}"
    elif [[ "$rest" == *:* ]]; then
        host_part="${rest%%:*}"
        port_part="${rest##*:}"
    fi

    if [ -n "$port_part" ]; then
        if [ "$port_part" != "$expected_port" ]; then
            log_error "OPSMESH_ADVERTISE_ADDR 端口与对外监听端口不一致："
            log_error "  当前值: ${adv}（端口 ${port_part}）；期望端口: ${expected_port}（来自 ${files_note}）"
            if [ "$expected_port" = "$default_port" ]; then
                log_error "  应写作 ${scheme}://${host_part}（默认端口不写，浏览器 Origin 会省略它）"
            else
                log_error "  应写作 ${scheme}://${host_part}:${expected_port}"
            fi
            log_error "  该值同时用于 bootstrap 下载地址与 CSRF Origin 校验基准，不一致时写操作会被 403 拒绝。"
            exit 1
        fi
        if [ "$expected_port" = "$default_port" ]; then
            log_error "OPSMESH_ADVERTISE_ADDR 不应写端口：${expected_port} 是 ${scheme} 默认端口，浏览器 Origin 会省略它。"
            log_error "  当前: ${adv}；应写作 ${scheme}://${host_part}"
            log_error "  （CSRF 校验是 host 字符串比对，带默认端口会与浏览器 Origin 不匹配 → 写操作 403）"
            exit 1
        fi
    elif [ "$expected_port" != "$default_port" ]; then
        log_error "OPSMESH_ADVERTISE_ADDR 缺少端口：对外监听在 ${expected_port}（${files_note}），浏览器 Origin 会带该端口。"
        log_error "  当前: ${adv}；应写作 ${scheme}://${host_part}:${expected_port}"
        exit 1
    fi

    log_ok "对外地址校验通过：${adv}（与浏览器 Origin 形态一致）"
}

check_compose_render() {
    local out proj
    if ! out="$(compose config 2>&1 >/dev/null)"; then
        log_error "docker compose 渲染失败（.env 缺变量或 compose 语法错误）："
        printf '%s\n' "$out" | head -20 >&2
        exit 1
    fi

    # 项目隔离硬门禁：项目名必须是 opsmesh*。
    # 若为目录名（如本目录名 docker），则与同名目录下的其他 compose 项目共享项目名，
    # 一次 down --remove-orphans 就会把对方的容器一起删掉（实测曾列出 17 个 Dify 容器）。
    proj="$(compose_project_name)"
    if [[ "$proj" != opsmesh* ]]; then
        log_error "Compose 项目名为 '${proj}'，存在跨项目误删风险："
        log_error "  ${COMPOSE_BASE} 顶部需保留 'name: opsmesh'（或设置环境变量 COMPOSE_PROJECT_NAME=opsmesh-*）。"
        log_error "  未显式命名时 Compose 用目录名当项目名，down --remove-orphans 会删除同一项目名下其他栈的容器。"
        exit 1
    fi

    log_ok "Compose 配置渲染通过（项目 ${proj}；$( [ "$PROXY" = true ] && echo '直连 + 反代 overlay' || echo '控制面直连 HTTPS' )）"
}

preflight_checks() {
    log_section "预检"

    check_command docker || exit 1
    check_command openssl || { log_error "openssl 未安装（证书与密钥生成需要）"; exit 1; }
    check_compose_version

    if ! docker info &>/dev/null; then
        log_error "Docker 守护进程未运行"
        exit 1
    fi
    log_ok "Docker 已运行（$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo 'unknown')）"

    check_disk_space

    if [ ! -f "$ENV_FILE" ]; then
        log_warn ".env 不存在，自动执行 init 生成随机口令与自签证书"
        do_init
    fi
    check_env_vars
    check_tls_certs
    check_ports
    check_proxy_consistency
    check_compose_render
}

# ============================================================
# init：生成 .env + 自签证书
# ============================================================
detect_host_ip() {
    local ip=""
    if command -v hostname &>/dev/null; then
        ip="$(hostname -I 2>/dev/null | awk '{print $1}')" || true
    fi
    if [ -z "$ip" ] && command -v ip &>/dev/null; then
        ip="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") {print $(i+1); exit}}')" || true
    fi
    printf '%s' "$ip"
}

detect_version() {
    local vf="${REPO_ROOT}/internal/version/version.go" v=""
    if [ -f "$vf" ]; then
        v="$(grep -E '^[[:space:]]*var Version = "' "$vf" 2>/dev/null | head -1 | sed 's/.*"\(.*\)".*/\1/')" || true
    fi
    if [ -z "$v" ] && [ -f "${REPO_ROOT}/deploy/helm/opsmesh/Chart.yaml" ]; then
        v="$(grep -E '^appVersion:' "${REPO_ROOT}/deploy/helm/opsmesh/Chart.yaml" 2>/dev/null | head -1 | sed 's/.*"\(.*\)".*/\1/')" || true
    fi
    printf '%s' "$v"
}

generate_env_file() {
    local jwt enc admin cfg_key mysql_pw mysql_root redis_pw grafana_pw provision federation version host ip advertise

    jwt="$(rand_b64 48)"
    enc="$(rand_b64 32)"
    cfg_key="$(rand_b64 32)"
    mysql_pw="$(rand_pw 32)"
    mysql_root="$(rand_pw 32)"
    redis_pw="$(rand_pw 32)"
    grafana_pw="$(rand_pw 32)"
    provision="$(rand_b64 32)"
    federation="$(rand_b64 32)"
    # 初始 admin 口令：前缀 Aa1 保证含大写/小写/数字（控制面强口令校验要求），
    # 纯 hex 全小写+数字会被拒绝启动。首登强制改密。
    # 固定前缀 Aa1! 保证「大写+小写+数字+特殊字符」四要素恒定满足，
    # 随机性全部来自 32 位 hex（128 bit 熵），远高于暴力破解门槛。
    admin="Aa1!$(rand_hex 16)"

    version="$(detect_version)"
    if [ -z "$version" ]; then
        log_error "无法从源码推断版本号（internal/version/version.go 或 Helm Chart.yaml 未找到）。"
        log_error "请在 .env 中手动设置 OPSMESH_VERSION（如 0.9.0，禁止 latest）。"
        exit 1
    fi

    ip="$(detect_host_ip)"
    if [ -n "$ip" ]; then
        host="$ip"
    else
        host="127.0.0.1"
    fi
    # 与下方 CONTROLPLANE_HTTP_PORT 同源：默认端口不是 443，因此 advertise 必须带端口
    # （浏览器 Origin 只在协议默认端口时才省略端口；不一致会被 CSRF Origin 校验 403）。
    local cp_port=8080
    advertise="https://${host}:${cp_port}"

    cat > "$ENV_FILE" <<EOF
# OpsMesh 生产环境配置
# 生成时间: $(date -u +"%Y-%m-%dT%H:%M:%SZ")
# 内含全部明文口令，勿提交仓库/勿外传；应仅部署用户可读（POSIX 0600 或平台等价 ACL）。
# 重新生成：./scripts/deploy.sh init --force（会覆盖本文件）

# === 核心安全（全部随机生成，无需手工修改）===
# JWT 签发密钥（HS256，≥32 字节；空值/短值在生产模式会被控制面拒绝启动）
JWT_SECRET=${jwt}
# kubeconfig 静态加密密钥（base64 的 32 字节 AES-256；等保三级要求静态加密）
ENCRYPTION_KEY=${enc}
# 内置 admin 初始口令（首登强制改密）。生产模式未配置时控制面拒绝启动（防管理员被锁死）
ADMIN_PASSWORD=${admin}
# config-svc secret 静态加密口令（SHA-256 派生 AES 密钥）
CONFIG_ENCRYPTION_KEY=${cfg_key}
# bootstrap 安装令牌签名密钥：留空=进程内随机（重启后未消费的安装令牌全部失效，是安全特性）
# 多副本或「重启后令牌仍需有效」场景须固定该值。
PROVISION_SECRET=${provision}
# 跨集群联邦密钥
FEDERATION_SECRET=${federation}

# === 数据库 / 缓存 ===
MYSQL_ROOT_PASSWORD=${mysql_root}
MYSQL_USER=opsmesh
MYSQL_PASSWORD=${mysql_pw}
REDIS_PASSWORD=${redis_pw}
MYSQL_PORT=3306
REDIS_PORT=6379

# === 部署形态 ===
# 镜像标签：禁止 latest（浮动标签导致回滚无法定位版本）。默认取自源码版本。
OPSMESH_VERSION=${version}
OPSMESH_TLS_DIR=./certs
# 控制面对外可达地址：必须与浏览器/agent 实际访问的地址【逐字符一致】。
# 用途：① bootstrap 安装命令里的下载地址；② 控制面 CSRF Origin 校验基准。
# 规则：浏览器 Origin 会省略协议默认端口（https 的 443 / http 的 80）——
#   端口是 443/80 时【不要写端口】，非默认端口【必须写端口】。
#   用 IP 访问就填 IP，用域名访问就填域名。填错表现为浏览器写操作 403，且无明显报错。
# 反代形态（--proxy）：填网关的对外地址，端口须与 GATEWAY_HTTPS_PORT 一致。
OPSMESH_ADVERTISE_ADDR=${advertise}

# === 服务端口（宿主侧映射端口）===
CONTROLPLANE_HTTP_PORT=${cp_port}
CONTROLPLANE_GRPC_PORT=9090
CONTROLPLANE_METRICS_PORT=9091
AUTH_SVC_HTTP_PORT=8100
DEVICE_SVC_HTTP_PORT=8101
TASK_SVC_HTTP_PORT=8102
ALERT_SVC_HTTP_PORT=8103
LOG_SVC_HTTP_PORT=8105
LOG_SVC_GRPC_PORT=9095
CONFIG_SVC_HTTP_PORT=8106
GPU_SVC_HTTP_PORT=8107
AIO_SVC_HTTP_PORT=8108
PORTAL_SVC_HTTP_PORT=8109

# === 可观测性端口（仅绑 127.0.0.1）===
PROMETHEUS_PORT=9092
GRAFANA_PORT=3000
LOKI_PORT=3100
OTEL_GRPC_PORT=4317
OTEL_HTTP_PORT=4318
OTEL_METRICS_PORT=8888
OTEL_HEALTH_PORT=13134

# === 反向代理 overlay（bash scripts/deploy.sh up --proxy 时生效）===
# 宿主机 80/443 被占用时改这两个端口，并同步修改 OPSMESH_ADVERTISE_ADDR
GATEWAY_HTTP_PORT=80
GATEWAY_HTTPS_PORT=443
GATEWAY_SERVER_NAME=_

# === Grafana ===
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=${grafana_pw}
GRAFANA_ROOT_URL=http://localhost:3000

# === 运行模式与安全开关 ===
PRODUCTION_MODE=true
REQUIRE_AUTH=true
COOKIE_SECURE=true
PUBLIC_REGISTER=false
ALLOW_PUBLIC_REGISTER=false
# 直连形态为 false；反代 overlay 会覆盖为 true（否则限流/审计拿到的是网关 IP）
TRUST_PROXY=false
GRPC_REQUIRE_SIGNATURE=true
# metrics 来源白名单（默认仅本机 + compose 网段；空值=不限来源）
METRICS_ALLOW_CIDR=127.0.0.0/8,172.28.0.0/16
# API 限流 req/s/IP（空=用代码默认：生产 200；0=显式关闭，控制面会打印告警）
CB_RATE_LIMIT_PER_SEC=
# P1-3 审计日志保留天数：超龄审计行由 leader 搬入 audit_log_archive 后从在线表删除。
# 0=永久保留。等保三级/ISO 27001 通常要求审计留存 ≥ 180 天（默认即 180），按合规要求调整。
AUDIT_RETENTION_DAYS=180

# === 可支撑性（P1-6）===
# 日志级别：debug|info|warn|error。排障期间临时改 debug，结束后改回 info
# （debug 量级显著上升；非法值会让控制面启动期直接失败而非静默退回）。
LOG_LEVEL=info
# 是否在 B/S 端口暴露 /debug/pprof/*：默认 false。开启后仍受 METRICS_ALLOW_CIDR 准入
# （生产形态该白名单为空即全拒）。pprof 能读进程内存与调用栈，仅排障期间开启、用完关闭。
DEBUG_PPROF=false

# === 存储后端（sql=MySQL 持久化；memory 仅用于演示，重启丢数据）===
DEVICE_STORE_TYPE=sql
TASK_STORE_TYPE=sql
ALERT_STORE_TYPE=sql
CONFIG_STORE_TYPE=sql
LOG_BACKEND=loki

# === 认证令牌有效期 ===
AUTH_ACCESS_TTL=900s
AUTH_REFRESH_TTL=168h

# === 告警通道（PagerDuty；启用时须填 ROUTING_KEY）===
PAGERDUTY_ENABLED=false
PAGERDUTY_ROUTING_KEY=
PAGERDUTY_API_URL=https://events.pagerduty.com/v2/enqueue

# === 可选集成 ===
OLLAMA_URL=http://ollama:11434

# === 日志与时区 ===
LOG_LEVEL=info
TZ=UTC
EOF

    # 权限必须**验证**而不是假定：NTFS/Git-Bash 上 chmod 会静默失败（`|| true`），
    # 此时文件仍是 0644 而日志照说"权限 0600"——一句关于凭据文件保护的不实陈述。
    chmod 600 "$ENV_FILE" 2>/dev/null || true
    mode="$(stat -c '%a' "$ENV_FILE" 2>/dev/null || stat -f '%Lp' "$ENV_FILE" 2>/dev/null || echo unknown)"
    if [ "$mode" = "600" ]; then
        log_ok "已生成 ${ENV_FILE}（随机口令/密钥，权限 0600）"
    else
        log_warn "已生成 ${ENV_FILE}，但实际权限为 ${mode}（chmod 未生效——本文件系统不支持 POSIX 权限）"
        log_warn "该文件含全部明文口令：请用平台 ACL 限制读取（Windows 例：icacls 文件 /inheritance:r /grant:r 当前用户:F），"
        log_warn "不要把 chmod 成功当成凭据已被保护。"
    fi

    if [ "$host" = "127.0.0.1" ]; then
        log_warn "未能自动探测本机 IP，OPSMESH_ADVERTISE_ADDR 暂设为 https://127.0.0.1:8080"
        log_warn "仅同机访问可用；远程 agent/浏览器访问前必须改成真实可达地址。"
    fi
}

do_init() {
    log_section "初始化（.env + 自签证书）"

    if [ -f "$ENV_FILE" ] && [ "$FORCE" != true ]; then
        log_info ".env 已存在，跳过生成（--force 可覆盖重建；覆盖会使已存数据卷的 MySQL 口令失效）"
    else
        if [ -f "$ENV_FILE" ] && [ "$FORCE" = true ]; then
            log_warn "--force：将覆盖现有 .env（MySQL/Redis 已初始化的数据卷会因口令变更而连不上）"
            confirm "确认覆盖 .env？" || exit 1
        fi
        generate_env_file
    fi

    local tls_dir adv
    tls_dir="$(tls_dir_abs)"
    adv="$(env_val OPSMESH_ADVERTISE_ADDR)"

    log_info "生成自签 TLS 证书 → ${tls_dir}"
    if [ "$FORCE" = true ]; then
        bash "$GEN_TLS" --dir "$tls_dir" --advertise "$adv" --force
    else
        bash "$GEN_TLS" --dir "$tls_dir" --advertise "$adv"
    fi

    echo ""
    log_ok "初始化完成"
    echo -e "  ${BOLD}下一步${NC}："
    echo -e "    1. 核对 ${CYAN}OPSMESH_ADVERTISE_ADDR${NC}（当前: $(env_val OPSMESH_ADVERTISE_ADDR)）"
    echo -e "       必须与浏览器/agent 实际访问的地址一致；用域名访问就填域名。"
    echo -e "    2. 启动：${CYAN}./scripts/deploy.sh up${NC}$( [ "$PROXY" = false ] && echo '  或反代形态: ./scripts/deploy.sh up --proxy' )"
    echo -e "    3. 初始 admin 口令：${CYAN}grep '^ADMIN_PASSWORD=' ${ENV_FILE}${NC}（首登强制改密）"
}

# ============================================================
# 构建
# ============================================================
build_images() {
    log_section "构建镜像"

    if [ "$NO_BUILD" = true ]; then
        log_warn "--no-build：跳过镜像构建，直接使用本地已有镜像"
        return 0
    fi

    compose build --parallel --pull 2>&1 | tee -a "$LOG_FILE"
    log_ok "镜像构建完成"
}

# ============================================================
# 服务等待
# ============================================================
svc_state() {
    local svc="$1" cid
    cid="$(compose ps -q "$svc" 2>/dev/null | head -1)"
    if [ -z "$cid" ]; then
        printf 'absent'
        return
    fi
    docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{if .State.Running}}running{{else}}stopped{{end}}{{end}}' "$cid" 2>/dev/null || printf 'unknown'
}

wait_for_healthy() {
    local svc="$1" max_wait="${2:-120}" interval=5 elapsed=0 state

    log_info "等待 ${svc} 就绪（最长 ${max_wait}s）..."
    while [ "$elapsed" -lt "$max_wait" ]; do
        state="$(svc_state "$svc")"
        case "$state" in
            healthy|running)
                log_ok "${svc} 就绪（${state}）"
                return 0
                ;;
            unhealthy|stopped)
                log_error "${svc} 状态异常：${state}"
                log_error "最近日志："
                compose logs --tail=30 "$svc" >&2 || true
                return 1
                ;;
        esac
        sleep $interval
        elapsed=$((elapsed + interval))
        printf '.'
    done
    echo ""
    log_error "${svc} 在 ${max_wait}s 内未就绪（最后状态：$(svc_state "$svc")）"
    compose logs --tail=30 "$svc" >&2 || true
    return 1
}

# ============================================================
# 启动各层
# ============================================================
start_infrastructure() {
    log_section "启动基础设施"
    log_info "启动 MySQL..."
    compose up -d mysql
    wait_for_healthy mysql 180
    verify_mysql_app_auth

    log_info "启动 Redis..."
    compose up -d redis
    wait_for_healthy redis 60
    log_ok "基础设施就绪"
}

# MySQL 只在「数据目录为空」时应用 MYSQL_ROOT_PASSWORD / MYSQL_USER / MYSQL_PASSWORD。
# 宿主上若已存在 opsmesh-mysql-data 卷（上一次部署留下的），它会继续用旧口令——
# 此时新 .env 里的随机口令与之不匹配，各服务连接被 Access denied 拒绝，
# 而 MySQL 自身仍是 healthy（表现为「数据库健康但服务全部起不来」）。
# 这里用应用账号实连一次，把该故障提前到基础设施阶段并给出两条明确出路。
verify_mysql_app_auth() {
    local user pass
    user="$(env_val MYSQL_USER opsmesh)"
    pass="$(env_val MYSQL_PASSWORD)"

    # 注意：不能用 mysqladmin ping 判断——MySQL 官方文档明确说明「Access denied 时
    # ping 仍返回 0」（因为服务器确实在运行）。必须用需要真正认证的语句。
    if compose exec -T -e MYSQL_PWD="$pass" mysql mysql -h 127.0.0.1 -u"$user" --batch --silent -e "SELECT 1" &>/dev/null; then
        log_ok "MySQL 应用账号认证通过（${user}）"
        return 0
    fi

    log_error "MySQL 应用账号 ${user} 认证失败——当前 .env 的口令与数据卷内的口令不一致。"
    log_error "原因：MySQL 仅在首次初始化数据目录时写入口令；复用已存在的数据卷会沿用旧口令。"
    log_error "二选一："
    log_error "  a) 恢复旧口令：把上次部署 .env 中的 MYSQL_ROOT_PASSWORD/MYSQL_PASSWORD 填回本次 .env（保留数据）"
    log_error "  b) 丢弃旧数据：./scripts/deploy.sh down --volumes 后重新 up（数据不可恢复）"
    exit 1
}

start_observability() {
    log_section "启动可观测栈"
    log_info "启动 Prometheus / Loki / OTel Collector / Grafana..."
    compose up -d prometheus loki otel-collector grafana
    wait_for_healthy prometheus 90
    # 告警规则热加载：alerts.yml 是宿主机 bind mount，升级后文件内容已变，但 Prometheus
    # 不会自动重读（实测：新增 opsmesh_audit_chain_alerts 组在不 reload 时始终不生效）。
    # 容器未重建 → 必须显式 reload，否则客户升级后静默沿用旧告警规则。
    if compose exec -T prometheus wget -qO- --post-data='' http://127.0.0.1:9090/-/reload >/dev/null 2>&1; then
        log_ok "Prometheus 告警规则已热加载"
    else
        log_warn "Prometheus 热加载失败（--web.enable-lifecycle 未开启？）——告警规则将在容器重建后生效"
    fi
    wait_for_healthy loki 90
    wait_for_healthy otel-collector 90
    wait_for_healthy grafana 90
    log_ok "可观测栈就绪"
}

start_services() {
    log_section "启动 OpsMesh 服务"

    log_info "启动 controlplane..."
    compose up -d controlplane
    wait_for_healthy controlplane 120

    local svc
    for svc in auth-svc device-svc task-svc alert-svc config-svc log-svc gpu-svc aio-svc portal-svc; do
        log_info "启动 ${svc}..."
        compose up -d "$svc"
        wait_for_healthy "$svc" 90
    done

    if [ "$PROXY" = true ]; then
        log_info "启动反代网关（Nginx）..."
        compose up -d gateway
        wait_for_healthy gateway 60
    fi

    log_ok "全部服务已启动"
}

# ============================================================
# 冒烟测试
# ============================================================
CURL_TLS_ARGS=()

setup_smoke_env() {
    CURL_TLS_ARGS=()
    local crt; crt="$(tls_dir_abs)/tls.crt"
    if [ -f "$crt" ] && command -v openssl &>/dev/null && is_self_signed "$crt"; then
        # 自签证书：冒烟测试自身用 -k（不掩盖生产证书问题——自签是本形态的既定选择）
        CURL_TLS_ARGS=(-k)
    fi
}

http_ok() {
    local url="$1"
    curl -fsS --max-time 8 "${CURL_TLS_ARGS[@]}" -o /dev/null "$url" 2>/dev/null
}

run_smoke_tests() {
    log_section "冒烟测试"
    setup_smoke_env

    local failures=0 scheme host port cp_url

    if [ "$PROXY" = true ]; then
        host="localhost"
        port="$(env_val GATEWAY_HTTPS_PORT 443)"
    else
        host="localhost"
        port="$(env_val CONTROLPLANE_HTTP_PORT 8080)"
    fi
    cp_url="https://${host}:${port}"

    # 1) 控制面健康检查（对外入口，失败即部署失败）
    log_info "控制面 ${cp_url}/healthz ..."
    if http_ok "${cp_url}/healthz"; then
        log_ok "控制面 /healthz 正常"
    else
        log_error "控制面 /healthz 无响应"
        failures=$((failures + 1))
    fi

    # 2) 控制面 REST API（未认证时可能 401，仅提示）
    log_info "控制面 ${cp_url}/api/v1/health ..."
    if http_ok "${cp_url}/api/v1/health"; then
        log_ok "控制面 REST API 正常"
    else
        log_warn "控制面 REST API 未返回 2xx（可能需要鉴权），请人工确认"
    fi

    # 3) 反代形态：网关健康 + HTTP→HTTPS 跳转端口是否保留
    if [ "$PROXY" = true ]; then
        local ghttp; ghttp="$(env_val GATEWAY_HTTP_PORT 80)"
        log_info "网关 http://127.0.0.1:${ghttp}/nginx-health ..."
        if curl -fsS --max-time 8 -o /dev/null "http://127.0.0.1:${ghttp}/nginx-health" 2>/dev/null; then
            log_ok "网关存活探针正常"
        else
            log_error "网关存活探针失败"
            failures=$((failures + 1))
        fi

        log_info "HTTP→HTTPS 跳转 ..."
        local loc
        loc="$(curl -sS --max-time 8 -o /dev/null -w '%{redirect_url}' "http://127.0.0.1:${ghttp}/healthz" 2>/dev/null || true)"
        if [ "$loc" = "https://localhost:${port}/healthz" ] || [[ "$loc" == https://*":${port}/healthz" ]]; then
            log_ok "跳转目标正确：${loc}"
        elif [ -z "$loc" ]; then
            log_warn "未取到跳转地址（网关可能尚未就绪）"
        else
            log_warn "跳转目标可疑：${loc}（期望 https://<host>:${port}/healthz，端口丢失会导致跳转到错误地址）"
        fi
    fi

    # 4) 可观测栈（Prometheus 失败视为部署失败，其余为提示）
    log_info "Prometheus ..."
    if curl -fsS --max-time 8 -o /dev/null "http://127.0.0.1:$(env_val PROMETHEUS_PORT 9092)/-/healthy" 2>/dev/null; then
        log_ok "Prometheus 正常"
    else
        log_error "Prometheus 无响应"
        failures=$((failures + 1))
    fi

    # 4b) 采集目标真实性：任何 target 长期 DOWN 都会让 alerts.yml 的
    # up==0 / MySQLDown / RedisDown 常驻 firing。假告警会让值班整体忽略告警，
    # 比不配告警更危险，故这里作为部署失败项拦截（新起的 blackbox 拨测 target
    # 需要 1 个抓取周期才转 up，故最多重试 ~45s）。
    # 查询带 `time() - timestamp(up) < 60`：只统计【仍在被采集但报 0】的目标，
    # 排除「已从配置中删除、样本进入 5 分钟 staleness 窗口」的历史 series
    # （否则改完 prometheus.yml 重启后，已删除的 job 会在窗口内造成误判）。
    local q_resp down_jobs
    down_jobs=""
    for _ in 1 2 3 4 5 6 7 8 9; do
        q_resp="$(curl -fsS --max-time 8 \
            --data-urlencode 'query=up == 0 and (time() - timestamp(up)) < 60' \
            "http://127.0.0.1:$(env_val PROMETHEUS_PORT 9092)/api/v1/query" 2>/dev/null || true)"
        if [ -z "$q_resp" ]; then
            down_jobs="__query_failed__"
            sleep 5
            continue
        fi
        down_jobs="$(printf '%s' "$q_resp" | grep -o '"job":"[^"]*"' \
            | sed 's/"job":"//; s/"$//' | sort -u | tr '\n' ' ' | sed 's/ *$//')"
        [ -z "$down_jobs" ] && break
        sleep 5
    done
    if [ -z "$down_jobs" ]; then
        log_ok "Prometheus 采集目标全部 UP（无 DOWN，不会产生假告警）"
    elif [ "$down_jobs" = "__query_failed__" ]; then
        log_warn "无法查询 Prometheus up 指标，跳过采集目标校验"
    else
        log_error "Prometheus 存在 DOWN 的采集 job：${down_jobs}（会触发 up==0 类假告警）"
        failures=$((failures + 1))
    fi

    log_info "Loki ..."
    if http_ok "http://127.0.0.1:$(env_val LOKI_PORT 3100)/ready"; then
        log_ok "Loki 正常"
    else
        log_warn "Loki 未就绪（日志检索可能暂不可用）"
    fi
    log_info "Grafana ..."
    if http_ok "http://127.0.0.1:$(env_val GRAFANA_PORT 3000)/api/health"; then
        log_ok "Grafana 正常"
    else
        log_warn "Grafana 未就绪（启动较慢，可稍后复查）"
    fi

    # otel-collector 是 distroless 镜像（无 shell/wget），容器内 healthcheck 无法实现，
    # 因此这里从宿主侧探其 health_check 扩展端口（compose 已发布 127.0.0.1:13134）。
    log_info "OTel Collector ..."
    if http_ok "http://127.0.0.1:$(env_val OTEL_HEALTH_PORT 13134)/"; then
        log_ok "OTel Collector 正常"
    else
        log_warn "OTel Collector 未就绪（:$(env_val OTEL_HEALTH_PORT 13134)，追踪/指标接入可能暂不可用）"
    fi

    # 5) 微服务健康检查（端口仅绑 127.0.0.1）
    local entry svc_name svc_rest svc_var svc_port svc_path
    for entry in \
        auth-svc:AUTH_SVC_HTTP_PORT:8100:/health \
        device-svc:DEVICE_SVC_HTTP_PORT:8101:/health \
        task-svc:TASK_SVC_HTTP_PORT:8102:/health \
        alert-svc:ALERT_SVC_HTTP_PORT:8103:/health \
        config-svc:CONFIG_SVC_HTTP_PORT:8106:/health \
        log-svc:LOG_SVC_HTTP_PORT:8105:/healthz \
        gpu-svc:GPU_SVC_HTTP_PORT:8107:/health \
        aio-svc:AIO_SVC_HTTP_PORT:8108:/health \
        portal-svc:PORTAL_SVC_HTTP_PORT:8109:/health
    do
        svc_name="${entry%%:*}"
        svc_rest="${entry#*:}"
        svc_var="${svc_rest%%:*}"
        svc_rest="${svc_rest#*:}"
        svc_port="$(env_val "$svc_var" "${svc_rest%%:*}")"
        svc_path="${svc_rest#*:}"
        if http_ok "http://127.0.0.1:${svc_port}${svc_path}"; then
            log_ok "${svc_name} 正常（:${svc_port}${svc_path}）"
        else
            log_warn "${svc_name} 未响应（:${svc_port}${svc_path}）"
        fi
    done

    # 6) gRPC TLS：docker healthcheck 只探 HTTP，gRPC 握手失败在这里才会暴露
    local grpc_port
    grpc_port="$(env_val CONTROLPLANE_GRPC_PORT 9090)"
    if command -v openssl &>/dev/null; then
        log_info "控制面 gRPC TLS 握手（127.0.0.1:${grpc_port}）..."
        if openssl s_client -connect "127.0.0.1:${grpc_port}" -brief </dev/null 2>&1 | grep -qE "CONNECTION ESTABLISHED|Protocol version"; then
            log_ok "gRPC 端口 TLS 握手正常"
        else
            log_warn "gRPC 端口 TLS 握手异常——agent 将无法注册，请检查证书挂载"
        fi
    fi

    if [ "$failures" -gt 0 ]; then
        log_error "${failures} 项冒烟测试失败"
        return 1
    fi
    log_ok "冒烟测试通过"
}

# ============================================================
# 部署信息
# ============================================================
print_access_info() {
    log_section "部署完成"

    local adv scheme rest
    adv="$(env_val OPSMESH_ADVERTISE_ADDR)"
    scheme="${adv%%://*}"; rest="${adv#*://}"; rest="${rest%%/*}"

    echo -e "${BOLD}访问入口${NC}"
    if [ "$PROXY" = true ]; then
        echo -e "  控制面（经网关）:  ${GREEN}${adv}${NC}"
        echo -e "  HTTP 跳转端口:     $(env_val GATEWAY_HTTP_PORT 80) → HTTPS $(env_val GATEWAY_HTTPS_PORT 443)"
    else
        echo -e "  控制面 Web/REST:   ${GREEN}${adv}${NC}"
    fi
    echo -e "  控制面 gRPC:       ${scheme}://${rest%%:*}:$(env_val CONTROLPLANE_GRPC_PORT 9090)  （agent 注册通道，TLS）"
    echo -e "  门户 UI:           http://127.0.0.1:$(env_val PORTAL_SVC_HTTP_PORT 8109)"
    echo ""
    echo -e "${BOLD}运维入口（仅绑 127.0.0.1，远端经 SSH 隧道）${NC}"
    echo -e "  Grafana:     http://127.0.0.1:$(env_val GRAFANA_PORT 3000)"
    echo -e "  Prometheus:  http://127.0.0.1:$(env_val PROMETHEUS_PORT 9092)"
    echo -e "  Loki:        http://127.0.0.1:$(env_val LOKI_PORT 3100)"
    echo -e "  例：ssh -L 3000:127.0.0.1:3000 <本机> 后浏览器访问 http://localhost:3000"
    echo ""
    echo -e "${BOLD}凭据${NC}"
    echo -e "  admin 初始口令:  grep '^ADMIN_PASSWORD=' ${ENV_FILE}   （首登强制改密）"
    echo -e "  Grafana:         grep '^GRAFANA_ADMIN_PASSWORD=' ${ENV_FILE}"
    echo -e "  MySQL/Redis:     同上，见 ${ENV_FILE}（应仅部署用户可读）"
    echo ""
    echo -e "${BOLD}安全提醒${NC}"
    echo -e "  1. 控制面 8080/9090 默认对全网卡发布：请用安全组/防火墙限制来源（仅放行运维网段与 agent 网段）。"
    echo -e "  2. 自签证书浏览器会告警；对外生产建议替换为受信 CA 证书（覆盖 $(env_val OPSMESH_TLS_DIR ./certs)/tls.crt|tls.key 后重启 controlplane）。"
    echo -e "  3. agent 首次纳管需信任该证书：--client-ca=$(env_val OPSMESH_TLS_DIR ./certs)/ca.crt"
    echo ""
    echo -e "${BOLD}常用命令${NC}"
    echo -e "  状态:  ./scripts/status.sh"
    echo -e "  日志:  ./scripts/logs.sh [service]"
    echo -e "  停止:  ./scripts/deploy.sh down        停止并移除容器（保留数据卷）"
    echo -e "         ./scripts/deploy.sh down --volumes   连数据卷一起删除（不可恢复）"
    echo ""
}

# ============================================================
# 子命令
# ============================================================
do_up() {
    preflight_checks
    build_images
    start_infrastructure
    start_observability
    start_services

    if ! run_smoke_tests; then
        log_error "部署未通过冒烟测试。排查：./scripts/logs.sh <service>"
        exit 1
    fi

    print_access_info
}

do_down() {
    log_section "停止 OpsMesh"
    if [ ! -f "$ENV_FILE" ]; then
        log_warn ".env 不存在，尝试按默认配置停止"
    fi
    if [ "$REMOVE_VOLUMES" = true ]; then
        log_warn "--volumes：将删除全部数据卷（MySQL/Redis/Prometheus/Grafana/Loki 数据不可恢复）"
        confirm "确认删除数据卷？" || exit 1
        compose down --volumes --remove-orphans
        log_ok "已停止（数据卷已删除）"
        log_warn "下次 up 会重新初始化 MySQL 并应用 .env 中的口令（旧数据已不可恢复）。"
    else
        compose down --remove-orphans
        log_ok "已停止（数据卷保留）"
    fi
}

do_smoke() {
    setup_smoke_env
    run_smoke_tests
}

do_config() {
    log_section "Compose 渲染校验"
    check_env_vars
    check_tls_certs
    check_proxy_consistency
    check_compose_render
    echo ""
    compose config 2>/dev/null | head -40 || true
}

usage() {
    cat <<'EOF'
OpsMesh 生产部署脚本

用法:
  deploy.sh [子命令] [参数]

子命令:
  init                生成 .env（随机口令/密钥）+ 自签 TLS 证书（幂等，不覆盖已有 .env）
  up                  预检 → 构建镜像 → 启动基础设施/可观测/服务 → 冒烟测试（默认子命令）
  down                停止并移除容器（保留数据卷）
  smoke               只跑冒烟测试（需服务已在运行）
  config              只做 .env 校验 + docker compose 渲染校验（打印前 40 行）
  help                显示本帮助

参数:
  --proxy             叠加 docker-compose.prod-proxy.yml（Nginx 终止 TLS，控制面转明文）
  -y, --yes           非交互模式：端口占用等确认一律视为同意（禁止在无人值守脚本里裸用）
  --no-build          up 时跳过镜像构建，直接使用已有镜像
  --force             init 时覆盖已有 .env / 证书（会覆盖密钥）
  --volumes, -v       down 时连数据卷一起删除（不可恢复）
  -h, --help          显示本帮助

示例:
  ./scripts/deploy.sh init && ./scripts/deploy.sh up
  ./scripts/deploy.sh up --proxy -y
  ./scripts/deploy.sh down --volumes
EOF
}

# ============================================================
# Main
# ============================================================
main() {
    local cmd="${1:-up}"
    [ $# -gt 0 ] && shift

    REMOVE_VOLUMES=false

    while [ $# -gt 0 ]; do
        case "$1" in
            --proxy)      PROXY=true ;;
            -y|--yes)     ASSUME_YES=true ;;
            --no-build)   NO_BUILD=true ;;
            --force)      FORCE=true ;;
            --volumes|-v) REMOVE_VOLUMES=true ;;
            -h|--help)    usage; exit 0 ;;
            *)            log_error "未知参数: $1"; usage >&2; exit 2 ;;
        esac
        shift
    done

    case "$cmd" in
        -h|--help|help) usage; exit 0 ;;
        init)   cd "$PROJECT_DIR"; do_init ;;
        up)     cd "$PROJECT_DIR"; do_up ;;
        down)   cd "$PROJECT_DIR"; do_down ;;
        smoke)  cd "$PROJECT_DIR"; do_smoke ;;
        config) cd "$PROJECT_DIR"; do_config ;;
        *)      log_error "未知子命令: $cmd"; usage >&2; exit 2 ;;
    esac
}

if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    main "$@"
fi
