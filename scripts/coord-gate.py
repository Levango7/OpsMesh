# coord-gate.py — 协作板门禁脚本（v2）
# 解析 docs/CLAIMS.md + docs/INBOX-*.md，把「当前状态」块回写进 docs/COORDINATION-BOARD.md。
# 退出码：0 干净 / 1 有未决项（issue/conflict/blocked）/ 2 结构错误。无三方依赖。
#
# 行为不变量（改判据前先想明白）：
# - 同 scope 存活 CLAIM 只许一个；DISPATCH 含 scope 时视为一次性 CLAIM（有效期到 ddl）。
# - CLAIM 过期无闭环 ⇒ issue；DISPATCH 过 ddl 无 STATUS/DONE ⇒ issue。
# - BLOCKED 未销号 ⇒ issue（lead 用 [NOTE] 或新 [DISPATCH] 裁定后由执行线 [STATUS] 销）。
# - 旧档案 docs/INBOX-ZCODE.md / docs/INBOX-LEVANGO.md 照常解析（历史行重放闭环）。
# - 必须在仓库根目录运行；读写目标固定为 docs/ 下的字面相对路径，realpath 不越界。

import re, sys, glob, datetime, os

CWD = os.path.realpath(".")


def guard(rel):
    """字面相对路径 + realpath 校验：拒绝 '..'，拒绝越出当前目录。"""
    parts = rel.replace("\\", "/").split("/")
    if ".." in parts or os.path.isabs(rel):
        print("::error::非法路径 %s" % rel)
        sys.exit(2)
    real = os.path.realpath(rel)
    if not real.startswith(CWD + os.sep):
        print("::error::路径越界 %s" % rel)
        sys.exit(2)
    return real


DOC = guard("docs/COORDINATION-BOARD.md")
if not os.path.exists(DOC):
    print("::error::请在仓库根目录运行（找不到 docs/COORDINATION-BOARD.md）")
    sys.exit(2)

LOGS = [guard("docs/CLAIMS.md")]
for name in ["docs/INBOX-ZCODE.md", "docs/INBOX-LEVANGO.md", "docs/INBOX-KILO.md",
             "docs/INBOX-WORKBUDDY.md", "docs/INBOX-QODER.md"]:
    p = guard(name)
    if os.path.exists(p):
        LOGS.append(p)
_INBOX_DIR = os.path.realpath("docs")
for extra in sorted(glob.glob(os.path.join(_INBOX_DIR, "INBOX-*.md"))):
    if extra not in LOGS:
        LOGS.append(extra)

KINDS = "CLAIM|RELEASE|DONE|TAKEOVER|CONFLICT|DISPATCH|STATUS|BLOCKED|NOTE|ROLE"
LINE = re.compile(r"^\[(%s)\]\s+(\S+)\s+(.*)$" % KINDS)


def norm(t):
    return t.replace("\ufeff", "").replace("\r\n", "\n").replace("\r", "\n")


def read(p):
    if not os.path.exists(p):
        return ""
    return norm(open(p, encoding="utf-8").read())


def parse_ts(ts):
    try:
        return (
            datetime.datetime.strptime(ts, "%Y-%m-%dT%H:%M:%SZ")
            .replace(tzinfo=datetime.timezone.utc)
            .timestamp()
        )
    except Exception:
        return 0.0


def kv(blob):
    out = {}
    for tok in blob.split():
        if "=" in tok:
            k, v = tok.split("=", 1)
            out[k] = v
    return out


def ttl_hours(blob):
    m = re.search(r"ttl=(\d+)h", blob)
    return int(m.group(1)) if m else 0


NOW = datetime.datetime.now(datetime.timezone.utc).timestamp()

claims = {}        # scope -> {who, ts, ttl_h, state, src, ddl_ts, ddl, by}
conflicts = set()
tasks = {}         # task id -> {to, by, ddl, ddl_ts, state, note, ts}
blocked = {}       # task id -> reason
status_notes = {}  # task id -> (ts, note)
lead = "?"

# 先收集全部事件，按时间戳全局重放——跨文件的动作必须按时序合并，
# 否则旧档案 INBOX-*（解析序靠后）里的过期 CLAIM 关不上 CLAIMS.md 里的 RELEASE。
events = []
for path in LOGS:
    for line in read(path).splitlines():
        m = LINE.match(line)
        if not m:
            continue
        kind, ts, blob = m.groups()
        events.append((parse_ts(ts), kind, ts, blob))
events.sort(key=lambda e: e[0])

