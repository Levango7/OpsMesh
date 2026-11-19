#!/usr/bin/env python3
"""Read-only ABAC M4 soak report from abac_decision_logs.

The script shells out to the MySQL command-line client (no pip dependencies) and
runs a fixed set of SELECT queries:

  * daily decision counts by mode and by agent/mode (allow/deny, shadow rows)
  * online/offline upload lag percentiles (TIMESTAMPDIFF on occurred_at -> received_at)
  * distinct bundle_id counts
  * error-ish deny reasons (expired / missing / verify / invalid / fail)
  * optional controlplane /metrics counters whose names contain "abac"

Output is a Markdown report on stdout (or --out FILE). The script never issues
an INSERT/UPDATE/DELETE/DDL statement; the SQL text is static and is additionally
checked to start with SELECT/WITH. Use a MySQL account granted only SELECT on
abac_decision_logs (see scripts/abac/README.md).

Exit status
-----------
0  report generated (a failed optional /metrics fetch is only a warning)
2  usage error, mysql CLI missing, connection/query failure, or --out write failure
"""

from __future__ import annotations

import argparse
import os
import re
import shutil
import subprocess
import sys
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from typing import Dict, Iterable, List, Optional, Sequence, Tuple
from urllib.error import HTTPError, URLError
from urllib.parse import unquote, urlsplit
from urllib.request import Request, urlopen

EXIT_OK = 0
EXIT_ERROR = 2

DEFAULT_ENV_DSN = "OPSMESH_SOAK_DSN"
DEFAULT_ENV_BASE_URL = "OPSMESH_SOAK_BASE_URL"
DEFAULT_DAYS = 7
DEFAULT_MYSQL_TIMEOUT = 120
DEFAULT_METRICS_TIMEOUT = 10

TABLE = "abac_decision_logs"


@dataclass
class DBConfig:
    host: str = "127.0.0.1"
    port: int = 3306
    user: str = "opsmesh_soak"
    password: str = ""
    database: str = "opsmesh"

    def label(self) -> str:
        return f"{self.user}@{self.host}:{self.port}/{self.database}"


def _valid_identifier(value: str, field: str) -> str:
    if not value or "\x00" in value or "\n" in value or "\r" in value:
        raise ValueError(f"invalid {field}")
    return value


def parse_dsn(raw: str) -> DBConfig:
    """Parse mysql://user:pass@host:3306/db, user:pass@host:3306/db, or Go-style tcp()."""
    text = raw.strip()
    if not text:
        raise ValueError("empty DSN")

    if text.startswith("mysql://"):
        parts = urlsplit(text)
        if parts.scheme != "mysql":
            raise ValueError("DSN scheme must be mysql://")
        user = unquote(parts.username or "")
        password = unquote(parts.password or "")
        host = parts.hostname or ""
        try:
            port = parts.port or 3306
        except ValueError as exc:
            raise ValueError(f"invalid DSN port: {exc}") from exc
        database = unquote(parts.path.lstrip("/"))
        if not host or not user or not database:
            raise ValueError("DSN must include user, host and database")
        return DBConfig(
            host=_valid_identifier(host, "host"),
            port=port,
            user=_valid_identifier(user, "user"),
            password=password,
            database=_valid_identifier(database, "database"),
        )

    go_style = re.match(
        r"^(?P<user>[^:@/]*)(?::(?P<password>[^@]*))?@tcp\((?P<host>[^:)]+)(?::(?P<port>\d+))?\)/(?P<database>[^?]+)",
        text,
    )
    if go_style:
        return DBConfig(
            host=_valid_identifier(go_style.group("host"), "host"),
            port=int(go_style.group("port") or 3306),
            user=_valid_identifier(go_style.group("user"), "user"),
            password=go_style.group("password") or "",
            database=_valid_identifier(go_style.group("database"), "database"),
        )

    plain = re.match(r"^(?P<userinfo>[^@]*)@(?P<hostport>[^/]+)/(?P<database>[^?]+)$", text)
    if not plain:
        raise ValueError(
            "DSN must look like mysql://user:pass@host:3306/db "
            "or user:pass@host:3306/db"
        )
    userinfo = plain.group("userinfo")
    if ":" in userinfo:
        user, password = userinfo.split(":", 1)
    else:
        user, password = userinfo, ""
    hostport = plain.group("hostport")
    if ":" in hostport:
        host, port_text = hostport.rsplit(":", 1)
        if not port_text.isdigit():
            raise ValueError("invalid DSN port")
        port = int(port_text)
    else:
        host, port = hostport, 3306
    return DBConfig(
        host=_valid_identifier(unquote(host), "host"),
        port=port,
        user=_valid_identifier(unquote(user), "user"),
        password=unquote(password),
        database=_valid_identifier(unquote(plain.group("database")), "database"),
    )


