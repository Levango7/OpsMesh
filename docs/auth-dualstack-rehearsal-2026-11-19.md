# Auth dual-stack rehearsal — 2026-11-19

**Scope:** TD-60 auth cutover rehearsal with real processes and real MySQL: controlplane
(`--store=mysql`) + auth-svc (`AUTH_SVC_STORE_TYPE=sql`) on one MySQL server, auth proxy
disabled at baseline, then briefly enabled with a one-user roster.

**Verdict:** matrix **22/22 PASS, 0 FAIL**. Teardown clean. **NO-GO for a literal 50/50
random switch on 11-25**; **conditional GO for the binary/roster batch path** (方案 C/D)
with the risks listed in §6.

---

## 1. Environment

| Item | Value |
|---|---|
| Repo / commit | `F:\Agent\kilo\workspace\OpsMesh` @ `4cbb62c0` (working tree only had pre-existing `docs/CLAIMS.md`, `docs/COORDINATION-BOARD.md`) |
| OS / toolchain | Windows, Go 1.26.6 windows/amd64, Docker Compose v5.7.0, curl 8.22.0 |
| MySQL | `mysql:8.0.33` container `opsmesh-auth-rehearsal-mysql`, host port `13306` |
| Controlplane store | MySQL database `opsmesh` (`opsmesh:opsmesh123@tcp(127.0.0.1:13306)/opsmesh?parseTime=true`) |
| auth-svc store | MySQL database `opsmesh_auth` on the **same server** (`.../opsmesh_auth?parseTime=true`) |
| Controlplane HTTP/gRPC/metrics | `19680 / 19681 / 19682` |
| auth-svc HTTP/gRPC | `19690 / 19691` |
| Redis | Not started. auth-svc ran with `AUTH_SVC_SESSION_STORE_ENABLED=false` |
| Shared secret | one HMAC secret for both processes (`rehearsal-shared-hmac-secret-...`) |

### 1.1 Why two schemas, not one database

The task says "same DB". This rehearsal used the **same MySQL server**, but auth-svc in
`opsmesh_auth` and controlplane in `opsmesh`, because the two products define tables with
the same names and different columns:

- controlplane: `internal/store/sqlstore/migrations/001_initial.sql:106-135`
  (`users.role_ids JSON`, `roles.permissions JSON`, no `must_change_password`).
- auth-svc: `services/auth-svc/internal/store/schema.sql`
  (`users.must_change_password`, `user_roles`, `role_permissions`, ...).

`CREATE TABLE IF NOT EXISTS` would silently keep the controlplane `users` table, then
auth-svc `seedDefaults` would fail on the missing columns. This is the architectural
boundary already documented in `docs/td60-auth-data-plane-proposal.md` §1/§2 and is a
hard prerequisite for any cutover: **same server is fine; same schema is not**.

### 1.2 Repo compose file defect hit during setup

`docker compose -f docker-compose.mysql.yml up -d mysql-master` fails to parse:

```
mapping key "ports" already defined at line 9 (at line 19)
```

`docker-compose.mysql.yml:9` and `:19` both define `ports`. A minimal equivalent compose
file was created outside the repo and used for this rehearsal:

```yaml
services:
  mysql:
    image: mysql:8.0.33
    container_name: opsmesh-auth-rehearsal-mysql
    ports: ["13306:3306"]
    environment:
      MYSQL_ROOT_PASSWORD: root1234
      MYSQL_ROOT_HOST: "%"
      MYSQL_DATABASE: opsmesh
      MYSQL_USER: opsmesh
      MYSQL_PASSWORD: opsmesh123
```

---

## 2. Exact commands

Working dir for logs/binaries:
`C:\Users\winge\AppData\Local\Temp\kilo\opsmesh-auth-rehearsal` (called `$W` below).

### 2.1 MySQL

```powershell
docker compose -f "$W\compose.mysql.yml" -p opsmesh-auth-rehearsal up -d
# wait for healthcheck
docker exec opsmesh-auth-rehearsal-mysql mysql -uroot -proot1234 -e `
  "CREATE DATABASE IF NOT EXISTS opsmesh_auth CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
