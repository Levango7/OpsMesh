# Race Sweep — 2026-11-18

Full `-race` sweep of the main Go module to preempt CI failures. No source changes were made.

## Environment

| Item | Value |
| --- | --- |
| OS | Windows 10/11 amd64 (`win32`) |
| Go | go1.26.6 windows/amd64 |
| C compiler | gcc.exe (Rev7, Built by MSYS2 project) 16.2.0, `C:\Users\winge\AppData\Local\msys64\ucrt64\bin` |
| CGO | `CGO_ENABLED=1` |
| Race runtime | default `GORACE` (no `halt_on_error` override) |
| Module | `github.com/Levango7/OpsMesh` (main module only) |
| Date | 2026-11-18 |

Preparation applied to every chunk:

```powershell
$env:PATH = "C:\Users\winge\AppData\Local\msys64\ucrt64\bin;" + $env:PATH
$env:CGO_ENABLED = '1'
```

`gcc --version` and `go version` both succeeded before the sweep.

## Coverage summary

Chunks were run sequentially, each with `-count=1`; all exited `0`.

| # | Package group | Packages | ok | FAIL | no test files | PASS | SKIP | DATA RACE | Wall | Exit |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | `./pkg/...` | 19 | 19 | 0 | 0 | 456 | 1 | 0 | ~55s | 0 |
| 2 | `./internal/store/...` | 7 | 7 | 0 | 0 | 715 | 50 | 0 | ~594s | 0 |
| 3 | `./internal/agent/...` | 1 | 1 | 0 | 0 | 294 | 2 | 0 | 53.5s | 0 |
| 4 | `./internal/controlplane/` | 1 | 1 | 0 | 0 | 2431 | 12 | 0 | 692.1s | 0 |
| 5 | `./internal/cmdb/... ./internal/k8slifecycle/... ./internal/ai/... ./internal/cost/...` | 6 | 6 | 0 | 0 | 318 | 5 | 0 | ~21s | 0 |
| 6 | remaining `./internal/...` (66 pkgs, see below) | 66 | 60 | 0 | 6 | 2210 | 2 | 0 | ~1560s (parallel) | 0 |
| **Total** | main module (all `pkg` + all 81 `internal`, `cmd/` excluded) | **100** | **94** | **0** | **6** | **6424** | **72** | **0** | — | **0** |

Chunk commands (with the generous timeouts actually used):

```powershell
go test -race ./pkg/... -count=1 -timeout 25m
go test -race ./internal/store/... -count=1 -timeout 25m
go test -race ./internal/agent/... -count=1 -timeout 25m
go test -race ./internal/controlplane/ -count=1 -timeout 30m
go test -race ./internal/cmdb/... ./internal/k8slifecycle/... ./internal/ai/... ./internal/cost/... -count=1 -timeout 25m
go test -race <66 remaining internal packages> -count=1 -timeout 25m
```

Chunk 6 package list (all remaining `internal/...` after excluding the groups above; excluded by name: `internal/store(/...)`, `internal/agent`, `internal/controlplane` exact package, `internal/cmdb(/...)`, `internal/k8slifecycle(/...)`, `internal/ai/...`, `internal/cost(/...)`):

`aiops, alertengine, approval, authctx, automation, circuitbreaker, compliance, config, controlplane/{auditsafe,backup,cmdbcollector,credentials,cutover,embed,enterpriseui,factory,grpc,loginguard,metricscache,paginate,pluginhost,presets,ratelimit,registry,svcproxy,tenantguard}, cron, dag, deploy, discover, discovery, domain, egress, events, extension, fulltext, gates, grpcx, grpcx/pb, grpcx/pb/opsmesh/task/v1, grpcx/pb/opsmesh/v1, helm, k8s, knowledge, logstore, logx, metrics, network, notify, orchestration, otelx, platform, platform/cloudprovider/{aliyun,aws,azure,huawei,tencent,types,volcengine}, platform/ssh, plugin, proto, secrets, tlsutil, version`

Packages with **no test files** (compiled under `-race`, nothing to run): `internal/controlplane/embed`, `internal/controlplane/registry`, `internal/discover`, `internal/grpcx/pb`, `internal/grpcx/pb/opsmesh/task/v1`, `internal/grpcx/pb/opsmesh/v1`.

