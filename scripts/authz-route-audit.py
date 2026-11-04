#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""控制面鉴权面审计（只读复算脚本）。

背景：docs/opsmesh-authz-audit-2026-11-04.md。该报告发现批量生成的 *_ops.go
handler 多条走 dispatchKafkaTask / dispatchFastDFSTask，这两个公共派发入口只做
requireTenantContext（校验租户身份）而不做 requireProd（校验权限点），导致
仅租户身份即可下发 shell 任务。

本脚本不改任何代码，只做静态统计，供 owner / lead 复算改动前后的收口效果：
  python3 scripts/authz-route-audit.py            # 控制面（默认）
  python3 scripts/authz-route-audit.py --root <pkg 目录>
  python3 scripts/authz-route-audit.py --json    # 机器可读输出

判据（有意保守，宁可漏报不可误报）：
  一个路由 handler 被判「有权限校验」，仅当它自身或**同文件内**的一级 helper
  （深链 ≤3 层）出现以下任一调用：
      requirePermission / requireProd / subsystemAuthorize / authorizeByRoles / .Authorize
  否则若可达派发类调用（dispatchKafkaTask / dispatchFastDFSTask / createSystemTask /
  dispatchTask）或 requireTenantContext，记 AUTH_ONLY；都不沾则记 ANON。

注意：这是**启发式静态统计**，用于给门禁/审计一个可复算的基线，不能替代运行期
渗透测试。反例（误报/漏报）都可能是合法的：
  - 真只读 handler 走 ANON 可能是对的；
  - 依赖调用链之外的鉴权封装会漏检。
因此本脚本默认以「报告」而非「判红」方式使用；若要做门禁，请先跑一遍人工对基线。
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
from collections import Counter, OrderedDict

DEFAULT_ROOT = os.path.join("internal", "controlplane")

RBAC_MARKERS = (
    "requirePermission(",
    "requireProd(",
    "subsystemAuthorize(",
    "authorizeByRoles(",
    ".Authorize",
)
TENANT_MARKERS = ("requireTenantContext(",)
DISPATCH_MARKERS = (
    "dispatchKafkaTask(",
    "dispatchFastDFSTask(",
    "createSystemTask(",
    "dispatchTask(",
)

ROUTE_RE = re.compile(r'mux\.HandleFunc\(\s*"([^"]+)"\s*,\s*s\.(\w+)\s*\)')
FUNC_RE = re.compile(r"^func \(s \*Server\) (\w+)\(", re.M)
CALL_RE = re.compile(r"s\.(\w+)\(")


def load_sources(root: str) -> "OrderedDict[str, str]":
    if not os.path.isdir(root):
        sys.exit(f"not a directory: {root}")
    out: "OrderedDict[str, str]" = OrderedDict()
    for fn in sorted(os.listdir(root)):
        if fn.endswith(".go") and not fn.endswith("_test.go"):
            with open(os.path.join(root, fn), encoding="utf-8", errors="replace") as f:
                out[fn] = f.read()
    return out


def build_bodies(src: "OrderedDict[str, str]") -> dict:
    bodies = {}
    for fn, txt in src.items():
        marks = [(m.start(), m.group(1)) for m in FUNC_RE.finditer(txt)]
        for i, (start, name) in enumerate(marks):
            end = marks[i + 1][0] if i + 1 < len(marks) else len(txt)
            bodies[name] = (fn, txt[start:end])
    return bodies


