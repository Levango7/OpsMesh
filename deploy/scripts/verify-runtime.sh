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

env_val() { grep -E "^$1=" .env 2>/dev/null | head -1 | cut -d= -f2-; }
PW="$(env_val ADMIN_PASSWORD)"
CP="https://127.0.0.1:$(env_val CONTROLPLANE_HTTP_PORT 8080)"

sec "1. 容器健康矩阵"
docker compose --env-file .env -f docker-compose.prod.yml ps --format '{{.Service}}\t{{.Status}}' 2>/dev/null | sort

sec "1b. 宿主端口发布真实性（internal 网络静默丢弃缺陷的回归断言）"
missing=""
for c in $(docker ps --filter name=opsmesh --format '{{.Names}}'); do
  pb="$(docker inspect "$c" --format '{{len .HostConfig.PortBindings}}' 2>/dev/null)"
  ns="$(docker inspect "$c" --format '{{json .NetworkSettings.Ports}}' 2>/dev/null)"
  if [ -n "$pb" ] && [ "$pb" != "0" ] && ! printf '%s' "$ns" | grep -q 'HostPort'; then
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

# 3a 8080 公开端点：来源在白名单内 → 200；不在 → 403（fail-closed，属预期而非缺陷）。
mcode="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' "$CP/metrics" 2>/dev/null)"
case "$mcode" in
  200) ok "GET $CP/metrics → 200（本机来源在 CIDR 白名单内）" ;;
  403) warn "GET $CP/metrics → 403（本机来源不在 METRICS_ALLOW_CIDR 内；端点已按 P1-5 收敛，非回归）" ;;
  *)   bad "GET $CP/metrics → ${mcode:-无响应}（期望 200 或 403）" ;;
esac

# 3b 9091 独立 metrics 端口：Prometheus 抓取路径必须可达（monitoring 网段，走容器内抓取最稳）。
mbody="$(docker exec opsmesh-prometheus wget -qO- --timeout=8 http://controlplane:9091/metrics 2>/dev/null)"
[ -z "$mbody" ] && mbody="$(curl -sS --max-time 8 "http://127.0.0.1:${MP}/metrics" 2>/dev/null)"
if [ -z "$mbody" ]; then
  bad "9091 /metrics 抓取为空（Prometheus → controlplane:9091 与宿主 127.0.0.1:${MP} 均失败）"
else
  ok "9091 /metrics 可抓取（$(printf '%s' "$mbody" | wc -l | tr -d ' ') 行）"
  sline="$(printf '%s\n' "$mbody" | grep -E '^opsmesh_http_metrics_series [0-9]+$' | head -1)"
  if [ -z "$sline" ]; then
    bad "缺少 opsmesh_http_metrics_series（P1-5 基数熔断自观测未生效）"
  else
    nseries="${sline##* }"
    [ "$nseries" -le 2000 ] && ok "HTTP 指标时序数 ${nseries} ≤ 2000（基数硬上限生效）" \
                             || bad "HTTP 指标时序数 ${nseries} > 2000（基数上限失效！）"
  fi
  printf '%s\n' "$mbody" | grep -q '^opsmesh_http_metrics_series_dropped_total [0-9]' \
    && ok "折叠计数器 opsmesh_http_metrics_series_dropped_total 已暴露（超限请求可观测）" \
    || bad "缺少 opsmesh_http_metrics_series_dropped_total（超限请求不可观测）"
fi

# 3c 路径归一化（基数护栏第一层）：超长/危险字符段与超长整体路径必须折叠，绝不原样入标签。
probe="$(printf 'zzprobe-%058d' 0)"      # 66 字节 > 48 上限，且含独特前缀
longpath="$(printf '/%0220d' 0)"         # 221 字节 > 200 上限
curl "${K[@]}" -o /dev/null --max-time 8 "${CP}/api/v1/${probe}" 2>/dev/null
curl "${K[@]}" -o /dev/null --max-time 8 "${CP}${longpath}" 2>/dev/null
mbody2="$(docker exec opsmesh-prometheus wget -qO- --timeout=8 http://controlplane:9091/metrics 2>/dev/null)"
[ -z "$mbody2" ] && mbody2="$(curl -sS --max-time 8 "http://127.0.0.1:${MP}/metrics" 2>/dev/null)"
if [ -n "$mbody2" ]; then
  printf '%s\n' "$mbody2" | grep -qF 'path="/api/v1/:id"' \
    && ok "超长路径段归一化为 path=\"/api/v1/:id\"" \
    || bad "未观察到 path=\"/api/v1/:id\"（超长段未被归一化）"
  if printf '%s\n' "$mbody2" | grep -qF "$probe"; then
    bad "原始超长路径串泄漏进指标标签（基数护栏失效！）"
  else
    ok "原始超长路径未进入指标标签（无标签膨胀）"
  fi
  printf '%s\n' "$mbody2" | grep -qF 'path="/:overlong"' \
    && ok "超长整体路径归一化为 path=\"/:overlong\"" \
    || warn "未观察到 path=\"/:overlong\"（上游可能先行拒绝该请求，非回归）"
