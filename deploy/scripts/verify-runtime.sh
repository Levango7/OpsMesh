#!/usr/bin/env bash
# OpsMesh 运行时黑盒断言（deploy.sh up 之后跑，只读、不改动被测系统）
#
# 用途：部署完成后的独立复验。与 deploy.sh 自带冒烟测试相互独立（不复用其函数/变量），
#       覆盖 P0-1 鉴权链路、P0-2 明文 HTTP、P0-3 企业版前端内置、P0-4 端口发布真实性、
#       P0-5 迁移版本门禁、P0-6 租户列落库、P0-7 持久化落库、监控假告警、多库隔离，
#       以及 P1-5 /metrics 准入·基数熔断·限流等回归断言。任一 FAIL 即退出码非 0。
#
# 前置：栈已由 `deploy/docker/scripts/deploy.sh up` 拉起。
# 用法：bash deploy/scripts/verify-runtime.sh
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DOCKER_DIR="${ROOT}/deploy/docker"
cd "$DOCKER_DIR" || exit 1

K=(-k -sS --max-time 10)
PASS=0; FAIL=0
ok()   { echo -e "  [PASS] $*"; PASS=$((PASS+1)); }
bad()  { echo -e "  [FAIL] $*"; FAIL=$((FAIL+1)); }
warn() { echo -e "  [WARN] $*"; }
sec()  { echo -e "\n=== $* ==="; }

# env_val KEY [DEFAULT]：从 .env 取值，取不到时用 DEFAULT。
# 必须有 DEFAULT：本函数原先只接一个参数，而调用点普遍写成 `env_val X 8080`
# ——第二个参数被静默吞掉，于是 **.env 里没有这个键的服务拿到空端口**，
# 断言拿到空响应体后全部判红（2026-10-02 实测：三域转正新增的
# INCIDENT/RUNBOOK/AUTOSCALER_SVC_HTTP_PORT 不在老 .env 里，§5b/§6 共 15 条假红）。
env_val() {
    local v
    v="$(grep -E "^$1=" .env 2>/dev/null | head -1 | cut -d= -f2- | tr -d '\r')"
    if [ -n "$v" ]; then printf '%s' "$v"; else printf '%s' "${2:-}"; fi
}
PW="$(env_val ADMIN_PASSWORD)"
CP="https://127.0.0.1:$(env_val CONTROLPLANE_HTTP_PORT 8080)"

sec "1. 容器健康矩阵"
docker compose --env-file .env -f docker-compose.prod.yml ps --format '{{.Service}}\t{{.Status}}' 2>/dev/null | sort

sec "1b. 宿主端口发布真实性（internal 网络静默丢弃缺陷的回归断言）"
missing=""
for c in $(docker ps --filter name=opsmesh --format '{{.Names}}'); do
  pb="$(docker inspect "$c" --format '{{len .HostConfig.PortBindings}}' 2>/dev/null)"
  ns="$(docker inspect "$c" --format '{{json .NetworkSettings.Ports}}' 2>/dev/null)"
  if [ -n "$pb" ] && [ "$pb" != "0" ] && ! grep -q 'HostPort' <<<"$ns"; then
    missing="$missing ${c#opsmesh-}"
  fi
done
if [ -z "$missing" ]; then
  ok "所有声明端口的容器均已真实发布到宿主（无 internal 网络静默丢弃）"
else
  bad "以下容器声明了宿主端口但未生效：${missing}"
fi

sec "2. 控制面对外入口（TLS + 健康）"
code="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' "$CP/healthz" 2>/dev/null)"
[ "$code" = "200" ] && ok "GET $CP/healthz → 200" || bad "GET $CP/healthz → ${code:-无响应}"

# 明文 HTTP 打到 TLS 端口必须拿不到业务响应（P0-2 回归断言）。
# 合法结果：000（连接被拒/重置）或 400（Go TLS 服务器对明文的固定回应
# "Client sent an HTTP request to an HTTPS server."）——400 恰恰证明明文未被服务。
# 只有 1xx/2xx/3xx 才算回归（说明明文真的被处理了）。
pcode="$(curl -sS --max-time 5 -o /dev/null -w '%{http_code}' "http://127.0.0.1:$(env_val CONTROLPLANE_HTTP_PORT 8080)/healthz" 2>/dev/null)"
case "$pcode" in
  000|"") ok "明文 HTTP 无法访问 TLS 端口（连接层拒绝，P0-2 生效）" ;;
  400)    ok "明文 HTTP 被 TLS 层拒绝（HTTP 400，未进入业务处理，P0-2 生效）" ;;
  *)      bad "明文 HTTP 竟返回 ${pcode}（P0-2 回归！）" ;;
esac

sec "3. metrics 端点准入 + 指标基数熔断（P1-5 回归）"
MP="$(env_val CONTROLPLANE_METRICS_PORT 9091)"
METRICS_CIDR="$(env_val METRICS_ALLOW_CIDR)"
# .env 未显式设置时 compose 仍注入默认白名单（本机 + 三个 compose 网段）。
[ -z "$METRICS_CIDR" ] && METRICS_CIDR="127.0.0.0/8,172.28.0.0/16（compose 默认）"
echo "  METRICS_ALLOW_CIDR=${METRICS_CIDR}"

# metrics_snapshot：取一份**完整**的控制面 /metrics 文本。
#
# 为什么需要"完整性判据"而不是"非空判据"：抓取有两条路径（docker exec 进 prometheus 容器 wget，
# 回退到宿主 curl）。2026-10-04 为"某序列不存在"的间歇性判红在这里反复量过四轮，最终定位到
# 元凶最终查明是**判定写法本身**（`printf … | grep -q` 撞上 pipefail 的 SIGPIPE 假阴性，见下面的写法约定）。
# process_cpu_seconds_total 由进程采集器无条件产出且位于渲染**末尾**，尾部缺失即快照被截过。
metrics_snapshot() {
  local body=""
  for _i in 1 2 3 4 5; do
    body="$(docker exec opsmesh-prometheus wget -qO- --timeout=8 http://controlplane:9091/metrics 2>/dev/null)"
    if [ -z "$body" ]; then
      body="$(curl -sS --max-time 8 "http://127.0.0.1:${MP}/metrics" 2>/dev/null)"
    fi
    if grep -qE '^process_cpu_seconds_total ' <<<"$body"; then
      printf '%s' "$body"
      return 0
    fi
    sleep 1
  done
  printf '%s' "$body"
  return 1
}

# dump_snapshot <名字> <快照>：把"当次读到的那一份"落盘并打印路径。
# 间歇性"某序列不存在"只有拿着那份快照才判得清是渲染没产出、读取被截，还是判定写法有问题；
# 没有 artifact 时所有解释都只是猜测（本轮为此多跑了四轮脚本）。
dump_snapshot() {
  d="/tmp/opsmesh-verify-$1-$(date -u +%Y%m%dT%H%M%SZ).txt"
  printf '%s\n' "$2" > "$d"
  echo "         当次快照已存 ${d}（$(wc -l < "$d") 行）"
}

# 写法约定（本脚本、以及 deploy.sh / validate-deploy-assets.sh 都适用）：
# **判定已捕获的字符串时不许用管道**，一律 `grep -q PAT <<<"$var"`。
#
# 原因不是风格问题而是退出码。本脚本开头是 `set -uo pipefail`，而原先到处写着
# `printf '%s' "$var" | grep -q PAT`：grep -q 命中后**立刻退出**，printf 还在写就被 SIGPIPE 打死，
# 管道退出码变成 141；pipefail 把它升格成"整条管道失败" ⇒ **命中了却判成没命中**。
# 2026-10-04 用同一份 679 行 /metrics 快照实测（样式确实在第 27 行）：
#   带 pipefail ⇒ 连续 10 次全部 MISS；去掉 pipefail ⇒ 连续 10 次全部 HIT。
# 之所以此前"有时是绿的"：快照体积在 64KB 管道缓冲区附近来回，样式位置又各不相同，
# 于是同一台机器连跑两次能给出**不同的**"缺序列清单"——这种假红会把人一路引向"产品缺指标"，
# 本轮就为此多跑了四轮脚本才定位到判定写法。
# herestring 由 bash 落成临时文件供 grep 读取：没有管道、没有生产者可被 SIGPIPE 打死，
# 行首锚 `^` 的语义与原来逐字一致（刻意不改用 `[[ == *… ]]`：那是整串子串匹配，
# `^opsmesh_http_metrics_series` 这类锚会失效，而指标名互为前缀时就会假命中）。

# 3a 8080 公开端点：来源在白名单内 → 200；不在 → 403（fail-closed，属预期而非缺陷）。
mcode="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' "$CP/metrics" 2>/dev/null)"
case "$mcode" in
  200) ok "GET $CP/metrics → 200（本机来源在 CIDR 白名单内）" ;;
  403) warn "GET $CP/metrics → 403（本机来源不在 METRICS_ALLOW_CIDR 内；端点已按 P1-5 收敛，非回归）" ;;
  *)   bad "GET $CP/metrics → ${mcode:-无响应}（期望 200 或 403）" ;;
esac

# 3b 9091 独立 metrics 端口：Prometheus 抓取路径必须可达（monitoring 网段，走容器内抓取最稳）。
mbody="$(metrics_snapshot)" || bad "9091 /metrics 快照残缺（尾部哨兵缺失，重试 5 次仍不完整）——本节按不可信处理"
if [ -z "$mbody" ]; then
  bad "9091 /metrics 抓取为空（Prometheus → controlplane:9091 与宿主 127.0.0.1:${MP} 均失败）"
else
  ok "9091 /metrics 可抓取（$(printf '%s\n' "$mbody" | wc -l | tr -d ' ') 行，尾部哨兵完整）"
  sline="$(grep -E '^opsmesh_http_metrics_series [0-9]+$' <<<"$mbody" | head -1)"
  if [ -z "$sline" ]; then
    bad "缺少 opsmesh_http_metrics_series（P1-5 基数熔断自观测未生效）"
    dump_snapshot metrics-3b "$mbody"
  else
    nseries="${sline##* }"
    [ "$nseries" -le 2000 ] && ok "HTTP 指标时序数 ${nseries} ≤ 2000（基数硬上限生效）" \
                             || bad "HTTP 指标时序数 ${nseries} > 2000（基数上限失效！）"
  fi
  if grep -q '^opsmesh_http_metrics_series_dropped_total [0-9]' <<<"$mbody"; then
    ok "折叠计数器 opsmesh_http_metrics_series_dropped_total 已暴露（超限请求可观测）"
  else
    bad "缺少 opsmesh_http_metrics_series_dropped_total（超限请求不可观测）"
    dump_snapshot metrics-3b "$mbody"
  fi
