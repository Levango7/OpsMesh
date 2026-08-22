#!/usr/bin/env python3
"""Strip AI-workflow markers (task IDs, P-ratings, audit finding IDs) from comments/docs.

Dry-run by default; pass --apply to rewrite files in place.
Only touches text files; skips .git/node_modules/dist and generated lock files.
"""
import re
import sys
import pathlib

ROOT = pathlib.Path(r"F:\Nexus\OpsMesh")
EXTS = {".go", ".js", ".vue", ".md", ".yaml", ".yml", ".bat", ".sh", ".sql", ".json"}
SKIP_NAMES = {"package-lock.json", "go.sum"}
SKIP_DIRS = {".git", "node_modules", "dist", "playwright-report", "test-results", ".zcode"}

# --- marker patterns (case-sensitive) ---
# Each pattern eats one optional trailing literal space (never a newline) so
# removal does not leave double spaces behind.
# task/任务 workflow item numbers: "task 98", "task270", "任务 261"（not 子任务）
RE_TASK = re.compile(r"(?<![A-Za-z0-9\u4e00-\u9fff])(?:task|任务)[ \t]*\d+ ?")
# P0/P1/P2 ratings with optional letter/number suffix: "P0-2", "P2-B4", "P1-10"
# （not incident severity like "P1（备份超 26h...）" where a paren follows）
RE_PRATING = re.compile(r"\bP[012](?:-[A-Za-z0-9]+)?\b(?!\s*[（(]) ?")
# audit finding IDs: compound H4-M8 / M4-4B / M1-4, and U-NN series
RE_AUDIT_COMPOUND = re.compile(r"\b(?:H|U|E|F|A|M|B|D)\d{1,2}-[A-Za-z0-9]{1,3}\b ?")
RE_AUDIT_U = re.compile(r"\bU-\d{2}\b ?")
# bare audit IDs (A3/B7/E4/H16...) only when glued to CJK context — bare M*/F* are
# kept on purpose: module-design.md / feature-design.md document M/F module numbering.
# A only single-digit (A1-A9): A01-A10 are OWASP Top-10 codes, must survive.
RE_AUDIT_BARE = re.compile(r"\b(?:A[1-9]|B\d{1,2}|E\d{1,2}|H\d{1,2})\b(?=[ \t]*[\u4e00-\u9fff：:）)]) ?")
RE_AUDIT_BARE_PAREN = re.compile(r"(?<=[（(])[ \t]*(?:A[1-9]|B\d{1,2}|E\d{1,2}|H\d{1,2})[ \t]*(?=[）)])")
# letter-hyphen-digit audit IDs (A-1 / B-4 / C-4 ...) — lookbehind excludes
# legit requirement IDs like UC-D-01 / BR-D-02 / TD-08.
RE_AUDIT_LH = re.compile(r"(?<![A-Za-z-])\b[A-H]-\d{1,2}\b ?")
# 安全债 85 -> 安全债
RE_DEBT = re.compile(r"安全债\s*\d+")
# ADR ghost references
RE_ADR = re.compile(r"ADR-001\s*Option\s*A")
# "P0/P1/P2 加固" style combined ratings -> plain wording
RE_PSLASH_JG = re.compile(r"P0\s*/\s*P1\s*/\s*P2\s*加固")
RE_PSLASH = re.compile(r"P0\s*/\s*P1\s*/\s*P2")

