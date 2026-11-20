# Auth dual-stack rehearsal r2 — 2026-11-19 gates G1-G9

**Scope:** second rehearsal of the TD-60 auth cutover, run against the fixed code on
`main@c2bcd884` (fix commit `256d759c`), with real `controlplane` + real `auth-svc` +
real MySQL 8 and Redis 7. This report verifies the nine 11-24 gates in
`docs/projects/TD-60-cutover-schedule.md` §3.1 and re-runs the round-1 matrix.

**Verdict:** **G1-G9: 9 PASS, 0 FAIL, 0 NOT-DONE** (two gate *commands* are stale and need
doc updates; both gates themselves pass on stronger evidence). Matrix **30/30 PASS**.
Teardown clean. **11-25 roster batch A (one non-admin user): conditional GO** — conditions
in §6. A literal 50/50 random switch remains **NO-GO** (unchanged from round 1).

---

## 1. Environment

| Item | Value |
|---|---|
| Repo / commit | `F:\Agent\kilo\workspace\OpsMesh` @ `c2bcd884` (working tree only pre-existing `docs/COORDINATION-BOARD.md`) |
| Fix under test | `256d759c` — contract alignment (duplicate keys / aliases / tenant / stale nested user) + compose port merge |
| OS / toolchain | Windows, Go 1.26.6, Docker 29.9.3, Compose v5.7.0, Python 3.14.3 |
| MySQL | `mysql:8.0.33`, container `opsmesh-auth-rehearsal-r2-mysql`, host `3306` |
| Redis | `redis:7.2-alpine`, container `opsmesh-auth-rehearsal-r2-redis`, host `6379`, password `redis123` |
| Controlplane store | `opsmesh:opsmesh123@tcp(127.0.0.1:3306)/opsmesh?parseTime=true` |
| auth-svc store | `opsmesh:opsmesh123@tcp(127.0.0.1:3306)/opsmesh_auth?parseTime=true` |
| Controlplane ports | HTTP `19680`, gRPC `19681`, metrics `19682` |
| auth-svc ports | HTTP `19690`, gRPC `19691` |
| Shared HMAC secret | one secret for both processes (`rehearsal-shared-hmac-secret-...`) |
| Admin credentials | seed `Rehearsal-Admin-2026x!` → changed on both sides to `Rehearsal-Admin-2026y!` |
| Redis session store | `AUTH_SVC_SESSION_STORE_ENABLED=true`, `AUTH_SVC_REDIS_ADDR=127.0.0.1:6379`, `REDIS_PASSWORD=redis123` |

Compose project: `opsmesh-auth-rehearsal-r2` (temporary file outside the repo; repo
`docker-compose.mysql.yml` was only used for the G5 parse check).

Binaries built without source changes:

```powershell
go build -o $W\opsmesh-cp.exe ./cmd/opsmesh
go build -o $W\auth-svc.exe ./cmd/auth-svc     # from services/auth-svc
```

Both services started against the same MySQL server with different schemas. Startup log
confirmed `MySQL store 已启用`, `Redis cache enabled`, `Redis SessionStore enabled`, and
`HTTP gateway enabled` on the auth side; `切流名册已装载 source=empty count=0
proxy_enabled=false` on the controlplane baseline.

---

## 2. Gate results (G1-G9)

### G1 — duplicate login JSON keys — **PASS**

Raw auth-svc login body (`POST /api/v1/auth/login`), strict detector:
`json.loads(..., object_pairs_hook=...)` asserts `len(keys) == len(set(keys))` in every
object.

```
duplicates= []
top_keys= ['changePasswordToken', 'deviceFP', 'mustChangePassword', 'needMFA', 'user']
user_keys= ['createdAt','email','id','mustChangePassword','permissions','roleIDs','status','tenantId','username']
roleIDs= ['role-admin']
roleIds present= False
```

Verified on both the initial-password login (`mustChangePassword=true`) and the
post-change login. Code now emits a single `roleIDs`
(`services/auth-svc/internal/http/gateway.go:1096`, `toPublicStoreUser` at `:1127`).

