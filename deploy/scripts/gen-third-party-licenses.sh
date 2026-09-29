#!/usr/bin/env bash
# gen-third-party-licenses.sh — 生成 / 校验 docs/third-party-licenses.md
#
# 目的：把"第三方依赖用了什么许可证"从主观描述变成**可机器复算的事实**。
#   - 完全离线：依赖清单来自各模块的 go.sum（不依赖代理或模块图解析），
#     许可证文本来自本地 Go 模块缓存（`go mod download` 之后必然存在）。
#   - 只做**分类与暴露**，不做**可否分发的判定**：GPL/LGPL/MPL/UNKNOWN 一律进
#     "需法务确认"段——商用可交付性属商务+法务决定，脚本不越权替它签字。
#
# 用法：
#   bash deploy/scripts/gen-third-party-licenses.sh            # 生成/覆盖 docs/third-party-licenses.md
#   bash deploy/scripts/gen-third-party-licenses.sh --check    # 只比对：与已提交文档不一致即 rc=1（CI 用）
#   bash deploy/scripts/gen-third-party-licenses.sh --emit-notice  # 汇编仓库根 NOTICE（Apache-2.0 §4(d) 上游署名）
#
# --check 为什么**不**做全文 diff（第一版做了，随即改掉）：
#   许可证识别依赖本地 Go 模块缓存，CI 的缓存必然比开发机残缺 → 同一份代码在 CI 会多出
#   NO-SOURCE 行，全文 diff 得到的是**假红**；反过来把 NO-SOURCE 整段忽略又会变成
#   「CI 读不到就当作已确认」的**空转绿**。故改成三条与环境无关的硬断言：
#     ① 清单集合相等（模块@版本增删改一律判红——版本号变了集合就变，这是主要的漂移形态）；
#     ② 两侧都读到 LICENSE 文本的模块，许可证必须一致；
#     ③ 本轮读不到源码的模块数超过 MIN_COVERAGE_PCT 允许的下限即判红（等于没检查就明说）。
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "$ROOT" || exit 1

OUT="docs/third-party-licenses.md"
MODE="${1:-gen}"
MIN_COVERAGE_PCT="${MIN_COVERAGE_PCT:-90}"
TMP="$(mktemp)"

PY=""
for _c in python3 python; do
    if command -v "$_c" >/dev/null 2>&1; then PY="$_c"; break; fi
done
if [ -z "$PY" ]; then
    echo "需要 python3 或 python（与 validate-deploy-assets.sh 同一依赖口径）"
    exit 2
fi
CHECK_MODE=gen
case "$MODE" in
    --check) CHECK_MODE=check ;;
    --emit-notice) CHECK_MODE=notice ;;
esac
"$PY" - "$TMP" "$CHECK_MODE" "$OUT" "$MIN_COVERAGE_PCT" <<'PYEOF'
import os, re, subprocess, sys, datetime, glob

root = os.getcwd()
tmp_path = sys.argv[1]        # 本轮生成结果（gen 模式由 bash 拷到 OUT）
mode = sys.argv[2]            # gen | check
doc_path = sys.argv[3]        # 已提交清单（check 模式的比对对象）
min_cov = int(sys.argv[4])    # 本轮至少要有多少百分比的模块能读到源码，否则判"门禁失明"
mode = sys.argv[2]
doc_path = sys.argv[3]
min_cov = int(sys.argv[4])

# ---------- 1) 依赖全集：来自所有 go.sum（module + version，去掉 /go.mod 行）----------
sums = [os.path.join(root, "go.sum")]
sums += glob.glob(os.path.join(root, "services", "*", "go.sum"))
sums += glob.glob(os.path.join(root, "operator", "go.sum"))
mods = {}
for f in sums:
    if not os.path.exists(f):
        continue
    for line in open(f, encoding="utf-8", errors="replace"):
        p = line.split()
        # go.sum 行形态：<module> <version> h1:…= / <module> <version>/go.mod h1:…=
        # ⇒ 版本在第二列，且必须**跳过 /go.mod 行**（那只是 go.mod 的哈希，不是构建里用的源码版本）
        if len(p) < 3:
            continue
        mod, ver = p[0], p[1]
        if not ver.startswith("v") or ver.endswith("/go.mod"):
            continue
        mods.setdefault(mod, set()).add(ver)

