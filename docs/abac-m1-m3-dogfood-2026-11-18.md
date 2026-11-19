# ABAC M1–M3 dogfood on real MySQL + Redis (2026-11-18)

Status: **PASS for verification items (a)–(f)**, with three defects/hazards found
(see §5). During the dogfood run itself no product source was modified; the three
actionable defects (D1–D3) were fixed afterwards in the same batch — see the
「缺陷修复（2026-11-18）」 section at the end of this document. D4 is a platform
limitation with no code change.

## 1. Environment

| Item | Value |
|---|---|
| Repo | `F:\Agent\kilo\workspace\OpsMesh` |
| Binary | `cmd/opsmesh` → `opsmesh 0.18.0 (commit=dev date=unknown)` |
| Go | go1.26.6 windows/amd64 |
| Docker engine | Docker 29.1.3 inside WSL2 `Ubuntu-24.04` (Windows Docker Desktop was not running) |
| Compose | Docker Compose v2.40.3 (`docker compose` from WSL) |
| Images | `mysql:8`, `redis:7` (from the repo compose file) |
| Compose project | `abacdogfood` (isolated; did not touch existing containers/volumes) |
| Host ports | MySQL `13306→3306`, Redis `16379→6379` |
| Controlplane | HTTP `18080`, gRPC `19090`, metrics `19091` |
| Agent | one locally registered agent, tenant `default` |
| Signing key | Ed25519, key ID `k1`, bundle TTL `1h` |
| Public key (B64) | `6ZAZOJ9P6guVSbNEXejP86VnBSoUwJxqjHLIP7D3v/U=` |

The repo `docker-compose.yaml` does not publish MySQL/Redis ports. A **temp** override
outside the repo published them for host-side binaries; the repo file itself was not edited:

`C:\Users\winge\AppData\Local\Temp\kilo\abac-dogfood\compose.override.yml`

```yaml
services:
  mysql:
    ports: ["13306:3306"]
  redis:
    ports: ["16379:6379"]
```

## 2. Stack bring-up

```bash
# in WSL Ubuntu-24.04
MYSQL_ROOT_PASSWORD=rootpass MYSQL_USER=opsmesh MYSQL_PASSWORD=opsmesh \
docker compose -p abacdogfood \
  -f /mnt/f/Agent/kilo/workspace/OpsMesh/docker-compose.yaml \
  -f /mnt/c/Users/winge/AppData/Local/Temp/kilo/abac-dogfood/compose.override.yml \
  up -d mysql redis
```

Observed:

```text
abacdogfood-mysql-1  Up 34 seconds (healthy)  0.0.0.0:13306->3306/tcp
abacdogfood-redis-1  Up 34 seconds (healthy)  0.0.0.0:16379->6379/tcp
```

Build:

```powershell
go build -o C:\Users\winge\AppData\Local\Temp\kilo\abac-dogfood\bin\opsmesh.exe ./cmd/opsmesh
# opsmesh 0.18.0 (commit=dev date=unknown)
```

Keypair generation (temp program using `crypto/ed25519`; private key kept only in the
temp workspace and never written into the repo):

```text
PRIVATE_B64=<redacted, temp only>
PUBLIC_B64=6ZAZOJ9P6guVSbNEXejP86VnBSoUwJxqjHLIP7D3v/U=
```

## 3. Controlplane

```powershell
opsmesh.exe --mode=controlplane --store=mysql `
  --mysql-dsn="opsmesh:opsmesh@tcp(127.0.0.1:13306)/opsmesh?parseTime=true" `
  --redis-addr=127.0.0.1:16379 `
  --demo --allow-insecure-store `
  --http-port=18080 --grpc-port=19090 --metrics-port=19091 `
  --advertise-addr=http://127.0.0.1:18080 `
  --jwt-secret="0123456789abcdef0123456789abcdef" `
  --abac-bundle-signing-key="<redacted Ed25519 private B64>" `
  --abac-bundle-key-id=k1 --abac-bundle-ttl=1h
