#!/usr/bin/env python3
"""TD-60 static consistency gate for the task / device / auth dual-track surfaces.

The gate compares the control-plane route surface (internal/controlplane) with the
three service gateways (services/{task,device,auth}-svc) and reports:

    domain  surface (METHOD path)  controlplane_present  service_present  severity

Severity rules
--------------
P0  a mutating operation (POST/PUT/PATCH/DELETE) exists on only one side, or a
    shared endpoint has a different pagination envelope / status vocabulary.
P1  a read-only operation exists only on the control-plane side.
P2  an operation exists only on the service side (extra / not yet dual-tracked).

Exit status
-----------
0  no unbaselined P0 divergence
1  at least one unbaselined P0 divergence
2  usage / IO error

`--json` emits a machine-readable report. `--baseline <file>` suppresses known
divergences listed in a checked-in JSON baseline (matched by domain + method +
normalized path).

Self-test
---------
Set TD60_TEST_FAIL=1 to inject a synthetic P0 (`POST /api/v1/td60-selftest`) and
verify the exit-code contract. The synthetic row never matches the baseline and
does not touch repository sources.

The parser is grep-style (Go source scanning, no build step): it understands
`mux.HandleFunc` / `mux.Handle` registrations, the common `switch r.Method` and
`parts[1] == "action"` dispatch idioms, explicit `GET /path` patterns, and the
svcproxy rule tables. It intentionally does not execute Go code.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, Iterable, List, Optional, Sequence, Set, Tuple

DOMAINS = ("task", "device", "auth")
MUTATING = {"POST", "PUT", "PATCH", "DELETE"}
HTTP_METHODS = {"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
ANY = "ANY"

DOMAIN_PREFIXES: Tuple[Tuple[str, Tuple[str, ...]], ...] = (
    ("task", (
        "/api/v1/tasks",
        "/api/v1/schedules",
        "/api/v1/approval",
        "/api/v1/task-svc",
        "/api/v1/canary",
    )),
    ("device", (
        "/api/v1/devices",
        "/api/v1/device-svc",
        "/api/v1/agents",
        "/api/v1/provision",
        "/api/v1/netsec",
    )),
    ("auth", (
        "/api/v1/auth",
        "/api/v1/auth-svc",
        "/api/v1/users",
        "/api/v1/roles",
        "/api/v1/permissions",
    )),
)

METHOD_RE = re.compile(r"http\.Method(Get|Post|Put|Patch|Delete|Head|Options)")
GUARD_RE = re.compile(r"r\.Method\s*(==|!=)\s*http\.Method(Get|Post|Put|Patch|Delete|Head|Options)")
HANDLER_CALL_RE = re.compile(r"\.((?:handle[A-Z]\w*|batchOverallStatus|aggStatus|normalizeTaskStatus))\s*\(")
STATUS_RE = re.compile(
    r'"(completed|succeeded|partial|pending|running|failed|blocked|dead_letter|deadLetter|done|cancelled)"'
)
ENVELOPE_RE = re.compile(r"hasMore|HasMore|pageSize|PageSize|\"total\"")

SKIP_HANDLERS = {"handleTaskProxy"}
IGNORED_SERVICE_PATHS = {"/health", "/ready", "/metrics", "/version", "/healthz", "/readyz"}
EXCLUDED_SERVICE_PREFIXES = ("/api/v1/cmdb", "/api/v1/discovery", "/api/v1/catalog")
EXCLUDED_CONTROLPLANE_PATHS = {"/api/v1/auth/oauth"}
EXCLUDED_FUNCS = ("federation", "metricsserver", "buildmetrics", "buildfederation")


@dataclass
class Func:
    name: str
    recv: str
    body: str
    file: str
    line: int


@dataclass
class Op:
    domain: str
    method: str
    path: str
    side: str
    handler: str = ""
    evidence: str = ""


@dataclass
class Divergence:
    domain: str
    surface: str
    controlplane_present: bool
    service_present: bool
    severity: str
    baseline: bool = False
    baseline_id: str = ""
    note: str = ""
    controlplane_evidence: str = ""
    service_evidence: str = ""


@dataclass
class Report:
    domains: int
    surfaces: int
    p0: int
    p1: int
    p2: int
    known: int
    rows: List[Divergence] = field(default_factory=list)
    baseline_used: str = ""
    stale_baseline: List[str] = field(default_factory=list)
    self_test: bool = False

    def counts(self) -> Dict[str, int]:
        return {
            "p0": self.p0,
            "p1": self.p1,
            "p2": self.p2,
            "known": self.known,
        }


def strip_go_comments(src: str) -> str:
    """Blank out // and /* */ comments while preserving offsets and newlines."""
    out: List[str] = []
    i = 0
    n = len(src)
    state = "code"
    while i < n:
        c = src[i]
        if state == "code":
            if c == "/" and i + 1 < n and src[i + 1] == "/":
                state = "line"
                out.append("  ")
                i += 2
                continue
            if c == "/" and i + 1 < n and src[i + 1] == "*":
                state = "block"
                out.append("  ")
                i += 2
                continue
            if c in ('"', "'"):
                state = "string" if c == '"' else "rune"
                out.append(c)
                i += 1
                continue
            if c == "`":
                state = "raw"
                out.append(c)
                i += 1
                continue
            out.append(c)
            i += 1
            continue
        if state == "line":
            if c == "\n":
                state = "code"
                out.append("\n")
            else:
                out.append(" ")
            i += 1
            continue
        if state == "block":
            if c == "*" and i + 1 < n and src[i + 1] == "/":
                state = "code"
                out.append("  ")
                i += 2
                continue
            out.append("\n" if c == "\n" else " ")
            i += 1
            continue
        if state in ("string", "rune"):
            out.append(c)
            quote = '"' if state == "string" else "'"
            if c == "\\" and i + 1 < n:
                out.append(src[i + 1])
                i += 2
                continue
            if c == quote:
                state = "code"
            i += 1
            continue
        if state == "raw":
            out.append(c)
            if c == "`":
                state = "code"
            i += 1
            continue
    return "".join(out)