docker exec opsmesh-auth-rehearsal-mysql mysql -uroot -proot1234 -e `
  "GRANT ALL PRIVILEGES ON opsmesh_auth.* TO 'opsmesh'@'%'; FLUSH PRIVILEGES;"
```

### 2.2 Build (no source changes)

```powershell
go build -o "$W\opsmesh-cp.exe" ./cmd/opsmesh
go build -o "$W\auth-svc.exe" ./cmd/auth-svc   # run in services/auth-svc
```

### 2.3 auth-svc (SQL, real MySQL)

```powershell
$env:AUTH_SVC_HTTP_ENABLED='true'
$env:AUTH_SVC_HTTP_PORT='19690'
$env:AUTH_SVC_GRPC_PORT='19691'
$env:AUTH_SVC_JWT_SECRET='rehearsal-shared-hmac-secret-0123456789abcdef'
$env:AUTH_SVC_STORE_TYPE='sql'
$env:AUTH_SVC_DSN='opsmesh:opsmesh123@tcp(127.0.0.1:13306)/opsmesh_auth?parseTime=true'
$env:AUTH_SVC_PUBLIC_REGISTER='true'
$env:AUTH_SVC_ADMIN_PASSWORD='Rehearsal-Admin-2026x!'
$env:AUTH_SVC_DEVICE_FP_ENABLED='false'
$env:AUTH_SVC_SESSION_STORE_ENABLED='false'
& "$W\auth-svc.exe" *> "$W\auth-svc.log"
```

Startup log confirmed: `MySQL store 已启用` and `HTTP gateway enabled`.

### 2.4 Controlplane — baseline (auth proxy OFF)

```powershell
$env:OPSMESH_JWT_SECRET='rehearsal-shared-hmac-secret-0123456789abcdef'
$env:OPSMESH_ADMIN_PASSWORD='Rehearsal-Admin-2026x!'
$env:OPSMESH_HTTP_TLS='off'
$env:AUTH_SVC_URL='http://127.0.0.1:19690'
$env:AUTH_SVC_PROXY_ENABLED='false'
& "$W\opsmesh-cp.exe" --http-port 19680 --grpc-port 19681 --metrics-port 19682 `
  --store mysql --mysql-dsn 'opsmesh:opsmesh123@tcp(127.0.0.1:13306)/opsmesh?parseTime=true' `
  --public-register=false --allow-insecure-store *> "$W\cp.log"
```

`AUTH_SVC_PROXY_ENABLED` semantics were verified in code, not inferred:

- `internal/controlplane/cutover/router.go:42-45` — `AuthEnabled()` is true only when the
  env value is exactly `"true"`; all route decisions return false otherwise.
- `internal/controlplane/auth_cutover.go:240-244` — forwarding additionally requires the
  svcproxy auth rule to be active (`Rule.IsActive()` reads the same env,
  `internal/controlplane/svcproxy/proxy.go:91-97`).
- Even with the master switch on, forwarding is **roster-gated** for login/me/logout
  (`cutover/router.go:104-113`); there is no random percentage path.
- Management paths are intercepted before forwarding:
  `internal/controlplane/service_proxy.go:24-30`.

### 2.5 Controlplane — proxy phase (brief)

```powershell
$env:AUTH_SVC_PROXY_ENABLED='true'
$env:AUTH_CUTOVER_ROSTER_FILE="$W\roster.txt"   # contains: cutover-user
# same --http-port/--grpc-port/--metrics-port/--store/--mysql-dsn as baseline
& "$W\opsmesh-cp.exe" ... *> "$W\cp-proxy.log"
```

After the proxy checks, the controlplane was stopped and restarted with
`AUTH_SVC_PROXY_ENABLED='false'` and **no** roster env (`cp-final.log`). The system was left
at the baseline before teardown.

### 2.6 Password/credential bootstrap

Both sides seed an `admin` with `mustChangePassword=true`. The same initial and final
passwords were used on both sides (`Rehearsal-Admin-2026x!` → `Rehearsal-Admin-2026y!`) so
"same credentials" is literal, not approximate.

---

## 3. Matrix results

All bodies below are actual responses. Tokens are redacted.

### A. Controlplane-local auth (proxy disabled)