# ---------- 2) 直接依赖：go.mod 的 require 块里没写 // indirect ----------
direct = set()
gomods = [os.path.join(root, "go.mod")]
gomods += glob.glob(os.path.join(root, "services", "*", "go.mod"))
gomods += glob.glob(os.path.join(root, "operator", "go.mod"))
for f in gomods:
    if not os.path.exists(f):
        continue
    txt = open(f, encoding="utf-8", errors="replace").read()
    for line in re.findall(r"^\s+([^\s]+)\s+(v[^\s]+)(.*)$", txt, re.M):
        if "indirect" not in line[2]:
            direct.add(line[0])

# ---------- 3) 许可证识别：本地模块缓存 + 签名表（按优先级匹配）----------
mc = subprocess.run(["go", "env", "GOMODCACHE"], capture_output=True, text=True, check=True).stdout.strip()

def cache_dir(mod, ver):
    esc = re.sub(r"([A-Z])", lambda m: "!" + m.group(1).lower(), mod)
    return os.path.join(mc, esc + "@" + ver)

SIGS = [
    # 宽松类：签名足够独特，整篇扫即可
    ("ISC",       ["PERMISSION TO USE, COPY, MODIFY, AND/OR DISTRIBUTE THIS SOFTWARE"]),
    ("Unlicense", ["THIS IS FREE AND UNLICENSED", "DEDICATED TO THE PUBLIC DOMAIN"]),
    ("MIT",       ["PERMISSION IS HEREBY GRANTED, FREE OF CHARGE"]),
    # BSD-3 与 BSD-2 的区分**只能靠 "NEITHER THE NAME OF" 这条禁承认条款**：
    # "COPYRIGHT OWNER OR CONTRIBUTORS" 两者都有，放进 any() 会把 BSD-2 全判成 BSD-3。
    ("BSD-3-Clause", ["NEITHER THE NAME OF"]),
    ("BSD-2-Clause", ["REDISTRIBUTION AND USE IN SOURCE AND BINARY FORMS"]),
    ("Apache-2.0",["APACHE LICENSE, VERSION 2.0", "APACHE LICENSE VERSION 2.0"]),
]
# copyleft 类必须**只看文件头部**再判：MPL-2.0 的 "Incompatible With Secondary Licenses" 附录里
# 会出现 "GNU AFFERO GENERAL PUBLIC LICENSE" 的**名字**——整篇扫会把 24 个 MPL 模块误判成 AGPL
# （第一版实测犯了这个错，而 AGPL 误报足以阻断商用决策）。
HEAD_SIGS = [
    ("MPL-2.0",   ["MOZILLA PUBLIC LICENSE", "MOZILLA PUBLIC LICENSE VERSION 2.0"]),
    ("MPL-1.1",   ["MOZILLA PUBLIC LICENSE VERSION 1.1"]),
    ("AGPL-3.0",  ["GNU AFFERO GENERAL PUBLIC LICENSE"]),
    ("LGPL-3.0",  ["GNU LESSER GENERAL PUBLIC LICENSE"]),
    ("LGPL-2.1",  ["GNU LIBRARY GENERAL PUBLIC LICENSE"]),
    ("GPL-3.0",   ["GNU GENERAL PUBLIC LICENSE VERSION 3", "GNU GENERAL PUBLIC LICENSE"]),
    ("GPL-2.0",   ["GNU GENERAL PUBLIC LICENSE VERSION 2"]),
    ("CDDL-1.0",  ["COMMON DEVELOPMENT AND DISTRIBUTION LICENSE"]),
    ("EPL-1.0",   ["ECLIPSE PUBLIC LICENSE"]),
]
COPYLEFT = {"GPL-2.0", "GPL-3.0", "AGPL-3.0", "LGPL-2.1", "LGPL-3.0", "MPL-1.1", "MPL-2.0", "CDDL-1.0", "EPL-1.0"}

