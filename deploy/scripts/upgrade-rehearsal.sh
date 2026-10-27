#!/usr/bin/env bash
# upgrade-rehearsal.sh — 存量库升级演练（切版前置；首次实跑 v0.16.0 → main，2026-10-27）
#
# 它回答三件"发布前必须知道、静态检查看不见"的问题：
#   A. 存量库（旧版本二进制建的库 + 有数据）能不能被新二进制的迁移安全推上去？数据有没有丢？
#   B. 升级后的库与全新安装的库**结构是否等价**？不等价 = 私有化交付里两条路径行为不同。
#   C. 迁移期间的 DDL 会不会把生产写入卡住？（升级前长事务体检）
#
# 判据取向：落在"结构等价 + 指纹不变 + 重放零 DDL + 无持锁事务"这四条通用不变式上，
# 不硬编码迁移文件名——以后新增迁移不需要改本脚本。
# 每个判据都抽成 judge_* 函数，--selftest 用红/绿两类样本走**同一个函数**，
# 防止"判据永远命中不了"的空转门（空转门比没有门更糟）。
#
# 退出码：0=全部通过；1=有判红；2=前置条件缺失（未取证 ≠ 干净）。
set -uo pipefail

PASS=0 FAIL=0 SKIP=0
# JUDGE_QUIET=1 时判据只记结论不记账——--selftest 用它来断言"该红的红、该绿的绿"，
# 否则自测里故意造的红样本会被当成演练本身的失败。
JUDGE_QUIET=0 LAST_VERDICT=""
ok()  { LAST_VERDICT=green; [[ $JUDGE_QUIET -eq 1 ]] && return 0; echo "  [PASS] $*"; PASS=$((PASS+1)); }
bad() { LAST_VERDICT=red;   [[ $JUDGE_QUIET -eq 1 ]] && return 0; echo "  [FAIL] $*"; FAIL=$((FAIL+1)); }
skip() { echo "  [SKIP] $*"; SKIP=$((SKIP+1)); }
note() { echo "  · $*"; }
# detail：判红时的补充明细，自测静音时一起压掉。
detail() { [[ $JUDGE_QUIET -eq 1 ]] || echo "        $*"; }

usage() {
    cat <<'EOF'
用法：
  OPSMESH_LEGACY_BIN=<旧版二进制> OPSMESH_HEAD_BIN=<待发布二进制> \
  BASE_DSN='user:pass@tcp(host:port)/' \
  bash deploy/scripts/upgrade-rehearsal.sh [--rows N] [--db-legacy NAME] [--db-fresh NAME] [--keep] [--selftest]

  BASE_DSN    以 "/" 结尾的管理 DSN（脚本自行拼库名，不带库名）
  --rows N    audit_log 种子行数（默认 100000；让 DDL 在有量的表上真跑一次）
  --keep      跑完不删两个演练库（默认删除）
  --selftest  只跑判据自测（红/绿样本 + SQL 列名可解析），不需要两个二进制
  MYSQL_CLI   覆盖取数命令，默认 "mysql -N"；容器场景例：
              MYSQL_CLI='docker exec -i mysql-ctr mysql -uroot -ppw -N'
EOF
}

# ------------------------------------------------------------------ 判据（四个）
# judge_trx <最大事务龄(秒)> <在跑事务数>
# 长事务会让迁移的 ALTER 排队，而排队的 ALTER 会连锁阻塞后续写入。
# 只用 trx_started 时刻差：trx_timer_wait 在 MySQL 8.4 已被移除，写死那种列名会让查询报错、
# 取到空串、被当成"没有长事务"——门就空转判绿了。
# 阈值取 1s 而非更大：实测**同一张表**上有 1–2s 龄的事务时，migrate 的 ALTER 就排队，
# 排队期间该表后续写入一起被阻塞（10 条 tasks 写入实测 23.0s）。
# 年龄只是代理指标——刚开的事务也可能跑几小时，真正的前提是"业务空闲"。
judge_trx() {
    local age="$1" n="$2"
    if ! [[ "$age" =~ ^-?[0-9]+$ ]]; then
        bad "读不到在跑事务龄期（取到 '${age:-空}'）⇒ 本项前置体检没有射程，判红而不是跳过"
    elif [[ "$age" -ge 1 ]]; then
        bad "存在已开 ${age}s 的事务（数=${n}）⇒ 迁移的 ALTER 会等元数据锁，等待期间**该表后续写入一起卡住**（同表实测：10 条 tasks 写入耗时 23.0s）。升级要在业务空闲窗口做，或先停控制面。定位：select trx_mysql_thread_id, trx_query from information_schema.innodb_trx。本项是全局口径，未落在被改表上的事务属保守误报，可用 performance_schema.metadata_locks 精确核对。"
    else
        ok "无 ≥1s 的在跑事务（数=${n}、最老 ${age}s）⇒ ALTER 不会因 MDL 排队卡写入"
    fi
}