# --- residue cleanup after marker removal ---
# SAFETY: only full-width （） parens and CJK-adjacent spaces are collapsed here —
# none of these occur in Go source structure, so this cannot corrupt code.
# Markdown-specific residue is handled line-scoped in clean_residue_md.
def clean_residue(s: str) -> str:
    # "+3" residue from "P0-2+3 加固" — must run before paren collapse
    s = re.sub(r"(?<=[（(\s])\+\d+(?=\s*[\u4e00-\u9fff])", "", s)
    # slash residue from "（P1-10 / H4-M8）" style pairs — only when the slash
    # is adjacent to CJK prose, never a real path like （/readyz ...）
    s = re.sub(r"（\s*/\s*）", "", s)
    s = re.sub(r"（\s*/\s+(?=[\u4e00-\u9fff])", "（", s)
    s = re.sub(r"(?<=[\u4e00-\u9fff])\s*/\s*）", "）", s)
    s = re.sub(r"（\s*[，。；、]\s*", "（", s)          # （，xxx -> （xxx
    s = re.sub(r"\s*[，、]\s*）", "）", s)              # xxx，） -> xxx）
    s = s.replace("（）", "")                            # empty full-width parens
    s = re.sub(r"（\s*）", "", s)
    s = re.sub(r"（\s*：\s*", "（", s)                  # （：xxx -> （xxx
    s = re.sub(r"（\s+", "（", s)                        # （ xxx -> （xxx
    s = re.sub(r"\s+）", "）", s)                        # xxx ） -> xxx）
    # orphan colon left where a leading marker was stripped — only when CJK
    # follows (prose); never touches Vue ":key=" bindings or code labels
    s = re.sub(r"^(\s*)[:：]\s*(?=[\u4e00-\u9fff])", r"\1", s, flags=re.M)
    s = re.sub(r"^(\s*#{1,6}\s+)[:：]\s*(?=[\u4e00-\u9fff])", r"\1", s, flags=re.M)
    s = re.sub(r"(//\s*)[:：]\s*(?=[\u4e00-\u9fff])", r"\1", s)
    # double space after // followed by CJK (residue; doc lists use "- " not CJK)
    s = re.sub(r"(//) {2}(?=[\u4e00-\u9fff])", r"\1 ", s)
    # double space after em dash left by removals (em dash is prose-only)
    s = re.sub(r"(?<=—) {2,}", " ", s)
    # empty ASCII parens preceded by CJK (code-safe: never matches real calls)
    s = re.sub(r"(?<=[\u4e00-\u9fff])\s*\(\s*\)", "", s)
    s = re.sub(r"[ \t]+//\s*$", "", s, flags=re.M)      # trailing bare //
    s = re.sub(r"[ \t]+$", "", s, flags=re.M)           # trailing whitespace
    return s


# markdown-only residue — line-scoped, and skipped inside fenced code blocks
# so shell comments / ASCII art in ``` fences are never touched.
def _fix_table_line(m):
    line = m.group(0)
    line = re.sub(r"\|\s{2,}", "| ", line)              # |  x -> | x
    line = re.sub(r"\s{2,}\|", " |", line)              # x  | -> x |
    return line


def clean_residue_md(s: str) -> str:
    parts = re.split(r"(^```.*$)", s, flags=re.M)
    out = []
    in_fence = False
    for part in parts:
        if part.startswith("```"):
            in_fence = not in_fence
            out.append(part)
            continue
        if not in_fence:
            part = re.sub(r"^(#{1,6})\s{2,}", r"\1 ", part, flags=re.M)  # ##  x -> ## x
            part = re.sub(r"^\|.*$", _fix_table_line, part, flags=re.M)
        out.append(part)
    return "".join(out)


def process_text(text: str, is_md: bool = False):
    counts = {}
    def sub(pat, repl, key):
        nonlocal text
        text, n = pat.subn(repl, text)
        counts[key] = counts.get(key, 0) + n
    sub(RE_PSLASH_JG, "安全加固", "pslash")
    sub(RE_PSLASH, "安全加固", "pslash")
    sub(RE_TASK, "", "task")
    sub(RE_PRATING, "", "prating")
    sub(RE_AUDIT_COMPOUND, "", "audit")
    sub(RE_AUDIT_U, "", "audit")
    sub(RE_AUDIT_BARE, "", "audit")
    sub(RE_AUDIT_BARE_PAREN, "", "audit")
    sub(RE_AUDIT_LH, "", "audit")
    sub(RE_DEBT, "安全债", "debt")
    sub(RE_ADR, "自研 gRPC 管控通道", "adr")
    text = clean_residue(text)
    if is_md:
        text = clean_residue_md(text)
    return text, counts


def iter_files():
    for p in ROOT.rglob("*"):
        if not p.is_file() or p.suffix.lower() not in EXTS or p.name in SKIP_NAMES:
            continue
        if p.name.endswith(".pb.go"):  # generated protobuf code, never hand-edit
            continue
        rel_parts = p.relative_to(ROOT).parts
        if any(part in SKIP_DIRS for part in rel_parts):
            continue
        # SQL migrations carry sha256 checksums verified at startup — editing
        # them (even comments) would make deployed databases refuse to boot.
        if "migrations" in rel_parts and p.suffix.lower() == ".sql":
            continue
        yield p


def main():
    apply = "--apply" in sys.argv
    total = {}
    changed = 0
    for p in iter_files():
        try:
            raw = p.read_bytes()
            text = raw.decode("utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        new, counts = process_text(text, is_md=(p.suffix.lower() == ".md"))
        if new != text:
            changed += 1
            for k, v in counts.items():
                total[k] = total.get(k, 0) + v
            if apply:
                p.write_text(new, encoding="utf-8", newline="")
            else:
                detail = " ".join(f"{k}={v}" for k, v in sorted(counts.items()))
                print(f"  {p.relative_to(ROOT)}  [{detail}]")
    print(f"\n{'APPLIED' if apply else 'DRY-RUN'}: {changed} files, totals={total}")


if __name__ == "__main__":
    main()
