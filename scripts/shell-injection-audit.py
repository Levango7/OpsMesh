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
  C. "raw_interp 型"（2026-11-08 批次 7 起单列）：行把**请求体字段 req.<Field> 直接
     作为 fmt.Sprintf 的实参**插进命令串，且该行没有任何收口标记、所在逻辑块也没有
     校验标记。A/B 判据看的是「格式串里有 %s」，会漏掉「实参直接就是 req.X」这类
     裸拼（netdiag_ops.go / synthetic_ops.go 的 13 处即是），C 把它们单列成可复算
     基线。C 与 A/B 是**两个独立口径**：A/B 的历史数字不受 C 影响（616→576→575 仍可比），
     进度另看 raw_interp_total。

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
    # 批次 9 新增的收口助手（internal/controlplane/elk_script.go）：
    # esAPIURL        —— ES URL 在 Go 层拼完整后整体 Quote（不能把 Quote 结果塞回
    #                   双引号段：双引号内单引号是字面量，URL 会带上 %27）
    # esQuoteJSONText —— 用户自由 JSON：校验合法性后整体 Quote
    # esJSONPayload   —— Go 层 map + json.Marshal 构造 JSON 后整体 Quote
    # esEcho          —— 回显消息整体 Quote（双引号段内 $ / 反引号会展开）
    # 这四个名字只在**收口后的**代码里出现（旧代码没有它们），故扩充 marker 集合
    # **不改变历史口径**：批次 7/8 的 raw_interp 数字（333 / 320 / 269）仍可比。
    "esAPIURL(",
    "esQuoteJSONText(",
    "esJSONPayload(",
    "esEcho(",
    # 批次 10 新增的收口助手（internal/controlplane/ops_script.go + graph_script.go
    # + kafka_script.go）。这些名字**只在收口后的代码里出现**（旧代码没有它们）
    # ⇒ 扩充 marker 集合**不改变历史口径**：批次 7/8/9 的 raw_interp 数字
    # （333 / 320 / 269）与 derived_interp 数字（286）仍可比。
    # opsURL        —— URL 在 Go 层拼完整后整体 Quote（不能塞回双引号段）
    # opsQuoteJSONText —— 用户自由 JSON：校验合法性后整体 Quote
    # opsJSONPayload   —— Go 层 map + json.Marshal 构造 JSON 后整体 Quote
    # opsEcho          —— 回显消息整体 Quote（双引号段内 $ / 反引号会展开）
    # graphCommandType —— 图库类型白名单（它是**命令名** ⇒ 命令注入）
    # graphIdent       —— 图库层标识符白名单
    # graphPropDefs    —— 图库层属性定义片段白名单
    # graphSecret      —— 图库层密钥白名单（与方言无关）
    # graphPath        —— 图库层备份/恢复路径白名单
    # cypherShell      —— cypher-shell 调用：整条语句整体 Quote（shell 层）
    # nebulaConsole    —— nebula-console 调用：整条语句整体 Quote（shell 层）
    # kafkaACLOperation   —— ACL operation 白名单（选项值）
    # kafkaACLResourceFlag —— ACL 资源**选项名**白名单
    # kafkaACLResourceName —— ACL 资源名白名单（放行 `*`）
    # kafkaResource    —— Kafka 资源名白名单
    # kafkaTopic        —— 单个 topic 名白名单
    # kafkaTopicList    —— 逗号分隔 topic 列表白名单（逐项校验）
    # kafkaHeredoc      —— heredoc 写入：拒绝「含等于分隔符的行」的内容
    # kafkaHeredocf     —— kafkaHeredoc 的模板版
    "opsURL(",
    "opsQuoteJSONText(",
    "opsJSONPayload(",
    "opsEcho(",
    "graphCommandType(",
    "graphIdent(",
    "graphPropDefs(",
    "graphSecret(",
    "graphPath(",
    "cypherShell(",
    "nebulaConsole(",
    "kafkaACLOperation(",
    "kafkaACLResourceFlag(",
    "kafkaACLResourceName(",
    "kafkaResource(",
    "kafkaTopic(",
    "kafkaTopicList(",
    # 批次 11 新增的收口助手（internal/controlplane/mq_script.go）。这些名字**只在收口后的
    # 代码里出现**（旧代码没有它们）⇒ 扩充 marker 集合**不改变历史口径**。
    # mqName / mqNameAllowEmpty —— MQ 资源名白名单（字母数字开头；仅字母数字与 ._:@-）
    # mqVersion             —— 版本号白名单（落下载 URL 的路径段）
    # mqNode                —— 节点名白名单（`rabbit@host`，含 `@`）
    # mqNodeList            —— 逗号/空白分隔节点列表：逐项白名单后以空格连接，
    #                           交给 `for node in %s`（不能整体 Quote）
    # mqCSVList             —— 逗号分隔的**单参数**列表：逐项白名单后重新以逗号连接
    # mqRocketTopic / mqRocketTopicAllowEmpty —— RocketMQ topic 白名单（额外放行 % 与 |）
    # mqPath                —— 绝对路径白名单（备份路径 / heredoc 证书路径；后者不能 Quote）
    "kafkaHeredoc(",
    "kafkaHeredocf(",
    "mqName(",
    "mqNameAllowEmpty(",
    "mqVersion(",
    "mqNode(",
    "mqNodeList(",
    "mqCSVList(",
    "mqRocketTopic(",
    "mqRocketTopicAllowEmpty(",
    "mqPath(",
    # 批次 12 新增的收口助手（internal/controlplane/ops_sql.go、vector_ext_ops.go）。
    # 这些名字**只在收口后的代码里出现**⇒ 扩充 marker 集合**不改变历史口径**。
    # opsHost           —— 主机名白名单（字母数字开头；._:-，: 放行 IPv6）
    # opsDBName         —— 库名/表名白名单（字母数字/_ 开头；含 . _ -）
    # opsDBNameAllowEmpty —— 可选库名/表名（空串放行）
    # opsSQLString      —— SQL 字符串字面量里的单引号双写（' -> ''）
    # vectorType        —— 向量库类型枚举白名单（milvus/qdrant/pgvector）
    "opsHost(",
    "opsHostAllowEmpty(",
    "opsDBName(",
    "opsDBNameAllowEmpty(",
    "opsSQLString(",
    "vectorType(",
)
VALIDATE_MARKERS = (
    "Validate",
    "MatchString(",
    "regexp.",
    # 批次 12/13 的校验型白名单助手（internal/controlplane/ops_sql.go 等）。
    # 这些助手**只在收口后的代码里出现**——扩充本集合**不改变历史口径**。
    "opsHost(",
    "opsDBName(",
    "opsDBNameAllowEmpty(",
    "opsSQLString(",
    "opsArgList(",
    "opsHeredocContent(",
    "opsOracleName(",
    "opsFileName(",
    "opsTableList(",
    "opsPatchID(",
    "opsSQLID(",
    "opsName(",
    "opsProfile(",
    "opsModule(",
    "opsSched(",
    "opsDeviceName(",
    "opsDigits(",
    "opsDomain(",
    "opsMemoryValue(",
    "opsSysctlParam(",
    "opsSysctlValue(",
    "opsHTTPURL(",
    "opsIdentifier(",
    "validateNetworkTarget(",
)
SPRINTF_RE = re.compile(r"fmt\.Sprintf\(")
FMT_VERB_RE = re.compile(r"%[sv]")
FMT_LITERAL_RE = re.compile(r'fmt\.Sprintf\(\s*[`"]([^`"]*)[`"]')
CONCAT_RE = re.compile(r"\+\s+[a-zA-Z_]\w*\s+\+")
POWERSHELL_RE = re.compile(r"powershell", re.I)
CMD_RE = re.compile(r"\breg\s|cmd /C|winget|choco|scoop|msiexec|sc\.exe", re.I)
JSONISH_RE = re.compile(r"WriteJSON|\"error|StatusBadRequest")
REQ_FIELD_RE = re.compile(r"\breq\.[A-Za-z_]\w*")

