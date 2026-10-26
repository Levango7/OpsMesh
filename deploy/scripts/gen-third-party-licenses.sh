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
#   bash deploy/scripts/gen-third-party-licenses.sh            # 生成/覆盖两个清单（Go 模块 + npm 包）
#   bash deploy/scripts/gen-third-party-licenses.sh --check    # 只比对：与已提交文档不一致即 rc=1（CI 用）
#   bash deploy/scripts/gen-third-party-licenses.sh --emit-notice  # 汇编仓库根 NOTICE（Apache-2.0 §4(d) 上游署名）
#
# 两条供应链、两种证据强度（2026-10-26 补 npm 时实测）：
#   - Go 模块：依赖集合来自 go.sum，许可证文本来自**本地模块缓存** ⇒ 缓存残缺时只能做集合对账，
#     故 --check 用「集合相等 + 两侧都读到时判定一致 + 源码覆盖率下限」三条与环境无关的断言。
#   - npm：依赖集合与许可证**同时**来自 web/enterprise/package-lock.json（lockfileVersion 3）。
#     实测 357 个条目 **全部自带 license 字段**（缺 0）⇒ 清单可完全离线、确定性地生成，
#     --check 能直接断言"集合 + 许可证"逐项一致，不需要 node_modules、也不受机器差异影响。
#   - 容器操作系统包：**不在**本脚本内——需要拉镜像才能枚举（见 deploy/scripts/collect-os-package-licenses.sh
#     与 docs/license-compliance-deliverable.md），本脚本只把「各 Dockerfile 的运行时基础镜像」
#     从文件里推导出来写进清单，防止文档口径与构建文件漂移（10-06 就出现过"文档说 distroless、
#     实际 12 个微服务镜像跑 alpine"的滞后方）。
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
OUT_NPM="docs/third-party-licenses-npm.md"
MODE="${1:-gen}"
MIN_COVERAGE_PCT="${MIN_COVERAGE_PCT:-90}"
TMP="$(mktemp)"
TMP_NPM="${TMP}.npm"

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
"$PY" - "$TMP" "$CHECK_MODE" "$OUT" "$MIN_COVERAGE_PCT" "$OUT_NPM" "$TMP_NPM" <<'PYEOF'
import os, re, subprocess, sys, datetime, glob

root = os.getcwd()
tmp_path = sys.argv[1]        # 本轮生成结果（gen 模式由 bash 拷到 OUT）
mode = sys.argv[2]            # gen | check
doc_path = sys.argv[3]        # 已提交清单（check 模式的比对对象）
min_cov = int(sys.argv[4])    # 本轮至少要有多少百分比的模块能读到源码，否则判"门禁失明"
mode = sys.argv[2]
doc_path = sys.argv[3]
min_cov = int(sys.argv[4])
npm_out = sys.argv[5]         # 已提交的 npm 清单（check 模式比对对象）
npm_tmp = sys.argv[6]         # 本轮 npm 生成结果（gen 模式由 bash 拷到 OUT_NPM）

# ---------- npm 侧的固定口径 ----------
# 许可证与版本**都来自 package-lock.json**（lockfileVersion>=3 的 packages[*].license）。
# 为什么不用 node_modules：CI 的各腿不保证装了前端依赖，用 node_modules 会让门禁在某些腿空转；
# 而锁文件是入库产物，任何腿读到的都是同一份 ⇒ 判据与环境无关（与 Go 侧刻意相反，原因见文件头）。
NPM_LOCK = os.path.join(root, "web/enterprise", "package-lock.json")
# copyleft 家族（与 Go 侧的 COPYLEFT 同源，但 npm 的 SPDX 串可能是复合表达式，故用子串匹配）
NPM_COPYLEFT_MARKS = ("MPL", "GPL", "LGPL", "AGPL", "EPL", "CDDL", "EUPL", "CC-BY-SA", "SSPL")