| # | Request | Status | Evidence |
|---|---|---|---|
| A1 | `POST /api/v1/auth/login` admin / initial | **200** | `mustChangePassword:true`, non-empty `changePasswordToken` |
| A2 | `POST /api/v1/auth/change-password` | **200** | response contains `token`; `mustChangePassword:false` at top level |
| A3 | `POST /api/v1/auth/login` admin / new password | **200** | body has `token` (len 2645), `mustChangePassword:false`; `user.username=admin` |
| A3 | `GET /api/v1/auth/me` (cookie jar) | **200** | `tenantId:"default"`, `mustChangePassword:false` |
| A4 | `POST /api/v1/auth/logout` | **200** | `{"message":"logged out"}` |
| A4 | `GET /api/v1/auth/me` after logout | **401** | `{"error":"missing Authorization header; session may have expired; please log in again"}` |

Pass: 6/6.

### B. Direct auth-svc (no controlplane in path)

| # | Request | Status | Evidence |
|---|---|---|---|
| B1 | `POST /api/v1/auth/login` admin / initial | **200** | `mustChangePassword:true`, `changePasswordToken` present, `opsmesh_at` cookie set |
| B2 | `POST /api/v1/auth/change-password` | **200** | `{"status":"password changed"}` |
| B3 | `POST /api/v1/auth/login` admin / new password | **200** | body has **no** `token` field; session is Set-Cookie only |
| B3 | `GET /api/v1/auth/me` | **200** | `mode:"self-validated"`, `tenantId:"default"` |
| B4 | `POST /api/v1/auth/refresh` | **200** | `{"status":"ok"}`, cookies rotated |
| B4 | `POST /api/v1/auth/logout` | **200** | `{"status":"ok","sessionRevocation":"unavailable (Redis disabled): access token remains valid until expiry; jti blacklist + refresh-token deletion applied"}` |
| B4 | `GET /api/v1/auth/me` after logout | **401** | `{"error":"authentication required"}` |
| B5 | `POST /api/v1/auth/register` | **201** | `{"status":"pending","userId":"user-f60c..."}` |
| B5 | `POST /api/v1/auth/login` pending + **correct** password | **403** | `{"error":"account pending admin approval"}` |
| B6 | `PUT /api/v1/users/{id}` status disabled | **200** | returned user `status:"disabled"` |
| B6 | `POST /api/v1/auth/login` disabled + **correct** password | **403** | `{"error":"account disabled"}` |
| B6 | `POST /api/v1/auth/login` disabled + **wrong** password | **401** | `{"error":"invalid credentials"}` |

Pass: 12/12. The A-2 ordering is confirmed: password is checked first; only a correct
password reveals account state, so wrong-password-on-disabled does not enumerate.

### C. auth-svc users list — legacy, envelope, tenant scope

| # | Request | Status | Evidence |
|---|---|---|---|
| C1 | `GET /api/v1/users` (no params) | **200** | top-level keys exactly `["users"]` (legacy shape) |
| C2 | `GET /api/v1/users?page=1&pageSize=2` | **200** | keys `hasMore,page,pageSize,total,users`; `total=9, page=1, pageSize=2, hasMore=true, len(users)=2` |
| C3 | `GET /api/v1/users?page=abc` | **400** | `{"error":"invalid page/pageSize"}` |
| C4 | tenant-a admin login, then `GET /api/v1/users` | **200** | only `tenant-a-admin, tenant-a-u1..u5`; no `admin`, `pending-user`, `disabled-user` from default tenant |

Pass: 4/4. Tenant scoping is token-claim based, not `X-Tenant-ID` based.

### D. Management plane stays local

| # | Request | Status | Evidence |
|---|---|---|---|
| D1 | controlplane `GET /api/v1/auth-svc/users` (admin cookie) | **404** | `{"error":"management plane is local"}` |

Pass: 1/1. This is the intended A-1 product decision, not a bare 404.

### E. Proxy phase and baseline restore