# ---- 判据 D（derived_interp）：派生局部变量形态 ----
#
# 为什么需要判据 D：判据 C 只抓「req.* 字面量**直接**作为 Sprintf 实参」，
# 但真实代码里更常见的是**派生局部变量**形态：
#
#     body := req.Body            // ← 污点源
#     ...
#     script = fmt.Sprintf(`... -d '%s' ...`, body)   // ← 注入点（判据 C 抓不到）
#
# 批次 8/9 的收口过程里，正是这种形态最容易被漏掉（收口时要逐处**溯源**到
# 派生变量才能确认覆盖完整）。
#
# 作用域：按 Go **函数（handler）边界**切块，变量表在块内维护 ⇒ 不会把 A handler
# 的派生变量配到 B handler 的脚本行（这是纯文件级扫描必然出现的误配）。
FUNC_RE = re.compile(r"^func\s")
DERIVE_RE = re.compile(r"^\s*(\w+)\s*:?=\s*req\.([A-Za-z_]\w*)\s*$")
CMD_ASSIGN_RE = re.compile(r"^\s*(?:script|cmd|command|args|argv)\s*:?=\s*fmt\.Sprintf\(")


def split_sprintf_args(text: str) -> list:
    """粗切 fmt.Sprintf( 的实参列表。

    只做**实参边界**切分，不解析字符串字面量内部的逗号——本判据只关心
    「实参是不是一个派生变量名」，内部逗号不影响结论（派生变量名是单个词）。
    """
    start = text.find("fmt.Sprintf(")
    if start < 0:
        return []
    rest = text[start + len("fmt.Sprintf("):]
    # 跳过格式串字面量
    if rest.startswith("`"):
        end = rest.find("`", 1)
        if end < 0:
            return []
        rest = rest[end + 1:]
    elif rest.startswith('"'):
        end = 1
        while end < len(rest):
            if rest[end] == "\\":
                end += 2
                continue
            if rest[end] == '"':
                break
            end += 1
        rest = rest[end + 1:]
    out, depth, cur = [], 0, ""
    for ch in rest:
        if ch in "([{":
            depth += 1
        elif ch in ")]}":
            if depth == 0:
                break
            depth -= 1
        if ch == "," and depth == 0:
            out.append(cur.strip())
            cur = ""
            continue
        cur += ch
    if cur.strip():
        out.append(cur.strip())
    return out


