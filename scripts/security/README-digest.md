# Dockerfile digest-pin verifier (TD-89 residual)

Local/CI-runnable gate that FAILS when any `FROM` line in the OpsMesh
Dockerfiles lacks an `@sha256:<64-hex>` digest pin. CI's existing check is
warn-only; this verifier makes the gap visible and actionable.

Files:

- `verify-digest-pins.sh` — POSIX-ish bash (`set -euo pipefail`)
- `verify-digest-pins.ps1` — PowerShell 5.1+ equivalent
- `digest-inventory.txt` — current unpinned `file:line` inventory (regenerate, do not hand-edit)

## Usage

Run from the repository root (scripts also resolve the repo root themselves).

```bash
# Gate mode: per-file PASS/FAIL with line numbers for unpinned lines.
./scripts/security/verify-digest-pins.sh

# Print manual pin / Renovate instructions.
./scripts/security/verify-digest-pins.sh --fix-hint

# Report mode: only "file:line" lines (used to regenerate the inventory).
./scripts/security/verify-digest-pins.sh --report > scripts/security/digest-inventory.txt || true
```

```powershell
.\scripts\security\verify-digest-pins.ps1
.\scripts\security\verify-digest-pins.ps1 -FixHint
.\scripts\security\verify-digest-pins.ps1 -Report > scripts\security\digest-inventory.txt
```

Exit codes: `0` all pinned, `1` any unpinned, `2` usage/prerequisite error.

## What is checked

- All `Dockerfile*` under the repo root, `deploy/` (recursive), and `operator/` (recursive).
- Every `FROM` line, including multi-stage (`AS`), `ARG`-driven bases, and
  `--platform=...` variants.
- `scratch` bases are skipped (implicitly pinned).
- `FROM $VAR` / `FROM ${VAR}` (ARG-based) bases are skipped with a warning.
- A pin is valid only as `@sha256:` followed by exactly 64 hexadecimal characters
  at the end of the image reference.

## How to get digests

For each unpinned image reference:

```bash
# Option A: crane (fast, no daemon required)
crane digest node:22-alpine

# Option B: Docker daemon
docker pull node:22-alpine
docker inspect --format '{{index .RepoDigests 0}}' node:22-alpine
```

Then append the digest to the `FROM` line:

```
FROM node:22-alpine@sha256:<64-hex> AS web
```

Repeat for every image in `digest-inventory.txt`.

## Renovate automation note

`renovate.json` already enables `pinDigests`. Renovate will open PRs that add
`@sha256:` pins to the `FROM` lines; merge them to close the gap automatically.
Until those PRs land, this verifier keeps the gap visible (`exit 1`).

Run locally to preview: `npx renovate` (requires a Renovate token and config).

## Current unpinned inventory

As of this kit's creation, **16** `FROM` lines are unpinned (see
`digest-inventory.txt`). The verifier exits `1` until they are pinned. Regenerate
the inventory after any change:

```bash
./scripts/security/verify-digest-pins.sh --report > scripts/security/digest-inventory.txt || true
```