fi

# 3c 路径归一化（基数护栏第一层）：超长/危险字符段与超长整体路径必须折叠，绝不原样入标签。
probe="$(printf 'zzprobe-%058d' 0)"      # 66 字节 > 48 上限，且含独特前缀
longpath="$(printf '/%0220d' 0)"         # 221 字节 > 200 上限
probe_code="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' --max-time 8 "${CP}/api/v1/${probe}" 2>/dev/null)"
long_code="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' --max-time 8 "${CP}${longpath}" 2>/dev/null)"
echo "  探针返回：超长段=${probe_code:-无响应} 超长整路径=${long_code:-无响应}（折叠标签只在请求真进入埋点链时才会出现）"
# 最多重试 5 次（每次隔 1s）等两个折叠标签出现：/metrics 的应用级计数带 TTL 缓存
# （--metrics-cache-ttl 默认 1s，P1-6），探针刚发完就抓有概率读到旧快照。
# 但**本轮真正的元凶不是缓存**——是 `printf | grep -q` 在 pipefail 下的 SIGPIPE 假阴性，
# 见文件头"写法约定"；改成 herestring 后这条断言从"偶发假红"变成稳定判定。
mbody2=""
for _try in 1 2 3 4 5; do
  mbody2="$(metrics_snapshot)" || true
  if grep -qF 'path="/api/v1/:id"' <<<"$mbody2" && grep -qF 'path="/:overlong"' <<<"$mbody2"; then
    break
  fi
  sleep 1
done
if [ -z "$mbody2" ]; then
  bad "3c 拿不到任何 /metrics 快照，无法判定路径归一化（按未验证判红）"
else
  if grep -qF 'path="/api/v1/:id"' <<<"$mbody2"; then
    ok "超长路径段归一化为 path=\"/api/v1/:id\"（探针 HTTP=${probe_code:-?}）"
  else
    bad "未观察到 path=\"/api/v1/:id\"（探针 HTTP=${probe_code:-无响应}）——超长段未被归一化，或请求没进入埋点链"
    dump_snapshot metrics-3c "$mbody2"
  fi
  if grep -qF "$probe" <<<"$mbody2"; then
    bad "原始超长路径串泄漏进指标标签（基数护栏失效！）"
    dump_snapshot metrics-3c-leak "$mbody2"
  else
    ok "原始超长路径未进入指标标签（无标签膨胀）"
  fi
  if grep -qF 'path="/:overlong"' <<<"$mbody2"; then
    ok "超长整体路径归一化为 path=\"/:overlong\""
  else
    # 刻意 warn：整路径超过 200 字节时上游路由可能先返回 404 而不进埋点，
    # 探针状态码就是区分"折叠没生效"与"请求没到埋点"的那一维。
    warn "未观察到 path=\"/:overlong\"（探针 HTTP=${long_code:-无响应}；上游先行拒绝则该折叠标签本就不会产生）"
  fi
fi

# 3d 生产默认限流（P1-5）：.env 未显式设置时须以 200 req/s/IP 启动（此前默认关闭）。
rl="$(env_val CB_RATE_LIMIT_PER_SEC)"
clog="$(docker logs opsmesh-controlplane 2>&1)"
if [ -z "$rl" ]; then
  grep -q '生产模式默认启用 API 限流 200' <<<"$clog" \
    && ok "生产模式默认启用限流 200 req/s/IP（启动期提示可见）" \
    || bad "未见默认限流启动提示（P1-5 默认启用失效）"
  grep -q 'API 限流已启用' <<<"$clog" \
    && ok "限流器已装载（日志含「API 限流已启用」）" \
    || bad "限流器未装载（生产默认限流未生效）"
elif [ "$rl" = "0" ]; then
  warn "CB_RATE_LIMIT_PER_SEC=0：限流被显式关闭（下方 429 实测将跳过）"
  grep -q '生产模式未启用 API 限流' <<<"$clog" \
    && ok "显式关闭限流时打印告警（可审计）" \
    || warn "显式关闭限流但未见启动告警"
else
  warn "CB_RATE_LIMIT_PER_SEC=${rl}：自定义限流阈值，按实际值实测"
fi

# 3e 限流实测：突发请求须出现 429（显式关闭时跳过，避免误报）。
# 用单条 curl 复用同一 keep-alive 连接连打 N 次——若用 `xargs -P` 逐请求起进程，
# Windows 上进程创建开销会把实际速率压到 ~200 req/s 附近（实测 1500 请求无一 429 的假阴性）。
if [ "$rl" = "0" ]; then
  warn "限流已显式关闭，跳过 429 突发实测"
else
  urls=()
  for _ in $(seq 1 800); do urls+=("$CP/api/v1/devices"); done
  codes="$(curl "${K[@]}" -o /dev/null -w '%{http_code}\n' "${urls[@]}" 2>/dev/null | grep -E '^[0-9]{3}$')"
  n429="$(printf '%s\n' "$codes" | grep -c '^429$' | tr -d ' ')"
  nok="$(printf '%s\n' "$codes" | grep -cE '^(401|403|200)$' | tr -d ' ')"
  if [ "$n429" -gt 0 ]; then
    ok "突发限流实测：800 请求中 429=${n429}，非 429=${nok}（令牌桶生效）"
  else
    bad "800 突发请求无一 429（限流未生效，P1-5 回归）"
  fi
  sleep 2  # 令牌回填（200/s、桶容量 200），避免影响后续断言
fi

# 3f 负向验证（fail-closed 实测）：另起一次性探针容器，白名单设成【不含实际来源】的网段，必须 403。
# 为什么不直接对被测栈做负向验证：Docker Desktop(WSL2) 的端口转发不保留真实来源 IP——
# 实测宿主 curl / 宿主经局域网 IP / 默认桥容器经 host.docker.internal，容器侧 remote 恒为
# 172.28.1.1（frontend 网桥网关），落在被测栈白名单内；来源区分能力只在裸机/K8s 部署下成立。
PROBE_PORT=29191
CP_IMG="$(docker inspect opsmesh-controlplane --format '{{.Config.Image}}' 2>/dev/null)"
if [ -z "$CP_IMG" ]; then
  warn "未取得控制面镜像名，跳过 fail-closed 负向探针"
elif curl -sS --max-time 2 -o /dev/null "http://127.0.0.1:${PROBE_PORT}/metrics" 2>/dev/null; then
  warn "宿主端口 ${PROBE_PORT} 已被占用，跳过 fail-closed 负向探针"
else
  docker rm -f opsmesh-cidr-probe >/dev/null 2>&1
  if docker run -d --rm --name opsmesh-cidr-probe --network opsmesh-frontend \
       -p "127.0.0.1:${PROBE_PORT}:9091" "$CP_IMG" --mode=controlplane --store=memory \
       --http-port=18080 --grpc-port=19090 --metrics-port=9091 \
       --metrics-allow-cidr=127.0.0.1/32 >/dev/null 2>&1; then
    pcode=""
    for _ in $(seq 1 15); do
      sleep 1
      pcode="$(curl -sS --max-time 3 -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PROBE_PORT}/metrics" 2>/dev/null)"
      [ -n "$pcode" ] && [ "$pcode" != "000" ] && break
    done
    plog="$(docker logs opsmesh-cidr-probe 2>&1 | grep 'metrics 访问被拒' | tail -1)"
    docker rm -f opsmesh-cidr-probe >/dev/null 2>&1
    case "$pcode" in
      403) ok "非白名单来源被拒（403 fail-closed 实测；探针白名单=127.0.0.1/32，实际来源 172.28.1.1）" ;;
      000|"") warn "fail-closed 探针未就绪（启动超时），负向验证未执行" ;;
      *)   bad "白名单不含实际来源时仍返回 ${pcode}（fail-closed 失效！）" ;;
    esac
    if [ -n "$plog" ]; then
      echo "    探针拒绝日志: $(printf '%s' "$plog" | sed -E 's/.*"msg":"([^"]*)".*"remote":"([^"]*)".*/msg=\1 remote=\2/')"
    fi
  else
    warn "fail-closed 探针容器启动失败（镜像/网络不可用），跳过负向验证"
  fi
fi

sec "4. 鉴权链路（P0-1 回归）"
# 4a 错误口令必须被拒
wcode="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' -X POST "$CP/api/v1/auth/login" \
  -H 'Content-Type: application/json' -d '{"username":"admin","password":"WrongPass123"}' 2>/dev/null)"
[ "$wcode" = "401" ] && ok "错误口令被拒（401）" || bad "错误口令返回 ${wcode}（期望 401）"

# 4b 预置弱口令 admin123 必须被拒
w2="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' -X POST "$CP/api/v1/auth/login" \
  -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin123"}' 2>/dev/null)"
[ "$w2" = "401" ] && ok "预置弱口令 admin123 被拒（401）" || bad "预置弱口令 admin123 返回 ${w2}（期望 401，P0-1 回归！）"

# 4c 正确口令登录。判据必须按「这个账号当前该不该改密」来定，而不是假设每次都是首登：
#    原判据写死 mustChangePassword=true，在存量库上必然假红（2026-10-02 升级实测），
#    而反过来若在首登态放行会话 token 又是在削弱 P0-1。所以取**独立观测量**——
#    数据库里的 must_change_password 标记——再断言 API 行为与它一致，两个方向都是硬判定。
flag="$(docker exec opsmesh-mysql sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -N -e "SELECT must_change_password FROM opsmesh.users WHERE username=\"admin\""' 2>/dev/null | tr -d '\r' | tail -1)"
resp="$(curl "${K[@]}" -X POST "$CP/api/v1/auth/login" \
  -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$PW\"}" 2>/dev/null)"