def is_plain_var(arg: str, var: str) -> bool:
    """实参是否**就是**该派生变量（裸传），而不是被 Quote/校验包过。

    `shellquote.Quote(body)`、`ValidateName(body, 255)` 这类都**不算**裸传——
    前者是收口、后者是白名单校验，两者都消除了注入面。
    """
    a = arg.strip()
    if not re.match(r"^[A-Za-z_]\w*$", a):
        return False
    if a != var:
        return False
    return True


def derived_findings(rel: str, lines: list) -> list:
    """判据 D：在**单个 handler 作用域**内找派生变量裸传入脚本的候选行。"""
    out = []
    bounds = [i for i, ln in enumerate(lines) if FUNC_RE.match(ln)]
    bounds.append(len(lines))
    for bi in range(len(bounds) - 1):
        start, end = bounds[bi], bounds[bi + 1]
        derived = {}  # 变量名 -> 源字段
        for i in range(start, end):
            line = lines[i]
            stripped = line.strip()
            if stripped.startswith("//"):
                continue
            m = DERIVE_RE.match(line)
            if m:
                derived[m.group(1)] = m.group(2)
                continue
            if not CMD_ASSIGN_RE.match(line):
                continue
            # 已在同一行收口/校验过的实参不算（Quote marker / Validate marker）
            if has_quote_marker(line):
                continue
            if has_validate_marker(block_window(lines, i)):
                continue
            for arg in split_sprintf_args(line):
                for var in derived:
                    if is_plain_var(arg, var):
                        out.append(
                            {
                                "file": rel,
                                "line": i + 1,
                                "kind": "derived_interp",
                                "ctx": classify(line),
                                "text": stripped[:200],
                                "var": var,
                                "source_field": derived[var],
                            }
                        )
                        break
    return out