def find_matching(text: str, start: int, open_ch: str = "{", close_ch: str = "}") -> int:
    if start < 0 or start >= len(text) or text[start] != open_ch:
        return -1
    depth = 0
    i = start
    quote = ""
    while i < len(text):
        c = text[i]
        if quote:
            if quote == "`":
                if c == "`":
                    quote = ""
            else:
                if c == "\\":
                    i += 2
                    continue
                if c == quote:
                    quote = ""
            i += 1
            continue
        if c in ('"', "'", "`"):
            quote = c
            i += 1
            continue
        if c == open_ch:
            depth += 1
        elif c == close_ch:
            depth -= 1
            if depth == 0:
                return i
        i += 1
    return -1


def split_top_level(text: str, sep: str = ",") -> List[str]:
    parts: List[str] = []
    depth = 0
    start = 0
    quote = ""
    i = 0
    while i < len(text):
        c = text[i]
        if quote:
            if quote == "`":
                if c == "`":
                    quote = ""
            else:
                if c == "\\":
                    i += 2
                    continue
                if c == quote:
                    quote = ""
            i += 1
            continue
        if c in ('"', "'", "`"):
            quote = c
            i += 1
            continue
        if c in "([{":
            depth += 1
        elif c in ")]}":
            depth -= 1
        elif c == sep and depth == 0:
            parts.append(text[start:i])
            start = i + 1
        i += 1
    parts.append(text[start:])
    return parts


def normalize_path(path: str) -> str:
    p = path.strip()
    p = re.sub(r"\{[^}]+\}", "*", p)
    p = re.sub(r":[A-Za-z_]\w*", "*", p)
    if len(p) > 1:
        p = p.rstrip("/")
    return p or "/"


def domain_for_path(path: str) -> Optional[str]:
    best: Optional[Tuple[int, str]] = None
    for domain, prefixes in DOMAIN_PREFIXES:
        for prefix in prefixes:
            if path == prefix or path.startswith(prefix + "/"):
                if best is None or len(prefix) > best[0]:
                    best = (len(prefix), domain)
    return best[1] if best else None