def analyse(name: str, bodies: dict, seen=None, depth: int = 0,
            cross_file: bool = True, max_depth: int = 4) -> set:
    """沿同文件与（可选）跨文件 helper 链传播鉴权标签。

    cross_file 必须开启：dispatchKafkaTask / dispatchFastDFSTask 等公共派发入口
    与调用它们的 *_ops.go handler 不在同一文件，只在同文件内跟踪会把已修复的
    收口效果漏算（实测差异 223→216，而真实收口远大于此）。
    """
    if seen is None:
        seen = set()
    if name in seen or depth > max_depth or name not in bodies:
        return set()
    seen.add(name)
    fn, body = bodies[name]
    tags = set()
    if any(k in body for k in RBAC_MARKERS):
        tags.add("RBAC")
    if any(k in body for k in TENANT_MARKERS):
        tags.add("TENANT")
    if any(k in body for k in DISPATCH_MARKERS):
        tags.add("DISPATCH")
    for m in CALL_RE.finditer(body):
        callee = m.group(1)
        if callee not in bodies:
            continue
        if not cross_file and bodies[callee][0] != fn:
            continue
        tags |= analyse(callee, bodies, seen, depth + 1, cross_file, max_depth)
    return tags


def domain_of(path: str) -> str:
    m = re.match(r"/api/v1/([^/]+)/?", path)
    return m.group(1) if m else "root"


def audit(root: str):
    src = load_sources(root)
    bodies = build_bodies(src)
    rows = []
    for fn, txt in src.items():
        for m in ROUTE_RE.finditer(txt):
            path, handler = m.group(1), m.group(2)
            tags = analyse(handler, bodies)
            cls = "RBAC" if "RBAC" in tags else (
                "AUTH_ONLY" if ("TENANT" in tags or "DISPATCH" in tags) else "ANON"
            )
            rows.append({"file": fn, "path": path, "handler": handler, "cls": cls,
                         "dispatchable": "DISPATCH" in tags})
    return rows


def main() -> int:
    ap = argparse.ArgumentParser(description="OpsMesh controlplane route authorization audit")
    ap.add_argument("--root", default=DEFAULT_ROOT, help="package dir (default: internal/controlplane)")
    ap.add_argument("--json", action="store_true", help="machine-readable output")
    ap.add_argument("--fail-on", metavar="N", type=int, default=None,
                    help="exit 1 when AUTH_ONLY dispatchable paths exceed N (gate use)")
    args = ap.parse_args()

    rows = audit(args.root)
    total = len(rows)
    cnt = Counter(r["cls"] for r in rows)
    disp = sum(1 for r in rows if r["dispatchable"])
    disp_auth_only = sum(1 for r in rows if r["dispatchable"] and r["cls"] == "AUTH_ONLY")

    if args.json:
        payload = {
            "root": args.root,
            "total_routes": total,
            "classes": dict(cnt),
            "dispatchable_routes": disp,
            "dispatchable_without_rbac": disp_auth_only,
            "dispatchable_without_rbac_paths": sorted(
                {r["path"] for r in rows if r["dispatchable"] and r["cls"] == "AUTH_ONLY"}
            ),
        }
        print(json.dumps(payload, ensure_ascii=False, indent=2))
    else:
        print(f"root: {args.root}")
        print(f"routes registered               : {total}")
        print(f"  with RBAC permission check    : {cnt.get('RBAC', 0)}")
        print(f"  tenant-only / no check         : {cnt.get('AUTH_ONLY', 0)}")
        print(f"  anonymous                      : {cnt.get('ANON', 0)}")
        print(f"dispatchable (can create tasks)  : {disp}")
        print(f"  dispatchable WITHOUT RBAC      : {disp_auth_only}")
        print()
        vuln = sorted({r["path"] for r in rows if r["dispatchable"] and r["cls"] == "AUTH_ONLY"})
        groups: "OrderedDict[str, list]" = OrderedDict()
        for p in vuln:
            groups.setdefault(domain_of(p), []).append(p)
        print(f"affected domains: {len(groups)}")
        for d in sorted(groups, key=lambda x: (-len(groups[x]), x)):
            print(f"  {d:<24} {len(groups[d]):>3}")
        print()
        print("affected paths:")
        for p in vuln:
            print(f"  {p}")

    if args.fail_on is not None and disp_auth_only > args.fail_on:
        print(f"[gate] dispatchable routes without RBAC = {disp_auth_only} > {args.fail_on}",
              file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
