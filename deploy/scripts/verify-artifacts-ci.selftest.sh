#!/usr/bin/env bash
# 参数解析自测——钉住 2026-11-02 v0.17.0 那次真红：
#   旧写法 `VER="${1:-${GITHUB_REF_NAME:-}}"` 在只传 `--pre-release` 时把选项当成版本号
#   （$1 恰是它），于是去等一个叫 `v--pre-release` 的 tag，20 次轮询后按"未知"退化，
#   再以选项串当版本调判据脚本 ⇒ usage + exit 2 ⇒ release 红在自家参数上。
# 判据取向：既要绿样本（正常取版本），也要红样本（三种"取不到版本"的形态都必须 exit 2）。
set -uo pipefail
S="$(dirname "$0")/verify-artifacts-ci.sh"
PASS=0 FAIL=0

chk() { # chk <期望VER> <期望PRE> <GITHUB_REF_NAME 或 -> [args...]
  local want_v="$1" want_p="$2" ref="$3"; shift 3
  local out rc
  if [ "$ref" = "-" ]; then
    out="$(env -u GITHUB_REF_NAME OPSMESH_VERIFY_DRY_RUN=1 bash "$S" "$@" 2>&1)"; rc=$?
  else
    out="$(GITHUB_REF_NAME="$ref" OPSMESH_VERIFY_DRY_RUN=1 bash "$S" "$@" 2>&1)"; rc=$?
  fi
  if [ "$rc" -eq 0 ] && [ "$out" = "VER=${want_v} PRE_RELEASE=${want_p}" ]; then
    echo "  [PASS] ref=${ref} args=[$*] ⇒ ${out}"; PASS=$((PASS+1))
  else
    echo "  [FAIL] ref=${ref} args=[$*] ⇒ rc=${rc} out=${out}（期望 VER=${want_v} PRE_RELEASE=${want_p}）"
    FAIL=$((FAIL+1))
  fi
}

chk_usage() { # chk_usage <说明> <ref 或 -> [args...]
  local desc="$1" ref="$2"; shift 2
  local out rc
  if [ "$ref" = "-" ]; then
    out="$(env -u GITHUB_REF_NAME OPSMESH_VERIFY_DRY_RUN=1 bash "$S" "$@" 2>&1)"; rc=$?
  else
    out="$(GITHUB_REF_NAME="$ref" OPSMESH_VERIFY_DRY_RUN=1 bash "$S" "$@" 2>&1)"; rc=$?
  fi
  if [ "$rc" -eq 2 ] && { [[ "$out" == *"用法:"* ]] || [[ "$out" == *"未知选项"* ]]; }; then
    echo "  [PASS] ${desc} ⇒ exit 2 + usage"; PASS=$((PASS+1))
  else
    echo "  [FAIL] ${desc} ⇒ rc=${rc} out=${out}（期望 exit 2 + usage）"; FAIL=$((FAIL+1))
  fi
}

echo "== verify-artifacts-ci.sh 参数解析自测 =="
# 绿样本
chk 0.17.0 1 v0.17.0 --pre-release          # 回归本体：只给选项时从 REF 取名
chk 0.17.0 0 v0.17.0                        # 无参数、只有 REF（未要 --pre-release）
chk 0.17.0 1 - 0.17.0 --pre-release         # 显式版本（无 REF 也成立）
chk 9.9.9  0 - v9.9.9                       # v 前缀剥离
chk 0.17.0 0 v0.17.0 0.17.0                 # 显式版本优先于 REF
# 红样本：三种取不到版本的形态必须 exit 2，不许带着垃圾串往下走
chk_usage "无版本且无 REF"           -
chk_usage "只给 --pre-release 且无 REF" -
chk_usage "未知选项"                 v0.17.0 --nonsense
echo "  参数解析自测：通过=${PASS} 失败=${FAIL}"
[ "$FAIL" -eq 0 ]