# judge_data <前行数> <前指纹> <后行数> <后指纹>
# 迁移只该加列/加索引，不该动行。
judge_data() {
    local bn="$1" bf="$2" an="$3" af="$4"
    if [[ "$bn" == "$an" && "$bf" == "$af" ]]; then
        ok "迁移未动数据：audit_log 行数与 CRC 指纹前后一致（${an} 行）"
    else
        bad "迁移改变了 audit_log 数据！行数 ${bn}→${an}、指纹 ${bf}→${af}"
    fi
}

# judge_equiv <定义漂移> <仅新装有> <仅升级有> <索引只在一侧>
# 通用不变式：升级后的库 ≡ 全新安装的库。
judge_equiv() {
    local dcols="$1" only_new="$2" only_old="$3" diff_idx="$4"
    if [[ -z "$dcols" && -z "$only_new" && -z "$only_old" && -z "$diff_idx" ]]; then
        ok "升级库与新装库结构等价（列定义、列存在性、索引集合全一致）"
    else
        bad "升级路径与新装路径结构不等价 ⇒ 私有化交付里'老客户升级'与'新客户安装'会行为不同"
        [[ -n "$dcols" ]]   && while IFS= read -r l; do detail "定义漂移 $l"; done <<<"$dcols"
        [[ -n "$only_new" ]] && while IFS= read -r l; do detail "仅新装有此列 $l（升级库缺）"; done <<<"$only_new"
        [[ -n "$only_old" ]] && while IFS= read -r l; do detail "仅升级库有此列 $l（新装没有）"; done <<<"$only_old"
        [[ -n "$diff_idx" ]] && while IFS= read -r l; do detail "索引只在一侧存在 $l"; done <<<"$diff_idx"
    fi
}

# judge_replay <rc> <本次应用条数>
judge_replay() {
    local rc="$1" applied="$2"
    if [[ "$rc" -eq 0 && "$applied" == "0" ]]; then
        ok "重放零 DDL（rc=0，本次未应用任何迁移）"
    else
        bad "重放不幂等：rc=$rc、本次又应用了 ${applied} 条"
    fi
}

# ------------------------------------------------------------------ 判据自测
S_PASS=0 S_FAIL=0
# expect_verdict <期望 green|red> <样本名> <判据函数> [参数...]
# 判据在 JUDGE_QUIET 下只记结论不记账，样本只断言"结论是否符合期望"——
# 这样故意造的红样本不会被误算成演练自身的失败，而"判据永远命中不了"会当场显形。
expect_verdict() {
    local want="$1" desc="$2"; shift 2
    LAST_VERDICT=""; JUDGE_QUIET=1
    "$@"
    JUDGE_QUIET=0
    if [[ "$LAST_VERDICT" == "$want" ]]; then
        S_PASS=$((S_PASS+1))
    else
        S_FAIL=$((S_FAIL+1)); echo "  [FAIL] 样本「$desc」期望判 $want、实得 ${LAST_VERDICT:-无判定} ⇒ 判据空转或误伤"
    fi
}