def classify(txt):
    head = txt[:400]
    for name, keys in SIGS:
        if any(k in txt for k in keys):
            return name
    for name, keys in HEAD_SIGS:
        if any(k in head for k in keys):
            # 版本号只看**标题行**：MPL-2.0 正文第 10 条会写"…除 1.1 之外…"，
            # 拿全文前若干字符判版本会把 24 个 MPL-2.0 误判成 MPL-1.1（实测犯过）。
            if name.startswith("MPL"):
                return "MPL-1.1" if "MOZILLA PUBLIC LICENSE VERSION 1.1" in txt[:80] else "MPL-2.0"
            if name == "GPL-3.0" and "VERSION 2" in txt[:80]:
                return "GPL-2.0"
            return name
    return "UNKNOWN"

def license_text(d):
    try:
        names = sorted(os.listdir(d))
    except OSError:
        return None
    for n in names:
        if re.match(r"^(LICENSE|LICENCE|COPYING)(\.(md|txt|rst))?$", n, re.I):
            p = os.path.join(d, n)
            if os.path.isfile(p):
                try:
                    # 折叠空白再匹配：Apache/GPL 的标题在原文里是
                    # "Apache License\n                           Version 2.0" 这种跨行缩进，
                    # 不折叠就会把一堆 Apache/GPL 模块误判成 UNKNOWN（第一版实测 50 个）。
                    t = open(p, encoding="utf-8", errors="replace").read(8192)
                    return " ".join(t.split()).upper()
                except OSError:
                    pass
    for n in names:  # 兜底：任意含 LICENSE 字样的文件
        if "LICEN" in n.upper() and os.path.isfile(os.path.join(d, n)):
            try:
                t = open(os.path.join(d, n), encoding="utf-8", errors="replace").read(8192)
                return " ".join(t.split()).upper()
            except OSError:
                pass
    return None

def semver_key(v):
    """按语义版本取最高，而不是字典序最大。
    字典序会把 v1.10.0 排在 v1.9.0 **之前**（'9' > '1'）⇒ 清单可能给某个模块配上
    一个根本没在用的旧版本，许可证判定跟着错。当前 6 个多版本模块恰好两种序一致
    （实测），但那是运气不是性质，故此处按数值段比较，预发布（-rc/-0.2001…伪版本）判低。
    """
    core = re.sub(r"^[vV]", "", v.split("+")[0])
    pre = "-" in core
    nums = core.split("-")[0].split(".")
    tup = tuple(int(x) if x.isdigit() else 0 for x in nums)
    return (tup + (0,) * (4 - len(tup)), 0 if pre else 1, v)

rows, missing_src, missing_file = [], [], []
multi_ver = {}
# Apache-2.0 §4(d)：上游若自带 NOTICE 文件，再分发时**必须**把其中的署名一并保留。
# 这是可机器核实的义务（不是"要不要请法务"的判断），故在此逐个登记，供 --emit-notice 汇编。
notices = []

def notice_file(d):
    try:
        names = sorted(os.listdir(d))
    except OSError:
        return None
    for n in names:
        if re.match(r"^NOTICE(\.\w+)?$", n, re.I) and os.path.isfile(os.path.join(d, n)):
            return n
    return None

for m in sorted(mods):
    vs = sorted(mods[m], key=semver_key)
    ver = vs[-1]
    if len(vs) > 1:
        multi_ver[m] = vs
    d = cache_dir(m, ver)
    if not os.path.isdir(d):
        missing_src.append(f"{m}@{ver}")
        lic = "NO-SOURCE"
    else:
        t = license_text(d)
        if t is None:
            missing_file.append(f"{m}@{ver}")
            lic = "NO-LICENSE-FILE"
        else:
            lic = classify(t)
        nf = notice_file(d)
        if nf:
            notices.append((m, ver, nf, d))
    rows.append((lic, m, ver, "直接" if m in direct else "间接"))