def npm_inventory():
    """返回 {(name, version): {"license": str, "dev": bool}}；锁文件缺失返回 None。"""
    if not os.path.exists(NPM_LOCK):
        return None
    import json
    with open(NPM_LOCK, encoding="utf-8") as f:
        d = json.load(f)
    inv = {}
    for path, meta in d.get("packages", {}).items():
        if not path or "node_modules/" not in path:
            continue                      # 根条目（"" 键）与 workspace 条目不是外部依赖
        name = path.split("node_modules/")[-1]
        inv[(name, meta.get("version", "?"))] = {
            "license": meta.get("license") or "UNKNOWN",
            "dev": bool(meta.get("dev")),
        }
    return inv


def npm_doc(inv):
    """渲染 npm 清单。分发面分「生产（进前端 bundle）」与「构建（不进产物）」两栏——
    这不是审美问题：devDependencies 的许可证义务在"不分发"的前提下与客户无关，
    把它们和随产物一起分发的包混在一张表里，会让义务清单失真。"""
    prod = {k: v for k, v in inv.items() if not v["dev"]}
    dev = {k: v for k, v in inv.items() if v["dev"]}
    by_lic_prod, by_lic_dev = {}, {}
    for (n, ver), meta in prod.items():
        by_lic_prod.setdefault(meta["license"], []).append((n, ver))
    for (n, ver), meta in dev.items():
        by_lic_dev.setdefault(meta["license"], []).append((n, ver))
    copyleft = sorted(
        [(n, ver, m["license"], "生产" if not m["dev"] else "构建")
         for (n, ver), m in inv.items()
         if any(tag in m["license"].upper() for tag in NPM_COPYLEFT_MARKS)]
    )
    now = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    out = []
    out.append("# 前端（npm）第三方依赖许可证清单（自动生成，勿手改）")
    out.append("")
    out.append("> 生成命令：`bash deploy/scripts/gen-third-party-licenses.sh`（CI 用 `--check` 阻止与锁文件漂移）  ")
    out.append(f"> 生成时间：{now}  ")
    out.append("> 依赖来源：`web/enterprise/package-lock.json`（版本与 `license` 字段**均取自锁文件**，不需要 node_modules）  ")
    out.append(f"> 条目数：{len(inv)}（生产 {len(prod)} / 构建 {len(dev)}）  ")
    out.append("")
    out.append("## 本清单**不做**的事（必须人判）")
    out.append("")
    out.append("- 不判定「某个许可证能否随 OpsMesh 商用分发」——商务 + 法务决定。")
    out.append("- 不区分「生产依赖里哪些代码真的进了最终 bundle」：bundler tree-shaking 可能排除部分模块，")
    out.append("  产物级权威清单是镜像 SBOM（release.yml 的 syft 产物）与前端构建产物分析。")
    out.append("- 复合 SPDX 表达式（如 `MIT OR Apache-2.0`）按原样列出，脚本不替客户选其中一个。")
    out.append("")
    out.append("## 汇总（按许可证 × 分发面）")
    out.append("")
    out.append("| 许可证 | 生产（进 bundle） | 构建（不进产物） |")
    out.append("|---|---|---|")
    for lic in sorted(set(by_lic_prod) | set(by_lic_dev),
                      key=lambda k: (-(len(by_lic_prod.get(k, [])) + len(by_lic_dev.get(k, []))), k)):
        out.append(f"| `{esc(lic)}` | {len(by_lic_prod.get(lic, []))} | {len(by_lic_dev.get(lic, []))} |")
    out.append("")
    out.append(f"## 需法务确认（{len(copyleft)} 个）")
    out.append("")
    if not copyleft:
        out.append("（无：npm 侧全部条目都落在宽松许可证家族内——**这是本轮实测结论**，不是假设）")
    else:
        out.append("| 包 | 版本 | 许可证 | 分发面 |")
        out.append("|---|---|---|---|")
        for n, ver, lic, face in copyleft:
            out.append(f"| `{esc(n)}` | `{ver}` | `{esc(lic)}` | {face} |")
    out.append("")
    out.append("## 生产依赖全量（随前端产物分发，许可证义务与客户相关）")
    out.append("")
    for lic in sorted(by_lic_prod):
        out.append(f"### `{esc(lic)}`（{len(by_lic_prod[lic])}）")
        out.append("")
        out.append("| 包 | 版本 | 许可证 | 分发面 |")
        out.append("|---|---|---|---|")
        for n, ver in sorted(by_lic_prod[lic]):
            out.append(f"| `{esc(n)}` | `{ver}` | `{esc(lic)}` | 生产 |")
        out.append("")
    out.append("## 构建依赖全量（不进交付产物，仅供供应链审查）")
    out.append("")
    for lic in sorted(by_lic_dev):
        out.append(f"### `{esc(lic)}`（{len(by_lic_dev[lic])}）")
        out.append("")
        out.append("| 包 | 版本 | 许可证 | 分发面 |")
        out.append("|---|---|---|---|")
        for n, ver in sorted(by_lic_dev[lic]):
            out.append(f"| `{esc(n)}` | `{ver}` | `{esc(lic)}` | 构建 |")
        out.append("")
    out.append(f"SUMMARY npm_total={len(inv)} npm_prod={len(prod)} npm_dev={len(dev)} "
               f"npm_copyleft={len(copyleft)}")
    out.append("")
    return "\n".join(out) + "\n"