def parse_time(value: str) -> datetime:
    text = value.strip()
    if re.fullmatch(r"\d{4}-\d{2}-\d{2}", text):
        dt = datetime.strptime(text, "%Y-%m-%d").replace(tzinfo=timezone.utc)
        return dt
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    dt = datetime.fromisoformat(text)
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.astimezone(timezone.utc)


def sql_time(dt: datetime) -> str:
    return dt.astimezone(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")


def assert_read_only(sql: str) -> None:
    head = sql.lstrip().split(None, 1)[0].upper()
    if head not in ("SELECT", "WITH"):
        raise RuntimeError(f"refusing to execute non-read-only SQL (starts with {head!r})")


def build_queries(start: datetime, end: datetime, reason_limit: int) -> Dict[str, str]:
    lo = sql_time(start)
    hi = sql_time(end)
    where = f"received_at >= '{lo}' AND received_at < '{hi}'"

    daily_mode = f"""
SELECT DATE(received_at) AS day,
       mode,
       COUNT(*) AS total,
       SUM(allowed = 1) AS allowed,
       SUM(allowed = 0) AS denied,
       SUM(reason LIKE 'shadow:%') AS shadow_rows,
       COUNT(DISTINCT agent_id) AS agents,
       COUNT(DISTINCT bundle_id) AS bundles
FROM {TABLE}
WHERE {where}
GROUP BY DATE(received_at), mode
ORDER BY day, mode
""".strip()

    daily_agent = f"""
SELECT DATE(received_at) AS day,
       agent_id,
       mode,
       COUNT(*) AS total,
       SUM(allowed = 1) AS allowed,
       SUM(allowed = 0) AS denied,
       SUM(reason LIKE 'shadow:%') AS shadow_rows,
       COUNT(DISTINCT bundle_id) AS bundles
FROM {TABLE}
WHERE {where}
GROUP BY DATE(received_at), agent_id, mode
ORDER BY day, agent_id, mode
""".strip()

    lag = f"""
WITH lag_rows AS (
    SELECT DATE(received_at) AS day,
           mode,
           TIMESTAMPDIFF(SECOND, occurred_at, received_at) AS lag_s,
           ROW_NUMBER() OVER (
               PARTITION BY DATE(received_at), mode
               ORDER BY TIMESTAMPDIFF(SECOND, occurred_at, received_at)
           ) AS rn,
           COUNT(*) OVER (PARTITION BY DATE(received_at), mode) AS cnt
    FROM {TABLE}
    WHERE {where}
)
SELECT day,
       mode,
       MAX(CASE WHEN rn = CEIL(cnt * 0.50) THEN lag_s END) AS p50_s,
       MAX(CASE WHEN rn = CEIL(cnt * 0.95) THEN lag_s END) AS p95_s,
       MIN(lag_s) AS min_s,
       MAX(lag_s) AS max_s,
       MAX(cnt) AS samples
FROM lag_rows
GROUP BY day, mode
ORDER BY day, mode
""".strip()

    reasons = f"""
SELECT DATE(received_at) AS day,
       LEFT(REPLACE(REPLACE(reason, '\\r', ' '), '\\n', ' '), 120) AS reason,
       COUNT(*) AS cnt
FROM {TABLE}
WHERE {where}
  AND allowed = 0
  AND LOWER(reason) REGEXP 'error|expired|no bundle|missing|verify|invalid|fail'
GROUP BY day, LEFT(REPLACE(REPLACE(reason, '\\r', ' '), '\\n', ' '), 120)
ORDER BY day, cnt DESC
LIMIT {int(reason_limit)}
""".strip()

    summary = f"""
SELECT COUNT(*) AS rows_total,
       SUM(allowed = 1) AS allowed,
       SUM(allowed = 0) AS denied,
       SUM(reason LIKE 'shadow:%') AS shadow_rows,
       COUNT(DISTINCT agent_id) AS agents,
       COUNT(DISTINCT bundle_id) AS bundles,
       MIN(received_at) AS first_received,
       MAX(received_at) AS last_received
FROM {TABLE}
WHERE {where}
""".strip()

    queries = {
        "summary": summary,
        "daily_mode": daily_mode,
        "daily_agent": daily_agent,
        "lag": lag,
        "reasons": reasons,
    }
    for sql in queries.values():
        assert_read_only(sql)
    return queries


def unescape_mysql_batch(value: str) -> str:
    out: List[str] = []
    i = 0
    while i < len(value):
        ch = value[i]
        if ch == "\\" and i + 1 < len(value):
            nxt = value[i + 1]
            out.append({"t": "\t", "n": "\n", "0": "\x00", "\\": "\\"}.get(nxt, nxt))
            i += 2
        else:
            out.append(ch)
            i += 1
    return "".join(out)


def run_query(mysql: str, db: DBConfig, sql: str, timeout: int) -> List[Dict[str, str]]:
    assert_read_only(sql)
    cmd = [
        mysql,
        "--batch",
        "--skip-column-names",
        "--default-character-set=utf8mb4",
        "--local-infile=0",
        "--protocol=tcp",
        "--connect-timeout=10",
        "--host",
        db.host,
        "--port",
        str(db.port),
        "--user",
        db.user,
        "--database",
        db.database,
        "--execute",
        sql,
    ]
    env = dict(os.environ)
    env["MYSQL_PWD"] = db.password
    try:
        proc = subprocess.run(
            cmd,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=timeout,
            check=False,
        )
    except OSError as exc:
        raise RuntimeError(f"failed to run mysql: {exc}") from exc
    except subprocess.TimeoutExpired as exc:
        raise RuntimeError(f"mysql query timed out after {timeout}s") from exc
    if proc.returncode != 0:
        detail = (proc.stderr or "").strip()
        if len(detail) > 800:
            detail = detail[:800] + "..."
        raise RuntimeError(f"mysql exit {proc.returncode}: {detail or 'no stderr'}")
    rows: List[Dict[str, str]] = []
    for line in proc.stdout.splitlines():
        if line == "":
            continue
        fields = [unescape_mysql_batch(part) for part in line.split("\t")]
        rows.append({str(i): value for i, value in enumerate(fields)})
    return rows


def pick(row: Dict[str, str], index: int) -> str:
    return row.get(str(index), "")


def to_int(value: str, default: int = 0) -> int:
    if value in ("", "NULL", "\\N"):
        return default
    try:
        return int(float(value))
    except ValueError:
        return default


def fmt_num(value: str) -> str:
    if value in ("", "NULL", "\\N"):
        return "-"
    try:
        return f"{int(float(value)):,}"
    except ValueError:
        return value


def md_cell(value: object) -> str:
    text = str(value)
    text = text.replace("\\", "\\\\").replace("|", "\\|")
    text = text.replace("\r", " ").replace("\n", " ").replace("\t", " ")
    return text.strip()


def md_table(headers: Sequence[str], rows: Iterable[Sequence[object]]) -> str:
    lines = [
        "| " + " | ".join(md_cell(h) for h in headers) + " |",
        "|" + "|".join("---" for _ in headers) + "|",
    ]
    for row in rows:
        cells = [md_cell(cell) for cell in row]
        if len(cells) != len(headers):
            cells = cells[: len(headers)] + [""] * (len(headers) - len(cells))
        lines.append("| " + " | ".join(cells) + " |")
    return "\n".join(lines)


def parse_metric_lines(text: str) -> List[Tuple[str, str, str]]:
    pattern = re.compile(
        r"^(?P<name>[a-zA-Z_:][a-zA-Z0-9_:]*)"
        r"(?:\{(?P<labels>[^}]*)\})?"
        r"\s+(?P<value>[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?)\s*$"
    )
    found: List[Tuple[str, str, str]] = []
    for line in text.splitlines():
        if "abac" not in line.lower() or line.startswith("#"):
            continue
        match = pattern.match(line.strip())
        if not match:
            continue
        name = match.group("name")
        if "abac" not in name.lower():
            continue
        found.append((name, match.group("labels") or "", match.group("value")))
    return sorted(found)


def fetch_metrics(base_url: str, timeout: int) -> Tuple[Optional[List[Tuple[str, str, str]]], str]:
    parts = urlsplit(base_url)
    if parts.scheme not in ("http", "https"):
        return None, f"unsupported metrics URL scheme {parts.scheme!r} (http/https only)"
    url = base_url.rstrip("/")
    if not url.endswith("/metrics"):
        url += "/metrics"
    request = Request(url, headers={"User-Agent": "opsmesh-abac-soak-report/1"})
    try:
        with urlopen(request, timeout=timeout) as response:
            body = response.read().decode("utf-8", errors="replace")
    except (HTTPError, URLError, TimeoutError, OSError) as exc:
        return None, f"{url}: {exc}"
    return parse_metric_lines(body), url


def render_report(
    *,
    db: DBConfig,
    start: datetime,
    end: datetime,
    summary: Optional[Dict[str, str]],
    daily_mode: Sequence[Dict[str, str]],
    daily_agent: Sequence[Dict[str, str]],
    lags: Sequence[Dict[str, str]],
    reasons: Sequence[Dict[str, str]],
    metrics: Optional[Sequence[Tuple[str, str, str]]],
    metrics_note: str,
) -> str:
    generated = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    out: List[str] = [
        "# ABAC decision soak report",
        "",
        f"- Generated: `{generated}`",
        f"- Window (UTC): `{sql_time(start)}` – `{sql_time(end)}` (end exclusive)",
        f"- Database: `{db.label()}`",
        f"- Source: `{TABLE}` (read-only SELECT)",
        "",
        "## Summary",
        "",
    ]

    if summary is None:
        out.append("_No summary row returned._")
    else:
        total = to_int(pick(summary, 0))
        allowed = to_int(pick(summary, 1))
        denied = to_int(pick(summary, 2))
        shadow = to_int(pick(summary, 3))
        out.append(
            md_table(
                ["metric", "value"],
                [
                    ["rows", f"{total:,}"],
                    ["allowed", f"{allowed:,}"],
                    ["denied", f"{denied:,}"],
                    ["deny rate", f"{(denied / total * 100):.2f}%" if total else "-"],
                    ["shadow-prefixed rows (would-be)", f"{shadow:,}"],
                    ["non-shadow rows (enforced or other)", f"{max(total - shadow, 0):,}"],
                    ["distinct agents", fmt_num(pick(summary, 4))],
                    ["distinct bundle IDs", fmt_num(pick(summary, 5))],
                    ["first received_at", pick(summary, 6) or "-"],
                    ["last received_at", pick(summary, 7) or "-"],
                ],
            )
        )

    out += ["", "## Daily counts by mode", ""]
    if daily_mode:
        out.append(
            md_table(
                ["day", "mode", "total", "allowed", "denied", "deny rate", "shadow rows", "agents", "bundles"],
                [
                    [
                        pick(row, 0),
                        pick(row, 1),
                        fmt_num(pick(row, 2)),
                        fmt_num(pick(row, 3)),
                        fmt_num(pick(row, 4)),
                        f"{(to_int(pick(row, 4)) / to_int(pick(row, 2)) * 100):.2f}%"
                        if to_int(pick(row, 2))
                        else "-",
                        fmt_num(pick(row, 5)),
                        fmt_num(pick(row, 6)),
                        fmt_num(pick(row, 7)),
                    ]
                    for row in daily_mode
                ],
            )
        )
    else:
        out.append("_No decision rows in the selected window._")

    out += ["", "## Daily counts by agent and mode", ""]
    if daily_agent:
        out.append(
            md_table(
                ["day", "agent", "mode", "total", "allowed", "denied", "shadow rows", "bundles"],
                [
                    [
                        pick(row, 0),
                        pick(row, 1),
                        pick(row, 2),
                        fmt_num(pick(row, 3)),
                        fmt_num(pick(row, 4)),
                        fmt_num(pick(row, 5)),
                        fmt_num(pick(row, 6)),
                        fmt_num(pick(row, 7)),
                    ]
                    for row in daily_agent
                ],
            )
        )
    else:
        out.append("_No rows._")

    out += ["", "## Upload lag (occurred_at -> received_at, seconds)", ""]
    if lags:
        out.append(
            md_table(
                ["day", "mode", "p50", "p95", "min", "max", "samples"],
                [
                    [
                        pick(row, 0),
                        pick(row, 1),
                        fmt_num(pick(row, 2)),
                        fmt_num(pick(row, 3)),
                        fmt_num(pick(row, 4)),
                        fmt_num(pick(row, 5)),
                        fmt_num(pick(row, 6)),
                    ]
                    for row in lags
                ],
            )
        )
    else:
        out.append("_No rows._")

    out += ["", "## Error-ish deny reasons (top N)", ""]
    if reasons:
        out.append(
            md_table(
                ["day", "reason (truncated)", "count"],
                [[pick(row, 0), pick(row, 1), fmt_num(pick(row, 2))] for row in reasons],
            )
        )
    else:
        out.append("_No deny reasons matching error/expired/no bundle/missing/verify/invalid/fail._")

    out += ["", "## Controlplane /metrics (ABAC counters, if exported)", ""]
    if metrics is None:
        out.append(f"_Metrics unavailable: {md_cell(metrics_note)}_")
    elif not metrics:
        out.append(
            "_No ABAC counters found. This build exports `opsmesh_abac_shadow_decisions_total` "
            "and `opsmesh_abac_enforce_decisions_total` at most; device bundle-fetch and "
            "decision-upload counters are not exported — use controlplane/agent logs._"
        )
    else:
        out.append(
            md_table(
                ["metric", "labels", "value"],
                [[name, labels or "-", value] for name, labels, value in metrics],
            )
        )
    out.append("")
    return "\n".join(out)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Generate a read-only Markdown soak report from abac_decision_logs.",
        epilog=(
            "Exit codes: 0 report generated; 2 usage/DB/mysql/output error. "
            "The script only runs SELECT statements and never writes to the database."
        ),
    )
    parser.add_argument(
        "--dsn",
        default=os.environ.get(DEFAULT_ENV_DSN),
        help=f"DSN (default env {DEFAULT_ENV_DSN}); forms: mysql://user:pass@host:3306/db or user:pass@host:3306/db",
    )
    parser.add_argument("--host", help="MySQL host (overrides DSN)")
    parser.add_argument("--port", type=int, help="MySQL port (overrides DSN)")
    parser.add_argument("--user", help="MySQL user (overrides DSN)")
    parser.add_argument("--password", help="MySQL password (overrides DSN; prefer env MYSQL_PWD/DSN)")
    parser.add_argument("--database", help="MySQL database (overrides DSN)")
    parser.add_argument("--mysql", default="mysql", help="mysql CLI path (default: mysql)")
    parser.add_argument(
        "--days",
        type=int,
        default=DEFAULT_DAYS,
        help=f"window length ending now, in days (default {DEFAULT_DAYS}; ignored with --since)",
    )
    parser.add_argument("--since", help="UTC start, YYYY-MM-DD or ISO-8601")
    parser.add_argument("--until", help="UTC end (exclusive), YYYY-MM-DD or ISO-8601")
    parser.add_argument("--base-url", default=os.environ.get(DEFAULT_ENV_BASE_URL), help=f"controlplane base URL (env {DEFAULT_ENV_BASE_URL}); fetches /metrics")
    parser.add_argument("--metrics-timeout", type=int, default=DEFAULT_METRICS_TIMEOUT, help="metrics fetch timeout seconds")
    parser.add_argument("--reason-limit", type=int, default=20, help="max error-ish reason rows (default 20)")
    parser.add_argument("--mysql-timeout", type=int, default=DEFAULT_MYSQL_TIMEOUT, help="per-query mysql timeout seconds")
    parser.add_argument("--out", help="write the Markdown report to this file instead of stdout")
    parser.add_argument("--print-sql", action="store_true", help="print the read-only SQL and exit without connecting")
    return parser