```

Startup applied **36 migrations** (`001_initial.sql` … `036_abac_decision_logs.sql`) and
reported `控制面已启动 http=18080 grpc=19090 metrics=19091`.
`GET /healthz` returned `200 {"checks":{"store":"ok"},"status":"ok"}`.

`--demo` + `--store=mysql` works for ABAC, but the seeded admin still has
`must_change_password=1`; login returns only a 5-minute `changePasswordToken`.
The password was changed (`admin123` → `Admin@2026-dogfood`) and a real JWT obtained.

Two policies were created through `POST /api/v1/abac` (both `202 Accepted`,
`{"action":"policy-create","taskID":""}`):

| ID | Subject | Resource | Action | Effect |
|---|---|---|---|---|
| `default\|read\|alice\|vm:42` | alice | vm:42 | read | allow |
| `default\|write\|bob\|db:prod` | bob | db:prod | write | allow |

MySQL confirmation:

```text
| tenant_id | id                        | subject | resource | action | effect | version |
| default   | default|read|alice|vm:42  | alice   | vm:42    | read   | allow  |       1 |
| default   | default|write|bob|db:prod | bob     | db:prod  | write  | allow  |       1 |
```

## 4. Agent and verification results

### 4.1 Agent registration / key provisioning

The agent was first started once to register (bare registration → tenant `default`),
then stopped. The per-agent HMAC secret issued by `store.Register` was read from
`agents.secret` and provisioned into `<data-dir>/agent.key` (0600 intent, ACL restricted
to the current user). This simulates the normal `install token + TLS` delivery path
(see D2 for why a pre-shared key does not work here).

Final agent run:

```powershell
opsmesh.exe --mode=agent --control-addr=http://127.0.0.1:18080 --grpc-port=19090 `
  --data-dir=...\agent-data --segment=default `
  --abac-bundle-enabled `
  --abac-bundle-public-keys="k1:6ZAZOJ9P6guVSbNEXejP86VnBSoUwJxqjHLIP7D3v/U=" `
  --abac-bundle-cache-path=...\abac-bundle.json `
  --abac-bundle-refresh-interval=5s `
  --abac-decision-report
```

### (a) Direct bundle GET with correct HMAC → 200 + ETag + signature verifies

Request signed as
`HMAC-SHA256(secret, "abac-bundle\n<agentID>\n<unix-seconds>")` with the
`X-OpsMesh-Agent-{ID,Timestamp,Signature}` headers:

```text
status=200
etag="36020fa89f7731fd251eac6bc67d0c6ced857c6ccaad434b363e46ed9ae4cdc2"
X-OpsMesh-Bundle-Key-Id=k1
X-OpsMesh-Bundle-Digest=36020fa89f7731fd251eac6bc67d0c6ced857c6ccaad434b363e46ed9ae4cdc2
bodyLen=528
bundleID=632ce444-d44a-484d-9632-d03190b1aba4
tenantScope=default
policies=2
```

Independent verifier (`pkg/abacbundle.KeySet.Verify`, Ed25519 public key above):

```text
verified=true schema=1 bundleID=632ce444-d44a-484d-9632-d03190b1aba4
tenant=default keyId=k1 policies=2
digest=36020fa89f7731fd251eac6bc67d0c6ced857c6ccaad434b363e46ed9ae4cdc2
expires=2026-11-18T07:43:04Z
```

A deliberately wrong signature returned `401 {"error":"unauthorized"}` and the
controlplane logged `agent HMAC 认证失败 reason=agent signature mismatch`.

### (b) `If-None-Match` → 304

Two-step request with the ETag returned by the first response:

```text
first:  status=200 etag="36020fa8...dc2" len=528
second: status=304 etag="36020fa8...dc2" len=0
```

The running agent (5 s refresh) produced **12 consecutive 304 responses** between
`14:43:09` and `14:44:04` (server log), with one initial 200 at `14:43:04`:

```text
14:43:04 ABAC 策略包已签发 ... policies=2 bundleID=632ce444-...
14:43:09 ABAC 策略包未变更（304） ... (repeats every ~5s)
...
14:44:04 ABAC 策略包未变更（304）
```

### (c) Agent LKG cache exists and is verifiable

```text
path   = ...\abac-bundle.json
size   = 528 bytes
content= {"bundle":{... "policies":[alice/vm:42/read allow, bob/db:prod/write allow]},"signature":"..."}
verify = verified=true (same digest/bundleID as the HTTP artifact)
```

Permissions note (Windows): the code writes the temp file with `0600`
(`internal/agent/abac_bundle_client.go:389`) and renames it. Win32 `os.Stat` cannot
represent Unix permission bits (`Mode=-a----`), so the literal `0600` is not observable
on this host. The file ACL was additionally restricted to the current user. On Linux the
0600 path is the same code path that is covered by unit tests.

### (d) Local evaluation through agent code

Driver built in a temp module importing `internal/agent` (replace → local repo) and using
`ABACBundleClient.EvaluateLocal` (which re-verifies the LKG signature before every decision):