def load_funcs(root: Path, exclude_tests: bool = True) -> Dict[str, Func]:
    funcs: Dict[str, Func] = {}
    for path in sorted(root.rglob("*.go")):
        if exclude_tests and path.name.endswith("_test.go"):
            continue
        if "vendor" in path.parts or ".gocache" in path.parts:
            continue
        try:
            src = path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError):
            continue
        text = strip_go_comments(src)
        for m in re.finditer(r"(?m)^func\s+(?:\(\s*\w+\s+\*?(\w+)\s*\)\s+)?(\w+)\s*\(", text):
            recv = m.group(1) or ""
            name = m.group(2)
            brace = text.find("{", m.end())
            if brace < 0:
                continue
            end = find_matching(text, brace)
            if end < 0:
                continue
            body = text[brace + 1:end]
            rel = str(path.relative_to(root)).replace("\\", "/")
            funcs.setdefault(name, Func(name=name, recv=recv, body=body, file=rel, line=text.count("\n", 0, m.start()) + 1))
    return funcs


def func_body(funcs: Dict[str, Func], name: str) -> str:
    f = funcs.get(name)
    return f.body if f else ""


def methods_from_text(text: str) -> Set[str]:
    return {m.group(1).upper() for m in METHOD_RE.finditer(text)}


def infer_handler_methods(funcs: Dict[str, Func], name: str, depth: int = 0) -> Set[str]:
    if not name or depth > 2:
        return set()
    body = func_body(funcs, name)
    if not body:
        return set()
    methods = methods_from_text(body)
    for m in GUARD_RE.finditer(body):
        methods.add(m.group(2).upper())
    if methods:
        return methods
    for callee in HANDLER_CALL_RE.findall(body):
        nested = infer_handler_methods(funcs, callee, depth + 1)
        if nested:
            methods |= nested
    return methods


def methods_for_clause(clause: str, funcs: Dict[str, Func]) -> Set[str]:
    methods = methods_from_text(clause)
    for m in GUARD_RE.finditer(clause):
        methods.add(m.group(2).upper())
    if methods:
        return methods
    for callee in HANDLER_CALL_RE.findall(clause):
        nested = infer_handler_methods(funcs, callee)
        if nested:
            methods |= nested
    return methods or {ANY}


def clause_handler(clause: str) -> str:
    calls = HANDLER_CALL_RE.findall(clause)
    return calls[0] if calls else ""


def iter_switches(body: str) -> Iterable[Tuple[str, List[Tuple[str, str]]]]:
    for m in re.finditer(r"switch\s*([^{]*)\{", body):
        head = " ".join(m.group(1).split())
        brace = body.find("{", m.start())
        end = find_matching(body, brace)
        if end < 0:
            continue
        block = body[brace + 1:end]
        marks = list(re.finditer(r"(?m)^[ \t]*(case\s+([^\n:]+)|default)\s*:", block))
        clauses: List[Tuple[str, str]] = []
        for idx, mark in enumerate(marks):
            expr = (mark.group(2) or "default").strip()
            clause_start = mark.end()
            clause_end = marks[idx + 1].start() if idx + 1 < len(marks) else len(block)
            clauses.append((expr, block[clause_start:clause_end]))
        yield head, clauses