### Per-package timings (chunk 2 and 5, slowest first)

| Package | Time |
| --- | --- |
| `internal/store/memory` | 392.8s |
| `internal/store/multischema` | 140.4s |
| `internal/store` | 42.1s |
| `internal/store/sqlstore` | 7.4s |
| `internal/store/model` | 2.1s |
| `internal/store/storekit` | 2.7s |
| `internal/store/storefail` | 1.4s |
| `internal/cmdb` | 5.9s |
| `internal/ai/agent` / `ai/eval` | 4.1s each |
| `internal/ai/gateway` | 3.8s |
| `internal/k8slifecycle` | 1.7s |
| `internal/cost` | 1.5s |

Slowest chunk 6 packages: `controlplane/credentials` 155.5s, `controlplane/backup` 96.3s, `gates` 94.5s, `controlplane/grpc` 90.4s, `orchestration` 60.3s, `helm` 39.8s, `controlplane/factory` 38.0s, `cmdbcollector` 29.8s, `platform` 25.6s.

## Data races

**None.** Zero `WARNING: DATA RACE` blocks across all six chunks (all logs scanned; 0 matches). No per-test failures (`--- FAIL:`) either.

## Skips (72 total)

Almost all skips are intentional environment gates; none indicate a race or a broken test.

### MySQL / external-DB integration gates — 65

Gated on `OPSMESH_TEST_MYSQL_DSN` (unset on this host, so the real-DB paths did not execute):

- `internal/store` (1): `TestSQLStore_StubModeSkipGuard`
- `internal/store/multischema` (1): `TestMultiSchemaSmoke_MySQLDSNBranch`
- `internal/store/sqlstore` (48):
  - Audit chain: `TestAuditChainIntegration_{WriteVerify,TamperDetected,DeleteMiddle,DeleteTail,LegacyRows,TenantScope,Archive,ArchiveTailRollback}`
  - Migrations: `TestRunMigrationsFreshDB`, `TestRunMigrationsIdempotent`, `TestSchemaMigrationsTable`, `TestRunMigrations_InitSchemaAlias`, `TestMigrationLock_ExcludesOtherSession`, `TestRunMigrations_VersionGate`, `TestRunMigrations_ChecksumGateFatal`, `TestRunMigrations_ReplayAfterHalfApplied`, `TestRunMigrations_ConcurrentStores`, `TestMigrationDownScripts_UnwindChain`, `TestRunMigrations_RebaselinesLegacyCRLFChecksum`, `TestMigration030NormalizesApprovalRequired_MySQL`, `TestMigrationLockWaitWiredIntoRunMigrations_MySQL`, `TestMigrationMDLFailFast_MySQL`
  - Refresh tokens: `TestSQLStore_CleanupRefreshTokens`, `TestConsumeRefreshToken_SQLStore{Concurrent,DoubleConsume,NonExistent,EmptyHash}`
  - Tasks: `TestCreateTask_ApprovalRequired_MySQL`, `TestCreateTask_NonApprovalTask_MySQL`, `TestAllTasks_CarriesClaimedAt_MySQL`, `TestSubmitResult_RejectsForgedClaimEpoch_MySQL`, `TestDepDownstreamFailedWhenUpstreamFails_MySQL`, `TestDepDownstreamReleasedWhenUpstreamDone_MySQL`, `TestReclaimStaleTasks_DeadLettersAfterBudget_MySQL`, `TestReclaimStaleTasks_HeadroomStaysPending_MySQL`, `TestTasksReadPaths_MySQL`
  - RBAC/tenancy: `TestSeedRBAC_BackfillsPresetRolePermissions`, `TestSeedRBAC_DoesNotReviveMustChangeAfterUserChangedPassword`, `TestSeedRBAC_ConcurrentReSeedIsIdempotent`, `TestSQLStore_RegisterRejectsCrossTenantRebind`, `TestSQLStore_TenantIsolation`
  - P03 / scan round-trip: `TestSQLStore_P03Secret`, `TestSQLStore_P03Config`, `TestSQLStore_P03Discovery`, `TestAgentsRealDBNullableColumnsRoundTrip`, `TestAlertsReadBackNullColumns`, `TestSQLABACDecisionLogs_AppendListDelete`