```text
agent bundle loaded: bundleID=632ce444-... tenant=default policies=2 keyId=k1
EvaluateLocal alice vm:42 read   allowed=true  policy="default|read|alice|vm:42"
EvaluateLocal alice vm:43 read   allowed=false policy="" reason="no matching policy (default deny)"
EvaluateLocal bob db:prod write  allowed=true  policy="default|write|bob|db:prod"
```

Expected allow / default-deny behavior matches the server engine.

### (e) Decision upload (M3) and MySQL rows

The same driver called `ABACDecisionReporter.RecordEvaluation` for the three decisions
above and `Flush`:

```text
reporter buffered=3
flush ok accepted=3 failed=0 buffered=0 dropped=0
```

Controlplane log: `ABAC 决策审计已接收 accepted=3`.

MySQL:

```sql
SELECT COUNT(*), MIN(mode), MAX(mode) FROM abac_decision_logs;
-- total=3, min_mode=device, max_mode=device

SELECT tenant_id, agent_id, COUNT(*) rows_n, SUM(allowed) allowed_n, SUM(1-allowed) denied_n
FROM abac_decision_logs GROUP BY tenant_id, agent_id;
-- default | VanguardLea-1794984140969882200 | 3 | 2 | 1
```

Rows (`request_id`, `policy_id`, `allowed`, `mode`):

```text
dogfood-allow-1 | default|read|alice|vm:42  | 1 | device
dogfood-deny-1  | (empty)                    | 0 | device
dogfood-allow-2 | default|write|bob|db:prod | 1 | device
```

All rows carry `tenant_id=default` from the authenticated agent identity, not from the
request body. `occurred_at` and `received_at` were both `2026-11-18 06:53:02` UTC
(sub-second flush, no measurable lag).

M4 note: there is no request gate wiring `EvaluateLocal → RecordEvaluation` yet, so a
plain running agent does not generate decisions by itself. The driver exercised the real
M2/M3 library code paths with the real per-agent credential; this is the expected M3
surface before M4.

### (f) Controlplane restart → policy survives

Controlplane stopped and started again with the same MySQL DSN/flags
(same JWT secret, same signing key). After restart:

```text
POLICY_COUNT=2
POLICY id=default|read|alice|vm:42  effect=allow
POLICY id=default|write|bob|db:prod effect=allow
EVAL_ALLOWED=True  MATCHED=default|read|alice|vm:42
EVAL2_ALLOWED=False REASON="no matching policy (default deny)"
```

Decision logs also survived: `total=3`, `policies=2` after the restart.

## 5. Defects / hazards found

No defect was found in the ABAC M1–M3 happy paths verified above. The following were
observed during the dogfood run:

### D1 (high, non-ABAC): agent spins on the task-svc stub when task-svc is down

- `internal/agent/agent.go:500-513` falls back to `NewStubTaskServiceClient()` when the
  task-svc gRPC dial fails.
- `internal/agent/task_service_client.go:157-175` has `StubTaskServiceClient.ClaimTask`
  always return a synthetic task `test-task-id` (`echo test`) instead of `nil` when empty.
- `internal/agent/agent.go:869-890` `drainTasks` loops until `ClaimTask` returns `nil`.
  Result: a tight infinite loop re-executing the synthetic task. During this dogfood run
  the agent log produced thousands of `任务入队 taskID=test-task-id` lines per second until
  the agent was stopped.
- Impact: any agent started without a reachable task-svc (the normal standalone dogfood
  setup) burns CPU and floods logs. Suggested fix: stub `ClaimTask` returns `(nil, nil)`
  when its map is empty, or fallback should be disabled outside tests.

### D2 (medium, ABAC integration): pre-shared gRPC key cannot authenticate ABAC HTTP

- Agent side: `internal/agent/agent.go:708-710` gives `--grpc-signature-key` top priority
  and stores it in `a.agentSecret`, which is then used by the M1/M3 HTTP clients
  (`internal/agent/agent.go:543,549`).
- Server side: `internal/controlplane/agent_hmac_auth.go:66-70` verifies the HTTP HMAC
  **only** against `store.AgentSecret(agentID)` (the random per-agent secret from the DB).
- Consequence: a deployment configured only with `--grpc-signature-key` (common for
  non-TLS fleets) gets `401` on both `/api/v1/agent/abac-bundle` and
  `/api/v1/agent/abac-decisions` forever.
- Workaround used here: provision the DB-issued `agents.secret` into
  `<data-dir>/agent.key` (equivalent to install-token + TLS delivery).
- Suggested fix: either make the server HTTP verifier also honor the configured pre-shared
  key, or reject/ignore pre-shared keys for ABAC and document that token+TLS delivery is
  required.