selftest() {
    S_PASS=0; S_FAIL=0
    echo "== 判据自测（每条判据既要有红样本、也要有绿样本防误伤）=="
    expect_verdict green "trx 无事务"              judge_trx -1 0
    expect_verdict green "trx 3 个刚开的事务"        judge_trx 0 3
    expect_verdict red   "trx 恰好 1s（阈值边界）"   judge_trx 1 1
    expect_verdict red   "trx 4s（同表事务实测会挡住 ALTER）" judge_trx 4 1
    expect_verdict red   "trx 25s 长事务"          judge_trx 25 1
    expect_verdict red   "trx 取到空串"            judge_trx "" 0
    expect_verdict red   "trx 查询报错"            judge_trx "ERROR" 0
    expect_verdict green "data 前后一致"           judge_data 100 42 100 42
    expect_verdict red   "data 少一行"             judge_data 100 42 99 42
    expect_verdict red   "data 行数不变但指纹变"    judge_data 100 42 100 43
    expect_verdict green "equiv 四项全空"          judge_equiv "" "" "" ""
    expect_verdict red   "equiv 列定义漂移"        judge_equiv "tasks.x [a|b]" "" "" ""
    expect_verdict red   "equiv 升级库缺列"        judge_equiv "" "tasks.y" "" ""
    expect_verdict red   "equiv 新装没有的列"      judge_equiv "" "" "tasks.z" ""
    expect_verdict red   "equiv 索引单边存在"      judge_equiv "" "" "" "tasks.idx"
    expect_verdict green "replay rc0 且零应用"     judge_replay 0 0
    expect_verdict red   "replay 又应用 6 条"      judge_replay 0 6
    expect_verdict red   "replay rc1"              judge_replay 1 0

    # SQL 面自证：判据读的列必须在目标服务器上解析成功。列名被上游删掉时查询报错、取到空串，
    # 会被误读成"没有长事务"——那正是空转门的成因，所以单独自证一次。
    local probe
    probe="$(sql_nopw information_schema "select ifnull(max(timestampdiff(second, trx_started, now(3))), -1) from information_schema.innodb_trx")"
    if [[ "$probe" =~ ^-?[0-9]+$ ]]; then
        S_PASS=$((S_PASS+1)); note "事务龄期查询在当前 MySQL 上可解析（取到 $probe）"
    else
        S_FAIL=$((S_FAIL+1)); echo "  [FAIL] 事务龄期查询解析失败（取到 '${probe:-空}'）⇒ 判据在这个 MySQL 版本上会空转"
    fi
    echo "  判据自测：通过=$S_PASS 失败=$S_FAIL（18 个红/绿样本 + 1 个 SQL 可解析性自证）"
    [[ "$S_FAIL" -eq 0 ]]
}

# ------------------------------------------------------------------ 参数
LEGACY="${OPSMESH_LEGACY_BIN:-}"; HEAD="${OPSMESH_HEAD_BIN:-}"; BASE_DSN="${BASE_DSN:-}"
ROWS=100000 DB_LEGACY=opsmesh_rehearse_legacy DB_FRESH=opsmesh_rehearse_fresh KEEP=0 SELFTEST=0
MYSQL_CLI="${MYSQL_CLI:-mysql -N}"
# 迁移本身必须有上限：等元数据锁时它是**无限期**的（见 ①），门不能自己挂死。
MIG_TO="${OPSMESH_REHEARSE_MIGRATE_TIMEOUT:-180}"
mig() { timeout "$MIG_TO" "$@"; }
while [[ $# -gt 0 ]]; do
    case "$1" in
        --rows) ROWS="$2"; shift 2 ;;
        --db-legacy) DB_LEGACY="$2"; shift 2 ;;
        --db-fresh) DB_FRESH="$2"; shift 2 ;;
        --keep) KEEP=1; shift ;;
        --selftest) SELFTEST=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "未知参数：$1" >&2; usage; exit 2 ;;
    esac
done

sql() { # sql <db> <statement>：取文本，错误也当文本返回（供判据识别"没射程"）
    local db="$1"; shift
    # MYSQL_CLI 故意按词拆分：它是调用方给的命令前缀，不是数据。
    # shellcheck disable=SC2086
    $MYSQL_CLI "$db" -e "$*" 2>&1 | grep -v 'Using a password'
}
sql_nopw() { # 只要 stdout（调用方自己保证解析失败会被判红，不靠这里吞错）
    local db="$1"; shift
    # shellcheck disable=SC2086
    $MYSQL_CLI "$db" -e "$*" 2>/dev/null | grep -v 'Using a password'
}
dsn() { printf '%s%s?parseTime=true' "$BASE_DSN" "$1"; }

if [[ $SELFTEST -eq 1 ]]; then
    [[ -z "$BASE_DSN" ]] && { echo "--selftest 也需要 BASE_DSN（最后一项要真连库自证 SQL 可解析）" >&2; exit 2; }
    selftest; exit $?
fi

for v in LEGACY HEAD BASE_DSN; do
    [[ -z "${!v}" ]] && { echo "缺少 $v（见 --help）" >&2; usage; exit 2; }
done
for b in "$LEGACY" "$HEAD"; do
    [[ -f "$b" ]] || { echo "二进制不存在：$b" >&2; exit 2; }
done

echo "== 存量库升级演练 =="
echo "  旧版=$LEGACY  待发布=$HEAD  audit_log 种子=$ROWS 行"