- `internal/cmdb` (4): `TestSearchCIsFulltextIntegration{WithIndex,TenantIsolation,MatchesLikeWithoutIndex,RecallNotNarrowed}`
- `internal/logstore` (1): `TestSQLNullRoundTripRealMySQL`
- `internal/controlplane` (1): `TestKubeconfigLegacyMigration_MySQL`

Report-mode gates (not run unless env var set):

- `TestSelectArityReport` in both `internal/cmdb` and `internal/store/sqlstore` — requires `OPSMESH_ARITY_REPORT=1`.
- `TestHTTPClientReal` (`internal/aiops`) — requires `OPSMESH_AIOPS_API_KEY` + `OPSMESH_AIOPS_BASE_URL`.

### Enterprise UI bundle not assembled — 9 (all in `internal/controlplane`)

Skipped because `web/enterprise/dist` assets are not embedded in this checkout:

`TestEnterpriseUI_SPAFallback`, `TestEnterpriseUI_UnlicensedShowsNoticePage`, `TestEnterpriseUI_UnlicensedSPADeepLinksAlsoGated`, `TestEnterpriseUI_LicensedServesBundle`, `TestEnterpriseUI_FeatureListGatesShell`, `TestEnterpriseAsset_PrecompressedNegotiation`, `TestEnterpriseAsset_UnlicensedReturns402`, `TestEnterpriseAsset_MissingIs404Not402`, `TestDashboardCTA_KeptForConversion`

### Platform / fake-client limitations — 3

- `internal/controlplane`: `TestHandleScaleDeployment_Happy` — "fake clientset GetScale panics on Deployment->Scale conversion"
- `internal/controlplane`: `TestK8sScaleDeployment_Happy` — "fake clientset GetScale not stable"
- `pkg/provision`: `TestAutoProvision_SSHPushSuccessLoop` — needs to bind TCP port 22; skipped on this Windows host (`端口 22 不可用`)

## Recommended CI matrix

Use Linux (`ubuntu-latest`) for race jobs: CGO + MySQL service are first-class there, and the Windows-only port-22 skip disappears.

| Job | Trigger | Packages | Config |
| --- | --- | --- | --- |
| `race-pkg` | every PR | `./pkg/...` | `CGO_ENABLED=1`, `-race -count=1 -timeout 15m` |
| `race-store` | PR touching `internal/store/**` or persistence, plus nightly | `./internal/store/...` | `-race -count=1 -timeout 30m`; MySQL 8 service + `OPSMESH_TEST_MYSQL_DSN`; `-p 2` |
| `race-agent` | every PR | `./internal/agent/...` | `-race -count=1 -timeout 15m` |
| `race-controlplane` | PR touching `internal/controlplane/**`, plus nightly | `./internal/controlplane/` | `-race -count=1 -timeout 30m`; pre-build enterprise bundle when the gated tests are wanted |
| `race-core` | every PR | `./internal/cmdb/... ./internal/k8slifecycle/... ./internal/ai/... ./internal/cost/...` | `-race -count=1 -timeout 15m`; MySQL service |
| `race-rest` | nightly + PR touching shared packages | remaining `internal/...` (chunk 6 list) | `-race -count=1 -timeout 30m -p 2` |
| `race-full` | nightly on `main` | all of the above, one invocation per shard | mirror local chunks; archive `go test -json` logs as artifacts |

Practical notes:

- `-p 2` (or `-p 1` on 2-vCPU runners) avoids OOM/CPU starvation: `internal/store/memory` alone takes ~6.5 min under `-race`, and chunk 6's parallel run is dominated by `controlplane/credentials`, `backup`, `gates`, and `grpc`.
- Keep `-timeout` at 30m per shard; the default 10m would mask results in `internal/store/...` and `internal/controlplane`.
- Provide `OPSMESH_TEST_MYSQL_DSN` in at least one nightly job; otherwise ~65 real-DB tests stay skipped and tenant-isolation/migration concurrency paths never run under `-race`.
- Build `web/enterprise/dist` before the controlplane job if the 9 enterprise-UI tests should execute; otherwise accept those skips.
- `cmd/` was intentionally excluded per task scope, and separate modules under `services/` (if any) are outside the main module and were not swept.
