#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""控制面 shell 注入面盘点（只读复算脚本）。

背景：docs/opsmesh-authz-audit-2026-11-04.md 的注入面系列批次。本仓的控制面
handler 普遍"查询走 API、执行走 shell"：把用户字段拼进 shell 脚本再经
dispatchKafkaTask 下发，agent 端按平台选择解释器（internal/agent/agent.go
shellCommand：Windows=cmd /C、其他=sh -c）。因此每一处 `%s` 裸拼都是潜在注入面，
需要一套可复算的盘点来跟踪收口进度。

本脚本不改任何代码，只做静态统计（供审计报告与后续批次划范围）：
  python3 scripts/shell-injection-audit.py                # 控制面（默认）
  python3 scripts/shell-injection-audit.py --root <目录>
  python3 scripts/shell-injection-audit.py --json         # 机器可读

判据（有意保守，宁可多报待人工甄别）：
  A. "Sprintf 型"：行含 `fmt.Sprintf(` 且格式串含 %s/%v；且**本行所在逻辑块**
     （向上到空行、上限 12 行）内没有出现收口标记（shellQuote( / shellquote. /
     QuotePowerShellSingle( / PowerShellEncodedCommand( / Validate*( /
     MatchString( / regexp）；且本行不构建 JSON/错误响应（含 WriteJSON /
     "error 的行不算）；且格式串含空白（"OpsMesh-%s-%s" 一类资源名拼装降噪）。
  B. "拼接型"：行含 `+ <标识符> +` 形态（如 ` + pkg + `）且没有收口标记。

  这不是"漏洞清单"：部分字段的合法域天然窄、上游已校验，或值是故意原样透传的
  参数串（加引号会改语义）。它给出的是**待人工甄别的候选行基线**——每个批次的
  目标是把某一文件/某一类的候选行数打到 0，并留守卫测试钉住。

上下文分类（决定可用的转义策略）：
  sh          POSIX 脚本（QuoteDouble 可证）；含 powershell 的记为 powershell；
  cmd         cmd.exe 脚本（转义语法不存在，只能白名单/拒绝式校验）；
  powershell  powershell -Command 嵌套（两层语法，需 PS 转义 + 外层校验）。
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
from collections import Counter

DEFAULT_ROOT = os.path.join("internal", "controlplane")

QUOTE_MARKERS = (
    "shellQuote(",
    "shellquote.",
    "QuoteDouble(",
    "Quote(",
    "QuotePowerShellSingle(",
    "PowerShellEncodedCommand(",
)
VALIDATE_MARKERS = ("Validate", "MatchString(", "regexp.")
SPRINTF_RE = re.compile(r"fmt\.Sprintf\(")
FMT_VERB_RE = re.compile(r"%[sv]")
FMT_LITERAL_RE = re.compile(r'fmt\.Sprintf\(\s*[`"]([^`"]*)[`"]')
CONCAT_RE = re.compile(r"\+\s+[a-zA-Z_]\w*\s+\+")
POWERSHELL_RE = re.compile(r"powershell", re.I)
CMD_RE = re.compile(r"\breg\s|cmd /C|winget|choco|scoop|msiexec|sc\.exe", re.I)
JSONISH_RE = re.compile(r"WriteJSON|\"error|StatusBadRequest")


def fmt_literal_has_space(line: str) -> bool:
    """格式串里是否有空白：没有空白的多半不是命令行（如资源名拼装 "OpsMesh-%s-%s"），
    按保守假阳性处理直接跳过——这是有意的降噪，命令行几乎总含空格。"""
    m = FMT_LITERAL_RE.search(line)
    if not m:
        return True  # 解析不出格式串：保守起见不跳过
    lit = m.group(1)
    return (" " in lit) or ("\\n" in lit)


def classify(line: str) -> str:
    if POWERSHELL_RE.search(line):
        return "powershell"
    if CMD_RE.search(line):
        return "cmd"
    return "sh"


def has_quote_marker(line: str) -> bool:
    return any(m in line for m in QUOTE_MARKERS)


def has_validate_marker(line: str) -> bool:
    return any(m in line for m in VALIDATE_MARKERS)


def block_window(lines, i: int, max_back: int = 12) -> str:
    """取 i 行所在"逻辑块"的回看窗口：向上直到空行（或上限 max_back 行）。

    本仓的惯例是校验紧贴在使用之前（case 分支体内，中途不打空行）；
    空行通常意味着换了一个 case/另一个语句块，故以空行为界，
    避免"上方的校验"误吞"下方无关的裸拼"（宁可多报不可漏报）。
    """
    j = i
    back = 0
    while j > 0 and back < max_back:
        if lines[j - 1].strip() == "":
            break
        j -= 1
        back += 1
    return "\n".join(lines[j : i + 1])


def scan(root: str):
    if not os.path.isdir(root):
        sys.exit("not a directory: %s" % root)
    findings = []
    files = []
    for dirpath, _dirnames, filenames in os.walk(root):
        for fn in sorted(filenames):
            if fn.endswith(".go") and not fn.endswith("_test.go"):
                files.append(os.path.join(dirpath, fn))

    for path in sorted(files):
        rel = os.path.relpath(path)
        with open(path, encoding="utf-8", errors="replace") as f:
            lines = f.read().split("\n")
        for i, line in enumerate(lines):
            stripped = line.strip()
            if stripped.startswith("//"):
                continue
            ctx = classify(line)
            if SPRINTF_RE.search(line) and FMT_VERB_RE.search(line):
                if has_quote_marker(line) or JSONISH_RE.search(line):
                    continue
                if not fmt_literal_has_space(line):
                    continue
                if has_validate_marker(block_window(lines, i)):
                    continue
                findings.append(
                    {"file": rel, "line": i + 1, "kind": "sprintf", "ctx": ctx, "text": stripped[:200]}
                )
            elif CONCAT_RE.search(line):
                if has_quote_marker(line) or JSONISH_RE.search(line):
                    continue
                if has_validate_marker(block_window(lines, i)):
                    continue
                findings.append(
                    {"file": rel, "line": i + 1, "kind": "concat", "ctx": ctx, "text": stripped[:200]}
                )
    return findings


def main() -> int:
    ap = argparse.ArgumentParser(description="shell 注入面盘点（只读）")
    ap.add_argument("--root", default=DEFAULT_ROOT, help="扫描目录（默认 %s）" % DEFAULT_ROOT)
    ap.add_argument("--json", action="store_true", help="机器可读输出")
    args = ap.parse_args()

    findings = scan(args.root)

    if args.json:
        print(
            json.dumps(
                {
                    "root": args.root,
                    "total": len(findings),
                    "by_kind": dict(Counter(f["kind"] for f in findings)),
                    "by_ctx": dict(Counter(f["ctx"] for f in findings)),
                    "findings": findings,
                },
                ensure_ascii=False,
                indent=2,
            )
        )
        return 0

    by_kind = Counter(f["kind"] for f in findings)
    by_ctx = Counter(f["ctx"] for f in findings)
    by_file = Counter(f["file"] for f in findings)

    print("shell-injection-audit: root=%s" % args.root)
    print("候选行合计: %d（sprintf=%d, concat=%d）" % (len(findings), by_kind["sprintf"], by_kind["concat"]))
    print("按上下文: sh=%d, cmd=%d, powershell=%d" % (by_ctx["sh"], by_ctx["cmd"], by_ctx["powershell"]))
    print()
    print("按文件（前 25）:")
    for fn, n in by_file.most_common(25):
        print("  %-52s %d" % (fn, n))
    print()
    print("明细:")
    for f in findings:
        print("  %s:%d [%s/%s] %s" % (f["file"], f["line"], f["kind"], f["ctx"], f["text"]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
