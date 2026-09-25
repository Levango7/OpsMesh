#!/usr/bin/env bash
# OpsMesh 运行时黑盒断言（deploy.sh up 之后跑，只读、不改动被测系统）
#
# 用途：部署完成后的独立复验。与 deploy.sh 自带冒烟测试相互独立（不复用其函数/变量），
#       覆盖 P0-1 鉴权链路、P0-2 明文 HTTP、P0-3 企业版前端内置、P0-4 端口发布真实性、
#       P0-5 迁移版本门禁、P0-6 租户列落库、P0-7 持久化落库、监控假告警、多库隔离等
#       回归断言。任一 FAIL 即退出码非 0。
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

sec "3. 公开 /metrics（无鉴权）—— 已知发现项复核"
mcode="$(curl "${K[@]}" -o /dev/null -w '%{http_code}' "$CP/metrics" 2>/dev/null)"
if [ "$mcode" = "200" ]; then
  warn "GET $CP/metrics 无需认证即返回 200（聚合计数对外可见）"
  echo "  ---- 前 12 行 ----"
  curl "${K[@]}" "$CP/metrics" 2>/dev/null | head -12 | sed 's/^/    /'
  echo "  ---- 是否含敏感聚合指标 ----"
  curl "${K[@]}" "$CP/metrics" 2>/dev/null | grep -E "^opsmesh_(devices|tasks|alerts|tickets)" | head -8 | sed 's/^/    /'
else
  ok "GET $CP/metrics → ${mcode}（未公开）"
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

echo ""
echo "==================================================="
echo "  断言汇总：PASS=${PASS}  FAIL=${FAIL}"
echo "==================================================="
[ "$FAIL" -eq 0 ]