for _, kind, ts, blob in events:
        f = kv(blob)
        who = f.get("from", "?")
        scope = f.get("scope", "")
        task = f.get("task", "")
        t = parse_ts(ts)

        if kind == "ROLE":
            if "lead" in f:
                lead = f["lead"]
            continue
        if kind == "NOTE":
            continue

        if kind in ("CLAIM", "DISPATCH") and scope:
            ddl = f.get("ddl", "")
            record = {
                "who": who if kind == "CLAIM" else f.get("to", "?"),
                "ts": t,
                "ttl_h": ttl_hours(blob) if kind == "CLAIM" else 0,
                "state": "open",
                "src": kind,
                "ddl_ts": parse_ts(ddl) if ddl else 0.0,
                "ddl": ddl,
                "by": who,
            }
            if scope in claims and claims[scope]["state"] == "open":
                conflicts.add(scope)
            claims[scope] = record

        if kind in ("RELEASE", "DONE", "TAKEOVER") and scope:
            if scope in claims:
                claims[scope]["state"] = "taken" if kind == "TAKEOVER" else "closed"

        if kind == "DISPATCH" and task:
            tasks[task] = {
                "to": f.get("to", "?"),
                "by": who,
                "ddl": f.get("ddl", ""),
                "ddl_ts": parse_ts(f.get("ddl", "")) if f.get("ddl") else 0.0,
                "state": "open",
                "note": f.get("note", "")[:60],
                "ts": t,
            }
        elif kind == "STATUS" and task:
            status_notes[task] = (t, f.get("note", "")[:60])
        elif kind == "BLOCKED" and task:
            blocked[task] = f.get("reason", "?")[:60]
            if task in tasks:
                tasks[task]["state"] = "blocked"
        elif kind in ("DONE", "RELEASE") and task:
            if task in tasks:
                tasks[task]["state"] = "done"
            blocked.pop(task, None)

# --- 渲染状态块 ---

active, issues = [], []
for scope, c in sorted(claims.items()):
    if c["state"] == "taken":
        issues.append("已被接管：%s（原认领 %s）" % (scope, c["who"]))
    elif c["state"] == "closed":
        continue
    elif c["src"] == "DISPATCH":
        if c["ddl_ts"] and NOW > c["ddl_ts"]:
            issues.append("DISPATCH 过 ddl 未闭环：%s <- %s（ddl %s）" % (scope, c["who"], c["ddl"] or "?"))
        else:
            active.append("  %-46s <- %s  (dispatch by %s, ddl %s)" % (scope, c["who"], c["by"], c["ddl"] or "-"))
    else:
        if c["ttl_h"] and c["ts"] and (NOW - c["ts"]) > c["ttl_h"] * 3600:
            issues.append("CLAIM 过期未闭环：%s（认领人 %s，ttl %dh）" % (scope, c["who"], c["ttl_h"]))
        else:
            active.append("  %-46s <- %s  (ttl=%dh by %s)" % (scope, c["who"], c["ttl_h"], c["by"]))

for s in list(conflicts):
    if claims.get(s, {}).get("state") != "open":
        conflicts.discard(s)

task_lines = []
for tid, k in sorted(tasks.items()):
    if k["state"] == "done":
        continue
    extra = ""
    if tid in status_notes:
        extra = " last='%s'" % status_notes[tid][1]
    task_lines.append("  %s <- %s  [%s] ddl=%s note=%s%s" % (tid, k["to"], k["state"], k["ddl"] or "-", k["note"], extra))
    if k["state"] == "open" and k["ddl_ts"] and NOW > k["ddl_ts"]:
        issues.append("任务过 ddl 未闭环：%s <- %s（ddl %s）" % (tid, k["to"], k["ddl"]))

for tid, reason in sorted(blocked.items()):
    issues.append("BLOCKED 未销号：%s（%s）" % (tid, reason))

block = ["LEAD: %s" % lead, "ACTIVE CLAIMS:"]
block.append("\n".join(active) if active else "  (无)")
block.append("")
block.append("TASKS:")
block.append("\n".join(task_lines) if task_lines else "  (无)")
if issues:
    block += ["", "ISSUES:"] + ["  " + i for i in issues]
if conflicts:
    block.append("")
    block.append("CONFLICT: 同 scope 重复认领：" + ", ".join(sorted(conflicts)))
block_txt = "\n".join(block)

doc = read(DOC)
m = re.search(r"```\nLEAD:.*?```", doc, re.S)
if not m:
    m = re.search(r"```\nACTIVE CLAIMS:.*?```", doc, re.S)
if not m:
    m = re.search(r"```\n（待门禁脚本首次回写）\n```", doc, re.S)
if not m:
    print("::error::docs/COORDINATION-BOARD.md 未找到状态块")
    sys.exit(2)
with open(DOC, "w", encoding="utf-8", newline="\n") as fh:
    fh.write(doc[: m.start()] + "```\n" + block_txt + "\n```" + doc[m.end():])

bad = len(issues) + len(conflicts) + len(blocked)
print(
    "coord-gate: lead=%s active=%d tasks=%d issues=%d conflict=%d blocked=%d"
    % (lead, len(active), len(task_lines), len(issues), len(conflicts), len(blocked))
)
sys.exit(1 if bad else 0)