def parse_npm_doc(path):
    """回收 {(name, version): license}。行格式由本脚本固定为四列，表头/分隔行按首列名排除。"""
    got = {}
    for line in open(path, encoding="utf-8", errors="replace"):
        if not line.startswith("| "):
            continue
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if len(cells) != 4:
            continue
        name, ver, lic = cells[0], cells[1], cells[2]
        if not (name.startswith("`") and ver.startswith("`") and lic.startswith("`")):
            continue                      # 表头与 |---| 分隔行不走这条
        key = (name.strip("`").replace("\\|", "|"), ver.strip("`"))
        got[key] = lic.strip("`").replace("\\|", "|")
    return got

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
L.append("## 基础镜像（第三条供应链：容器操作系统包）")
L.append("")
L.append("下表是各 Dockerfile 的**运行时**基础镜像，由脚本从 `FROM` 行推导（取每个文件的最后一个 stage；")
L.append("构建期 `golang:*` / `node:*` 不进产物，故不计入）。**这一栏是推导值不是手抄**——")
L.append("10-06 的许可决策材料曾按「distroless 最省事」描述，而实际 12 个微服务镜像跑在 alpine、")
L.append("agent 镜像跑在 debian-slim 上；手写口径会落后于构建文件，推导不会。")
L.append("")
L.append("| Dockerfile | 运行时基础镜像 |")
L.append("|---|---|")
_base_rows = []
for _p in sorted(glob.glob(os.path.join("**", "Dockerfile*"), recursive=True)):
    if "node_modules" in _p:
        continue
    _img = None
    for _line in open(_p, encoding="utf-8", errors="replace"):
        _m = re.match(r"^\s*FROM\s+(\S+)", _line, re.I)
        if _m:
            _img = _m.group(1)
    if _img:
        _base_rows.append((_p.replace(os.sep, "/"), _img))
for _rel, _img in _base_rows:
    L.append(f"| `{_rel}` | `{_img}` |")
if not _base_rows:
    L.append("| （未找到 Dockerfile——判据失明，请检查工作目录） | — |")
L.append("")
L.append("镜像内的系统包（alpine 的 busybox/apk-tools、debian 的 coreutils/bash 等）触发的是")
L.append("**二进制再分发**条款（GPL-2.0/LGPL 等），与 Go/npm 两侧结论不同：基线实测、源码获取途径")
L.append("与书面要约见 `docs/license-compliance-deliverable.md`；")
L.append("枚举命令：`bash deploy/scripts/collect-os-package-licenses.sh <镜像>`。")
L.append("")
L.append("前端 npm 包是第四条供应链，独立成文：`docs/third-party-licenses-npm.md`（本脚本同时生成）。")
L.append("")
# ---------- npm 清单（与 Go 清单同一轮生成，失败即整轮失败）----------
# 锁文件缺失 = npm 侧的判定面不存在。这里**判红而不是跳过**：跳过会让 CI 绿着、
# 而清单文件永远不更新（"缺了某条供应链"与"该供应链干净"必须能区分）。
_npm_inv = npm_inventory()
if _npm_inv is None:
    print("[FAIL] 找不到 web/enterprise/package-lock.json —— npm 判定面不存在（判红而不是跳过）")
    sys.exit(1)