# 命令变量的构造行：`script := fmt.Sprintf(` / `cmd = fmt.Sprintf(` …
# 为什么要限定"命令变量"：判据 C 看的是**实参**是不是 req.<Field>，但实参进的是
# 不是命令串，取决于这个 Sprintf 的结果被拿去干什么。只认命令变量可排除
# "先拼一段文案、之后再单独 Quote"的形态（如 synthetic_ops.go 的 msg := 行）——
# 那种形态本身是安全的（文案不进脚本）。
COMMAND_VAR_RE = re.compile(r"\b(?:script|cmd|command|shellScript|payload)\b\s*(?::=|=)\s*fmt\.Sprintf\(")


def _skip_format_literal(rest: str) -> str:
    """跳过 fmt.Sprintf 的格式串字面量，返回其后的实参部分。

    为什么要跳：格式串里的 `%s` 不是"实参"，判据 C 看的是**实参**是不是
    `req.<Field>`（用户字段直插）。反引号串里可含双引号，双引号串里可含
    转义引号，故按各自的词法规则扫描闭合位置。
    """
    if not rest:
        return rest
    q = rest[0]
    if q == "`":
        end = rest.find("`", 1)
        return rest[end + 1 :] if end != -1 else rest
    if q == '"':
        i = 1
        while i < len(rest):
            if rest[i] == "\\":
                i += 2
                continue
            if rest[i] == '"':
                return rest[i + 1 :]
            i += 1
        return rest
    return rest


def raw_req_interp(line: str) -> bool:
    """该行是否把 `req.<Field>` 直接作为 fmt.Sprintf 的实参（用户字段裸插）。"""
    if not COMMAND_VAR_RE.search(line):
        return False
    m = SPRINTF_RE.search(line)
    if not m:
        return False
    args = _skip_format_literal(line[m.end() :])
    return bool(REQ_FIELD_RE.search(args))


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


# ---- 判据 E / F（raw_arg / raw_fprintf）：**实参级**污点 ----
# 背景：既有 A/B/C/D 都是**逐行**判定——同一行只要出现一个被 Quote 过的参数，整行即被跳过，
# 于是同行其余**裸**实参（req.* 直插）逃逸出盘点；且既有判定只认 `fmt.Sprintf(`，
# **`fmt.Fprintf(&builder, ...)` 这一整类脚本拼装从未被计入**（批次 12/13 的「0」是口径受限的零）。
#
# 判据 E：fmt.Sprintf 的**字符串槽**（%s/%v/%q）实参里裸引用 req.<字段> ⇒ 命中。
# 判据 F：fmt.Fprintf 的**目标**是脚本 builder（&x / x）且格式串含换行（脚本行特征）时，
#         其**字符串槽**实参里裸引用 req.<字段> ⇒ 命中。
#
# 数值槽（%d/%x/%f…）不可能携带 shell 元字符 ⇒ 不报（int 字段在 JSON 解码期即被类型挡掉）。
#
# 与 A/B/C/D **独立计数**——历史口径（616→576→575、333/320/269、286）均不受影响。
#
# 收口约定（批次 14 起）：脚本拼装里的用户字段**不再直接以 req.* 出现在实参位**——
# 要么整体 Quote/转义（shellquote.Quote / opsEcho / opsSQLString / strconv.Quote …），
# 要么用「已校验的局部变量」（xxxV）承接校验结果。校验一旦被误删，判据 E/F 会立刻复红。