def resolve_config(parser: argparse.ArgumentParser, args: argparse.Namespace) -> DBConfig:
    try:
        db = parse_dsn(args.dsn) if args.dsn else DBConfig()
    except ValueError as exc:
        parser.error(f"invalid DSN: {exc}")
    if args.host:
        db.host = args.host
    if args.port is not None:
        if not (1 <= args.port <= 65535):
            parser.error("--port must be 1..65535")
        db.port = args.port
    if args.user:
        db.user = args.user
    if args.password is not None:
        db.password = args.password
    if args.database:
        db.database = args.database
    try:
        _valid_identifier(db.host, "host")
        _valid_identifier(db.user, "user")
        _valid_identifier(db.database, "database")
    except ValueError as exc:
        parser.error(str(exc))
    return db


def resolve_window(parser: argparse.ArgumentParser, args: argparse.Namespace) -> Tuple[datetime, datetime]:
    now = datetime.now(timezone.utc).replace(microsecond=0)
    try:
        if args.since:
            start = parse_time(args.since)
        else:
            if args.days <= 0:
                parser.error("--days must be > 0")
            start = now - timedelta(days=args.days)
        if args.until:
            end = parse_time(args.until)
        else:
            end = now
    except ValueError as exc:
        parser.error(f"invalid time: {exc}")
    if end <= start:
        parser.error("--until must be later than --since/--days window")
    return start, end