echo "  登录响应（脱敏，token 截断）: $(echo "$resp" | sed -E 's/("(token|changePasswordToken|accessToken|refreshToken)"[[:space:]]*:[[:space:]]*")[^"]*/\1<...>/g')"
forced="no"
grep -q '"mustChangePassword"[[:space:]]*:[[:space:]]*true' <<<"$resp" && forced="yes"
hasToken="no"
grep -qE '"(token|accessToken|refreshToken)"[[:space:]]*:[[:space:]]*"[^"]+"' <<<"$resp" && hasToken="yes"

case "$flag" in
  1)
    # 库里要求改密（预置口令仍在用 / 首次交付）：必须强制改密、必须发一次性令牌、不得发会话 token
    [ "$forced" = "yes" ] && ok "库内 must_change_password=1 ⇒ API 返回强制改密标记" \
                           || bad "库内要求改密（must_change_password=1）但 API 未标记强制改密（P0-1 回归）"
    grep -q '"changePasswordToken"' <<<"$resp" \
        && ok "下发一次性 changePasswordToken" \
        || bad "未下发 changePasswordToken（无法完成强制改密，等于锁死账号）"
    [ "$hasToken" = "no" ] && ok "强制改密期间未下发可用会话 token" \
                           || bad "强制改密期间仍下发了会话 token（改密闸失效）"
    ;;
  0)
    # 库里已不需要改密（口令被改过）：不得再卡改密流程，且必须能拿到正常会话 token
    [ "$forced" = "no" ] && ok "库内 must_change_password=0 ⇒ 不再要求改密（标记与真实口令一致）" \
                          || bad "库内不要求改密却返回 mustChangePassword=true（seedRBAC 标记未随口令状态收敛，重启即锁死管理员）"
    grep -q '"changePasswordToken"' <<<"$resp" \
        && bad "不需要改密却下发 changePasswordToken" \
        || ok "不需要改密时不下发一次性改密令牌（语义一致）"
    [ "$hasToken" = "yes" ] && ok "已改过口令的账号能拿到会话 token（不被误锁在改密流程）" \
                            || bad "口令已改过却拿不到会话 token（登录卡在改密流程，P0-1 的反向缺陷）"
    ;;
  *)
    warn "读不到 opsmesh.users.must_change_password（MySQL 容器不可用？），跳过 4c 的一致性判定"
    ;;
esac

sec "5. 微服务 /metrics 真实性核对"
# 预期值 = 源码中是否注册了 mux.Handle("/metrics", metrics.GetHandler())。
# 本节只覆盖端口连通性（9 个）；三域转正后暴露 /metrics 的服务已达 12 个，
# 全 12 个的**内容**断言在下面的 5b 节做（那里连不上正文即判红，故不缺口的不是本节）。
check_metrics() {
  local name="$1" port="$2" expect="$3"
  local c; c="$(curl -sS --max-time 6 -o /dev/null -w '%{http_code}' "http://127.0.0.1:${port}/metrics" 2>/dev/null)"
  if [ "$expect" = "yes" ]; then
    [ "$c" = "200" ] && ok "${name} :${port}/metrics → 200（源码声明暴露）" || bad "${name} :${port}/metrics → ${c}（源码声明应暴露）"
  else
    [ "$c" = "404" ] && ok "${name} :${port}/metrics → 404（源码声明未暴露，符合预期）" || warn "${name} :${port}/metrics → ${c}（预期 404，请复核）"
  fi
}
check_metrics device-svc "$(env_val DEVICE_SVC_HTTP_PORT 8101)" yes
check_metrics task-svc   "$(env_val TASK_SVC_HTTP_PORT 8102)"   yes
check_metrics alert-svc  "$(env_val ALERT_SVC_HTTP_PORT 8103)"  yes
check_metrics auth-svc   "$(env_val AUTH_SVC_HTTP_PORT 8100)"   yes
check_metrics config-svc "$(env_val CONFIG_SVC_HTTP_PORT 8106)" yes
check_metrics log-svc    "$(env_val LOG_SVC_HTTP_PORT 8105)"    yes
check_metrics gpu-svc    "$(env_val GPU_SVC_HTTP_PORT 8107)"    yes
check_metrics aio-svc    "$(env_val AIO_SVC_HTTP_PORT 8108)"    yes
check_metrics portal-svc "$(env_val PORTAL_SVC_HTTP_PORT 8109)" yes

sec "5b. 微服务指标语义（SET 冒充 counter / 恒零仪表 / 基数熔断 的黑盒回归）"
# 断言对象 = pkg/metrics 的 exposition：12 个服务的 /metrics 全部走它（gpu-svc 用的是同一个包，
# 只是 import 别名不同；它自己的 internal/metrics 是领域采集器，不是 /metrics 处理器）。
#
# 硬断言只放**启动后必然成立**的事实；事件驱动的序列一律放软断言。
# 反过来若为了怕假红而全部放软，就又退回"有指标无验证"——所以分档，而不是一律放宽。
SVC_PORTS="auth-svc:8100 device-svc:8101 task-svc:8102 alert-svc:8103 incident-svc:8104 \
log-svc:8105 config-svc:8106 gpu-svc:8107 aio-svc:8108 portal-svc:8109 runbook-svc:8110 \
autoscaler-svc:8111"
# svc_port 名字→宿主端口：服务目录名 task-svc 经大小写/下划线转换已是 TASK_SVC，
# 所以键名是 TASK_SVC_HTTP_PORT（早先写成拼 "_SVC_HTTP_PORT" 会变成 TASK_SVC_SVC_HTTP_PORT，
# 查不到 ⇒ 端口为空 ⇒ 12 个服务的指标断言全读空正文、全判红）。
svc_port() { local e; e="$(printf '%s' "$1" | tr 'a-z-' 'A-Z_')"; env_val "${e}_HTTP_PORT" "$2"; }