RAW_ARG_SAFE_WRAPPERS = (
    "shellquote.Quote(",
    "shellquote.QuotePowerShellSingle(",
    "shellquote.PowerShellEncodedCommand(",
    "shellQuote(",
    "opsEcho(",
    "opsSQLString(",
    "opsHeredocContent(",
    "opsQuoteJSONText(",
    "esEcho(",
    "esAPIURL(",
    "opsURL(",
    "strconv.Quote(",
    "strconv.Itoa(",
    "strconv.FormatBool(",
)
RAW_ARG_STRING_VERBS = "svq"
_RAW_ARG_VERB_RE = re.compile(r"%[-+ #0]*[0-9.]*([svqdxobcefg])")
_RAW_ARG_REQ_RE = re.compile(r"\breq\.[A-Za-z_]\w*")


def _raw_arg_split_args(argstr):
    """按顶层逗号切分实参（处理括号/引号/反引号内的逗号）。"""
    args, depth, cur, i = [], 0, [], 0
    in_str = None
    while i < len(argstr):
        c = argstr[i]
        if in_str:
            if c == "\\" and in_str != "`":
                cur.append(c)
                i += 1
                if i < len(argstr):
                    cur.append(argstr[i])
                    i += 1
                continue
            if c == in_str:
                in_str = None
            cur.append(c)
            i += 1
            continue
        if c in "\"'`":
            in_str = c
            cur.append(c)
            i += 1
            continue
        if c in "([{":
            depth += 1
        elif c in ")]}":
            depth -= 1
        if c == "," and depth == 0:
            args.append("".join(cur).strip())
            cur = []
            i += 1
            continue
        cur.append(c)
        i += 1
    if "".join(cur).strip():
        args.append("".join(cur).strip())
    return args


def _raw_arg_call_end(s, start):
    """返回调用的匹配右括号位置（跳过字符串字面量）。"""
    depth, i, in_str = 0, start, None
    while i < len(s):
        c = s[i]
        if in_str:
            if c == "\\" and in_str != "`":
                i += 2
                continue
            if c == in_str:
                in_str = None
            i += 1
            continue
        if c in "\"'`":
            in_str = c
            i += 1
            continue
        if c == "(":
            depth += 1
        elif c == ")":
            depth -= 1
            if depth == 0:
                return i
        i += 1
    return -1


def _raw_arg_verbs(lit):
    """抽取格式串里的动词序列（忽略 %%）。"""
    verbs = []
    i = 0
    while i < len(lit):
        if lit[i] == "%":
            if i + 1 < len(lit) and lit[i + 1] == "%":
                i += 2
                continue
            m = _RAW_ARG_VERB_RE.match(lit, i)
            if m:
                verbs.append(m.group(1))
                i = m.end()
                continue
            i += 1
            continue
        i += 1
    return verbs


def _raw_arg_scan_args(args, verbs):
    """返回字符串槽里裸引用 req.* 的实参列表。"""
    out = []
    for idx, a in enumerate(args):
        if idx >= len(verbs):
            break
        if verbs[idx] not in RAW_ARG_STRING_VERBS:
            continue
        if not _RAW_ARG_REQ_RE.search(a):
            continue
        if any(w in a for w in RAW_ARG_SAFE_WRAPPERS):
            continue
        out.append(a)
    return out