### D3 (medium, compose integration): `--control-addr` is reused as the HTTP ABAC base

- `docker-compose.yaml:211` sets `--control-addr=http://controlplane:9090` (the gRPC port).
- `internal/agent/agent.go:543` passes `cfg.ControlAddr` verbatim to
  `NewABACBundleClient`, which appends `/api/v1/agent/abac-bundle` and performs an HTTP GET.
- Consequence: enabling `--abac-bundle-enabled` on the stock compose agent would send HTTP
  to the gRPC listener and fail. In this dogfood we used the HTTP port
  (`http://127.0.0.1:18080`), which is what the config comment says `--control-addr`
  should be.
- Suggested fix: add a dedicated HTTP base URL (e.g. `--control-http-addr`) or make the
  compose agent point `--control-addr` at `controlplane:8080` (gRPC target derivation
  already replaces the port with `--grpc-port`).

### D4 (platform limitation, not a code defect): Windows cannot represent 0600

`os.Stat` on the cache file reports `-a----` and no Unix mode bits. The writer does call
`Chmod(0o600)` (`internal/agent/abac_bundle_client.go:389`); on Windows only the ACL is
meaningful. Linux/macOS must be used for a literal `mode=0600` assertion.

## 6. Clean teardown

- Agent: stopped (phase-1 and phase-2 instances both exited).
- Controlplane: stopped (port 18080/19090 no longer listening).
- MySQL/Redis: `docker compose -p abacdogfood stop mysql redis` →

```text
abacdogfood-redis-1  Exited (0)
abacdogfood-mysql-1  Exited (0)
```

No unrelated containers or volumes were removed. The only other running container
observed (`heuristic_elion`) was left untouched.

## 7. Soak recommendation for M4 (7 days)

M4 is the request gate that will make `EvaluateLocal` run on real request paths. Watch:

1. **Bundle fetch health**
   - `200` vs `304` ratio per agent per hour. In steady state expect >99% `304`; any
     sustained `200` rate without a policy change means canonicalization/ETag drift.
   - Refresh cadence: 5 s → ~17,280 requests/agent/day. Track p95 fetch latency and
     timeouts; one goroutine per agent must remain stable for 7 days.
   - `401` reason split (missing headers / timestamp skew / signature mismatch). Clock
     skew should be monitored on hosts; any `signature mismatch` after startup is a
     credential-rotation or key-source bug.

2. **Signature / bundle integrity**
   - Zero `Verify` failures and zero `ErrNoBundle` after the first successful fetch.
   - `bundleId`/`digest` should change only when policies change (or TTL expiry).
   - Cache: 0600 on POSIX, atomic rename leaves no `.abac-bundle-*.tmp` residue,
     restart loads LKG before the first successful fetch (offline path).

3. **Decision upload (M3)**
   - `accepted / failed / dropped` from `ABACDecisionReporter.Stats()` per agent.
     Target: `dropped=0`, `failed` only during controlplane restarts.
   - `received_at - occurred_at` p50/p95 (DB query). Expect sub-second online; flag
     >30 s lag or any batch >500 records (413 path) as a regression.
   - `abac_decision_logs` growth and index health; retention is 720 h with a 24 h sweep.
     Verify the retention job does not delete offline backfill (`received_at` basis).

4. **Parity between server and device**
   - Replay the 32 shared fixtures (`pkg/abacbundle/testdata` + `abac_eval_parity_test.go`)
     daily against the deployed controlplane and a live agent; mismatch count must be 0.
   - Include deny-overrides, wildcard/prefix patterns, condition equality, empty bundle
     (default deny), and expired-bundle behavior.
   - Alert on `server.evaluate` vs `EvaluateLocal` divergence for the same request tuple.

5. **Enforcement / rollout**
   - Track the M4 gate's allow/deny volume and `BundleExpired` decisions; define
     fail-open/fail-closed per route before the soak.
   - Policy propagation time: `policy-create` → next agent bundle (should be
     ≤ refresh interval + write latency). Watch for write-through failures (policy-create
     returning 500 while the cache still has the old policy).

6. **Restart / failover drills**
   - Restart the controlplane mid-soak: policies and decision logs must survive (verified
     here for a single restart), agents must keep their LKG and resume 304 polling.
   - Verify MySQL reconnect and Redis outage do not break bundle signing (signing is local;
     store reads must degrade predictably).

Suggested daily report: per-agent 200/304 counts, 401 reasons, decisions
accepted/failed/dropped, DB lag p95, parity mismatches, and cache-file checksum.