open(npm_tmp, "w", encoding="utf-8", newline="\n").write(npm_doc(_npm_inv))

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
# ---------- npm 侧断言：集合 + 许可证逐项一致 ----------
# 与 Go 侧不同，这里两侧数据**都来自入库文件**（锁文件 vs 已提交清单）⇒ 完全确定，
# 不需要覆盖率下限，也不会因为机器差异产生假红/空转绿。
_npm_fresh = npm_inventory()
if _npm_fresh is None:
    ok = False
    print("[FAIL] 找不到 web/enterprise/package-lock.json —— npm 判定面缺失（判红而不是跳过）")
elif not os.path.exists(npm_out):
    ok = False
    print(f"[FAIL] 缺少 {npm_out} —— npm 清单没生成（先跑不带参数的一次生成）")
else:
    _doc_map = parse_npm_doc(npm_out)
    _fset, _dset = set(_npm_fresh), set(_doc_map)
    _add, _rem = sorted(_fset - _dset), sorted(_dset - _fset)
    if _add or _rem:
        ok = False
        print(f"[FAIL] npm 清单与锁文件不一致：清单缺 {len(_add)} 个、清单多 {len(_rem)} 个"
              f"（口径：以锁文件为准）")
        for _k in _add[:15]:
            print(f"  + {_k[0]}@{_k[1]} ({_npm_fresh[_k]['license']})")
        for _k in _rem[:15]:
            print(f"  - {_k[0]}@{_k[1]} ({_doc_map[_k]})")
    _chg = [(k, _doc_map[k], _npm_fresh[k]["license"]) for k in sorted(_fset & _dset)
            if _doc_map[k] != _npm_fresh[k]["license"]]
    if _chg:
        ok = False
        print(f"[FAIL] npm 清单里 {len(_chg)} 个包的许可证与锁文件不符")
        for _k, _a, _b in _chg[:15]:
            print(f"  {_k[0]}@{_k[1]}: {_a} -> {_b}")
    if not (_add or _rem or _chg):
        print(f"[PASS] npm 清单与锁文件一致（{len(_fset)} 个包，"
              f"生产 {sum(1 for v in _npm_fresh.values() if not v['dev'])} 个）")

if ok:
    print(f"[PASS] 依赖许可证清单与 go.sum 一致（{len(fresh)} 个模块，源码覆盖率 {cov}%）")
sys.exit(0 if ok else 1)
PYEOF
PYRC=$?

# ---------- 生成模式：把本轮结果落到文档 ----------
if [ "$CHECK_MODE" = "gen" ]; then
    if [ "$PYRC" != 0 ]; then
        # 生成失败时**不覆盖**已有文档：半截输出被当成"已重新生成"是最坏情形。
        # 两个清单同一口径：要么都写，要么都不写（只更新一个会留下互相矛盾的两份清单）。
        rm -f "$TMP" "$TMP_NPM"; echo "[FAIL] 生成失败（python rc=${PYRC}），未覆盖 $OUT / $OUT_NPM"; exit 1
    fi
    cp "$TMP" "$OUT"
    cp "$TMP_NPM" "$OUT_NPM"
    rm -f "$TMP" "$TMP_NPM"
    echo "[OK] 已写入 $OUT"
    echo "[OK] 已写入 $OUT_NPM"
    exit 0
fi
# check 模式：结论必须由退出码带出去。此前这里是 `rm -f "$TMP"` 收尾，
# 末命令恒为 0 ⇒ 门禁无论判出什么都绿灯（本机故障注入实测抓到，见报告 §23）。
rm -f "$TMP" "$TMP_NPM"
exit "$PYRC"