### G2 — users list exact key set, no legacy aliases — **PASS**

Direct auth-svc, after all test users were created (6 users):

```
auth_users_final=200
count= 6
top_keys= ['users']                       # legacy shape unchanged
alias_hits= []                            # no created_at / must_change_password / role_ids / roleIds
key_sets= [('createdAt','email','id','mustChangePassword','permissions','roleIDs','status','tenantId','username')]
```

Envelope (`?page=1&pageSize=2`): top keys exactly
`['hasMore','page','pageSize','total','users']`; `page=1 pageSize=2 total=1 hasMore=false`
at that moment; every user carries the same nine canonical keys.

Controlplane local `/api/v1/users`: top keys `['users']`, first-user key set identical to
auth-svc's, `alias_hits=[]`; envelope `total=3 page=1 pageSize=2 hasMore=true`.

`?page=abc` → `400 {"error":"invalid page/pageSize"}`.

### G3 — change-password nested user flag — **PASS**

Controlplane first-login change against real MySQL:

```
HTTP/1.1 200 OK
top_mustChangePassword= False
nested_mustChangePassword= False
token_len= 2645
```

This is the D4 fix (`internal/controlplane/auth_login.go:548-560`): the user is reloaded
after `store.ChangePassword` before `authUserWithPerms(u)` builds the response.

### G4 — seeded admin tenant in login body and /me — **PASS**

auth-svc seeded admin now writes `tenant_id='default'`
(`services/auth-svc/internal/store/mysql.go:228-240`). Observed:

```
login user.tenantId= default
/me   tenantId    = default
```

The initial login body also returned `user.tenantId="default"` (previously `""`). Note the
two payloads have different nesting (`login.user.tenantId` vs flat `/me.tenantId`), so
"byte identical" is not literal; the tenant value is consistent.

### G5 — repo MySQL compose parses — **PASS**

```
docker compose -f docker-compose.mysql.yml config   → exit 0
```

Rendered JSON: `mysql-master` has exactly one port entry
(`[{'mode':'ingress','target':3306,'published':'3306','protocol':'tcp'}]`), and each slave
has one `ports` block and one `environment` mapping (no duplicate keys). The duplicate
blocks removed by `256d759c` are gone (`docker-compose.mysql.yml:9-10`).

### G6 — separate schemas on one server — **PASS (gate command is stale)**

- auth-svc DSN contains `/opsmesh_auth?`; controlplane DSN contains `/opsmesh?`; both point
  at `127.0.0.1:3306`.
- Schema shape proves separation:
  - `opsmesh`: `roles.permissions` JSON exists; **no** `user_roles` / `role_permissions`.
  - `opsmesh_auth`: `user_roles` and `role_permissions` exist; `roles.permissions` absent.
- Independent rows: local user `batch-a-user` was created only in `opsmesh`
  (`SELECT ... FROM opsmesh.users` → active, `opsmesh_auth.users` → 0 rows). After
  `cutover-migrate`, the same username exists independently in both schemas:
  `opsmesh.users` active and `opsmesh_auth.users` active with `must_change_password=1`.

**Stale gate command:** §3.1 G6 says `opsmesh.users` must return **0 rows** for
`must_change_password`. It now returns **1 row**:
`opsmesh.users.must_change_password tinyint(1)` exists in the current controlplane schema
(added by a later migration), exactly like `opsmesh_auth.users.must_change_password`. That
query no longer discriminates schemas. Replace it with the table-shape/row checks above;
the gate itself passes.

### G7 — Redis session revocation — **PASS**

```
redis-cli ... ping                → PONG
auth-svc log                      → Redis SessionStore enabled — stateful session management active
after login                       → GET session:<sha256(at)> returns the session JSON
                                    {"session_id":"f6b86e...","user_id":"user-admin","tenant_id":"default",...}
POST /api/v1/auth/logout          → 200 {"sessionRevocation":"revoked","status":"ok"}
after logout                      → GET session:<sha256(at)> returns nil
GET /auth/me with old Bearer at   → 401 {"error":"invalid or expired token"}
```