def raw_arg_findings(rel, src):
    """判据 E：fmt.Sprintf 实参里裸引用的 req.*（仅字符串槽）。"""
    out = []
    lines = src.split("\n")
    for m in re.finditer(r"fmt\.Sprintf\(", src):
        end = _raw_arg_call_end(src, m.end() - 1)
        if end < 0:
            continue
        args = _raw_arg_split_args(src[m.end() : end])
        if not args:
            continue
        verbs = _raw_arg_verbs(args[0])
        for a in _raw_arg_scan_args(args[1:], verbs):
            line_no = src[: m.start()].count("\n") + 1
            out.append(
                {
                    "file": rel,
                    "line": line_no,
                    "kind": "raw_arg",
                    "ctx": classify(lines[line_no - 1]) if line_no <= len(lines) else "sh",
                    "text": a[:200],
                }
            )
    return out


_RAW_FPRINT_BUILDER_RE = re.compile(r"&[A-Za-z_]\w*|Builder|builder")


def raw_fprintf_findings(rel, src):
    """判据 F：fmt.Fprintf(&builder, ...) 里的裸 req.*（仅字符串槽）。"""
    out = []
    lines = src.split("\n")
    for m in re.finditer(r"fmt\.Fprintf?\(|fmt\.Fprintln\(", src):
        end = _raw_arg_call_end(src, m.end() - 1)
        if end < 0:
            continue
        args = _raw_arg_split_args(src[m.end() : end])
        if len(args) < 2:
            continue
        target = args[0]
        if not _RAW_FPRINT_BUILDER_RE.search(target):
            continue
        fmt = args[1]
        # 脚本行特征：格式串含**换行转义**（Go 源码里是反斜杠+n 两个字符）。
        # 注意：这里读的是**源码文本**，不是运行时的字符串——所以判据是 "\n"（两字符），
        # 而不是真换行（写成真换行会让整条判据恒不命中 ⇒ F 永远是 0）。
        if "\\n" not in fmt:
            continue
        verbs = _raw_arg_verbs(fmt)
        rest = args[2:]
        for a in _raw_arg_scan_args(rest, verbs):
            line_no = src[: m.start()].count("\n") + 1
            out.append(
                {
                    "file": rel,
                    "line": line_no,
                    "kind": "raw_fprintf",
                    "ctx": classify(lines[line_no - 1]) if line_no <= len(lines) else "sh",
                    "text": a[:200],
                }
            )
    return out


def scan(root: str):
    if not os.path.isdir(root):
        sys.exit("not a directory: %s" % root)
    findings = []
    raw_findings = []
    derived_findings_list = []
    raw_arg_list = []
    raw_fprintf_list = []
    files = []
    for dirpath, _dirnames, filenames in os.walk(root):
        for fn in sorted(filenames):
            if fn.endswith(".go") and not fn.endswith("_test.go"):
                files.append(os.path.join(dirpath, fn))

    for path in sorted(files):
        try:
            rel = os.path.relpath(path)
        except ValueError:
            # 跨盘符（Windows：root 在 C:、CWD 在 F:）时 relpath 会抛错，
            # 退化为相对 root 的路径，保证「扫任意目录」都能跑。
            rel = os.path.relpath(path, root)
        with open(path, encoding="utf-8", errors="replace") as f:
            lines = f.read().split("\n")
        for i, line in enumerate(lines):
            stripped = line.strip()
            if stripped.startswith("//"):
                continue
            ctx = classify(line)
            # 判据 C（raw_interp）：用户字段 req.* 直接作为实参插进命令串。
            # 与 A/B 独立计数——A/B 的历史口径（616→576→575）不受影响。
            if raw_req_interp(line) and not has_quote_marker(line):
                if not has_validate_marker(block_window(lines, i)):
                    raw_findings.append(
                        {"file": rel, "line": i + 1, "kind": "raw_interp", "ctx": ctx, "text": stripped[:200]}
                    )
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
        # 判据 D（derived_interp）：派生局部变量形态（按 handler 作用域）。
        # 与 A/B/C **独立计数**——A/B 的历史口径与 C 的 333/320/269 均不受影响。
        derived_findings_list.extend(derived_findings(rel, lines))
        # 判据 E / F（实参级污点；与 A/B/C/D 独立计数）。
        _src = "\n".join(lines)
        raw_arg_list.extend(raw_arg_findings(rel, _src))
        raw_fprintf_list.extend(raw_fprintf_findings(rel, _src))
    return findings, raw_findings, derived_findings_list, raw_arg_list, raw_fprintf_list