| # | Request | Status | Evidence |
|---|---|---|---|
| E1 | controlplane `POST /api/v1/auth/login` cutover-user, `AUTH_SVC_PROXY_ENABLED=true` + roster | **200** | body is auth-svc shape (no `token`, has `changePasswordToken`, `needMFA`, auth-svc user id); `opsmesh_at` cookie set |
| E2 | controlplane `GET /api/v1/auth/me` same session | **200** | auth-svc `/me` shape (`mode:"self-validated"`) |
| E3 | controlplane `POST /api/v1/auth/logout` | **200** | auth-svc logout body (`sessionRevocation` present) |
| E4 | route log | **PASS** | `cp-proxy.log` has `切流名册已装载` and `切流路由：转 auth-svc` for `endpoint=login`, `me`, `logout`, `user=cutover-user` |
| E5 | stop proxy, restart `AUTH_SVC_PROXY_ENABLED=false`, login cutover-user | **200** | body is local shape: `token` present (len 373), no `changePasswordToken`; `cp-final.log` has **0** route lines |

Pass: 5/5. The proxy was disabled again and the system left at baseline before teardown.

**Total: 22/22 PASS.**

---

## 4. Defects / findings

### D1 (High, deployment) — repo MySQL compose is invalid YAML

`docker-compose.mysql.yml:9` and `:19` both define `ports` for `mysql-master`;
Docker Compose v5.7 refuses to parse the file. The documented command in the file header
(`docker compose -f docker-compose.mysql.yml up -d`) cannot run. This rehearsal used an
equivalent temp compose. Fix by deleting the duplicate block at `:19-20` (and the duplicate
`environment` blocks on the slaves).

### D2 (Medium, API contract) — auth-svc login response has duplicate JSON keys

`services/auth-svc/internal/http/gateway.go:1096-1097` emits both `"roleIDs"` and
`"roleIds"` with the same value. A strict JSON decoder rejects the body; observed directly:

```
ConvertFrom-Json : ... contains the duplicated keys 'roleIDs' and 'roleIds'
```

This affects the login response for every auth-svc user. `/auth/me` is clean (only
`roleIDs`), so the same client succeeds on `/me` but fails on login if it parses strictly.
Fix by dropping the transitional alias from login (or emitting it only in the list surface
where it is intentionally documented as an alias).

### D3 (Medium, API contract) — users list carries mixed legacy aliases

`services/auth-svc/internal/http/gateway.go:670-675` adds `created_at` (protobuf object
such as `{"seconds":...}`), `must_change_password`, and `role_ids` alongside the canonical
`createdAt` / `mustChangePassword` / `roleIDs`; `toPublicUser` also adds duplicate
`roleIDs`/`roleIds` (`:1096-1097`). This is intentional dual-track compatibility, but any
consumer that rejects duplicate keys or expects `createdAt` to be the only timestamp field
will break. The frontend uses the canonical names, so this is not a blocker for the current
UI; it is a blocker for strict API consumers and should be retired at 100%.

### D4 (Low) — controlplane change-password returns a stale nested user

On successful first-login password change, the top-level response says
`mustChangePassword:false`, but the nested `user.mustChangePassword` is still `true`
because the in-memory `u` is not refreshed after `store.ChangePassword`
(`internal/controlplane/auth_login.go:567`, response built from `s.authUserWithPerms(u)`).
The next login/`/me` correctly returns `false`. A client that trusts the nested user object
will incorrectly keep showing the change-password dialog. Fix by reloading/updating the
user before building the response.

### D5 (Low, identity consistency) — seeded auth-svc admin has empty tenant in login body

The seeded admin row has no `tenant_id` (`services/auth-svc/internal/store/mysql.go:234`),
so the direct login body returns `"tenantId":""`, while `/auth/me` returns
`"tenantId":"default"` (via `tenantOrDefault`, `gateway.go:1148-1152`). Tenant scoping is
functionally correct (empty is treated as `default`), but the two responses are not
byte-consistent. Normalize the seed row or the login mapping.

### D6 (Constraint, not a code defect) — the two auth systems cannot share one schema

See §1.1. `services/auth-svc/internal/store/schema.sql` and
`internal/store/sqlstore/migrations/001_initial.sql:106-135` define the same table names
with different shapes. A cutover runbook must state the DSNs explicitly and must not point
auth-svc at the controlplane schema. This is the main "same DB" trap.

### D7 (Risk, deployment) — Redis disabled changes logout guarantees