def analyze_handler(funcs: Dict[str, Func], handler: str, prefix_mode: bool) -> List[Tuple[str, str, str]]:
    """Return (method, suffix, handler_hint) for a registered handler.

    suffix "" means the registration path itself; otherwise the suffix is
    appended to the `{id}` placeholder of a prefix registration.
    """
    body = func_body(funcs, handler)
    ops: Dict[Tuple[str, str], str] = {}

    def add(method: str, suffix: str, hint: str = "") -> None:
        key = (method.upper(), suffix)
        if key not in ops or (hint and not ops[key]):
            ops[key] = hint

    for head, clauses in iter_switches(body):
        head_norm = head.strip()
        for expr, clause in clauses:
            if expr == "default":
                continue
            mine = methods_from_text(expr)
            hint = clause_handler(clause)

            if head_norm.startswith("r.Method"):
                if mine:
                    for method in mine:
                        add(method, "", hint)
                else:
                    for method in methods_for_clause(clause, funcs):
                        add(method, "", hint)
                continue

            if "parts[1]" in expr:
                for action in re.findall(r'parts\[1\]\s*==\s*"([^"]+)"', expr):
                    for method in (mine or methods_for_clause(clause, funcs)):
                        add(method, "/" + action, hint)
                continue

            if "HasSuffix" in expr:
                for action in re.findall(r'HasSuffix\s*\([^,]+,\s*"/([^"]+)"\s*\)', expr):
                    for method in (mine or methods_for_clause(clause, funcs)):
                        add(method, "/" + action, hint)
                continue

            if re.search(r"len\(parts\)\s*==\s*1", expr):
                for method in (mine or methods_for_clause(clause, funcs)):
                    add(method, "", hint)
                continue

            if head_norm.startswith("parts[1]") or head_norm.startswith("part[1]"):
                for action in re.findall(r'"([^"]+)"', expr):
                    for method in (mine or methods_for_clause(clause, funcs)):
                        add(method, "/" + action, hint)
                continue

            if head_norm.startswith("subAction") or "subAction" in head_norm or re.fullmatch(r"[a-z]\w*", head_norm):
                for action in re.findall(r'"([^"]+)"', expr):
                    for method in (mine or methods_for_clause(clause, funcs)):
                        add(method, "/" + action, hint)
                continue

            if not head_norm:
                for action in re.findall(r'"([^"]+)"', expr):
                    if action.startswith("http.Method") or action == "default":
                        continue
                    if "parts[1]" in expr or "subAction" in expr:
                        for method in (mine or methods_for_clause(clause, funcs)):
                            add(method, "/" + action, hint)

    for m in re.finditer(r"if\s+len\(parts\)\s*==\s*1\s*\{", body):
        brace = body.find("{", m.start())
        end = find_matching(body, brace)
        if end < 0:
            continue
        block = body[brace + 1:end]
        methods = methods_for_clause(block, funcs)
        if methods == {ANY}:
            continue
        hint = clause_handler(block)
        for method in methods:
            add(method, "", hint)

    for m in re.finditer(r"if\s+([^{]*HasSuffix[^{]*)\{", body):
        condition = m.group(1)
        brace = body.find("{", m.start())
        end = find_matching(body, brace)
        if end < 0:
            continue
        block = body[brace + 1:end]
        actions = re.findall(r'HasSuffix\s*\([^,]+,\s*"/([^"]+)"\s*\)', condition)
        if not actions:
            continue
        methods = methods_from_text(condition)
        block_methods = methods_for_clause(block, funcs)
        if block_methods != {ANY}:
            methods |= block_methods
        if not methods:
            continue
        hint = clause_handler(block)
        for action in actions:
            for method in methods:
                add(method, "/" + action, hint)

    if not prefix_mode:
        for m in GUARD_RE.finditer(body):
            add(m.group(2).upper(), "")

    has_base = any(suffix == "" for _, suffix in ops)
    if not has_base and not (prefix_mode and ops):
        methods = infer_handler_methods(funcs, handler)
        if not methods:
            methods = {ANY}
        for method in methods:
            add(method, "", handler)

    if not ops:
        add(ANY, "", handler)

    return [(method, suffix, hint) for (method, suffix), hint in sorted(ops.items())]


def extract_registrations(text: str, rel_file: str) -> Tuple[List[Tuple[str, str, int, int]], List[Tuple[str, str, int, int]]]:
    literal: List[Tuple[str, str, int, int]] = []
    dynamic: List[Tuple[str, str, int, int]] = []
    for m in re.finditer(r"\b([A-Za-z_]\w*)\.Handle(?:Func)?\s*\(", text):
        open_paren = text.find("(", m.start())
        close_paren = find_matching(text, open_paren, "(", ")")
        if close_paren < 0:
            continue
        args = split_top_level(text[open_paren + 1:close_paren])
        if len(args) < 2:
            continue
        arg0 = args[0].strip()
        arg1 = args[1]
        refs = re.findall(r"(\w+)\.(handle[A-Z]\w+)", arg1)
        handler = refs[-1][1] if refs else ""
        if not handler:
            bare = re.search(r"\b(handle[A-Z]\w+)\b", arg1)
            handler = bare.group(1) if bare else ""
        line = text.count("\n", 0, m.start()) + 1
        pm = re.fullmatch(r'"([^"]+)"', arg0, re.S)
        if pm:
            literal.append((pm.group(1), handler, line, m.start()))
        elif re.fullmatch(r"[A-Za-z_]\w*", arg0):
            dynamic.append((arg0, handler, line, m.start()))
    return literal, dynamic