Wiring: `services/auth-svc/internal/cache/session.go:157` (`Revoke` returns
`ErrSessionStoreUnavailable` when Redis is off), `internal/http/gateway.go:289-300`
(creates/revokes by `sha256(access token)`), `cmd/auth-svc/main.go:160-168` (enables the
store only when Redis is configured and reachable). The response no longer contains
`unavailable (Redis disabled)`.

### G8 — OTP pre-migration and shadow login — **PASS**

Implementation is real and was exercised, not just read:

```
opsmesh cutover-migrate --users batch-a-user --force-change ...
[cutover-migrate] 本批结果：新建 1 · 收编 0 · 名册已有 0 · 失败 0 · 名册新增 1
[cutover-migrate] 一次性口令已逐用户写入 ...\credentials.txt（权限 0600...）
```

- `opsmesh_auth.users`: `batch-a-user | active | must_change_password=1 | tenant_id=default`.
- `credentials.txt` contains one OTP line (header comments + `batch-a-user<TAB>OTP`).
- `cutover-migrate` performs a login verification per created user
  (`internal/controlplane/cutover/migrate.go:529-575`, `loginVerify`), then logs out.
- Independent shadow login with that OTP: `200`, `mustChangePassword=true`,
  `changePasswordToken` present (len 445), user `batch-a-user`, tenant `default`.

Operational caveat: OTP **delivery** to the user and the "re-login once" notice are
process steps; this rehearsal proves the artifact is generated and usable, not that a
human received it. On Windows the 0600 mode is a best-effort emulation; verify ACLs on the
real host.

### G9 — roster excludes admin; non-admin proxied, admin local — **PASS**

Roster written by `cutover-migrate` (one batch user):

```
admin lines (exact match) = 0
non-comment entries       = 1      batch-a-user
total physical lines      = 2      (comment header + user)
loader log                = source=file:...\roster.txt count=1 proxy_enabled=true
```

Negative control — roster containing `admin`:

```
exit_code= 1
[controlplane] 初始化失败: 切流名册装载失败: 名册条目 "admin" 是保留账号
（两侧 seed 同名，写入即构成跨库凭证混淆面）——它永不进名册
```

Loader fail-fast: `internal/controlplane/cutover/roster.go:182-184`.

Proxy-phase evidence:

- Batch login through controlplane: auth-svc shape (`top_keys` =
  `changePasswordToken, deviceFP, mustChangePassword, needMFA, user`; no `token`).
- `GET /api/v1/auth/me` through controlplane: `mode:"self-validated"`, tenant `default`.
- Logout through controlplane: auth-svc body `{"sessionRevocation":"revoked","status":"ok"}`.
- Route log contains exactly three route lines, all for the roster user:

```
endpoint=login   user=batch-a-user  status=200
endpoint=me      user=batch-a-user
endpoint=logout  user=batch-a-user
```

- Admin login through the same proxy-enabled controlplane: **local** shape
  (`top_keys` = `mustChangePassword, token, user`; `token` len 2645; no
  `changePasswordToken`). Route log lines mentioning `admin`: **0**.

**Stale gate command:** §3.1 G9 uses `wc -l` and expects 1. The tool writes a comment
header, so the file has 2 physical lines and 1 entry; the loader counts 1. Use the
non-comment count or the loader's `count` field.

---

## 3. Round-1 matrix re-run

All bodies are actual responses; tokens redacted. Total **30/30 PASS**.

### A. Controlplane-local auth (proxy disabled)

| # | Request | Status | Evidence |
|---|---|---|---|
| A1 | `POST /auth/login` admin / initial | **200** | `mustChangePassword:true`, `changePasswordToken` len 64 |
| A2 | `POST /auth/change-password` | **200** | top and nested `mustChangePassword:false`, token issued |
| A3 | `POST /auth/login` admin / new password | **200** | `token` len 2645, `mustChangePassword:false`, user `admin` |
| A3 | `GET /auth/me` | **200** | `tenantId:"default"`, `mustChangePassword:false` |
| A4 | `POST /auth/refresh` | **200** | `{"user":{...}}`; `opsmesh_at` rotated |
| A5 | `POST /auth/logout` | **200** | `{"message":"logged out"}` |
| A5 | `GET /auth/me` after logout | **401** | `{"error":"token has been revoked"}` |