by_lic = {}
for r in rows:
    by_lic.setdefault(r[0], []).append(r)

def esc(s):
    return s.replace("|", "\\|")

now = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
L = []
L.append("# 第三方依赖许可证清单（自动生成，勿手改）")
L.append("")
L.append("> 生成命令：`bash deploy/scripts/gen-third-party-licenses.sh`（CI 用 `--check` 阻止文档与 go.sum 漂移）  ")
L.append(f"> 生成时间：{now}  ")
L.append(f"> 依赖来源：根模块 + `services/*/go.mod` + `operator/go.mod` 的全部 `go.sum`（{len(mods)} 个 module@version）  ")
L.append("> 许可证识别：读本地 Go 模块缓存里各模块自带的 LICENSE 文件，按签名表分类为常见 SPDX 名。")
L.append("")
L.append("## 本清单**不做**的事（必须人判）")
L.append("")
L.append("- 不判定「某个许可证能否随 OpsMesh 商用分发」——那是商务 + 法务决定（P1-7）。")
L.append("- 下面「需法务确认」段只是**把风险面摊开**：copyleft（GPL/LGPL/AGPL/MPL/CDDL/EPL）与识别失败项。")
L.append("- 静态链接/动态插件、是否随镜像分发、源码提供义务等边界，都改变结论，脚本不猜。")
L.append("")
L.append("## 汇总")
L.append("")
L.append("| 许可证 | 模块数 |")
L.append("|---|---|")
for lic in sorted(by_lic, key=lambda k: (-len(by_lic[k]), k)):
    L.append(f"| `{lic}` | {len(by_lic[lic])} |")
L.append("")
review = [r for r in rows if r[0] in COPYLEFT or r[0] in ("UNKNOWN", "NO-LICENSE-FILE", "NO-SOURCE")]
L.append(f"## 需法务确认（{len(review)} 个）")
L.append("")
if not review:
    L.append("（无：所有依赖都落在宽松许可证家族内）")
else:
    L.append("| 许可证 | 模块 | 版本 | 依赖类型 | 为什么要看它 |")
    L.append("|---|---|---|---|---|")
    for lic, m, v, kind in sorted(review, key=lambda r: (r[0], r[1])):
        why = "copyleft：可能有衍生/源码提供义务" if lic in COPYLEFT else ("未识别到已知许可证签名" if lic == "UNKNOWN" else "缓存里没找到 LICENSE 文件，需人工看仓库")
        L.append(f"| `{lic}` | `{esc(m)}` | {v} | {kind} | {why} |")
L.append("")
L.append(f"## 上游自带 NOTICE 的模块（{len(notices)} 个）")
L.append("")
L.append("Apache-2.0 §4(d) 是**可机械核实**的义务（不是判断题）：再分发的产物含上游 NOTICE 时，")
L.append("必须把其中的署名收进自己的 NOTICE。汇编命令：")
L.append("`bash deploy/scripts/gen-third-party-licenses.sh --emit-notice`（写仓库根 `NOTICE`，逐个逐字保留）。")
L.append("")
if not notices:
    L.append("（本轮无：缓存里没读到任何上游 NOTICE 文件——注意这可能是缓存缺口而非事实）")
else:
    L.append("| 模块 | 版本 | 上游 NOTICE 文件名 |")
    L.append("|---|---|---|")
    for m, v, nf, _d in sorted(notices):
        L.append(f"| `{esc(m)}` | {v} | `{nf}` |")