mbad=0
for sp in $SVC_PORTS; do
  svc="${sp%%:*}"; dflt="${sp##*:}"; port="$(svc_port "$svc" "$dflt")"
  body="$(curl -sS --max-time 8 "http://127.0.0.1:${port}/metrics" 2>/dev/null)"
  if [ -z "$body" ]; then bad "${svc} :${port}/metrics 取不到正文（进程未响应，指标语义无从判定）"; mbad=$((mbad+1)); continue; fi
  probs=""
  grep -q '^# TYPE business_metrics gauge$'                     <<<"$body" || probs="${probs} business_metrics 的 TYPE 不是 gauge"
  grep -q '^# TYPE business_metrics_total counter$'             <<<"$body" || probs="${probs} business_metrics_total 的 TYPE 不是 counter"
  grep -q '^# TYPE active_connections gauge$'                   <<<"$body" || probs="${probs} active_connections 家族缺失"
  grep -qE "^service_info\{service=\"${svc}\"\} 1$"          <<<"$body"   || probs="${probs} service_info 未带 service=\"${svc}\"（Init 的参数没落到 exposition）"
  for f in http_metrics_series http_metrics_series_dropped_total business_metrics_series business_metrics_series_dropped_total; do
    grep -qE "^${f} [0-9]" <<<"$body" || probs="${probs} 缺 ${f}（基数熔断不可观测）"
  done
  # 反向断言：这两类"复生"就是本轮修掉的缺陷回来了
  if g="$(grep -oE '^business_metrics\{name="[^"]*_total"' <<<"$body" | head -1)"; [ -n "$g" ]; then
      probs="${probs} gauge 家族里出现 *_total 名（${g}）＝SET 冒充 counter 的旧缺陷复生"
  fi
  grep -qE '^queue_depth([ {]|$)' <<<"$body" && probs="${probs} queue_depth 又出现了（该序列因恒零已删除）"

  if [ -z "$probs" ]; then ok "${svc} :${port} 指标语义正确（家族/类型/熔断可观测/无恒零仪表）"
  else bad "${svc} :${port} 指标语义不符:${probs}"; mbad=$((mbad+1)); fi
done

# 软断言：只在事件发生后才会出现的序列，存在就记 PASS，不存在只 WARN。
# 不断言之所以合理：它们要么等调度 tick、要么等真实丢弃/失败发生，缺席不代表没接线。
soft() { local svc="$1" port="$2" pat="$3" why="$4" mtext=""
  # 先落变量再判：`curl … | grep -q` 在 pipefail 下会因为 grep 早退、curl 被 SIGPIPE 打死而
  # **假阴性**（微服务 /metrics 有几十 KB，越过管道缓冲就会踩到），见文件头的写法约定。
  mtext="$(curl -sS --max-time 8 "http://127.0.0.1:${port}/metrics" 2>/dev/null)"
  if grep -qE "$pat" <<<"$mtext"; then
      ok "${svc} 已产出业务序列 ${pat}（事件驱动，本轮真发生了）"
  else
      warn "${svc} 暂无 ${pat} —— ${why}（缺席不等于未接线，故不判红）"
  fi; }
soft task-svc   "$(svc_port task-svc 8102)"     'business_metrics_total\{name="task_reclaimed"'      '调度器 reclaim tick 未到（约 30s 一次）'
soft task-svc   "$(svc_port task-svc 8102)"     'business_metrics_total\{name="task_scheduled_fired"' '调度器 fire tick 未到'
soft device-svc "$(svc_port device-svc 8101)"   'business_metrics\{name="device_total"'             'RecordDeviceMetrics 由请求/自动纳管触发，尚无该路径'
soft log-svc    "$(svc_port log-svc 8105)"    'business_metrics_total\{name="log_memory_dropped"' '内存环形缓冲尚未发生淘汰（未写满 cap）'
soft autoscaler-svc "$(svc_port autoscaler-svc 8111)" 'business_metrics\{name="autoscaler_decision_history_entries"' '尚无任何扩缩容决策（RPC 触发型）'

# 熔断不可绕过：路径归一化必须真把数字 ID 并掉，否则上限形同虚设。
tp="$(svc_port task-svc 8102)"
for junk in 1 99999; do
  curl -sS --max-time 5 -o /dev/null "http://127.0.0.1:${tp}/api/v1/tasks/${junk}" 2>/dev/null
done
nseq="$(curl -sS --max-time 8 "http://127.0.0.1:${tp}/metrics" 2>/dev/null | grep -cE '^http_requests_total\{.*path="/api/v1/tasks/[0-9]+"' || true)"
if [ "${nseq:-0}" = "0" ]; then
  ok "task-svc 数字 ID 路径未各自成序列（NormalizePath 生效，上限不被同路由的实例撑爆）"
else
  bad "task-svc 有 ${nseq} 条 /api/v1/tasks/<数字> 原始路径序列 ⇒ NormalizePath 未生效，基数熔断可被绕过"
fi

sec "6. 微服务健康检查（12 个，与出厂 compose 栈的服务清单一致）"
# 12 个而不是 9 个：incident/runbook/autoscaler 三域已于 v0.10.0 进栈，此前这里漏列，
# 症状是「部署自检全绿但从没探过那三个容器」——它们的健康路径还与其它服务不同（/api/v1/health），
# 直接照抄 /health 会得到假的失败，所以逐个按 compose 的 healthcheck 写。
for e in "auth-svc:$(env_val AUTH_SVC_HTTP_PORT 8100):/health" \
         "device-svc:$(env_val DEVICE_SVC_HTTP_PORT 8101):/health" \
         "task-svc:$(env_val TASK_SVC_HTTP_PORT 8102):/health" \
         "alert-svc:$(env_val ALERT_SVC_HTTP_PORT 8103):/health" \
         "incident-svc:$(env_val INCIDENT_SVC_HTTP_PORT 8104):/api/v1/health" \
         "log-svc:$(env_val LOG_SVC_HTTP_PORT 8105):/healthz" \
         "config-svc:$(env_val CONFIG_SVC_HTTP_PORT 8106):/health" \
         "gpu-svc:$(env_val GPU_SVC_HTTP_PORT 8107):/health" \
         "aio-svc:$(env_val AIO_SVC_HTTP_PORT 8108):/health" \
         "portal-svc:$(env_val PORTAL_SVC_HTTP_PORT 8109):/health" \
         "runbook-svc:$(env_val RUNBOOK_SVC_HTTP_PORT 8110):/api/v1/health" \
         "autoscaler-svc:$(env_val AUTOSCALER_SVC_HTTP_PORT 8111):/api/v1/health"; do
  n="${e%%:*}"; r="${e#*:}"; p="${r%%:*}"; path="${r#*:}"
  c="$(curl -sS --max-time 6 -o /dev/null -w '%{http_code}' "http://127.0.0.1:${p}${path}" 2>/dev/null)"
  [ "$c" = "200" ] && ok "${n} :${p}${path} → 200" || bad "${n} :${p}${path} → ${c:-无响应}"
done

sec "7. Prometheus 采集目标真实性"
PT="http://127.0.0.1:$(env_val PROMETHEUS_PORT 9092)"
tjson="$(curl -sS --max-time 10 "$PT/api/v1/targets" 2>/dev/null)"
if [ -n "$tjson" ]; then
  if command -v python >/dev/null 2>&1; then
    echo "$tjson" | python -c "
import json,sys
try:
    d=json.load(sys.stdin)
except Exception as e:
    print('  [FAIL] targets JSON 解析失败:',e); sys.exit(1)
ts=d.get('data',{}).get('activeTargets',[])
print(f'  活动 target 数：{len(ts)}')
bad=0
for t in sorted(ts,key=lambda x:x['scrapeUrl']):
    h=t['health']; print(f\"    {h:8s} {t['scrapeUrl']}\")
    if h!='up': bad+=1
print(f'  [{'PASS' if bad==0 else 'FAIL'}] UP={len(ts)-bad} DOWN={bad}')
" 2>/dev/null || warn "python 解析 targets 失败"
  else
    echo "$tjson" | head -c 500
  fi
  # 上面只看 DOWN，但**漏配一个 job 时该 target 根本不存在**，不会以下降形式暴露——
  # 这就是 incident/runbook/autoscaler 三域进栈后指标长期无人抓却没被发现的原因。
  # 所以把出厂栈的目标清单写成显式断言，缺一即红。
  MISS_JOBS=""
  for j in opsmesh-controlplane auth-svc device-svc task-svc alert-svc incident-svc \
           log-svc config-svc gpu-svc aio-svc portal-svc runbook-svc autoscaler-svc; do
    # 容忍 `"job":"x"` 与 `"job": "x"` 两种 JSON 排版：Prometheus 自己出的是紧凑形，
    # 但经代理/pretty-print 之后带空格，判据不能靠运气。
    if ! grep -qE "\"job\"[[:space:]]*:[[:space:]]*\"${j}\"" <<<"$tjson"; then
      MISS_JOBS="${MISS_JOBS} ${j}"
    fi
  done
  if [ -n "${MISS_JOBS// }" ]; then
    bad "Prometheus activeTargets 缺少出厂栈目标：${MISS_JOBS}（漏配 job 不显示为 DOWN，只会静默不采）"
  else
    ok "13 个 OpsMesh 目标均在 activeTargets 内（无静默漏采）"
  fi
else
  bad "Prometheus /api/v1/targets 无响应"
fi

sec "7b. Prometheus 告警状态（黑盒 probe + docker job 修复后应 0 firing）"
ajson="$(curl -sS --max-time 10 "$PT/api/v1/alerts" 2>/dev/null)"
if [ -n "$ajson" ]; then
  firing="$(printf '%s' "$ajson" | grep -o '"state":"firing"' | wc -l | tr -d ' ')"
  pending="$(printf '%s' "$ajson" | grep -o '"state":"pending"' | wc -l | tr -d ' ')"
  echo "  firing=${firing} pending=${pending}"
  if [ "$firing" = "0" ]; then
    ok "无 firing 告警（mysql/redis/docker 假告警已消除）"
  else
    bad "存在 ${firing} 条 firing 告警（疑似监控假告警回归）："
    printf '%s' "$ajson" | grep -o '"alertname":"[^"]*"' | sort -u | sed 's/^/    /'
  fi
else
  bad "Prometheus /api/v1/alerts 无响应"
fi

sec "7c. 出厂告警规则的运行时健康（health=err 的规则＝永不触发的假告警）"
# 为什么单列（2026-10-02 真机实测教训）：一条规则可以**语法合法、引用的序列真实存在、
# 服务也确实被抓取**，却在真实 Prometheus 里评估失败——本轮就抓到一条：
#   increase({__name__=~"http_metrics_series_dropped_total|business_metrics_series_dropped_total"}[30m])
# 报 "vector cannot contain metrics with the same labelset"（函数结果会丢掉 __name__，
# 而这两个名字在每个微服务上同时存在）。后果与其它静默同类：规则在，告警永远不会来。
# 静态检查拦不住它，只有真实评估过一次才是证据。
rjson="$(curl -sS --max-time 10 "$PT/api/v1/rules?type=alert" 2>/dev/null)"
PYBIN="$(command -v python3 || command -v python)"
if [ -z "$rjson" ]; then
  bad "Prometheus /api/v1/rules 无响应"
elif [ -z "$PYBIN" ]; then
  warn "没有 python，跳过规则运行时健康检查"
else
  printf '%s' "$rjson" | "$PYBIN" -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception as e:
    print("  rules JSON 解析失败:", e); sys.exit(1)
groups = d.get("data", {}).get("groups", [])
tot = 0
broken = []
states = {}
for g in groups:
    for r in g.get("rules", []):
        if r.get("type") != "alerting" and "alerts" not in r:
            continue          # 记录规则（recording rule）不参与本判定
        tot += 1
        h = r.get("health")
        err = r.get("lastError")
        if h != "ok" or err:
            broken.append((g.get("name"), r.get("name"), h, err))
        for a in (r.get("alerts") or []):
            states[a.get("state")] = states.get(a.get("state"), 0) + 1
print("  规则组=%d 告警规则=%d 状态分布=%s" % (len(groups), tot, states or "（全部 inactive）"))
if tot == 0:
    print("  没有取到任何告警规则——本节会空转"); sys.exit(1)
if broken:
    for b in broken:
        print("  规则评估异常：group=%s alert=%s health=%s lastError=%s" % b)
    sys.exit(1)
sys.exit(0)
' >/tmp/rules-health.out 2>&1
  rhrc=$?
  cat /tmp/rules-health.out | sed 's/^/          /'
  if [ "$rhrc" = "0" ]; then
    ok "出厂告警规则在真实 Prometheus 里全部 health=ok（无评估失败的哑规则）"
  else
    bad "有告警规则在 Prometheus 里评估失败（见上面 lastError）——它语法合法但永远不会触发"
  fi
  rm -f /tmp/rules-health.out
fi

sec "7d. 告警送达链路（规则 → Alertmanager → 外发通道）"
# 为什么单独一节：出厂规则一直在全绿评估，而 `alerting:` 段曾长期是注释状态、栈里没有
# alertmanager 容器 —— 那种情况下 /api/v1/alerts 照样显示 firing，却没有任何人会被叫醒。
# "配了告警"与"有人收到"之间差的正是下面这三跳，所以每一跳都要有可观察的证据。
# 判据用 HTTP 状态码，不用响应体：Alertmanager 的 /-/healthy 成功时**响应体是空的**
# （本机实测：容器 healthy、curl 200，但 grep "healthy" 匹配不到任何字）。
am_code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 8 "http://127.0.0.1:${ALERTMANAGER_PORT:-9094}/-/healthy" 2>/dev/null)"
if [ "$am_code" = "200" ]; then
  ok "Alertmanager 健康（:$(env_val ALERTMANAGER_PORT 9094)/-/healthy → 200）"
else
  bad "Alertmanager 不健康或未启动（:$(env_val ALERTMANAGER_PORT 9094)/-/healthy → ${am_code:-无响应}）"
fi
# Prometheus 是否真的把 AM 当作活动通知端（这一条断了，规则再对也不会发）。
am_disc="$(curl -sS --max-time 8 "$PT/api/v1/alertmanagers" 2>/dev/null)"
if grep -q '"activeAlertmanagers":\[{' <<<"$am_disc" ; then
  ok "Prometheus 有活动 Alertmanager 端点（告警会离开 Prometheus）"
else
  bad "Prometheus 的 activeAlertmanagers 为空——规则会评估、会 firing，但不会送到任何人"
  printf '%s' "$am_disc" | head -c 200 | sed 's/^/         /'; echo ""
fi
# 最后一段：AM 里是否真的有外发通道。没有就明说，不假装配好了。
# 匹配串必须是 webhook_configs:（带下划线和冒号）——早先写的是 grep -q 'webhook'，
# 而 AM 的 /api/v2/status 会回显整份生效配置（含 pagerduty_url 等默认值），
# 泛匹配会让"没有外发通道"的栈被误报成"已配置"。
am_status="$(curl -sS --max-time 8 "http://127.0.0.1:${ALERTMANAGER_PORT:-9094}/api/v2/status" 2>/dev/null)"
if grep -q 'webhook_configs:' <<<"$am_status" ; then
  ok "Alertmanager 已加载外发通道（生效配置里含 webhook_configs 收件段）"
else
  warn "Alertmanager 无外发通道：告警停在 AM 里，不会转发给任何渠道 —— 在 .env 设 ALERT_WEBHOOK_URL 后重跑 deploy.sh up"
fi

sec "7e. 外发是否真的落地（用 Alertmanager 自己的计数，不看它配了什么）"
# 7d 证明的是"配置里有收件段"，而配置在不代表发得出去：地址写错、对端要证书、
# 网络不通、 bearer 过期——都只会在**真正出事那条通知**上暴露。AM 暴露了按 integration
# 维度的发送计数，所以这里直接读它，把"最后一公里"变成可机器判定的事实。
# 注意口径：这些计数是 **AM 进程启动以来的累计值**（重启即归零），不是本次部署的增量。
if grep -q 'webhook_configs:' <<<"$am_status" ; then
  am_metrics="$(curl -sS --max-time 8 "http://127.0.0.1:${ALERTMANAGER_PORT:-9094}/metrics" 2>/dev/null)"
  got_f="$(printf '%s' "$am_metrics" | grep -E '^alertmanager_alerts_received_total\{status="firing"' | awk '{print $2}' | head -1)"
  sent="$(printf '%s' "$am_metrics" | grep -E '^alertmanager_notifications_total\{integration="webhook"' | awk '{print $2}' | head -1)"
  failed="$(printf '%s' "$am_metrics" | grep -E '^alertmanager_notifications_failed_total\{integration="webhook"' | awk '{s+=$2} END{printf "%g", s+0}')"
  req_failed="$(printf '%s' "$am_metrics" | grep -E '^alertmanager_notification_requests_failed_total\{integration="webhook"' | awk '{s+=$2} END{printf "%g", s+0}')"

  if [ -z "$sent" ]; then
    warn "读不到 alertmanager_notifications_total{integration=\"webhook\"}：AM 版本或指标名变了，本节判定失效"
  else
    # 第二跳的落地量（Prometheus → AM 真的收到过规则告警）。
    if [ -n "$got_f" ] && [ "$got_f" != "0" ]; then
      ok "Prometheus 已投递过 ${got_f} 条 firing 告警到 AM（alerts_received_total）"
    else
      warn "AM 至今没收到任何 firing 告警：activeAlertmanagers 非空只证明「指得到」，不证明「送得过」"
    fi
    if [ "$sent" = "0" ]; then
      warn "外发段已配置但 AM 一次都没发过：要么还没告警触发，要么路由没指到这个 receiver——最后一公里未经证明"
    elif [ "$failed" != "0" ] || [ "$req_failed" != "0" ]; then
      # 两个计数不是同一回事（本机实测）：notifications_failed_total 只统计**拿到了 HTTP 响应**
      # 且状态不合规的失败（带 reason 标签）；对端 DNS 解析不了 / 连不上 / 超时这类传输层失败
      # 只进 notification_requests_failed_total，前者会一直是 0。只看一个就会漏报。
      bad "Alertmanager 外发失败：请求级失败 ${req_failed} 次（对端不可达/超时这一类），通知级失败 ${failed} 次（累计发送 ${sent} 次）"
      printf '%s\n' "$am_metrics" | grep -E '^alertmanager_notifications_failed_total\{integration="webhook"' \
        | awk '$2+0>0 {print "         ", $0}'
      echo "         AM 仍在按 repeat_interval 重试；单次瞬时失败会累计在请求级计数里"
      echo "         排查：docker logs opsmesh-alertmanager | grep -i 'Notify attempt failed'（err= 里是原话）、收件端可达性与凭证"
    else
      ok "外发已成功 ${sent} 次、失败 0 次（AM 自计；计数为 AM 启动以来累计）"
    fi
  fi
else
  warn "无外发通道，本节无从判定（先按 7d 的提示配 ALERT_WEBHOOK_URL 再重跑 deploy.sh up）"
fi
sec "8. 数据库落库核对（P0-14 device-svc 建表回归 + 多库隔离）"
MYSQL_C="$(docker ps --filter name=opsmesh-mysql --format '{{.Names}}' | head -1)"
if [ -n "$MYSQL_C" ]; then
  U="$(env_val MYSQL_USER 2>/dev/null)"; [ -z "$U" ] && U=opsmesh
  PWDB="$(env_val MYSQL_PASSWORD)"
  echo "  容器=${MYSQL_C} 用户=${U}"
  echo "  ---- 库清单（期望 opsmesh + 5 个微服务独立库）----"
  docker exec "$MYSQL_C" sh -c "mysql -u'$U' -p'$PWDB' -N -e 'SHOW DATABASES;'" 2>/dev/null | sed 's/^/    /'
  # 注意：SQL 里不要出现反引号——它要穿过 bash 双引号 + docker exec 的 sh -c，
  # 反引号会被 sh 当命令替换执行掉，导致 SQL 变成 "FROM ;" 语法错误。
  # 库名/表名均为本脚本硬编码字面量，无注入面。
  for db in opsmesh opsmesh_device opsmesh_task opsmesh_alert opsmesh_config opsmesh_log; do
    tb="$(docker exec "$MYSQL_C" sh -c "mysql -u'$U' -p'$PWDB' -N -e 'SHOW TABLES FROM $db;'" 2>/dev/null | tr '\n' ' ')"
    if [ -n "$tb" ]; then ok "库 ${db} 有表：$(echo "$tb" | tr -s ' ')"
    else
      case "$db" in
        opsmesh_device|opsmesh_task|opsmesh_alert|opsmesh_config)
          bad "库 ${db} 无任何表（声明了 SQL 存储却未建表——P0-7/P0-14 类回归！）" ;;
        *)
          warn "库 ${db} 无表（该服务可能用 memory 存储或尚未建表）" ;;
      esac
    fi
  done
  echo "  ---- 关键表行数 ----"
  for pair in "opsmesh:users" "opsmesh:devices" "opsmesh_device:devices" "opsmesh_task:tasks"; do
    d="${pair%%:*}"; t="${pair#*:}"
    cnt="$(docker exec "$MYSQL_C" sh -c "mysql -u'$U' -p'$PWDB' -D $d -N -e 'SELECT COUNT(*) FROM $t;'" 2>/dev/null)"
    [ -n "$cnt" ] && ok "${d}.${t} 可查（${cnt} 行）" || warn "${d}.${t} 不可查（表名可能不同）"
  done
  echo "  ---- P0-7 修复实证（alert/config 持久化必须真实可用）----"
  for pair in "opsmesh_alert:alerts" "opsmesh_config:config_entries"; do
    d="${pair%%:*}"; t="${pair#*:}"
    cnt="$(docker exec "$MYSQL_C" sh -c "mysql -u'$U' -p'$PWDB' -D $d -N -e 'SELECT COUNT(*) FROM $t;'" 2>/dev/null)"
    if [ -n "$cnt" ]; then ok "${d}.${t} 可查（${cnt} 行）——持久化未静默降级"
    else bad "${d}.${t} 不可查（P0-7 回归：该服务疑似仍回退 memory！）"
    fi
  done