## 缺陷修复（2026-11-18）

D1–D3 已修复；D4 为平台限制，不涉及代码变更。未提交 commit。

### D1（high）：存根客户端不再伪造任务，空闲不空转

- `internal/agent/task_service_client.go:158` `StubTaskServiceClient.ClaimTask` 改为：
  有预置任务时领取并删除一条，否则返回 `(nil, nil)` 表示“当前无工作”。
  删除了恒返回 `test-task-id` / `echo test` 的合成任务。
- `internal/agent/agent.go:999` `claimTask` 增加空响应/空 `taskId` 防护：
  `resp == nil || taskId == ""` 一律返回 `(nil, nil)`，不再把零值任务投给 worker。
- `internal/agent/agent.go:919` `drainTasks` 对 `nil` 任务立即结束本轮，
  由 `dispatchLoop` 的 ticker 按 `TaskDispatchInterval` 退避后再次领取；真实 gRPC 客户端行为不变。
- 测试：`internal/agent/task_idle_test.go`
  - `TestStubTaskServiceClient_ClaimTaskIdleReturnsEmpty`（空存根返回 nil；预置任务只领取一次）
  - `TestAgentClaimTask_EmptyResponseIsIdle`（nil 响应与空 taskID 均视为空闲）
  - `TestDispatchLoop_StubIdleNoSpin`（600ms / 50ms ticker 窗口内 ClaimTask 有界且 `taskCh` 为空）
  - 结果：3/3 PASS。

### D2（medium）：ABAC HTTP HMAC 改用 DB 签发的注册密钥

- `internal/agent/agent.go:736` 新增 `resolveABACSecret`，优先级：
  Register 响应密钥（并落盘 `agent.key`）→ 本机 `agent.key` → `--grpc-signature-key`。
- `internal/agent/agent.go:802` 注册成功后分别解析 gRPC 密钥（`resolveAgentKey`，优先级不变）
  与 ABAC 密钥；`a.abacSecret` 独立保存，HTTP 通道不再复用预共享 gRPC 密钥。
- `internal/agent/agent.go:546` / `:766` ABAC bundle 与 decision reporter 构造改用
  `a.abacSecret`。回退到预共享密钥时打印显式告警（服务端按 `store.AgentSecret` 验签会 401）。
- 服务端 `internal/controlplane/agent_hmac_auth.go:67` 未改动（仍只认 DB 密钥）。
- 测试：`internal/agent/abac_credential_test.go`
  - `TestResolveABACSecret_PrefersRegistrationSecret`（两者都在时用注册密钥，且 gRPC 仍用预共享）
  - `TestResolveABACSecret_PrefersPersistedKeyOverConfiguredKey`
  - `TestResolveABACSecret_FallbackToConfiguredKey`（无注册密钥才回退；全无则为空）
  - `TestAgentABACBundleClientWiring`（客户端实际持有 `db-secret` 而非 `pre-shared`）
  - 结果：4/4 PASS。

### D3（medium）：ABAC HTTP 基地址独立配置

- `internal/config/config.go:539/794/1027` 新增 `ABACBundleBaseURL`
  （`--abac-bundle-base-url` / `OPSMESH_ABAC_BUNDLE_BASE_URL`，默认空）。
- `internal/agent/agent.go:757` 新增 `abacHTTPBaseURL()`：显式配置优先，
  为空时回退 `ControlAddr`（单机旧行为零变化）；bundle 与 decision reporter 均使用它。
- `docker-compose.yaml:213` agent 新增 `--abac-bundle-base-url=http://controlplane:8080`
  （controlplane HTTP 端口，见同文件 `8080:8080`）；`--control-addr=http://controlplane:9090`
  保持不变。
- 测试：
  - `internal/agent/abac_credential_test.go` `TestABACHTTPBaseURL_OverrideAndDefault`（覆盖优先/默认派生）
  - `internal/config/config_abac_base_url_test.go` `TestLoad_ABACBundleBaseURL_Env`、
    `TestLoad_ABACBundleBaseURL_FlagWinsOverEnv`、`TestLoad_ABACBundleBaseURL_DefaultEmpty`
  - 结果：4/4 PASS。

### 验证命令与结果

```text
go build ./...                                   exit 0
go vet ./internal/agent/... ./internal/config/... exit 0
gofmt -l internal                                （无输出）
go test ./internal/agent/ -count=1               ok  43.370s  exit 0
go test ./internal/config/... -count=1           ok   0.241s  exit 0
```

新增测试定向运行：agent 7/7 PASS（0.813s），config 3/3 PASS（0.149s）。

