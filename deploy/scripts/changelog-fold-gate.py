#!/usr/bin/env python3
# changelog-fold-gate.py —— CHANGELOG 归版账目的判据（部署资产门禁第 16 节）。
#
# 判据：`## [Unreleased]` 的标题日期不得早于最新已发布版本的标题日期——早于即说明它随
# 某个已发布版本发出去了，标题却还写着未发布（客户按 Keep-a-Changelog 读这份文件，
# 会把已交付的东西当成欠账）。
#
# 为什么是「日期比较」而不是「查 git tag」：本门禁所在的 CI job 是浅检出（未设
# fetch-depth: 0），拿不到 tag 列表；真要查 tag 会让这道门禁在 CI 里静默空转。
#
# 为什么还要一本豁免账（TD-96）：**tag 之后才落地**的块，作者按工作发生日写标题——
# 日期早于发布日，可它确实没发货、不该归版。日期这一个信号分辨不出「真漏归版」与
# 「tag 后落地」，所以后者在 changelog-fold-exempt.tsv 里显式记账，判据读账放行，
# 并在 SUMMARY 里报出条数（豁免不许变成后门）。
# 账目不能静默增长：账里每条都必须仍对应一个当前的 [Unreleased] 块、且该块确实早于
# 发布日；否则是过期条目 ⇒ 判红（块已归版/改名就删行，块日期已不早于发布日就说明
# 它已不需要豁免）。
#
# 用法: changelog-fold-gate.py [CHANGELOG.md] [豁免账.tsv]
# 输出: 首行 SUMMARY released_max=<日期|NONE> released_blocks=N unreleased=N exempt=N
#       offenders=N stale=N；其后每条一行 `OFFENDER L<n>: …` 或 `STALE L<n>: …`
# 退出码: 0=通过（含被豁免的块） 1=有 offender / 过期条目 / 无发布基准 3=用法错误
import os
import re
import sys

# 控制台默认编码在 Windows 上是 cp936：offender 明细含中文，重定向时按 utf-8 写
# 才不会变成 UnicodeEncodeError（那会把「判红」伪装成「脚本崩了」）。
try:
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
except Exception:
    pass

DATE = re.compile(r" — (\d{4}-\d{2}-\d{2})")


# 标题行的严格定义：`## [x.y.z]` / `## [Unreleased]` 独占一行，或后跟 " — 描述"。
# 为什么这么严：正文里存在以 `## [Unreleased]` 开头的**散文行**（例如「把 `## [Unreleased]`
# 一次性改成 `## [0.12.0]`」），松判会把它当成无日期标题 ⇒ 假阳性判红。判据一旦会因散文
# 误报，下一次真错标就会被当成「又是老毛病」而被忽略。
def is_unreleased(line):
    return line == "## [Unreleased]" or line.startswith("## [Unreleased] — ")


def is_version(line):
    return bool(re.match(r"^## \[\d+\.\d+\.\d+\]( — |$)", line))


def load_ledger(path):
    """读豁免账：TSV，`<块标题整行>\t<理由>\t<登记日期>`；`#` 开头与空行忽略。"""
    entries = []
    if not os.path.exists(path):
        return entries
    with open(path, encoding="utf-8") as fh:
        for lineno, raw in enumerate(fh.read().split("\n"), 1):
            line = raw.rstrip("\r")
            if not line.strip():
                continue
            # 注释以 `#` 开头，可**条目正文本身就是块标题、也以 `## ` 开头**——
            # 只按 `#` 前缀排注释会把账里的每一条都当注释吃掉（实测：exempt 恒 0），
            # 故注释 = `#` 开头且不是 `## [` 标题行。
            head = line.lstrip()
            if head.startswith("#") and not head.startswith("## ["):
                continue
            parts = line.split("\t")
            title = parts[0].strip()
            if not title:
                continue
            entries.append((lineno, title, parts[1].strip() if len(parts) > 1 else ""))
    return entries


def main(argv):
    if len(argv) > 3:
        print(USAGE_LINE, file=sys.stderr)
        return 3
    changelog = argv[1] if len(argv) > 1 else "CHANGELOG.md"
    if len(argv) > 2:
        ledger_path = argv[2]
    else:
        ledger_path = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                   "changelog-fold-exempt.tsv")
    if not os.path.exists(changelog):
        print("SUMMARY released_max=NONE released_blocks=0 unreleased=0 exempt=0 "
              "offenders=1 stale=0")
        print("OFFENDER L0: 找不到 %s（本节无从核对，判红而不是放行）" % changelog)
        return 1

    with open(changelog, encoding="utf-8") as fh:
        lines = fh.read().split("\n")

    rel_dates = [DATE.search(l).group(1) for l in lines if is_version(l) and DATE.search(l)]
    if not rel_dates:
        print("SUMMARY released_max=NONE released_blocks=%d unreleased=0 exempt=0 "
              "offenders=1 stale=0" % sum(1 for l in lines if is_version(l)))
        print("OFFENDER L0: CHANGELOG 里一个已发布版本标题（## [x.y.z] — 日期）都没有——"
              "本节失去基准，判红而不是放行")
        return 1

    cut = max(rel_dates)
    n_ver = sum(1 for l in lines if is_version(l))

    ledger = load_ledger(ledger_path)
    exempt_titles = {title for _, title, _ in ledger}

    offenders, stale = [], []
    n_un = 0
    matched_ledger = set()
    for lineno, line in enumerate(lines, 1):
        if not is_unreleased(line):
            continue
        n_un += 1
        match = DATE.search(line)
        day = match.group(1) if match else None
        if not day:
            offenders.append("L%d: [Unreleased] 标题没有日期，无法判它是否该归版：%s"
                             % (lineno, line[:56]))
            continue
        if day >= cut:
            continue
        if line.strip() in exempt_titles:
            matched_ledger.add(line.strip())
        else:
            offenders.append("L%d: 日期 %s 早于最新发布版本日期 %s，却仍标 [Unreleased]：%s"
                             % (lineno, day, cut, line[:56]))

    for lineno, title, _reason in ledger:
        if title not in matched_ledger:
            stale.append("L%d: 豁免条目不生效（该标题当前不是「早于 %s 的 [Unreleased] 块」）"
                         "——块已归版/改名就删行，块日期已不早于发布日则本就无需豁免：%s"
                         % (lineno, cut, title[:56]))

    print("SUMMARY released_max=%s released_blocks=%d unreleased=%d exempt=%d offenders=%d "
          "stale=%d" % (cut, n_ver, n_un, len(matched_ledger), len(offenders), len(stale)))
    for item in offenders:
        print("OFFENDER " + item)
    for item in stale:
        print("STALE " + item)
    return 1 if (offenders or stale) else 0


USAGE_LINE = "用法: changelog-fold-gate.py [CHANGELOG.md] [豁免账.tsv]"

if __name__ == "__main__":
    sys.exit(main(sys.argv))