With Redis disabled, auth-svc logout returns
`sessionRevocation:"unavailable (Redis disabled): access token remains valid until expiry; jti blacklist + refresh-token deletion applied"`.
In this rehearsal the jti blacklist did invalidate the token (`/me` after logout was 401),
but Redis-backed session revocation is absent. If the cutover requires prompt revocation of
all sessions for a user (e.g. `cutover-unroute`), enable Redis for auth-svc; otherwise the
jti/RT path is the only protection and short-lived access tokens remain valid until expiry.

---

## 5. Teardown

- Controlplane and auth-svc background processes stopped by PID.
- MySQL torn down with:
  `docker compose -f "$W\compose.mysql.yml" -p opsmesh-auth-rehearsal down -v`
  (container, network, and named volume removed).
- Post-check: no container matching `opsmesh-auth-rehearsal`; ports
  `13306, 19680, 19681, 19682, 19690, 19691` all free; volume absent.
- No product code changed. `git status` still shows only the pre-existing
  `docs/CLAIMS.md` and `docs/COORDINATION-BOARD.md` modifications; this report is the only
  new file.

---

## 6. Go / No-Go for the 11-25 switch

### 50/50 random switch: **NO-GO**

1. **It is not implemented.** The only mention of `AUTH_SVC_PROXY_RATIO` is a comment in
   `internal/controlplane/svcproxy/proxy.go:93`; no Go code reads it. Routing is binary:
   `AUTH_SVC_PROXY_ENABLED` master switch + roster match
   (`internal/controlplane/cutover/router.go:42-45,104-113`).
2. **Auth is stateful and the two sides do not share state.** Users live in different
   schemas; refresh tokens and session stores are side-owned. Randomly sending the same
   browser to the two sides produces split sessions: a login handled by auth-svc cannot be
   refreshed or `/me`-validated by the controlplane, and vice versa. This is the
   conclusion already recorded in `docs/td60-auth-data-plane-proposal.md` §1 ("50/50 随机
   切流在 auth 域不可等价").
3. The contract defects above (duplicate keys, mixed aliases) would make a 50/50 split look
   like random client breakage rather than a clean A/B signal.

If the 11-25 plan is literally "route 50% of auth requests at random", it should be
**postponed** for auth. A 50/50 split is appropriate only for stateless domains (e.g. task
reads); it is not a valid auth cutover mechanism.

### Binary/roster batch cutover (方案 C/D): **conditional GO**

The mechanism itself passed end-to-end on real processes and real MySQL: local baseline,
direct auth-svc, roster-based forwarding (login/me/logout), and baseline restore. Go on
11-25 only if all of the following are true:

- The batch's users are pre-created and approved on auth-svc, with one-time passwords
  delivered, and each user has completed a shadow login on auth-svc.
- `AUTH_SVC_PROXY_ENABLED=true` and `AUTH_CUTOVER_ROSTER_FILE` contains exactly the batch;
  `admin` is never in the roster (the loader fail-fasts on reserved accounts).
- The auth-svc DSN points at a **separate** schema (e.g. `opsmesh_auth`), never the
  controlplane schema.
- Redis is enabled for auth-svc if immediate per-user session revocation is required for
  rollback (`cutover-unroute`).
- D2/D3 are fixed or the release explicitly accepts that non-frontend API consumers may
  reject auth-svc login/list payloads.
- The rollback path is rehearsed: trim/clear the roster (soft, ~20-30 s hot reload) and/or
  set `AUTH_SVC_PROXY_ENABLED=false` + restart (hard); use `cutover-unroute` rather than
  hand-editing the roster so auth-svc sessions are revoked.
- Watch during the window: controlplane route-log counts per endpoint/user, 401/403
  distribution (403 only after correct password), 429 from the per-IP burst-5 login guard,
  502/503 from auth-svc unavailability, and refresh failures (side-ownership errors).
- Keep the 11-25 blast radius small: one low-risk tenant / internal batch first, not a
  whole-domain flip.

**Bottom line:** a 50/50 auth switch on 11-25 is No-Go; a binary roster-gated batch of
pre-migrated users is Go, with D1 (compose), D2/D3 (contract) and D6 (separate schema)
addressed first.