L.append("")
L.append(f"## 同一模块存在多个版本（{len(multi_ver)} 个）")
L.append("")
L.append("上表**按模块取语义最高版本**判定许可证。以下是 `go.sum` 里同时出现多个版本的模块——")
L.append("低版本若许可证不同，本清单不会体现，故逐个列出供核对。")
L.append("另需注意：`go.sum` 记录的是**曾参与解析**的版本集合，它是实际构建清单（`go list -m all`）的超集，")
L.append("不等于「这些代码都进了产物」。要精确到产物级依赖，用镜像 SBOM（release.yml 的 syft 产物）。")
L.append("")
if not multi_ver:
    L.append("（无：每个模块在 go.sum 里只有一个版本）")
else:
    L.append("| 模块 | go.sum 内全部版本 | 本清单判定用 |")
    L.append("|---|---|---|")
    for m, vs in sorted(multi_ver.items()):
        L.append(f"| `{esc(m)}` | {', '.join(vs)} | `{vs[-1]}` |")
L.append("")
L.append("## 全量清单")
L.append("")
for lic in sorted(by_lic):
    L.append(f"### `{lic}`（{len(by_lic[lic])}）")
    L.append("")
    L.append("| 模块 | 版本 | 依赖类型 |")
    L.append("|---|---|---|")
    for _, m, v, kind in sorted(by_lic[lic], key=lambda r: r[1]):
        L.append(f"| `{esc(m)}` | {v} | {kind} |")
    L.append("")
L.append("## 基础镜像（另一条供应链线，不在此清单）")
L.append("")
L.append("Go 依赖不等于镜像内容：镜像里还有 `FROM` 的操作系统包。见")
L.append("`Dockerfile` / `Dockerfile.agent` / `Dockerfile.service` / `deploy/docker/Dockerfile.*` 的 `FROM`")
L.append("（已钉 digest，由 Renovate 维护），以及 Trivy 扫描结果——两侧的许可证义务不同（镜像内系统包")
L.append("通常触发 GPL/LGPL 的**二进制再分发**条款，需单独结论）。")
L.append("")
open(tmp_path, "w", encoding="utf-8", newline="\n").write("\n".join(L) + "\n")

# ---------- 4) check：与环境无关的三条硬断言 ----------
UNREADABLE = {"NO-SOURCE", "NO-LICENSE-FILE"}

def parse_doc(path):
    """从已提交清单的「## 全量清单」段回收 (module, version) -> license。
    许可证在「### `XXX`（n）」分组标题里，表格三列只有 模块/版本/依赖类型。"""
    m = {}
    in_full = False
    cur = None
    for line in open(path, encoding="utf-8", errors="replace"):
        if line.startswith("## 全量清单"):
            in_full = True
            continue
        if in_full and line.startswith("## "):
            break
        if not in_full:
            continue
        h = re.match(r"^### `(.+)`", line)
        if h:
            cur = h.group(1)
            continue
        p = [c.strip() for c in line.strip().strip("|").split("|")]
        if len(p) != 3 or cur is None:
            continue
        mod, ver, _kind = p
        if mod in ("模块", "") or set(mod) <= set("-: "):
            continue
        m[(mod.replace("\\|", "|").strip("`"), ver)] = cur
    return m

fresh = {(m, v): lic for lic, m, v, _k in rows}

if mode == "notice":
    # 仓库根 NOTICE：头部是本项目自己的署名（与 LICENSE 附录逐字一致），
    # 之后逐个**逐字**保留上游 NOTICE 全文——§4(d) 要的就是"保留"，不是"转述"。
    N = []
    N.append("OpsMesh")
    N.append("Copyright 2026 OpsMesh Contributors")
    N.append("")
    N.append("This product includes software developed as part of OpsMesh.")
    N.append("")
    N.append("This product distributes third-party open source components. The license")
    N.append("inventory is machine-generated (docs/third-party-licenses.md); the notices")
    N.append("below are reproduced verbatim from the NOTICE files shipped inside those")
    N.append("dependencies, as required by Section 4(d) of the Apache License, Version 2.0.")
    N.append("")
    N.append("This file is generated by deploy/scripts/gen-third-party-licenses.sh")
    N.append("(--emit-notice). Do not edit by hand: regenerate instead.")
    N.append("")
    for m, v, nf, d in sorted(notices):
        try:
            body = open(os.path.join(d, nf), encoding="utf-8", errors="replace").read().rstrip()
        except OSError:
            body = "(读取失败)"
        N.append("=" * 78)
        N.append(f"{m}@{v}   (upstream file: {nf})")
        N.append("-" * 78)
        N.append(body)
        N.append("")
    target = os.path.join(root, "NOTICE")
    open(target, "w", encoding="utf-8", newline="\n").write("\n".join(N) + "\n")
    print(f"[OK] 已写入 NOTICE（{len(notices)} 段上游署名；缓存读不到的模块不会出现在这里）")
    sys.exit(0)