def main() -> int:
    ap = argparse.ArgumentParser(description="shell 注入面盘点（只读）")
    ap.add_argument("--root", default=DEFAULT_ROOT, help="扫描目录（默认 %s）" % DEFAULT_ROOT)
    ap.add_argument("--json", action="store_true", help="机器可读输出")
    args = ap.parse_args()

    findings, raw_findings, derived_findings_list, raw_arg_list, raw_fprintf_list = scan(args.root)

    if args.json:
        print(
            json.dumps(
                {
                    "root": args.root,
                    "total": len(findings),
                    "by_kind": dict(Counter(f["kind"] for f in findings)),
                    "by_ctx": dict(Counter(f["ctx"] for f in findings)),
                    "findings": findings,
                    "raw_interp_total": len(raw_findings),
                    "raw_interp_by_file": dict(Counter(f["file"] for f in raw_findings)),
                    "raw_interp": raw_findings,
                    "derived_interp_total": len(derived_findings_list),
                    "derived_interp_by_file": dict(
                        Counter(f["file"] for f in derived_findings_list)
                    ),
                    "derived_interp": derived_findings_list,
                    "raw_arg_total": len(raw_arg_list),
                    "raw_arg_by_file": dict(Counter(f["file"] for f in raw_arg_list)),
                    "raw_arg": raw_arg_list,
                    "raw_fprintf_total": len(raw_fprintf_list),
                    "raw_fprintf_by_file": dict(Counter(f["file"] for f in raw_fprintf_list)),
                    "raw_fprintf": raw_fprintf_list,
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
    print()
    print("=" * 72)
    print("判据 C（raw_interp，用户字段 req.* 直插实参）合计: %d" % len(raw_findings))
    raw_by_file = Counter(f["file"] for f in raw_findings)
    for fn, n in raw_by_file.most_common(25):
        print("  %-52s %d" % (fn, n))
    print()
    for f in raw_findings:
        print("  %s:%d [%s/%s] %s" % (f["file"], f["line"], f["kind"], f["ctx"], f["text"]))
    print()
    print("=" * 72)
    print("判据 D（derived_interp，派生局部变量形态）合计: %d" % len(derived_findings_list))
    der_by_file = Counter(f["file"] for f in derived_findings_list)
    for fn, n in der_by_file.most_common(25):
        print("  %-52s %d" % (fn, n))
    print()
    for f in derived_findings_list:
        print(
            "  %s:%d [%s/%s] var=%s <- req.%s | %s"
            % (
                f["file"],
                f["line"],
                f["kind"],
                f["ctx"],
                f["var"],
                f["source_field"],
                f["text"],
            )
        )
    print()
    print("=" * 72)
    print("判据 E（raw_arg，Sprintf 实参级裸 req.*）合计: %d" % len(raw_arg_list))
    for fn, n in Counter(f["file"] for f in raw_arg_list).most_common(25):
        print("  %-52s %d" % (fn, n))
    print()
    for f in raw_arg_list:
        print("  %s:%d [%s/%s] %s" % (f["file"], f["line"], f["kind"], f["ctx"], f["text"]))
    print()
    print("=" * 72)
    print("判据 F（raw_fprintf，Fprintf→脚本 builder 实参级裸 req.*）合计: %d" % len(raw_fprintf_list))
    for fn, n in Counter(f["file"] for f in raw_fprintf_list).most_common(25):
        print("  %-52s %d" % (fn, n))
    print()
    for f in raw_fprintf_list:
        print("  %s:%d [%s/%s] %s" % (f["file"], f["line"], f["kind"], f["ctx"], f["text"]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