def func_ranges(text: str) -> List[Tuple[int, int, str]]:
    ranges: List[Tuple[int, int, str]] = []
    for m in re.finditer(r"(?m)^func\s+(?:\([^)]*\)\s+)?(\w+)\s*\(", text):
        brace = text.find("{", m.end())
        if brace < 0:
            continue
        end = find_matching(text, brace)
        if end >= 0:
            ranges.append((m.start(), end, m.group(1)))
    return ranges


def enclosing_func(ranges: List[Tuple[int, int, str]], pos: int) -> str:
    for start, end, name in ranges:
        if start <= pos <= end:
            return name
    return ""


def relevant_service_path(domain: str, path: str) -> bool:
    if path in IGNORED_SERVICE_PATHS:
        return False
    if path.startswith(EXCLUDED_SERVICE_PREFIXES):
        return False
    if path.startswith("/api/v1/"):
        return True
    return domain == "device" and path in ("/install.sh", "/bin/opsmesh-agent")


def extract_map_loop_routes(text: str) -> List[Tuple[str, str, int]]:
    routes: List[Tuple[str, str, int]] = []
    if "range" not in text or "mux.Handle" not in text:
        return routes
    map_blocks = []
    for m in re.finditer(r"map\[string\][^{]*\{", text):
        brace = text.find("{", m.start())
        end = find_matching(text, brace)
        if end >= 0:
            map_blocks.append(text[brace + 1:end])
    if not map_blocks:
        return routes
    keys: List[str] = []
    for block in map_blocks:
        keys.extend(re.findall(r'"((?:/api/v1/)[^"]+)"', block))
    if not keys:
        return routes
    handler = ""
    hm = re.search(r"\.(handle[A-Z]\w+)\s*\(", text)
    if hm:
        handler = hm.group(1)
    for key in sorted(set(keys)):
        pos = text.find(key)
        routes.append((key, handler, text.count("\n", 0, pos if pos >= 0 else 0) + 1))
    return routes


def parse_routes(root: Path, forced_domain: Optional[str]) -> Tuple[List[Op], Dict[str, Func]]:
    funcs = load_funcs(root)
    ops: List[Op] = []
    seen: Set[Tuple[str, str, str]] = set()

    def push(method: str, path: str, handler: str, evidence: str) -> None:
        domain = forced_domain or domain_for_path(path)
        if not domain:
            return
        if forced_domain and not relevant_service_path(forced_domain, path):
            return
        if not forced_domain and path in EXCLUDED_CONTROLPLANE_PATHS:
            return
        if handler in SKIP_HANDLERS:
            return
        normalized = normalize_path(path)
        key = (domain, method, normalized)
        if key in seen:
            return
        seen.add(key)
        ops.append(Op(domain=domain, method=method, path=normalized, side="service" if forced_domain else "controlplane", handler=handler, evidence=evidence))

    for path in sorted(root.rglob("*.go")):
        if path.name.endswith("_test.go"):
            continue
        if "vendor" in path.parts or ".gocache" in path.parts:
            continue
        try:
            text = strip_go_comments(path.read_text(encoding="utf-8"))
        except (OSError, UnicodeDecodeError):
            continue
        rel = str(path.relative_to(root)).replace("\\", "/")
        ranges = func_ranges(text)
        literal, _dynamic = extract_registrations(text, rel)
        for pattern, handler, line, pos in literal:
            if any(part in enclosing_func(ranges, pos).lower() for part in EXCLUDED_FUNCS):
                continue
            evidence = f"{rel}:{line}"
            method = ""
            mm = re.match(r"^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(/\S*)$", pattern)
            if mm:
                method = mm.group(1)
                pattern = mm.group(2)
            if pattern.endswith("/") and len(pattern) > 1:
                base = normalize_path(pattern[:-1]) + "/*"
                analyzed = analyze_handler(funcs, handler, prefix_mode=True)
                for meth, suffix, _hint in analyzed:
                    push(meth, base + suffix, _hint or handler, evidence)
            else:
                base = normalize_path(pattern)
                if method:
                    push(method, base, handler, evidence)
                else:
                    for meth, _suffix, _hint in analyze_handler(funcs, handler, prefix_mode=False):
                        push(meth, base, _hint or handler, evidence)
        for pattern, handler, line in extract_map_loop_routes(text):
            evidence = f"{rel}:{line} (map-loop)"
            for meth in (infer_handler_methods(funcs, handler) or {ANY}):
                push(meth, pattern, handler, evidence)
    return ops, funcs