if mode != "check":
    print(f"生成完成：{len(mods)} 个模块 / {len(by_lic)} 种许可证；直接依赖 {len(direct & set(mods))} 个；需确认 {len(review)} 个")
    sys.exit(0)

if not os.path.exists(doc_path):
    print(f"[FAIL] 缺少 {doc_path} —— 先跑不带参数的一次生成")
    sys.exit(1)
committed = parse_doc(doc_path)

ok = True
added = sorted(set(fresh) - set(committed))
removed = sorted(set(committed) - set(fresh))
if added or removed:
    ok = False
    print(f"[FAIL] 依赖清单与 {doc_path} 不一致：新增 {len(added)} 个、移除 {len(removed)} 个")
    for m, v in added[:20]:
        print(f"  + {m}@{v} ({fresh[(m, v)]})")
    for m, v in removed[:20]:
        print(f"  - {m}@{v} ({committed[(m, v)]})")
    if len(added) + len(removed) > 40:
        print(f"  …（共 {len(added) + len(removed)} 处，已截断）")

changed = [(k, committed[k], fresh[k]) for k in sorted(set(fresh) & set(committed))
           if committed[k] != fresh[k]
           and committed[k] not in UNREADABLE and fresh[k] not in UNREADABLE]
if changed:
    ok = False
    print(f"[FAIL] {len(changed)} 个模块的许可证判定发生变化（同一 module@version 却识别成不同许可证）")
    for (m, v), a, b in changed[:20]:
        print(f"  {m}@{v}: {a} -> {b}")

unreadable = [(m, v) for (m, v), lic in fresh.items() if lic in UNREADABLE]
cov = 100 * (len(fresh) - len(unreadable)) // max(len(fresh), 1)
if unreadable:
    print(f"[WARN] 本轮本地模块缓存读不到 {len(unreadable)} 个模块的 LICENSE"
          f"（源码覆盖率 {cov}%）——这些模块**未被核对**，不等于已确认。")
if cov < min_cov:
    ok = False
    print(f"[FAIL] 源码覆盖率 {cov}% < 门禁下限 {min_cov}%：本轮等于没检查（空转绿）。"
          f" 先执行 go mod download all（含 services/* 与 operator）再重跑。")
if ok:
    print(f"[PASS] 依赖许可证清单与 go.sum 一致（{len(fresh)} 个模块，源码覆盖率 {cov}%）")
sys.exit(0 if ok else 1)
PYEOF
PYRC=$?

# ---------- 生成模式：把本轮结果落到文档 ----------
if [ "$CHECK_MODE" = "gen" ]; then
    if [ "$PYRC" != 0 ]; then
        # 生成失败时**不覆盖**已有文档：半截输出被当成"已重新生成"是最坏情形。
        rm -f "$TMP"; echo "[FAIL] 生成失败（python rc=${PYRC}），未覆盖 $OUT"; exit 1
    fi
    cp "$TMP" "$OUT"
    rm -f "$TMP"
    echo "[OK] 已写入 $OUT"
    exit 0
fi
# check 模式：结论必须由退出码带出去。此前这里是 `rm -f "$TMP"` 收尾，
# 末命令恒为 0 ⇒ 门禁无论判出什么都绿灯（本机故障注入实测抓到，见报告 §23）。
rm -f "$TMP"
exit "$PYRC"
