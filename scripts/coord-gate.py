import re, sys, datetime, os

os.chdir(r"F:\Agent\kilo\workspace\OpsMesh")

DOC = "docs/COORDINATION-BOARD.md"
INBOXES = ["docs/INBOX-ZCODE.md", "docs/INBOX-LEVANGO.md"]

def norm(t):
    return t.replace('\ufeff', '').replace('\r\n', '\n').replace('\r', '\n')

def read(p):
    if not os.path.exists(p):
        print("::error::missing %s" % p); sys.exit(2)
    return norm(open(p, encoding="utf-8").read())

def parse(ts):
    try:
        return datetime.datetime.strptime(ts, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=datetime.timezone.utc).timestamp()
    except Exception:
        return 0

claims, conflicts = {}, set()
now = datetime.datetime.now(datetime.timezone.utc).timestamp()
# 字段顺序自由：from/scope/ttl/reason 以 key=value 成对出现，顺序不限
# （第一版把 from→scope 写成紧邻，被 ttl= 夹在中间的真实留言打脸）
ONE = re.compile(r'^\[(CLAIM|RELEASE|DONE|TAKEOVER)\]\s+(\S+)\s+(.*)$')


def kv(blob):
    out = {}
    for tok in blob.split():
        if "=" in tok:
            k, v = tok.split("=", 1)
            out[k] = v
    return out

for path in INBOXES:
    for line in read(path).splitlines():
        m = ONE.match(line)
        if not m: continue
        kind, ts, blob = m.groups()
        f = kv(blob)
        who, scope = f.get("from", "?"), f.get("scope", "?")
        if kind == "CLAIM":
            ttl = re.search(r'ttl=(\d+)h', blob)
            if scope in claims and claims[scope]["state"] == "open":
                conflicts.add(scope)
            claims[scope] = {"who": who, "ts": parse(ts),
                             "ttl_h": int(ttl.group(1)) if ttl else 0, "state": "open"}
        else:
            if scope in claims:
                claims[scope]["state"] = "taken" if kind == "TAKEOVER" else "closed"

active, issues = [], []
for scope, c in sorted(claims.items()):
    if c["state"] == "taken":
        issues.append("taken-over: %s (was %s)" % (scope, c["who"]))
    elif c["state"] == "closed":
        continue
    elif c["ttl_h"] and c["ts"] and (now - c["ts"]) > c["ttl_h"] * 3600:
        issues.append("CLAIM expired: %s (by %s, ttl %dh)" % (scope, c["who"], c["ttl_h"]))
    else:
        active.append("  %-46s <- %s  (ttl=%dh)" % (scope, c["who"], c["ttl_h"]))
for s in list(conflicts):
    if claims.get(s, {}).get("state") != "open":
        conflicts.discard(s)

block = ["ACTIVE CLAIMS:", ("\n".join(active)) if active else "  (none)"]
if issues: block += ["", "ISSUES:"] + ["  " + i for i in issues]
if conflicts: block += ["", "CONFLICT: duplicate CLAIM on: " + ", ".join(sorted(conflicts))]
block = "\n".join(block)

doc = read(DOC)
m = re.search(r'```\nACTIVE CLAIMS:.*?```', doc, re.S)
if not m:
    print("::error::status block not found"); sys.exit(2)
open(DOC, "w", encoding="utf-8").write(doc[:m.start()] + "```\n" + block + "\n```" + doc[m.end():])
bad = len(issues) + len(conflicts)
print("coord-gate: active=%d issues=%d conflict=%d" % (len(active), len(issues), len(conflicts)))
sys.exit(1 if bad else 0)
