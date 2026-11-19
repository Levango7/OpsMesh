# OpsMesh 控制面负载基线

> 由 `scripts/loadtest/run-loadgen.sh` / `run-loadgen.ps1` 自动追加。
> 环境：单机 controlplane `--demo --store=memory`，压测进程与目标同机。
> 判据与 k6 基线一致：overall p99 < 300ms 且错误率 < 0.1%（2xx/3xx 视为成功）。
> 未显式提供 `TOKEN` 时，包装器用 demo 的 `admin/admin123` 登录并完成强制改密以取得 JWT。

| 日期 (UTC) | commit | 场景 | VUs | RPS | p50 (ms) | p95 (ms) | p99 (ms) | 错误率 | 结论 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 2026-11-17T04:19:04Z | 89012346-dirty | loadgen | 50 | 48726.67 | 0.56 | 3.31 | 6.44 | 58.590% | FAIL | 无节流探测 think=0；触发租户限流 1000 rps（429），非基线 |
| 2026-11-17T04:26:34Z | 89012346-dirty | loadgen | 50 | 250.00 | 0.51 | 2.08 | 5.25 | 0.000% | PASS | demo memory :18080 30s think=1s (pwsh) |
| 2026-11-17T04:28:09Z | 89012346-dirty | loadgen | 50 | 249.99 | 0.51 | 2.00 | 5.86 | 0.000% | PASS | demo memory :18080 30s think=1s (bash) |