else
  bad "未找到 opsmesh-mysql 容器"
fi

sec "8b. 存储层吞错（Store 不返回 error，静默丢数据只能在这里看见）"
# 为什么单独一节：Store 接口的读路径不返回 error，失败只进日志与
# opsmesh_store_write_failures_total。真机实测（v0.11.0，2026-10-02）：alerts 表每一列都可空
# （migrations/001_initial.sql:134-148），写入侧统一走 nullString()/nullTime()，而读侧把
# silenced_until / updated_at 直接 Scan 进 time.Time ⇒「从未被静默过的告警」整行读不回来，
# 每 10 秒报一次 Alerts 扫描失败、累计 1844 次，而客户看到的现象只是"告警页是空的"。
# 先看日志（独立观测量），再看指标，最后交叉核对两者——v0.11.0 的真实形态正是"日志一直报、
# 指标一直 0"，任何一侧单独看都会漏。
scan_drop="$(docker logs --since 10m opsmesh-controlplane 2>&1 | grep -c 'Alerts 扫描失败' || true)"
if [ "${scan_drop:-0}" = "0" ]; then
  ok "近 10 分钟无 Alerts 扫描丢行"
else
  bad "近 10 分钟 Alerts 扫描失败 ${scan_drop} 次：alerts 可空列必须用 sql.Null* 读回，否则整行被静默丢弃"
fi
sf="$(metrics_snapshot | awk '/^opsmesh_store_write_failures_total /{print $2}' | head -1)"
# 判据取"近 10 分钟的增量"而不是累计值，理由是断言的可信度而不是这条计数本身：
# 它是"进程启动以来累计"，长跑实例迟早非零（DB 抖动、context deadline exceeded 都会进来；
# 本轮真机就有 74 次这类历史吞错），按累计判红 ⇒ 这一节**永远红** ⇒ 运维学会无视它，
# 那才是真正的失效。日志侧（scan_drop）本来就是 10 分钟窗口，两侧现在同一个窗口。
win="$(curl -sS --max-time 8 --get \
    --data-urlencode 'query=sum(increase(opsmesh_store_write_failures_total[10m]))' \
    "$PT/api/v1/query" 2>/dev/null \
    | sed -n 's/.*"value":\[[^,]*,"\([0-9.eE+-]*\)"\].*/\1/p')"
win_gt0="$(awk -v v="${win:-0}" 'BEGIN{print (v+0>0)?"1":"0"}')"
if [ -z "$sf" ]; then
  bad "抓取面上没有 opsmesh_store_write_failures_total 序列——吞错无从监控，本节判红（不判跳过）"
elif [ "$sf" = "0" ] && [ "${scan_drop:-0}" != "0" ]; then
  bad "指标与日志矛盾：日志近 10 分钟有 ${scan_drop} 次吞错，而抓取面 opsmesh_store_write_failures_total=0"
  echo "         ⇒ /metrics 这条装配路径没推该计数（v0.11.0 实测形态：1844 次吞错 vs 指标 0）"
elif [ -z "$win" ]; then
  bad "取不到窗口增量（Prometheus 不可达或该序列没被抓取）——按'无法证明没有吞错'判红，不判跳过"
elif [ "$win_gt0" = "1" ]; then
  bad "近 10 分钟存储层吞掉 ${win} 次读写（进程累计 ${sf}）：接口不报错，但数据在悄悄丢"
  echo "         定位：docker logs opsmesh-controlplane 2>&1 | grep '\\[store\\]' | tail -20"
  echo "         明细：GET /api/v1/admin/store-failures（需管理员身份；含按操作聚合 + 最近样本）"
elif [ "$sf" != "0" ]; then
  ok "近 10 分钟无新增存储层吞错（进程启动以来累计 ${sf} 次，均在窗口之外）"
  echo "         历史吞错多为 DB 抖动/超时；按操作聚合看：GET /api/v1/admin/store-failures"
else
  ok "存储层吞错 0（自控制面进程启动以来累计）"
fi

sec "9. 反代/入口形态"
if [ "$(env_val PROXY)" = "true" ]; then
  echo "  PROXY=true（nginx 终结 TLS）"
  curl -sS --max-time 6 -o /dev/null -w '  http://127.0.0.1:%{http_code} → redirect=%{redirect_url}\n' "http://127.0.0.1:$(env_val GATEWAY_HTTP_PORT 80)/healthz" 2>/dev/null
