#!/usr/bin/env bash
# OpsMesh Dockerfile digest-pin verifier (TD-89 residual).
# Fails (exit 1) when any FROM line lacks an @sha256:<64-hex> digest pin.

set -euo pipefail

FIX_HINT=0
REPORT=0
for arg in "$@"; do
  case "$arg" in
    --fix-hint) FIX_HINT=1 ;;
    --report) REPORT=1 ;;
    -h|--help)
      cat <<'EOF'
Usage: scripts/security/verify-digest-pins.sh [--fix-hint] [--report]

Verifies every FROM line in Dockerfile* files (repo root, deploy/, operator/)
contains an @sha256:<64-hex> digest pin. scratch and ARG-based FROM ($VAR)
are skipped with a warning.

Flags:
  --fix-hint   Print manual pin / Renovate instructions for unpinned images.
  --report     Print only "file:line" lines for unpinned FROMs (for inventory).

Exit codes: 0 all pinned, 1 any unpinned.
EOF
      exit 0
      ;;
    *)
      printf 'unknown argument: %s\n' "$arg" >&2
      exit 2
      ;;
  esac
done

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

FILE_LIST=()
while IFS= read -r -d '' f; do
  FILE_LIST+=("$f")
done < <( { find . -maxdepth 1 -type f -name 'Dockerfile*'; [ -d deploy ] && find deploy -type f -name 'Dockerfile*'; [ -d operator ] && find operator -type f -name 'Dockerfile*'; } | sort -u | tr '\n' '\0' )

if [ "${#FILE_LIST[@]}" -eq 0 ]; then
  echo "error: no Dockerfile* found." >&2
  exit 2
fi

total_unpinned=0
unpinned_entries=()
unpinned_images=()

for f in "${FILE_LIST[@]}"; do
  rel="${f#./}"
  file_fail=0
  file_lines=()
  n=0
  while IFS= read -r raw || [ -n "$raw" ]; do
    n=$((n + 1))
    trimmed="${raw#"${raw%%[![:space:]]*}"}"
    first="${trimmed%%[[:space:]]*}"
    upper_first="$(printf '%s' "$first" | tr '[:lower:]' '[:upper:]')"
    [ "$upper_first" = "FROM" ] || continue

    rest="${trimmed#"$first"}"
    rest="${rest#"${rest%%[![:space:]]*}"}"
    read -ra tokens <<< "$rest"

    image=""
    skip_next=0
    for tok in "${tokens[@]}"; do
      if [ "$skip_next" -eq 1 ]; then skip_next=0; continue; fi
      case "$tok" in
        --platform=*) continue ;;
        --platform) skip_next=1; continue ;;
        [Aa][Ss]) break ;;
        *) image="$tok"; break ;;
      esac
    done
    [ -n "$image" ] || continue

    low_image="$(printf '%s' "$image" | tr '[:upper:]' '[:lower:]')"
    if [ "$low_image" = "scratch" ]; then
      printf 'WARN %s:%d: scratch base skipped\n' "$rel" "$n" >&2
      continue
    fi
    case "$image" in
      \$*)
        printf 'WARN %s:%d: ARG-based FROM skipped (%s)\\n' "$rel" "$n" "$image" >&2
        continue
        ;;
    esac

    if [[ "$image" =~ @sha256:[0-9a-fA-F]{64}$ ]]; then
      :
    else
      file_fail=1
      total_unpinned=$((total_unpinned + 1))
      unpinned_entries+=("$rel:$n")
      _found=0
      for _existing in "${unpinned_images[@]:-}"; do
        if [ "$_existing" = "$image" ]; then _found=1; break; fi
      done
      if [ "$_found" -eq 0 ]; then
        unpinned_images+=("$image")
      fi
      file_lines+=("  line $n: $raw")
      if [ "$REPORT" -eq 1 ]; then
        printf '%s:%d\n' "$rel" "$n"
      fi
    fi
  done < "$f"

  if [ "$REPORT" -eq 0 ]; then
    if [ "$file_fail" -eq 0 ]; then
      printf 'PASS  %s\n' "$rel"
    else
      printf 'FAIL  %s\n' "$rel"
      for l in "${file_lines[@]}"; do printf '%s\n' "$l"; done
    fi
  fi
done

if [ "$REPORT" -eq 0 ]; then
  printf '\nSummary: %d unpinned FROM line(s) across %d file(s)\n' "$total_unpinned" "${#FILE_LIST[@]}"
fi

if [ "$FIX_HINT" -eq 1 ]; then
  printf '\n--fix-hint--\n'
  printf 'Manual pin: append @sha256:<64-hex> to each unpinned image reference.\n'
  printf '  crane digest <image>\n'
  printf '  # or: docker pull <image> && docker inspect --format "{{index .RepoDigests 0}}" <image>\n'
  if [ "${#unpinned_images[@]}" -gt 0 ]; then
    for img in "${unpinned_images[@]}"; do
      printf '  crane digest %s\n' "$img"
    done
  fi
  # shellcheck disable=SC2016 # 字面反引号是 markdown 内联代码样式（不是命令替换）
  printf 'Renovate: pinDigests is already enabled; run `npx renovate` (or wait for automated PRs).\n'
fi

if [ "$total_unpinned" -gt 0 ]; then
  exit 1
fi
exit 0
