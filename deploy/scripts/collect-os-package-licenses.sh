#!/usr/bin/env bash
# collect-os-package-licenses.sh — 从一个容器镜像里枚举「操作系统包 → 许可证」，供第三方合规交付使用。
#
# 为什么单独一个脚本而不是并进 gen-third-party-licenses.sh：
#   那个脚本的两条供应链（go.sum / package-lock.json）都只读**入库文件**，任何机器结果一致；
#   本脚本要跑镜像，结果取决于镜像内容 ⇒ 天然是"快照"，必须带镜像 digest 才能被复核。
#   把两者混在一起会让"确定性门禁"里混进一条需要网络/容器的腿（CI 各腿口径不一致）。
#
# 为什么不用 Trivy 的 license 报告：Trivy 报的是包元数据里的声明，且需要漏洞库；
#   这里要的是**镜像内实际装的包**与发行版自带的许可证声明（apk db 的 L: 字段 / Debian 的
#   /usr/share/doc/*/copyright），两者可互相印证，且不依赖任何外部数据库。
#
# 用法：
#   bash deploy/scripts/collect-os-package-licenses.sh <镜像>            # markdown 到 stdout
#   bash deploy/scripts/collect-os-package-licenses.sh <镜像> --out F   # 同时写文件
#
# 退出码：0 枚举成功（即使 copyleft=0 也是成功）；2 用法错误；3 镜像不支持（既非 apk 也非 dpkg）；
#         4 docker 不可用 —— **调用方必须把 3/4 当作"未取证"，不得当作"干净"**。

set -uo pipefail

IMG="${1:-}"
OUT=""
if [ "${2:-}" = "--out" ]; then OUT="${3:-}"; fi
if [ -z "$IMG" ]; then
    echo "用法: $0 <镜像> [--out <文件>]" >&2
    exit 2
fi
if ! command -v docker >/dev/null 2>&1; then
    echo "[FAIL] 本机没有 docker：无法枚举镜像内系统包（未取证 ≠ 干净）" >&2
    exit 4
fi

# 只读探测：先用最小 shell 判断包管理器，再把原始数据整段吐出来，**解析放在宿主机**做。
# 这样做的好处：镜像里不需要 python/awk 可用性，解析逻辑也能在宿主机单测。
RAW="$(docker run --rm --network none --entrypoint sh "$IMG" -c '
  if [ -f /lib/apk/db/installed ]; then
    echo "@@DISTRO apk"
    cat /lib/apk/db/installed
  elif [ -f /var/lib/dpkg/status ]; then
    echo "@@DISTRO dpkg"
    cat /var/lib/dpkg/status
    echo "@@COPYRIGHTS"
    for f in /usr/share/doc/*/copyright; do
      echo "@@PKG $f"
      head -c 6000 "$f" 2>/dev/null
      echo
    done
  else
    echo "@@DISTRO unsupported"
  fi
')" || { echo "[FAIL] 起容器失败（镜像是否已拉取？）：$IMG" >&2; exit 4; }

DIGEST="$(docker image inspect --format '{{if .RepoDigests}}{{index .RepoDigests 0}}{{else}}{{.Id}}{{end}}' "$IMG" 2>/dev/null)"

# ⚠️ 原始数据必须走**文件**传给 python，不能走 stdin：
# `python3 - <<'PY'` 的 heredoc 本身就从 stdin 读脚本，此时 `sys.stdin.read()` 只会读到空串
# ⇒ 判据静默变成"unsupported"（本轮第一版正是这么错的：容器里明明有 apk db，却报 @@UNSUPPORTED）。
TMPRAW="$(mktemp)"
printf '%s' "$RAW" > "$TMPRAW"

REPORT="$(python3 - "$IMG" "$DIGEST" "$TMPRAW" <<'PY'
import re, sys

image, digest, raw_path = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(raw_path, encoding="utf-8", errors="replace").read()

if "@@DISTRO apk" in text:
    distro = "alpine"
    body = text.split("@@DISTRO apk", 1)[1]
    pkgs = []
    for block in body.split("\n\n"):
        name = ver = lic = None
        for line in block.splitlines():
            if line.startswith("P:"): name = line[2:].strip()
            elif line.startswith("V:"): ver = line[2:].strip()
            elif line.startswith("L:"): lic = line[2:].strip()
        if name:
            pkgs.append((name, ver or "?", lic or "UNKNOWN"))
elif "@@DISTRO dpkg" in text:
    distro = "debian"
    status_part, _, copy_part = text.partition("@@COPYRIGHTS")
    # 1) dpkg status：只取 installed 的包与版本
    installed = {}
    for block in status_part.split("\n\n"):
        name = ver = state = None
        for line in block.splitlines():
            if line.startswith("Package: "): name = line[9:].strip()
            elif line.startswith("Version: "): ver = line[9:].strip()
            elif line.startswith("Status: "): state = line[8:].strip()
        if name and state and state.endswith(" installed"):
            installed[name] = ver or "?"
    # 2) copyright：从 /usr/share/doc/<pkg>/copyright 的 License: 行取声明
    lic_map = {}
    cur = None
    for line in copy_part.splitlines():
        if line.startswith("@@PKG "):
            path = line[6:].strip()
            m = re.search(r"/usr/share/doc/([^/]+)/copyright$", path)
            cur = m.group(1).split(":")[0] if m else None   # 去掉 :amd64 之类的架构后缀
            lic_map.setdefault(cur, set())
            continue
        if cur and line.startswith("License:"):
            lic_map[cur].add(line.split(":", 1)[1].strip())
    pkgs = []
    for name, ver in sorted(installed.items()):
        decls = sorted(lic_map.get(name, []))
        pkgs.append((name, ver, " / ".join(decls) if decls else "NO-DECLARED-LICENSE"))
else:
    print("@@UNSUPPORTED " + image)
    sys.exit(3)

COPY_MARKS = ("GPL", "LGPL", "AGPL")   # 只把强/弱 copyleft 单列；MPL 在 OS 包里少见且不是本轮义务面
copyleft = [p for p in pkgs if any(m in (p[2] or "").upper() for m in COPY_MARKS)]
nol = [p for p in pkgs if p[2] == "NO-DECLARED-LICENSE"]

out = []
out.append(f"## 镜像 `{image}` 的系统包许可证")
out.append("")
out.append(f"> 镜像 digest：`{digest or '未知'}`  ")
out.append(f"> 包管理器：`{distro}`；已安装包数：{len(pkgs)}；含 GPL/LGPL/AGPL 声明的包：{len(copyleft)}；无声明：{len(nol)}  ")
out.append("> 枚举命令：`bash deploy/scripts/collect-os-package-licenses.sh " + image + "`")
out.append("")
out.append("| 包 | 版本 | 许可证声明 |")
out.append("|---|---|---|")
for name, ver, lic in copyleft:
    out.append(f"| `{name}` | `{ver}` | `{lic}` |")
if not copyleft:
    out.append("| （无） | — | — |")
out.append("")
out.append(f"SUMMARY image={image} distro={distro} pkg_total={len(pkgs)} copyleft={len(copyleft)} "
           f"no_license={len(nol)} digest={digest or 'n/a'}")
print("\n".join(out))
PY
)"
PYRC=$?
rm -f "$TMPRAW"
printf '%s\n' "$REPORT"
if [ -n "$OUT" ]; then printf '%s\n' "$REPORT" > "$OUT"; echo "[OK] 已写入 $OUT" >&2; fi
exit "$PYRC"
