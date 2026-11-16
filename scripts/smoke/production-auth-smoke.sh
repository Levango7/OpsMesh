#!/usr/bin/env bash
# production-auth-smoke.sh — verifies the production auth baseline end to end.
#
# What it proves:
#   1. --store=mysql (persistent) without --production/--require-auth fail-fasts
#      unless --allow-insecure-store is passed.
#   2. --production implies require-auth: /healthz stays public, but
#      GET /api/v1/devices is 401 for anonymous and bogus-bearer callers.
#   3. A NON-production instance (memory + --demo --allow-insecure-store)
#      returns 200 for anonymous GET /api/v1/devices — the difference is real.
#
# Both server processes are always killed via trap. Exit 0 = all PASS.

set -u -o pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/opsmesh-smoke.XXXXXX")" || { echo "FAIL: mktemp"; exit 1; }
BIN="$TMP/opsmesh-smoke"
CERT="$ROOT/deploy/docker/certs/tls.crt"
KEY="$ROOT/deploy/docker/certs/tls.key"

PROD_PID=""
DEV_PID=""
FAILS=0

cleanup() {
  for pid in "$PROD_PID" "$DEV_PID"; do
    [ -n "$pid" ] || continue
    kill "$pid" 2>/dev/null || true
    # Git Bash/Windows fallback: MSYS kill may not reach the native process.
    if command -v taskkill >/dev/null 2>&1; then
      taskkill //PID "$pid" //F >/dev/null 2>&1 || true
    fi
  done
  [ -n "$PROD_PID" ] && wait "$PROD_PID" 2>/dev/null || true
  [ -n "$DEV_PID" ] && wait "$DEV_PID" 2>/dev/null || true
  rm -rf "$TMP" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

pass() { printf 'PASS  %s\n' "$1"; }
fail() { printf 'FAIL  %s\n' "$1"; FAILS=$((FAILS + 1)); }
info() { printf 'INFO  %s\n' "$1"; }

assert_status() {
  local label="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$label (HTTP $got)"; else fail "$label (want HTTP $want, got ${got:-none})"; fi
}

port_free() {
  ! (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}

pick_port() {
  local p i
  for i in 1 2 3 4 5 6 7 8 9 10; do
    p=$(( (RANDOM % 20000) + 30000 ))
    if port_free "$p"; then printf '%s' "$p"; return 0; fi
  done
  echo "FAIL: cannot find a free TCP port" >&2
  exit 1
}

wait_health() {
  local port="$1" pid="$2" i code
  for i in $(seq 1 150); do
    if ! kill -0 "$pid" 2>/dev/null; then return 1; fi
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "http://127.0.0.1:$port/healthz" 2>/dev/null || true)"
    [ "$code" = "200" ] && return 0
    sleep 0.2
  done
  return 1
}

http_status() {
  local url="$1"; shift
  curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$@" "$url" 2>/dev/null || true
}

show_logs() {
  for f in "$TMP/prod.log" "$TMP/dev.log"; do
    [ -s "$f" ] || continue
    info "---- $(basename "$f") (tail) ----"
    tail -n 40 "$f" || true
  done
}

if [ ! -f "$CERT" ] || [ ! -f "$KEY" ]; then
  fail "test TLS cert/key not found under deploy/docker/certs"
  exit 1
fi

info "building controlplane binary"
if ! (cd "$ROOT" && go build -o "$BIN" ./cmd/opsmesh); then
  fail "go build ./cmd/opsmesh"
  exit 1
fi

# --- assertion 1: fail-fast for persistent store without production/auth -----
info "checking fail-fast: --store=mysql without --production"
"$BIN" --mode=controlplane --store=mysql --mysql-dsn='u:p@tcp(127.0.0.1:3306)/ops_device' \
  --http-port="$(pick_port)" --grpc-port="$(pick_port)" --metrics-port="$(pick_port)" \
  --data-dir="$TMP/ff-data" >"$TMP/ff.log" 2>&1
FF_RC=$?
if [ "$FF_RC" -ne 0 ] && grep -q "persistent store without --production" "$TMP/ff.log"; then
  pass "fail-fast for persistent store without --production (exit=$FF_RC)"
else
  fail "fail-fast for persistent store without --production (exit=$FF_RC)"
  sed -n '1,20p' "$TMP/ff.log" 2>/dev/null || true
fi

# --- instance A: production baseline ----------------------------------------
P1="$(pick_port)"; G1="$(pick_port)"; M1="$(pick_port)"
JWT='smoke-jwt-secret-0123456789abcdef0123456789'
ENC='MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY='

info "starting production instance on :$P1 (memory store, --production)"
"$BIN" --mode=controlplane --store=memory --production \
  --http-port="$P1" --grpc-port="$G1" --metrics-port="$M1" \
  --data-dir="$TMP/prod-data" \
  --tls-cert="$CERT" --tls-key="$KEY" --http-tls=off \
  --jwt-secret="$JWT" --encryption-key="$ENC" \
  --admin-password='Sm0ke-Test!Passw0rd' \
  --log-level=info >"$TMP/prod.log" 2>&1 &
PROD_PID=$!

# --- instance B: non-production, explicit insecure opt-in --------------------
P2="$(pick_port)"; G2="$(pick_port)"; M2="$(pick_port)"
info "starting non-production instance on :$P2 (memory + --demo --allow-insecure-store)"
"$BIN" --mode=controlplane --store=memory --demo --allow-insecure-store \
  --http-port="$P2" --grpc-port="$G2" --metrics-port="$M2" \
  --data-dir="$TMP/dev-data" \
  --log-level=info >"$TMP/dev.log" 2>&1 &
DEV_PID=$!

if wait_health "$P1" "$PROD_PID"; then pass "production instance became healthy on :$P1"; else fail "production instance did not become healthy on :$P1"; fi
if wait_health "$P2" "$DEV_PID"; then pass "non-production instance became healthy on :$P2"; else fail "non-production instance did not become healthy on :$P2"; fi

if [ "$FAILS" -eq 0 ]; then
  assert_status "prod GET /healthz (no credential)" "200" "$(http_status "http://127.0.0.1:$P1/healthz")"
  assert_status "prod GET /api/v1/devices (anonymous)" "401" "$(http_status "http://127.0.0.1:$P1/api/v1/devices")"
  assert_status "prod GET /api/v1/devices (bogus bearer)" "401" "$(http_status "http://127.0.0.1:$P1/api/v1/devices" -H "Authorization: Bearer bogus-token")"
  assert_status "dev GET /api/v1/devices (anonymous, non-production)" "200" "$(http_status "http://127.0.0.1:$P2/api/v1/devices")"
else
  info "skipping HTTP assertions because a server failed to start"
fi

if [ "$FAILS" -ne 0 ]; then
  show_logs
  printf '\nRESULT: FAIL (%d failure(s))\n' "$FAILS"
  exit 1
fi

printf '\nRESULT: PASS\n'
exit 0