def main(argv: Optional[Sequence[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    db = resolve_config(parser, args)
    start, end = resolve_window(parser, args)
    queries = build_queries(start, end, max(1, args.reason_limit))

    if args.print_sql:
        for name, sql in queries.items():
            print(f"-- {name}\n{sql};\n")
        return EXIT_OK

    mysql = shutil.which(args.mysql) or (args.mysql if os.path.isfile(args.mysql) else None)
    if not mysql:
        print(
            f"soak-report: 'mysql' CLI not found ({args.mysql!r}). Install the MySQL 8 client "
            "or pass --mysql <path>. No database connection was attempted.",
            file=sys.stderr,
        )
        return EXIT_ERROR

    try:
        summary_rows = run_query(mysql, db, queries["summary"], args.mysql_timeout)
        daily_mode = run_query(mysql, db, queries["daily_mode"], args.mysql_timeout)
        daily_agent = run_query(mysql, db, queries["daily_agent"], args.mysql_timeout)
        lags = run_query(mysql, db, queries["lag"], args.mysql_timeout)
        reasons = run_query(mysql, db, queries["reasons"], args.mysql_timeout)
    except RuntimeError as exc:
        print(f"soak-report: {exc}", file=sys.stderr)
        return EXIT_ERROR

    metrics: Optional[List[Tuple[str, str, str]]] = None
    metrics_note = "not requested"
    if args.base_url:
        metrics, metrics_note = fetch_metrics(args.base_url, max(1, args.metrics_timeout))
        if metrics is None:
            print(f"soak-report: warning: metrics fetch failed: {metrics_note}", file=sys.stderr)

    report = render_report(
        db=db,
        start=start,
        end=end,
        summary=summary_rows[0] if summary_rows else None,
        daily_mode=daily_mode,
        daily_agent=daily_agent,
        lags=lags,
        reasons=reasons,
        metrics=metrics,
        metrics_note=metrics_note,
    )

    if args.out:
        try:
            with open(args.out, "w", encoding="utf-8", newline="\n") as handle:
                handle.write(report)
        except OSError as exc:
            print(f"soak-report: cannot write {args.out}: {exc}", file=sys.stderr)
            return EXIT_ERROR
        print(f"soak-report: wrote {args.out}", file=sys.stderr)
    else:
        print(report)
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