Observation: controlplane refresh returns a user object, auth-svc refresh returns
`{"status":"ok"}`. The frontend consumes only the status code, so this is not a gate
failure, but strict clients will see two shapes.

### B. Direct auth-svc (no controlplane in path)

| # | Request | Status | Evidence |
|---|---|---|---|
| B1 | `POST /auth/login` admin / initial | **200** | `mustChangePassword:true`, `changePasswordToken` present |
| B2 | `POST /auth/change-password` | **200** | `{"status":"password changed"}`; nested `mustChangePassword:false` |
| B3 | `POST /auth/login` admin / new password | **200** | no `token` field (cookie-only); `mustChangePassword:false` |
| B3 | `GET /auth/me` | **200** | `mode:"self-validated"`, `tenantId:"default"` |
| B4 | `POST /auth/refresh` | **200** | `{"status":"ok"}`; `opsmesh_at` rotated |
| B4 | `POST /auth/logout` | **200** | `sessionRevocation:"revoked"` |
| B4 | `GET /auth/me` after logout | **401** | `invalid or expired token` |
| B5 | `POST /auth/register` | **201** | `status:"pending"`, `userId` returned |
| B5 | `POST /auth/login` pending + correct | **403** | `{"error":"account pending admin approval"}` |
| B6 | `PUT /users/{id}` status disabled | **200** | returned `status:"disabled"` |
| B6 | `POST /auth/login` disabled + correct | **403** | `{"error":"account disabled"}` |
| B6 | `POST /auth/login` disabled + wrong | **401** | `{"error":"invalid credentials"}` |

Ordering holds: the password is checked first; a wrong password on a disabled account
returns 401, so account state is not enumerable.

### C. auth-svc users — legacy, envelope, tenant scope

| # | Request | Status | Evidence |
|---|---|---|---|
| C1 | `GET /users` | **200** | top keys exactly `["users"]` |
| C2 | `GET /users?page=1&pageSize=2` | **200** | `hasMore,page,pageSize,total,users`; `total=1,pageSize=2,hasMore=false` at that point |
| C3 | `GET /users?page=abc` | **400** | `{"error":"invalid page/pageSize"}` |
| C4 | tenant-a admin login, then `GET /users` | **200** | only `tenant-a-admin, tenant-a-u1`; no default-tenant users |

Tenant scoping is token-claim based (`tenant-a` from the login token), not `X-Tenant-ID`.

### D. Management plane stays local

| # | Request | Status | Evidence |
|---|---|---|---|
| D1 | controlplane `GET /api/v1/auth-svc/users` | **404** | `{"error":"management plane is local"}` |

### E. Proxy phase and baseline restore

| # | Request | Status | Evidence |
|---|---|---|---|
| E1 | controlplane login `batch-a-user` (roster) | **200** | auth-svc shape, no local `token` |
| E2 | controlplane `/auth/me` same session | **200** | `mode:"self-validated"` |
| E3 | controlplane logout same session | **200** | `sessionRevocation:"revoked"` |
| E4 | route log | **PASS** | exactly login/me/logout, all `user=batch-a-user`; 0 admin lines |
| E5 | admin login, proxy phase | **200** | local shape `token` len 2645, no `changePasswordToken` |
| E6 | baseline restore: proxy off, no roster | **PASS** | loader `count=0`; `cp-restore.log` route lines = 0; `batch-a-user` local login **200** with local-shape token len 1328 |

The system was left at the baseline (proxy off) before teardown.

---

## 4. Defects / findings

### R2-D1 (Low, documentation) — G6 verification query is stale

`docs/projects/TD-60-cutover-schedule.md:115` expects
`opsmesh.users.must_change_password` to return 0 rows. The current controlplane schema
also has that column (migration added it), so both schemas return 1 row. The gate
intention still passes via table-shape and independent-row evidence (§2 G6). Update the
command so the 11-24 sign-off cannot be failed or falsely passed by a stale discriminator.