# 判据自测在真跑前也走一遍：坏判据不该有机会去"验证"发布。
selftest || { echo "判据自测未过，拒绝继续（避免拿空转门的结论去切版）" >&2; exit 1; }

# ---------------------------------------------------------------- ① 环境与前置体检
echo; echo "① 前置体检（连接性 / 长事务 / MDL 超时口径）"
VER="$(sql_nopw information_schema "select version()")"
if [[ -z "$VER" ]]; then
    echo "  [FAIL] 连不上 MySQL（MYSQL_CLI=$MYSQL_CLI）——判红而不是跳过" >&2
    exit 2
fi
ok "MySQL 可达：$VER"
judge_trx "$(sql_nopw information_schema "select ifnull(max(timestampdiff(second, trx_started, now(3))), -1) from information_schema.innodb_trx")" \
          "$(sql_nopw information_schema "select count(*) from information_schema.innodb_trx")"
LWT="$(sql_nopw information_schema "select @@global.lock_wait_timeout")"
note "全局 lock_wait_timeout=${LWT:-未取到} 秒；迁移连接若不显式设小，DDL 等锁是**无限期**的（MySQL 默认 31536000=1 年）"

# ---------------------------------------------------------------- ② 造存量库
echo; echo "② 用旧版二进制建存量库（模拟客户已有库）并灌数据"
sql information_schema "drop database if exists $DB_LEGACY; create database $DB_LEGACY" >/dev/null
T0=$(date +%s)
mig "$LEGACY" migrate --store=mysql --mysql-dsn="$(dsn "$DB_LEGACY")" > /tmp/rehearse-legacy.log 2>&1
LRC=$?; LT=$(($(date +%s) - T0))
if [[ $LRC -ne 0 ]]; then
    echo "  [FAIL] 旧版迁移失败（rc=$LRC），演练无法继续" >&2
    tail -5 /tmp/rehearse-legacy.log >&2; exit 1
fi
LV="$(sql_nopw "$DB_LEGACY" "select ifnull(max(version),-1) from schema_migrations")"
ok "存量库建成：水位=$LV，旧版迁移耗时 ${LT}s"

if [[ "$(sql_nopw "$DB_LEGACY" "select count(*) from information_schema.tables where table_schema='$DB_LEGACY' and table_name='audit_log'")" == "1" ]]; then
    sql "$DB_LEGACY" "SET cte_max_recursion_depth = $((ROWS + 1000)); INSERT INTO audit_log (id, tenant_id, user_id, action, target, detail, created_at, entry_hash, prev_hash) WITH RECURSIVE s(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM s WHERE n < $ROWS) SELECT n + 5000000, 'default', CONCAT('u', n % 37), 'task_create', CONCAT('/api/v1/tasks/', n), CONCAT('seed-', n), NOW() - INTERVAL n SECOND, SHA2(CONCAT('e', n), 256), SHA2(CONCAT('p', n - 1), 256) FROM s;" >/dev/null
    AN="$(sql_nopw "$DB_LEGACY" "select count(*) from audit_log")"
    AF="$(sql_nopw "$DB_LEGACY" "select ifnull(sum(crc32(concat(id, action, target, entry_hash))),0) from audit_log")"
    ok "种子数据就位：audit_log=${AN} 行 指纹=${AF}"
else
    AN=""; AF=""; skip "audit_log 不存在，未灌数据 ⇒ ③④ 的 DDL 是在空表上跑的，别把它当'有量验证'"
fi

# ---------------------------------------------------------------- ③ 升级到待发布版
echo; echo "③ 用待发布二进制把存量库推上去（计时）"
T0=$(date +%s)
mig "$HEAD" migrate --store=mysql --mysql-dsn="$(dsn "$DB_LEGACY")" > /tmp/rehearse-up.log 2>&1
URC=$?; UT=$(($(date +%s) - T0))
APPLIED="$(grep -cE '已应用' /tmp/rehearse-up.log)"
TOLERATED="$(grep -cE '幂等放行' /tmp/rehearse-up.log)"
if [[ $URC -ne 0 ]]; then
    bad "存量库升级失败（rc=$URC）"; tail -8 /tmp/rehearse-up.log
else
    ok "升级成功：新应用 ${APPLIED} 条、幂等放行 ${TOLERATED} 条（撞重复列/索引属预期，靠 information_schema 二次核实放行），耗时 ${UT}s"