else
  echo "  PROXY 未启用：控制面自身在 $(env_val CONTROLPLANE_HTTP_PORT 8080) 终结 TLS（P0-2 单机形态）"
fi

sec "10. 企业版前端内置（P0-3 回归）"
# 企业版前端在构建期经 go:embed 打进控制面二进制，/enterprise/ 由控制面自身提供。
# 关键不变量：真产物（非占位）、SPA 回退、缺包必须 404（不得回退 HTML）、SPA 入口不做身份门禁。
ehead="$(curl "${K[@]}" -D - -o /dev/null -w '%{http_code}' "$CP/enterprise/" 2>/dev/null)"
ecode="$(printf '%s' "$ehead" | tail -1)"
ebody="$(curl "${K[@]}" "$CP/enterprise/" 2>/dev/null)"
# 授权面决定这一节验什么：社区授权下 /enterprise/ 返回的就是「企业版 · 未授权」说明页
# （license_gate 的既定契约，见 internal/controlplane/enterprise_ui.go），
# 此时拿不到 SPA 入口与 assets 引用属**预期**。一律按企业授权去断言，会把交付口径读反成假红
# （2026-10-04 本机栈就是社区授权，SPA 那条连续判红）。
lic_hdr="$(grep -i '^x-opsmesh-license:' <<<"$ehead" | tr -d '\r')"
lic_community=0
case "$lic_hdr" in *[Cc]ommunity*) lic_community=1 ;; esac
echo "  X-OpsMesh-License=${lic_hdr:-（未带该头）}"

if [ "$ecode" != "200" ]; then
  bad "GET $CP/enterprise/ → ${ecode:-无响应}（企业版前端未交付：镜像未装配？）"
else
  ok "GET $CP/enterprise/ → 200"
  if grep -qi 'X-OpsMesh-Enterprise-Bundle: *placeholder' <<<"$ehead" ; then
    bad "企业版前端为占位页（镜像构建未装配前端产物，P0-3 回归！）"
  elif grep -q 'OPSMESH_ENTERPRISE_BUNDLE_PLACEHOLDER' <<<"$ebody" ; then
    bad "企业版前端 body 含占位标记（P0-3 回归！）"
  else
    if [ "$lic_community" = "1" ]; then
      # 社区授权下拿到的是未授权说明页，**看不出**镜像里是否真装配了前端产物；
      # 这一条只在企业授权下才是"非占位"的证明，措辞必须与判据一致。
      ok "未见占位标记（社区授权下不足以证明已装配真产物，需企业授权才验得到）"
    else
      ok "企业版前端为真实构建产物（非占位页）"
    fi
  fi
  # 入口必须引用 /enterprise/assets/ 下的资源，且首个 JS 资源可 200 取到。
  if [ "$lic_community" = "1" ]; then
    if grep -q '企业版 · 未授权' <<<"$ebody"; then
      ok "社区授权：/enterprise/ 按契约返回未授权说明页（SPA 资产断言需企业授权，本实例不适用）"
    else
      bad "授权头是 community，但页面既不是未授权说明页也没有 SPA 引用（内容来源不明）"
      dump_snapshot enterprise-community "$ebody"
    fi
  elif grep -q '/enterprise/assets/' <<<"$ebody" ; then
    ok "SPA 入口引用 /enterprise/assets/ 资源"
    asset="$(printf '%s' "$ebody" | grep -o '/enterprise/assets/[^"'"'"']*\.js' | head -1)"
    if [ -n "$asset" ]; then
      acode="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' "$CP$asset" 2>/dev/null)"
      [ "$acode" = "200" ] && ok "SPA 首个 JS 资源可取（${asset##*/} → 200）" \
                           || bad "SPA 首个 JS 资源不可取（${asset##*/} → ${acode:-无响应}）"
      # 带内容哈希的 assets 应长缓存。
      acc="$(curl "${K[@]}" -D - -o /dev/null "$CP$asset" 2>/dev/null | grep -i '^cache-control:' | tr -d '\r')"
      case "$acc" in
        *immutable*) ok "assets 长缓存生效（$(printf '%s' "$acc" | cut -d' ' -f2-)）" ;;
        *)           warn "assets 未见 immutable 长缓存：${acc:-无 Cache-Control}" ;;
      esac
      # 预压缩协商：br 与 gzip 都不能退化成「200 但发未压缩原文」——那种失败无错误码，
      # 只表现为体积翻数倍，必须逐编码断言 Content-Encoding 实际生效。
      ident_len="$(curl "${K[@]}" -H 'Accept-Encoding: identity' -o /dev/null -w '%{size_download}' "$CP$asset" 2>/dev/null)"
      for enc in br gzip; do
        ehdr="$(curl "${K[@]}" -D - -H "Accept-Encoding: $enc" -o /dev/null "$CP$asset" 2>/dev/null | grep -i '^content-encoding:' | tr -d '\r')"
        elen="$(curl "${K[@]}" -H "Accept-Encoding: $enc" -o /dev/null -w '%{size_download}' "$CP$asset" 2>/dev/null)"
        if [ -n "$ehdr" ]; then
          # 旁路命中：必须声明对应编码，且体积小于未压缩体（否则是「声明了却没压」的坏包）。
          case "$ehdr" in
            *"$enc"*)
              if [ -n "$ident_len" ] && [ -n "$elen" ] && [ "$elen" -lt "$ident_len" ] 2>/dev/null; then
                ok "预压缩旁路 $enc 生效（${elen}B < 未压缩 ${ident_len}B）"
              else
                bad "预压缩旁路 $enc 体积未变小（${elen}B vs ${ident_len}B）——旁路文件可能损坏或未压缩"
              fi ;;
            *) bad "Accept-Encoding: $enc 返回了错误的 Content-Encoding（${ehdr}）" ;;
          esac
        else
          # 无旁路文件属正常（vite 只对超阈值资源产出），但此时必须是未压缩原文。
          [ "$elen" = "$ident_len" ] && warn "$asset 无 .$([ "$enc" = gzip ] && echo gz || echo br) 旁路（返回未压缩原文，功能正确）" \
                                     || warn "$asset 未声明 Content-Encoding 但体积异常（${elen}B vs ${ident_len}B）"
        fi
      done
    else
      warn "未从 SPA 入口解析到 .js 资源引用（构建产物结构可能变化）"
    fi
  else
    bad "SPA 入口未引用 /enterprise/assets/（构建 base 前缀或装配目录可能不对）"
  fi
  # SPA 路由回退（Vue Router createWebHistory）
  fbcode="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' "$CP/enterprise/devices" 2>/dev/null)"
  [ "$fbcode" = "200" ] && ok "SPA 深链回退 /enterprise/devices → 200" \
                        || bad "SPA 深链回退 /enterprise/devices → ${fbcode:-无响应}（应 200 回退 index.html）"
  # 缺包必须 404：回退 HTML 会让浏览器报 MIME 错误，排障成本高。
  mcode="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' "$CP/enterprise/assets/__missing__.js" 2>/dev/null)"
  [ "$mcode" = "404" ] && ok "缺失分包 → 404（未回退 HTML）" \
                       || bad "缺失分包返回 ${mcode}（应 404）"
  # 路径穿越不得命中任何文件
  tcode="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' --path-as-is "$CP/enterprise/assets/../../healthz" 2>/dev/null)"
  case "$tcode" in
    200) bad "路径穿越 /enterprise/assets/../../healthz → 200（越权命中非前端资源！）" ;;
    *)   ok "路径穿越被拒（→ ${tcode:-连接失败}）" ;;
  esac
fi

# 个人版引导页：已装配时保留企业版入口 CTA（占位时应被服务端剥离，此处仅在 200 时校验）。
dcode="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' -H 'X-Tenant-ID: default' "$CP/" 2>/dev/null)"
if [ "$dcode" = "200" ]; then
  roothtml="$(curl "${K[@]}" -H 'X-Tenant-ID: default' "$CP/" 2>/dev/null)"
  if grep -q 'href="/enterprise/"' <<<"$roothtml"; then
    ok "个人版引导页提供企业版入口（/enterprise/）"
  else
    warn "个人版引导页未见 /enterprise/ 入口（若前端为占位状态属预期，占位时应剥离 CTA）"
  fi
else
  warn "GET / → ${dcode}（未携带有效租户上下文时属预期，跳过 CTA 校验）"
fi