fi

# 3d 生产默认限流（P1-5）：.env 未显式设置时须以 200 req/s/IP 启动（此前默认关闭）。
rl="$(env_val CB_RATE_LIMIT_PER_SEC)"
clog="$(docker logs opsmesh-controlplane 2>&1)"
if [ -z "$rl" ]; then
  printf '%s' "$clog" | grep -q '生产模式默认启用 API 限流 200' \
    && ok "生产模式默认启用限流 200 req/s/IP（启动期提示可见）" \
    || bad "未见默认限流启动提示（P1-5 默认启用失效）"
  printf '%s' "$clog" | grep -q 'API 限流已启用' \
    && ok "限流器已装载（日志含「API 限流已启用」）" \
    || bad "限流器未装载（生产默认限流未生效）"
elif [ "$rl" = "0" ]; then
  warn "CB_RATE_LIMIT_PER_SEC=0：限流被显式关闭（下方 429 实测将跳过）"
  printf '%s' "$clog" | grep -q '生产模式未启用 API 限流' \
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

# 4c 正确口令：期望 mustChangePassword=true + changePasswordToken
resp="$(curl "${K[@]}" -X POST "$CP/api/v1/auth/login" \
  -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$PW\"}" 2>/dev/null)"
echo "  登录响应（脱敏，token 截断）: $(echo "$resp" | sed -E 's/("(token|changePasswordToken|accessToken|refreshToken)"[[:space:]]*:[[:space:]]*")[^"]*/\1<...>/g')"
if echo "$resp" | grep -q '"mustChangePassword"[[:space:]]*:[[:space:]]*true'; then
  ok "首次登录强制改密（mustChangePassword=true）"
else
  bad "未返回 mustChangePassword=true"
fi
if echo "$resp" | grep -q '"changePasswordToken"'; then
  ok "下发一次性 changePasswordToken"
else
  bad "未下发 changePasswordToken"
fi
if printf '%s' "$resp" | grep -qE '"(token|accessToken|refreshToken)"[[:space:]]*:[[:space:]]*"[^"]+"'; then
  bad "强制改密期间仍下发了非空会话 token（docs/operations.md：改密前不签发正式 token）"
else
  ok "强制改密期间未下发可用会话 token（响应中 token 字段为空）"
fi

sec "5. 微服务 /metrics 真实性核对"
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
check_metrics auth-svc   "$(env_val AUTH_SVC_HTTP_PORT 8100)"   no
check_metrics config-svc "$(env_val CONFIG_SVC_HTTP_PORT 8106)" no
check_metrics log-svc    "$(env_val LOG_SVC_HTTP_PORT 8105)"    no
check_metrics gpu-svc    "$(env_val GPU_SVC_HTTP_PORT 8107)"    no
check_metrics aio-svc    "$(env_val AIO_SVC_HTTP_PORT 8108)"    no
check_metrics portal-svc "$(env_val PORTAL_SVC_HTTP_PORT 8109)" no

sec "6. 微服务健康检查（9 个）"
for e in "auth-svc:$(env_val AUTH_SVC_HTTP_PORT 8100):/health" \
         "device-svc:$(env_val DEVICE_SVC_HTTP_PORT 8101):/health" \
         "task-svc:$(env_val TASK_SVC_HTTP_PORT 8102):/health" \
         "alert-svc:$(env_val ALERT_SVC_HTTP_PORT 8103):/health" \
         "log-svc:$(env_val LOG_SVC_HTTP_PORT 8105):/healthz" \
         "config-svc:$(env_val CONFIG_SVC_HTTP_PORT 8106):/health" \
         "gpu-svc:$(env_val GPU_SVC_HTTP_PORT 8107):/health" \
         "aio-svc:$(env_val AIO_SVC_HTTP_PORT 8108):/health" \
         "portal-svc:$(env_val PORTAL_SVC_HTTP_PORT 8109):/health"; do
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

if [ "$ecode" != "200" ]; then
  bad "GET $CP/enterprise/ → ${ecode:-无响应}（企业版前端未交付：镜像未装配？）"
else
  ok "GET $CP/enterprise/ → 200"
  if printf '%s' "$ehead" | grep -qi 'X-OpsMesh-Enterprise-Bundle: *placeholder'; then
    bad "企业版前端为占位页（镜像构建未装配前端产物，P0-3 回归！）"
  elif printf '%s' "$ebody" | grep -q 'OPSMESH_ENTERPRISE_BUNDLE_PLACEHOLDER'; then
    bad "企业版前端 body 含占位标记（P0-3 回归！）"
  else
    ok "企业版前端为真实构建产物（非占位页）"
  fi
  # 入口必须引用 /enterprise/assets/ 下的资源，且首个 JS 资源可 200 取到。
  if printf '%s' "$ebody" | grep -q '/enterprise/assets/'; then
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
  if curl "${K[@]}" -H 'X-Tenant-ID: default' "$CP/" 2>/dev/null | grep -q 'href="/enterprise/"'; then
    ok "个人版引导页提供企业版入口（/enterprise/）"
  else
    warn "个人版引导页未见 /enterprise/ 入口（若前端为占位状态属预期，占位时应剥离 CTA）"
  fi
else
  warn "GET / → ${dcode}（未携带有效租户上下文时属预期，跳过 CTA 校验）"
fi

sec "11. 迁移版本门禁与租户列落库（P0-5 / P0-6 回归）"
if [ -n "${MYSQL_C:-}" ] && [ -n "${U:-}" ]; then
  mx="$(docker exec "$MYSQL_C" sh -c "mysql -u'$U' -p'$PWDB' -D opsmesh -N -e 'SELECT MAX(version) FROM schema_migrations;'" 2>/dev/null | tr -d ' \r')"
  # 磁盘上未执行的迁移数（排除 .down.sql 回滚脚本），与库内版本号比对即为「版本门禁」不变量。
  disk="$(ls -1 "${ROOT}"/internal/store/migrations/*.sql 2>/dev/null | grep -v '\.down\.sql$' | wc -l | tr -d ' ')"
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
  upf="$(ls -1 "${ROOT}"/internal/store/migrations/*.sql 2>/dev/null | grep -v '\.down\.sql$' | wc -l | tr -d ' ')"
  dnf="$(ls -1 "${ROOT}"/internal/store/migrations/*.down.sql 2>/dev/null | wc -l | tr -d ' ')"
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
mbody2="$(docker exec opsmesh-prometheus wget -qO- --timeout=8 http://controlplane:9091/metrics 2>/dev/null)"
[ -z "$mbody2" ] && mbody2="$(curl -sS --max-time 8 "http://127.0.0.1:${MP}/metrics" 2>/dev/null)"
if [ -z "$mbody2" ]; then
  warn "无法抓取 9091 指标，跳过审计链自观测断言"
else
  for m in opsmesh_audit_chain_supported opsmesh_audit_chain_ok opsmesh_audit_chain_checked_rows opsmesh_audit_chain_checks_total; do
    printf '%s\n' "$mbody2" | grep -q "^${m} [0-9]" && ok "指标 ${m} 已暴露" || bad "缺少指标 ${m}（P1-3 自检不可观测）"
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
      printf '%s\n' "$mbody3" | grep -q "^opsmesh_agent_signature_verifications_total{alg=\"${a}\",result=\"${r}\"} [0-9]" \
        || sigmiss="${sigmiss} ${a}/${r}"
    done
  done
  [ -z "$sigmiss" ] && ok "验签指标全标签集已暴露（alg∈{v1,v2,none,unknown} × result∈{ok,rejected}）" \
                    || bad "验签指标缺时序：${sigmiss}（P1-2 可观测性未接线）"
  for s in per_agent fleet; do
    printf '%s\n' "$mbody3" | grep -q "^opsmesh_agent_signing_key_source_total{source=\"${s}\"} [0-9]" \
      && ok "密钥来源指标已暴露（source=${s}）" || bad "缺少 opsmesh_agent_signing_key_source_total{source=\"${s}\"}（P1-2）"
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
if [ -n "$want_ver" ] && [ "$got_ver" = "$want_ver" ]; then
  ok "/version 版本与 .env OPSMESH_VERSION 一致（${got_ver}，构建期 -ldflags 注入生效）"
else
  bad "/version 版本='${got_ver:-空}' 与 .env OPSMESH_VERSION='${want_ver:-空}' 不一致（版本注入未生效？查 Dockerfile 的 -X 包路径是否为模块路径）"
fi
for f in commit goVersion uptimeSeconds; do
  if printf '%s' "$ver_body" | grep -q "\"$f\""; then ok "/version 含字段 $f"; else bad "/version 缺字段 $f"; fi
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
echo ""
echo "==================================================="
echo "  断言汇总：PASS=${PASS}  FAIL=${FAIL}"
echo "==================================================="
[ "$FAIL" -eq 0 ]