def parse_proxy_rules(proxy_file: Path) -> List[Dict[str, str]]:
    if not proxy_file.exists():
        return []
    text = strip_go_comments(proxy_file.read_text(encoding="utf-8"))
    rules: List[Dict[str, str]] = []
    for table in ("Rules", "DeviceExtras", "TaskExtras"):
        m = re.search(rf"var\s+{table}\s*=\s*\[\]Rule\s*\{{", text)
        if not m:
            continue
        brace = text.find("{", m.start())
        end = find_matching(text, brace)
        if end < 0:
            continue
        block = text[brace + 1:end]
        domain_marks = list(re.finditer(r'Domain:\s*"([^"]+)"', block))
        for idx, mark in enumerate(domain_marks):
            seg_end = domain_marks[idx + 1].start() if idx + 1 < len(domain_marks) else len(block)
            seg = block[mark.start():seg_end]
            public = re.search(r'PublicPrefix:\s*"([^"]+)"', seg)
            upstream = re.search(r'UpstreamPrefix:\s*"([^"]+)"', seg)
            if not public or not upstream:
                continue
            rules.append({
                "table": table,
                "domain": mark.group(1),
                "public": public.group(1),
                "upstream": upstream.group(1),
            })
    return rules


def contract_signature(funcs: Dict[str, Func], handler: str, depth: int = 0, seen: Optional[Set[str]] = None) -> Tuple[bool, Set[str]]:
    if not handler or depth > 2:
        return False, set()
    if seen is None:
        seen = set()
    if handler in seen:
        return False, set()
    seen.add(handler)
    body = func_body(funcs, handler)
    if not body:
        return False, set()
    envelope = bool(ENVELOPE_RE.search(body))
    status = set(m.group(1) for m in STATUS_RE.finditer(body))
    for callee in HANDLER_CALL_RE.findall(body):
        nested_env, nested_status = contract_signature(funcs, callee, depth + 1, seen)
        envelope = envelope or nested_env
        status |= nested_status
    return envelope, status


def op_key(op: Op) -> Tuple[str, str]:
    return (op.method, op.path)


def build_rows(
    cp_ops: List[Op],
    svc_ops: List[Op],
    cp_funcs: Dict[str, Func],
    svc_funcs: Dict[str, Func],
    proxy_rules: List[Dict[str, str]],
) -> Tuple[List[Divergence], int]:
    rows: List[Divergence] = []
    surfaces: Set[Tuple[str, str, str]] = set()

    for domain in DOMAINS:
        cp = {op_key(op): op for op in cp_ops if op.domain == domain}
        svc = {op_key(op): op for op in svc_ops if op.domain == domain}
        keys = sorted(set(cp) | set(svc), key=lambda k: (k[1], k[0]))
        for key in keys:
            method, path = key
            surfaces.add((domain, method, path))
            cop = cp.get(key)
            sop = svc.get(key)
            cp_present = cop is not None
            svc_present = sop is not None
            severity = ""
            note = ""
            if cp_present and svc_present:
                if method != ANY:
                    cp_env, cp_status = contract_signature(cp_funcs, cop.handler or "")
                    svc_env, svc_status = contract_signature(svc_funcs, sop.handler or "")
                    if method == "GET" and cp_env != svc_env:
                        severity = "P0"
                        note = "pagination envelope differs"
                    elif cp_status and svc_status and cp_status != svc_status:
                        severity = "P0"
                        note = "status vocabulary differs: cp=" + ",".join(sorted(cp_status)) + " svc=" + ",".join(sorted(svc_status))
            elif cp_present:
                severity = "P0" if method in MUTATING else "P1"
                note = "missing on service"
            else:
                severity = "P0" if method in MUTATING else "P2"
                note = "service-only extra"
            rows.append(Divergence(
                domain=domain,
                surface=f"{method} {path}",
                controlplane_present=cp_present,
                service_present=svc_present,
                severity=severity,
                note=note,
                controlplane_evidence=cop.evidence if cop else "",
                service_evidence=sop.evidence if sop else "",
            ))

    for rule in proxy_rules:
        domain = rule["domain"]
        if domain not in DOMAINS:
            continue
        upstream = rule["upstream"].rstrip("/")
        if upstream.startswith(EXCLUDED_SERVICE_PREFIXES):
            continue
        covered = any(
            op.domain == domain and (op.path == upstream or op.path.startswith(upstream + "/"))
            for op in svc_ops
        )
        surfaces.add((domain, "PROXY", "proxy:" + rule["public"]))
        rows.append(Divergence(
            domain=domain,
            surface=f"PROXY proxy:{rule['public']}",
            controlplane_present=True,
            service_present=covered,
            severity="" if covered else "P1",
            note=f"{rule['table']} rule -> {upstream}" + ("" if covered else "; no service routes under upstream prefix"),
        ))
    return rows, len(surfaces)