sec "11. 迁移版本门禁与租户列落库（P0-5 / P0-6 回归）"
# 用 shell glob 而不是 `ls -1 | grep -v | wc -l`：后者是 shellcheck SC2010 的反模式
# （文件名含空格/换行即错），且管道尾的 wc 会把 ls 的失败吞成 0。
# 无匹配时 glob 会留下字面模式本身，故逐个 -e 判存在。
count_sql() { # $1=目录 $2=是否只数回滚脚本（yes|no）
    local n=0 f
    for f in "$1"/*.sql; do
        [[ -e "$f" ]] || continue
        case "$f" in
            *.down.sql) [[ "$2" == yes ]] && n=$((n + 1)) ;;
            *) [[ "$2" == no ]] && n=$((n + 1)) ;;
        esac
    done
    printf '%s' "$n"
}
if [ -n "${MYSQL_C:-}" ] && [ -n "${U:-}" ]; then
  mx="$(docker exec "$MYSQL_C" sh -c "mysql -u'$U' -p'$PWDB' -D opsmesh -N -e 'SELECT MAX(version) FROM schema_migrations;'" 2>/dev/null | tr -d ' \r')"
  # 磁盘上未执行的迁移数（排除 .down.sql 回滚脚本），与库内版本号比对即为「版本门禁」不变量。
  disk="$(count_sql "${ROOT}/internal/store/migrations" no)"
  if [ -n "$mx" ] && [ "$mx" != "NULL" ]; then
    ok "schema_migrations 最大版本=${mx}（磁盘迁移文件 ${disk} 个）"
    if [ "$mx" = "$disk" ]; then
      ok "版本门禁一致：库内版本数 == 磁盘迁移文件数（无未执行迁移被漏跑）"
    else
      bad "版本不一致：库内=${mx} 磁盘=${disk}（启动期迁移未跑完，或磁盘多出未执行文件）"
    fi
    # 防篡改基线：每条已应用迁移都须有非空 checksum（改动既有迁移文件会在启动期被 checksum 门禁拦下）。
    nock="$(docker exec "$MYSQL_C" sh -c "mysql -u'$U' -p'$PWDB' -D opsmesh -N -e \"SELECT COUNT(*) FROM schema_migrations WHERE checksum IS NULL OR checksum='';\"" 2>/dev/null | tr -d ' \r')"
    [ "$nock" = "0" ] && ok "全部已应用迁移均有 checksum（防篡改基线就绪）" \
                      || bad "有 ${nock} 条迁移缺 checksum（checksum 门禁失效）"
    # P0-6：用户表租户列 + 唯一索引须真实落库（否则 JWT 无法携带租户、隔离退化）。
    tcol="$(docker exec "$MYSQL_C" sh -c "mysql -u'$U' -p'$PWDB' -D opsmesh -N -e \"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='opsmesh' AND table_name='users' AND column_name='tenant_id';\"" 2>/dev/null | tr -d ' \r')"
    if [ "$tcol" = "1" ]; then
      ok "users.tenant_id 列已落库（P0-6 数据模型就绪）"
      # 迁移 018 须把历史行回填为默认租户，否则存量用户的 JWT 租户为空 → 隔离静默退化。
      nempty="$(docker exec "$MYSQL_C" sh -c "mysql -u'$U' -p'$PWDB' -D opsmesh -N -e \"SELECT COUNT(*) FROM users WHERE tenant_id IS NULL OR tenant_id='';\"" 2>/dev/null | tr -d ' \r')"
      [ "$nempty" = "0" ] && ok "users 历史行租户已回填（无空租户用户）" \
                         || bad "有 ${nempty} 个用户 tenant_id 为空（P0-6 回填缺失，隔离会退化）"
    else
      bad "users.tenant_id 列缺失（P0-6 迁移未生效！）"
    fi
    # 领取侧租户过滤依赖 tasks.tenant_id 可比较，列缺失会导致过滤静默失效。
    tcol2="$(docker exec "$MYSQL_C" sh -c "mysql -u'$U' -p'$PWDB' -D opsmesh -N -e \"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='opsmesh' AND table_name='tasks' AND column_name='tenant_id';\"" 2>/dev/null | tr -d ' \r')"
    [ "$tcol2" = "1" ] && ok "tasks.tenant_id 列已落库（领取侧租户过滤可生效）" \
                       || warn "tasks.tenant_id 列缺失（若领取侧过滤未依赖此列可忽略）"
  else
    bad "无法读取 opsmesh.schema_migrations（迁移体系未生效？）"
  fi
  # 回滚脚本齐备性：每个迁移都应随附 .down.sql（P0-5 可回滚交付物）。
  upf="$(count_sql "${ROOT}/internal/store/migrations" no)"
  dnf="$(count_sql "${ROOT}/internal/store/migrations" yes)"
  [ "$upf" = "$dnf" ] && ok "回滚脚本齐备（up=${upf} down=${dnf}）" \
                      || bad "回滚脚本缺失：up=${upf} down=${dnf}（P0-5 可回滚性不达标）"
else
  warn "未取得 MySQL 容器/凭据上下文，跳过 P0-5/P0-6 落库断言"
fi

sec "12. 租户伪造拒绝（P0-6 回归，无状态可重复）"
# requireAuth 下「只带 X-Tenant-ID 头、不带任何可验证凭证」必须被拒——
# 否则任意调用方换个租户头即可横向越权（http_infra.go 的信任边界）。
fcode="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' -H 'X-Tenant-ID: attacker-tenant' "$CP/api/v1/devices" 2>/dev/null)"
case "$fcode" in
  401) ok "伪造租户头被拒（/api/v1/devices → 401）" ;;
  403) ok "伪造租户头被拒（/api/v1/devices → 403）" ;;
  *)   bad "仅带伪造 X-Tenant-ID 头竟返回 ${fcode}（P0-6 越权回归！）" ;;
esac
fcode2="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' -H 'X-Tenant-ID: attacker-tenant' "$CP/api/v1/users" 2>/dev/null)"
case "$fcode2" in
  401|403) ok "伪造租户头访问用户管理被拒（→ ${fcode2}）" ;;
  *)       bad "伪造租户头访问 /api/v1/users 返回 ${fcode2}（越权回归！）" ;;
esac

sec "13. 审计链防篡改与保留策略（P1-3 回归）"
# 13a 迁移 019 落库：链式列 / 链头表 / 归档表 / 检索索引必须真实存在。
if [ -n "${MYSQL_C:-}" ] && [ -n "${U:-}" ]; then
  q() { docker exec "$MYSQL_C" sh -c "mysql -u'$U' -p'$PWDB' -D opsmesh -N -e \"$1\"" 2>/dev/null | tr -d ' \r'; }
  ccol="$(q "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='opsmesh' AND table_name='audit_log' AND column_name IN ('prev_hash','entry_hash');")"
  [ "$ccol" = "2" ] && ok "audit_log 已落 prev_hash/entry_hash 链式列" \
                    || bad "audit_log 链式列缺失（期望 2 列，实为 ${ccol:-查询失败}；P1-3 迁移未生效！）"
  for t in audit_chain_head audit_log_archive audit_archive_meta; do
    tcnt="$(q "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='opsmesh' AND table_name='$t';")"
    [ "$tcnt" = "1" ] && ok "表 ${t} 已建" || bad "表 ${t} 缺失（P1-3 迁移未生效！）"
  done
  icnt="$(q "SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema='opsmesh' AND table_name='audit_log' AND index_name='idx_audit_entry_hash';")"
  [ "${icnt:-0}" -ge 1 ] && ok "audit_log.idx_audit_entry_hash 已建（链式窗口查询不全表扫）" \
                         || bad "链式索引 idx_audit_entry_hash 缺失（P1-3 迁移未生效！）"
  hrow="$(q "SELECT COUNT(*) FROM audit_chain_head WHERE id=1;")"
  [ "$hrow" = "1" ] && ok "audit_chain_head 单行链头就绪（多副本串行化基线）" \
                    || bad "audit_chain_head 无 id=1 单行（并发写入将无法串行化）"
  # 13b 链写入活性（真实运行期证据，非静态配置）：最新审计行必须已纳入链，且链头指向它。
  chalive="$(q "SELECT IFNULL((SELECT entry_hash<>'' FROM audit_log ORDER BY id DESC LIMIT 1),0);")"
  [ "$chalive" = "1" ] && ok "最新审计行已带 entry_hash（运行期链式写入生效）" \
                       || warn "最新审计行无 entry_hash（可能尚无审计事件，或写入降级为非链式——查控制面日志）"
  if [ "$chalive" = "1" ]; then
    hcons="$(q "SELECT (last_hash = (SELECT entry_hash FROM audit_log WHERE entry_hash<>'' ORDER BY id DESC LIMIT 1)) FROM audit_chain_head WHERE id=1;")"
    [ "$hcons" = "1" ] && ok "链头 last_hash == 最新链式行 entry_hash（链头跟随，尾部未被删改）" \
                       || bad "链头与最新链式行不一致（尾部行被删除或链头被改动，P1-3 完整性告警！）"
  fi
  nlegacy="$(q "SELECT COUNT(*) FROM audit_log WHERE entry_hash IS NULL OR entry_hash='';")"
  [ "${nlegacy:-0}" = "0" ] && ok "无链前遗留行（全部审计行已纳入哈希链）" \
                            || warn "有 ${nlegacy} 条链前遗留行（迁移 019 之前写入，未纳入链，自检会如实计数）"
  # 13c 保留策略配置已接线（--audit-retention-days 出现在控制面启动参数中）。
  ret="$(docker inspect opsmesh-controlplane --format '{{json .Config.Cmd}}' 2>/dev/null | grep -o 'audit-retention-days=[0-9]*' | head -1)"
  [ -n "$ret" ] && ok "控制面已接线保留策略（${ret}）" \
                || bad "控制面启动参数未见 --audit-retention-days（P1-3 保留策略未接线！）"
else
  warn "未取得 MySQL 容器/凭据上下文，跳过 P1-3 落库断言"
fi

# 13d 校验端点准入：未带凭证必须 401（404 说明路由未注册，501 说明后端不支持——均应告警）。
vcode="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' "$CP/api/v1/audit/verify" 2>/dev/null)"
case "$vcode" in
  401) ok "GET /api/v1/audit/verify 未认证 → 401（端点已注册且受鉴权保护）" ;;
  404) bad "GET /api/v1/audit/verify → 404（路由未注册，P1-3 端点缺失！）" ;;
  *)   warn "GET /api/v1/audit/verify 未认证 → ${vcode:-无响应}（期望 401，请复核鉴权链路）" ;;
esac

# 13e 链自检循环的运行时自观测：leader 每 60s 归档 + 自检，指标须为「已支持且自洽」。
mbody2="$(metrics_snapshot)"
if [ -z "$mbody2" ]; then
  bad "无法抓取 9091 指标——审计链自观测断言无法执行，按未验证判红（warn 会让这一节在人眼里变成绿的）"
else
  for m in opsmesh_audit_chain_supported opsmesh_audit_chain_ok opsmesh_audit_chain_checked_rows opsmesh_audit_chain_checks_total; do
    if grep -q "^${m} [0-9]" <<<"$mbody2"; then ok "指标 ${m} 已暴露"; else bad "缺少指标 ${m}（P1-3 自检不可观测）"; dump_snapshot metrics-13e "$mbody2"; fi
  done
  ctot="$(printf '%s\n' "$mbody2" | grep -E '^opsmesh_audit_chain_checks_total [0-9]+$' | head -1 | awk '{print $2}')"
  csup="$(printf '%s\n' "$mbody2" | grep -E '^opsmesh_audit_chain_supported [0-9]+$' | head -1 | awk '{print $2}')"
  cok="$(printf '%s\n' "$mbody2" | grep -E '^opsmesh_audit_chain_ok [0-9]+$' | head -1 | awk '{print $2}')"
  crow="$(printf '%s\n' "$mbody2" | grep -E '^opsmesh_audit_chain_checked_rows [0-9]+$' | head -1 | awk '{print $2}')"
  echo "  supported=${csup:-?} ok=${cok:-?} checked_rows=${crow:-?} checks_total=${ctot:-?}"
  [ "${ctot:-0}" -ge 1 ] && ok "链自检已实际执行（checks_total=${ctot}，leader 循环在跑）" \
                         || bad "链自检从未执行（checks_total=0，leader 维护循环未生效！）"
  [ "${csup:-0}" = "1" ] && ok "存储后端支持链式校验（supported=1）" \
                         || bad "supported=${csup:-0}（SQL 后端应支持链式校验）"
  [ "${cok:-0}" = "1" ] && ok "链自检结论自洽（ok=1）" \
                        || bad "ok=${cok:-0}（链完整性校验未通过，P1-3 告警：疑似篡改或尾部删除）"
fi

sec "14. agent 身份绑定与 per-agent 密钥（P1-2 回归）"
# 14a 交付资产接线：prod compose 默认开启签名验证（.env 可覆盖为 false 仅用于开发形态）。
sig_env="$(docker inspect opsmesh-controlplane --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null | grep '^OPSMESH_GRPC_REQUIRE_SIGNATURE=' | head -1)"
case "$sig_env" in
  OPSMESH_GRPC_REQUIRE_SIGNATURE=true) ok "控制面已接线 gRPC 签名验证（${sig_env}）" ;;
  "")                                  bad "控制面容器未见 OPSMESH_GRPC_REQUIRE_SIGNATURE（P1-2 交付资产未接线！）" ;;
  *)                                   warn "签名验证被显式关闭（${sig_env}）——仅开发形态可接受，生产应收敛为 true" ;;
esac

# 14b 验签可观测性：指标族必须存在（固定标签全量输出，含 0 值）。
mbody3="$(docker exec opsmesh-prometheus wget -qO- --timeout=8 http://controlplane:9091/metrics 2>/dev/null)"
[ -z "$mbody3" ] && mbody3="$(curl -sS --max-time 8 "http://127.0.0.1:${MP}/metrics" 2>/dev/null)"
if [ -z "$mbody3" ]; then
  warn "无法抓取 9091 指标，跳过 P1-2 可观测性断言"
else
  sigmiss=""
  for a in v1 v2 none unknown; do
    for r in ok rejected; do
      grep -q "^opsmesh_agent_signature_verifications_total{alg=\"${a}\",result=\"${r}\"} [0-9]" <<<"$mbody3" \
        || sigmiss="${sigmiss} ${a}/${r}"
    done
  done
  [ -z "$sigmiss" ] && ok "验签指标全标签集已暴露（alg∈{v1,v2,none,unknown} × result∈{ok,rejected}）" \
                    || bad "验签指标缺时序：${sigmiss}（P1-2 可观测性未接线）"
  for s in per_agent fleet; do
    if grep -q "^opsmesh_agent_signing_key_source_total{source=\"${s}\"} [0-9]" <<<"$mbody3"; then
      ok "密钥来源指标已暴露（source=${s}）"
    else
      bad "缺少 opsmesh_agent_signing_key_source_total{source=\"${s}\"}（P1-2）"
      dump_snapshot metrics-sig "$mbody3"
    fi
  done
  # 运行期活性（条件式）：本次部署后若尚无 agent 流量，如实 WARN 而不是伪造 PASS。
  v2ok="$(printf '%s\n' "$mbody3" | grep -E '^opsmesh_agent_signature_verifications_total\{alg="v2",result="ok"\} [0-9]+$' | head -1 | awk '{print $2}')"
  if [ "${v2ok:-0}" -ge 1 ]; then
    ok "已有 agent 用 v2（覆盖载荷）签名通过验签（v2/ok=${v2ok}）"
  else
    warn "本次部署后尚无 agent 验签流量（v2/ok=0）——纳管 agent 后应转为 ≥1，届时可复跑本脚本复查"
  fi
  v1ok="$(printf '%s\n' "$mbody3" | grep -E '^opsmesh_agent_signature_verifications_total\{alg="v1",result="ok"\} [0-9]+$' | head -1 | awk '{print $2}')"
  [ "${v1ok:-0}" = "0" ] && ok "无 v1（不覆盖载荷）遗留算法流量" \
                         || warn "存在 v1 签名流量（v1/ok=${v1ok}，滚动升级未收尾；v1 不覆盖载荷，见告警 OpsMeshAgentSignatureLegacyAlg）"
  flt="$(printf '%s\n' "$mbody3" | grep -E '^opsmesh_agent_signing_key_source_total\{source="fleet"\} [0-9]+$' | head -1 | awk '{print $2}')"
  [ "${flt:-0}" = "0" ] && ok "无全舰队预共享密钥兜底使用（per-agent 密钥隔离生效）" \
                        || warn "存在全舰队预共享密钥兜底验签（fleet=${flt}，单机泄漏即全舰队可冒充）"
fi

# 14c 落库：per-agent 密钥列已生成（有 agent 时）。
if [ -n "${MYSQL_C:-}" ] && [ -n "${U:-}" ]; then
  atot="$(q "SELECT COUNT(*) FROM agents;")"
  asec="$(q "SELECT COUNT(*) FROM agents WHERE secret IS NOT NULL AND secret<>'';")"
  if [ "${atot:-0}" = "0" ]; then
    warn "agents 表为空（本次部署后未纳管 agent），跳过 per-agent 密钥落库断言"
  elif [ "${asec:-0}" -ge 1 ]; then
    ok "已注册 agent 持 per-agent 密钥（${asec}/${atot}，P1-2 密钥隔离基线）"
    [ "${asec:-0}" = "${atot:-0}" ] || warn "有部分 agent 无 per-agent 密钥（${asec}/${atot}；常见于老库注册或未跑迁移）"
  else
    bad "agents 表有 ${atot} 台 agent 但无一持有 per-agent 密钥（P1-2 密钥生成未生效！）"
  fi
fi

sec "15. 可支撑性端点（P1-6 回归：版本注入 / 诊断面默认关闭 / 匿名不可读）"
# 15a /version 无鉴权可读（刻意），且必须报告**构建注入的版本**——
#     -X 的包路径写成 opsmesh/... 时链接器静默忽略：构建成功、产物照跑、版本恒为默认值。
#     这条断言就是防它复发的黑盒门禁。
ver_body="$(curl "${K[@]}" --max-time 10 "$CP/version" 2>/dev/null)"
ver_code="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' --max-time 10 "$CP/version" 2>/dev/null)"
if [ "$ver_code" = "200" ]; then
  ok "GET /version → 200（无鉴权，供现场核对构建）"
else
  bad "GET /version → ${ver_code}（期望 200）"
fi
want_ver="$(env_val OPSMESH_VERSION)"
got_ver="$(printf '%s' "$ver_body" | tr ',' '\n' | sed -nE 's/^\s*"version"\s*:\s*"([^"]*)".*/\1/p' | head -1)"
# 两侧的书写习惯本就不同：.env 的 OPSMESH_VERSION 是**镜像 tag**（0.11.0，不带 v），
# 而发布流水线给 -X version.Version 注入的是 **git tag**（v0.11.0，带 v）。
# 直接字符串相等会把一次完全正常的部署判成"版本注入未生效"（2026-10-02 升 0.11.0 实测）。
# 归一化只去掉前导 v——不做模糊匹配，避免把 0.11.1 说成 0.11.0。
gv="$(printf '%s' "$got_ver" | sed 's/^[vV]//')"
wv="$(printf '%s' "$want_ver" | sed 's/^[vV]//')"
if [ -n "$wv" ] && [ -n "$gv" ] && [ "$gv" = "$wv" ]; then
  ok "/version 版本与 .env OPSMESH_VERSION 一致（${got_ver}，构建期 -ldflags 注入生效）"
else
  bad "/version 版本='${got_ver:-空}' 与 .env OPSMESH_VERSION='${want_ver:-空}' 不一致（版本注入未生效？查 Dockerfile 的 -X 包路径是否为模块路径，以及镜像是否由本次 .env 的版本构建）"
fi
for f in commit goVersion uptimeSeconds; do
  if grep -q "\"$f\"" <<<"$ver_body" ; then ok "/version 含字段 $f"; else bad "/version 缺字段 $f"; fi
done

# 15b 诊断面在出厂形态下默认关闭 / 需鉴权（安全断言：这些面不能对匿名来源开放）
pprof_code="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' --max-time 10 "$CP/debug/pprof/goroutine" 2>/dev/null)"
if [ "$pprof_code" = "404" ]; then
  ok "pprof 未注册（--debug-pprof 默认 false）"
else
  bad "pprof 返回 ${pprof_code}（期望 404：出厂形态不应暴露进程内存/调用栈剖面）"
fi
# 级别开关用 POST 探（GET 会先被 405 拦下，测不到鉴权）
lvl_code="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' -X POST --max-time 10   -H 'Content-Type: application/json' -d '{"level":"debug"}'   "$CP/api/v1/admin/loglevel" 2>/dev/null)"
if [ "$lvl_code" = "401" ]; then
  ok "匿名 POST /api/v1/admin/loglevel → 401（运行期改级别需身份）"
else
  bad "匿名 POST /api/v1/admin/loglevel → ${lvl_code}（期望 401）"
fi
for ep in api/v1/admin/config api/v1/admin/diagnostics; do
  ac="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' --max-time 10 "$CP/$ep" 2>/dev/null)"
  if [ "$ac" = "401" ]; then
    ok "匿名 GET /$ep → 401（诊断材料不匿名开放）"
  else
    bad "匿名 GET /$ep → ${ac}（期望 401）"
  fi
done

# 集合端点空值形状（null-vs-[] 契约）。独立脚本单源：本处只调用与记账，
# 断言细节与其自身报告见 deploy/scripts/probe-collection-shapes.sh。
# 该缺陷类（nil 切片直出 null → 前端列表崩）静态检查全盲，2026-10-03 由本巡检
# 在真实栈上抓到 alerts/incidents 两处，修复后此处应恒绿。
sec "16. 集合端点空值形状（null-vs-[] 契约，18 端点）"
SHAPES_LOG="$(mktemp)"
# 必须用 ${SCRIPT_DIR}（绝对路径）：本脚本开头已 cd 到 deploy/docker，
# 而 "$(dirname "$0")" 在从仓库根调用时是**相对**路径 `deploy/scripts`，在新 cwd 下不存在，
# 于是巡检永远报 "No such file or directory" —— 一条**永远红**的断言（2026-10-04 实测连红四轮）。
if BASE_URL="$CP" bash "${SCRIPT_DIR}/probe-collection-shapes.sh" >"$SHAPES_LOG" 2>&1; then
  ok "集合端点形状巡检：18 端点全部 200 且为 []/{…:[]}"
else
  bad "集合端点形状巡检失败（详见 ${SHAPES_LOG}）"
  sed 's/^/    /' "$SHAPES_LOG" | tail -8
fi
rm -f "$SHAPES_LOG"

echo ""
echo "==================================================="
echo "  断言汇总：PASS=${PASS}  FAIL=${FAIL}"
echo "==================================================="
[ "$FAIL" -eq 0 ]