fi
HV="$(sql_nopw "$DB_LEGACY" "select ifnull(max(version),-1) from schema_migrations")"
if [[ "$HV" != "$LV" ]]; then
    ok "水位推进 $LV → $HV"
else
    note "水位未变（$HV）——存量库已是目标结构（新应用 $APPLIED 条）"
fi
if [[ -n "$AF" ]]; then
    judge_data "$AN" "$AF" "$(sql_nopw "$DB_LEGACY" "select count(*) from audit_log")" \
                         "$(sql_nopw "$DB_LEGACY" "select ifnull(sum(crc32(concat(id, action, target, entry_hash))),0) from audit_log")"
fi

# ---------------------------------------------------------------- ④ 通用不变式：升级 == 新装
echo; echo "④ 结构等价性：升级后的库 vs 全新安装的库"
sql information_schema "drop database if exists $DB_FRESH; create database $DB_FRESH" >/dev/null
mig "$HEAD" migrate --store=mysql --mysql-dsn="$(dsn "$DB_FRESH")" > /tmp/rehearse-fresh.log 2>&1
FRC=$?
if [[ $FRC -ne 0 ]]; then
    bad "全新安装迁移失败（rc=$FRC），等价性无从对照"; tail -5 /tmp/rehearse-fresh.log
else
    DIFF_COLS="$(sql_nopw information_schema "
        select concat(c.table_name,'.',c.column_name,' [升级库=',c.column_type,
                      if(c.is_nullable='YES','?','!'), ' def=', ifnull(c.column_default,'NULL'),
                      ' | 新装=', f.column_type, if(f.is_nullable='YES','?','!'),
                      ' def=', ifnull(f.column_default,'NULL'), ']')
        from information_schema.columns c
        join information_schema.columns f
          on f.table_schema='$DB_FRESH' and f.table_name=c.table_name and f.column_name=c.column_name
        where c.table_schema='$DB_LEGACY'
          and concat(c.column_type,c.is_nullable,ifnull(c.column_default,'~'))
           <> concat(f.column_type,f.is_nullable,ifnull(f.column_default,'~'))
        group by 1 order by 1")"
    ONLY_NEW="$(sql_nopw information_schema "
        select concat(f.table_name,'.',f.column_name) from information_schema.columns f
        left join information_schema.columns c
          on c.table_schema='$DB_LEGACY' and c.table_name=f.table_name and c.column_name=f.column_name
        where f.table_schema='$DB_FRESH' and c.column_name is null group by 1")"
    ONLY_OLD="$(sql_nopw information_schema "
        select concat(c.table_name,'.',c.column_name) from information_schema.columns c
        left join information_schema.columns f
          on f.table_schema='$DB_FRESH' and f.table_name=c.table_name and f.column_name=c.column_name
        where c.table_schema='$DB_LEGACY' and f.column_name is null group by 1")"
    DIFF_IDX="$(sql_nopw information_schema "
        select concat(x.tbl,'.',x.idx) from (
          select t.table_schema sch, t.table_name tbl, s.index_name idx
          from information_schema.tables t join information_schema.statistics s
            on s.table_schema=t.table_schema and s.table_name=t.table_name
          where t.table_schema in ('$DB_LEGACY','$DB_FRESH') and t.table_type='BASE TABLE'
          group by t.table_schema, t.table_name, s.index_name) x
        group by x.tbl, x.idx having count(distinct x.sch) = 1 order by 1")"
    judge_equiv "$DIFF_COLS" "$ONLY_NEW" "$ONLY_OLD" "$DIFF_IDX"
fi

# ---------------------------------------------------------------- ⑤ 幂等重放
echo; echo "⑤ 幂等重放（升级必须可重复执行）"
mig "$HEAD" migrate --store=mysql --mysql-dsn="$(dsn "$DB_LEGACY")" > /tmp/rehearse-replay.log 2>&1
RRC=$?
judge_replay "$RRC" "$(grep -cE '已应用' /tmp/rehearse-replay.log)"

# ---------------------------------------------------------------- 收尾
if [[ $KEEP -eq 0 ]]; then
    sql information_schema "drop database if exists $DB_LEGACY; drop database if exists $DB_FRESH" >/dev/null
fi
echo; echo "==================================================="
echo "  存量库升级演练：PASS=$PASS  FAIL=$FAIL  SKIP=$SKIP"
echo "  SUMMARY legacy=$LV upgraded=$HV ddl_s=${UT}s rows=${AN:-0} tolerated=${TOLERATED:-0}"
echo "==================================================="
[[ "$FAIL" -eq 0 ]]