def baseline_keys(entries: Sequence[dict]) -> Dict[Tuple[str, str, str], dict]:
    keys: Dict[Tuple[str, str, str], dict] = {}
    for entry in entries:
        domain = str(entry.get("domain", "")).strip()
        method = str(entry.get("method", "")).strip().upper()
        path = str(entry.get("path", "")).strip()
        surface = str(entry.get("surface", "")).strip()
        if surface and (not method or not path):
            parts = surface.split(None, 1)
            if len(parts) == 2:
                method, path = parts[0].upper(), parts[1].strip()
        if not domain or not method or not path:
            continue
        keys[(domain, method, normalize_path(path))] = entry
    return keys


def apply_baseline(rows: List[Divergence], entries: Sequence[dict]) -> Tuple[List[str], int]:
    keys = baseline_keys(entries)
    used: Set[Tuple[str, str, str]] = set()
    suppressed = 0
    for row in rows:
        if not row.severity:
            continue
        method, path = row.surface.split(None, 1)
        key = (row.domain, method.upper(), normalize_path(path))
        entry = keys.get(key)
        if entry is None:
            continue
        row.baseline = True
        row.baseline_id = str(entry.get("id", ""))
        suppressed += 1
        used.add(key)
    stale = [str(entry.get("id") or entry.get("surface") or entry) for key, entry in keys.items() if key not in used]
    return stale, suppressed


def render_table(rows: List[Divergence]) -> str:
    header = f"{'DOMAIN':<8}{'CP':<5}{'SVC':<5}{'SEV':<8}{'BASELINE':<10}SURFACE"
    lines = [header, "-" * len(header)]
    for row in sorted(rows, key=lambda r: (DOMAINS.index(r.domain), r.surface)):
        sev = row.severity or "-"
        cp = "yes" if row.controlplane_present else "no"
        svc = "yes" if row.service_present else "no"
        known = "yes" if row.baseline else "no"
        lines.append(f"{row.domain:<8}{cp:<5}{svc:<5}{sev:<8}{known:<10}{row.surface}")
    return "\n".join(lines)


def load_baseline(path: Optional[Path]) -> List[dict]:
    if path is None:
        return []
    if not path.exists():
        raise FileNotFoundError(f"baseline not found: {path}")
    data = json.loads(path.read_text(encoding="utf-8"))
    if isinstance(data, dict):
        entries = data.get("divergences", [])
    else:
        entries = data
    if not isinstance(entries, list):
        raise ValueError("baseline must be a JSON array or an object with a divergences array")
    return entries


