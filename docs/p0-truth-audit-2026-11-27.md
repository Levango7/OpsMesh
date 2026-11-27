# P0 Truth Audit — 2026-11-27

Scope: documentation only. No runtime code changed. Evidence gathered by grep/counts in `F:\Agent\kilo\workspace\OpsMesh`.

## 1. README / DELIVERY capability claims vs code evidence

### 1.1 Code-level “not implemented / 501” surfaces
- `grep ErrNotImplemented` (Go): 11 files; sentinel defined in `internal/platform/cloudprovider/types/types.go:63-64` and aliased in `internal/platform/cloudprovider/provider.go:44-45`.
- `grep http.StatusNotImplemented` (non-test, non-generated): 14 files, including `internal/controlplane/ha.go`, `internal/controlplane/server_lifecycle.go`, `internal/deploy/handler.go`, `internal/logstore/handler.go`, `internal/ai/agent/handler.go`, `internal/controlplane/audit_query.go`.
- Gate: `internal/gates/not_implemented_doc_gate_test.go` pins 7 routes in `docs/api-reference.md` “未接线能力（显式 501 清单）”: `/api/v1/ha/failover`, `/api/v1/audit/verify`, `/api/v1/tasks/{id}/retry`, `/api/v1/tasks/{id}/dead-letter`, `/api/v1/deploys/{id}/auto-advance`, `/api/v1/logs`, `/api/v1/ai/agent/execute`.

### 1.2 Cloud providers
- `internal/platform/cloudprovider/registry.go` registers 6 providers (aliyun/aws/azure/tencent/huawei/volcengine) and comments claim all are “真实实现”.
- `internal/platform/cloudprovider/provider.go` package comment still says “aliyun 已接入真实实现，其余为 stub” (stale).
- All six implement `CreateResource/DeleteResource/SyncTags` in code, but capability depth varies (e.g. tencent `CreateResource` only `instance`). Memory note claiming “change operations remain ErrNotImplemented” is stale vs code.

### 1.3 K8s lifecycle
- `README.md` row 9 claims `upgrade/rollback/cert-rotate 后端 501，internal/k8slifecycle/handler.go:490/553/570`.
- Actual: handlers are registered (`internal/k8slifecycle/handler.go:1534-1538`) and `runUpgrade/runRollback/runCertRotation` are implemented; comment at `internal/k8slifecycle/handler.go:183` still says cert rotation execution chain not implemented. Evidence is stale/mismatched.

### 1.4 ABAC
- Default is opt-in: `--abac-enforce` default empty, `--abac-enforce-preset` default empty (`internal/config/config.go:805-806`); docs/projects confirm default off. Any claim of default-on enforcement is an overclaim.

### 1.5 Tenant isolation
- Controlplane proxy strips client tenant headers (`internal/controlplane/service_proxy.go:103-110`), but direct microservice access remains fail-open in several services.
- `services/device-svc/internal/http/gateway.go:248/295` uses `if tenantID != "" && dev.TenantID != tenantID` (omit param skips check). See `docs/semantic-rulings-2026-11-01.md`.

### 1.6 Injection residues
- `scripts/shell-injection-audit.py` latest run: `raw_interp=0`, `derived_interp=4`, `raw_arg=30`, `raw_fprintf=2` (36 residual findings), e.g. `internal/controlplane/os_tuning_ops.go:485`, `internal/controlplane/security_ext_ops.go:102`, `internal/controlplane/ssh_trust_ops.go:184/229`, `internal/controlplane/docker_ops.go:60-88`, `internal/controlplane/git_unix_ops.go:647/673/687`.

### 1.7 Dead / ratcheted API surfaces
- `internal/gates/ops_route_ratchet_gate_test.go`: 408 registered write routes, ledger claims 0 unguarded after dispatch-port gating; 284 historical unguarded routes were closed via dispatch port.
- Capability evidence gate (`internal/gates/capability_evidence_gate_test.go`) only checks path existence, not functional availability.

## 2. Likely overclaims
1. Cloud provider write ops: docs/memory and package comments lag code; capability depth (supported resource types) undocumented.
2. K8s upgrade/rollback/cert rotation: README 501 claim and code comment contradict implemented handlers.
3. ABAC enforcement default: opt-in only; default off.
4. Tenant fail-open: direct service access and query-based tenant checks.
5. Injection residues: 36 residual audit findings remain.
6. Dead API surfaces: 501 endpoints and ratcheted write routes; frontend calls may target 501 endpoints.

## 3. Stale numeric claims (re-verify)
- DELIVERY.md: 1,411 repo files / 1,382 Go files / 651 test files / 6,460 test funcs / 318 frontend files / 13 services.
- Actual counts: 1,605 Go files / 776 test files / 6,948 test funcs / 346 frontend files / 13 service dirs.
- DELIVERY.md toolchain go1.26.6 vs actual go1.26.11; go.mod declares go 1.26.0.
- README service count “12 + tf-provider” vs actual 13 dirs.

## 4. Prioritized P0 fixes
P0-1 (doc correction): `README.md` K8s row 9 and `internal/k8slifecycle/handler.go:183` comment; align 501 status with implementation or mark partial. Paths: `README.md`, `internal/k8slifecycle/handler.go`.
P0-2 (doc correction): cloud provider status in `internal/platform/cloudprovider/provider.go` package comment and `registry.go`; publish capability matrix. Paths: `internal/platform/cloudprovider/provider.go`, `registry.go`, `docs/api-reference.md`.
P0-3 (guard test): ABAC default assertion (`--abac-enforce` empty unless preset) in config tests. Path: `internal/config/config.go`.
P0-4 (API truth): keep `docs/api-reference.md` 501 list synced via `internal/gates/not_implemented_doc_gate_test.go`.
P0-5 (security): remove query-based tenant fail-open in `services/device-svc/internal/http/gateway.go:248/295` or enforce `RequireTenant`; document threat model in `docs/semantic-rulings-2026-11-01.md`.
P0-6 (injection): close 36 residual findings listed by `scripts/shell-injection-audit.py`; add guard tests per op family.
P0-7 (numeric): refresh DELIVERY.md/README counts or add generation script.

## Summary
Report written to `docs/p0-truth-audit-2026-11-27.md`. Key risks: stale K8s 501 claim, cloud provider capability drift, tenant fail-open in direct service access, 36 injection residues, stale numeric counts.