### R2-D2 (Low, documentation/tooling) — G9 roster line count vs comment header

`cutover-migrate` writes `# cutover-migrate ... added=N` as the first line. `wc -l` yields
2 while the loader count and non-comment entry count are 1. Change the gate to
`grep -vc '^#'` or to the loader `count` field.

### R2-D3 (Info, contract) — refresh response shape differs by side

Controlplane `POST /auth/refresh` returns `{"user":{...}}`; auth-svc returns
`{"status":"ok"}`. Frontend `postEmpty` only checks the status code, so no current
breakage; document it before retiring the dual track.

### R2-D4 (Info, contract) — empty `changePasswordToken` key remains

auth-svc login always includes `changePasswordToken`; it is `""` after a normal login.
Clients must test non-empty rather than key presence. Not a gate; relevant to API
consumers that switch sides.

### R2-D5 (Info, ops) — Windows file mode for OTP credentials

`cutover-migrate` requests 0600 on the credentials file; Windows does not enforce POSIX
modes. Verify ACLs on the real host before writing batch A credentials.

---

## 5. Teardown

- Controlplane and auth-svc foreground/background processes stopped by tool PID.
- Compose project removed with:
  `docker compose -f <temp>\compose.rehearsal.yml -p opsmesh-auth-rehearsal-r2 down -v`
  (both containers, the project network, and both named volumes removed).
- Post-check: no container/network/volume matching `opsmesh-auth-rehearsal-r2`; ports
  `3306, 6379, 19680, 19681, 19682, 19690, 19691` all free.
- No product code changed. `git status` shows only the pre-existing
  `docs/COORDINATION-BOARD.md` modification; this report is the only new file.
- No commit created.

---

## 6. Go / No-Go for 11-25

### 50/50 request-level random switch — **NO-GO (unchanged)**

The mechanism still does not exist (`AUTH_SVC_PROXY_RATIO` remains a comment only), and
auth state is side-owned: controlplane `opsmesh` vs auth-svc `opsmesh_auth`, separate
refresh tokens and session stores. Randomly sending one browser to two sides produces
split sessions. Use roster batches (D+C), not request-level randomness.

### Roster batch A (one non-admin user) — **conditional GO**

The gate set passes on real processes, real MySQL and real Redis. Write the 11-25 roster
only if all of the following are signed:

1. G1-G9 signed using the corrected G6/G9 commands (or the owner explicitly accepts the
   stronger evidence in §2). No gate may be signed from the stale query alone.
2. The batch A user is pre-created and `active` on auth-svc, has completed a shadow login,
   and the OTP plus the "re-login once" notice are delivered through the approved channel
   before the roster is written.
3. The production auth-svc deployment has Redis enabled and monitored; G7's revocation
   guarantee disappears if `Redis SessionStore disabled` is logged.
4. §3 P5 (7-day baseline) is frozen, §8 roles are filled, and the rollback drill
   (`cutover-unroute`, soft ~20-23 s) has been run for this batch.
5. `admin` is never in the roster (loader fail-fast verified), the roster contains exactly
   the one batch user, and the controlplane is restarted with
   `AUTH_SVC_PROXY_ENABLED=true` + `AUTH_CUTOVER_ROSTER_FILE`.
6. Watch during the window: route-log counts per endpoint/user, 401/403 distribution
   (403 only after a correct password), per-IP 429, 502/503 from auth-svc, and refresh
   failures from side ownership.

If any of 1-6 is not true on 11-24, **HOLD**: do not write the roster, do not restart the
cutover, use 11-24 as the fix buffer. Task/device dates are unaffected.

**Bottom line:** the nine auth gates are green on the fixed code, and the roster-gated
batch path works end-to-end including Redis revocation and admin exclusion. The remaining
work for 11-24 is documentary (G6/G9 commands, refresh-shape note) plus the operational
delivery of OTP/notification and the 7-day baseline — not a code blocker for batch A.