def domain_stats(cp_ops: List[Op], svc_ops: List[Op]) -> Dict[str, Dict[str, int]]:
    stats: Dict[str, Dict[str, int]] = {}
    for domain in DOMAINS:
        cp = {op_key(op) for op in cp_ops if op.domain == domain}
        svc = {op_key(op) for op in svc_ops if op.domain == domain}
        stats[domain] = {
            "controlplane": len(cp),
            "service": len(svc),
            "shared": len(cp & svc),
            "controlplane_only": len(cp - svc),
            "service_only": len(svc - cp),
        }
    return stats


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Static TD-60 task/device/auth dual-track consistency gate.",
        epilog="Self-test: set TD60_TEST_FAIL=1 to inject a synthetic P0 and verify exit status 1.",
    )
    parser.add_argument("--repo", type=Path, default=None, help="repository root (default: parent of this script)")
    parser.add_argument("--baseline", type=Path, default=None, help="JSON baseline of known divergences")
    parser.add_argument("--json", action="store_true", help="emit machine-readable JSON")
    parser.add_argument("--emit-baseline", type=Path, default=None, help="write current divergences as a baseline JSON file")
    parser.add_argument("--verbose", action="store_true", help="print aligned rows too")
    return parser


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = build_parser().parse_args(argv)
    repo = (args.repo or Path(__file__).resolve().parent.parent).resolve()
    controlplane_dir = repo / "internal" / "controlplane"
    if not controlplane_dir.is_dir():
        print(f"error: control-plane directory not found: {controlplane_dir}", file=sys.stderr)
        return 2

    try:
        cp_ops, cp_funcs = parse_routes(controlplane_dir, forced_domain=None)
        svc_ops: List[Op] = []
        svc_funcs: Dict[str, Func] = {}
        for domain in DOMAINS:
            root = repo / "services" / f"{domain}-svc"
            if not root.is_dir():
                print(f"warning: service directory not found: {root}", file=sys.stderr)
                continue
            ops, funcs = parse_routes(root, forced_domain=domain)
            svc_ops.extend(ops)
            svc_funcs.update(funcs)
        proxy_rules = parse_proxy_rules(controlplane_dir / "svcproxy" / "proxy.go")
    except OSError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    rows, surfaces = build_rows(cp_ops, svc_ops, cp_funcs, svc_funcs, proxy_rules)

    try:
        entries = load_baseline(args.baseline)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    stale, known = apply_baseline(rows, entries)

    self_test = os.environ.get("TD60_TEST_FAIL") == "1"
    if self_test:
        rows.append(Divergence(
            domain="task",
            surface="POST /api/v1/td60-selftest",
            controlplane_present=True,
            service_present=False,
            severity="P0",
            note="synthetic TD60_TEST_FAIL=1 divergence",
        ))
        surfaces += 1

    active = [r for r in rows if r.severity and not r.baseline]
    p0 = sum(1 for r in active if r.severity == "P0")
    p1 = sum(1 for r in active if r.severity == "P1")
    p2 = sum(1 for r in active if r.severity == "P2")
    report = Report(
        domains=len(DOMAINS),
        surfaces=surfaces,
        p0=p0,
        p1=p1,
        p2=p2,
        known=known,
        rows=rows,
        baseline_used=str(args.baseline) if args.baseline else "",
        stale_baseline=stale,
        self_test=self_test,
    )

    if args.emit_baseline:
        payload = {
            "note": "Known TD-60 P1/P2 (and accepted current P0) divergences. Regenerate intentionally.",
            "divergences": [
                {
                    "domain": r.domain,
                    "surface": r.surface,
                    "severity": r.severity or "P2",
                    "note": r.note,
                }
                for r in rows
                if r.severity
            ],
        }
        args.emit_baseline.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")

    if args.json:
        out = {
            "generated_at": datetime.now(timezone.utc).isoformat(),
            "repo": str(repo),
            "baseline": report.baseline_used,
            "self_test": report.self_test,
            "counts": report.counts(),
            "domain_counts": domain_stats(cp_ops, svc_ops),
            "summary": f"TD60 domains={report.domains} surfaces={report.surfaces} p0={report.p0} p1={report.p1} p2={report.p2}",
            "stale_baseline": report.stale_baseline,
            "rows": [asdict(r) for r in report.rows],
        }
        print(json.dumps(out, indent=2, ensure_ascii=False))
    else:
        if args.verbose:
            visible = report.rows
        else:
            visible = [r for r in report.rows if r.severity and not r.baseline]
        print(render_table(visible))
        if report.stale_baseline:
            print(f"warning: stale baseline entries (no current divergence): {len(report.stale_baseline)}", file=sys.stderr)
        if report.self_test:
            print("self-test: TD60_TEST_FAIL=1 injected POST /api/v1/td60-selftest (P0)")
        print(f"baseline: suppressed {report.known} known divergence(s)")
        print(f"TD60 domains={report.domains} surfaces={report.surfaces} p0={report.p0} p1={report.p1} p2={report.p2}")

    return 1 if p0 else 0


if __name__ == "__main__":
    sys.exit(main())
