# Security scanning kit (R7)

Scheduled/local Trivy scans that produce a reviewable report artifact. CI already
runs a Trivy filesystem gate on pull requests; this kit covers the missing half:
a repeatable weekly scan (or on-demand local scan) with a timestamped report and a
gate-friendly exit code.

`.github/workflows/` is frozen by `AGENTS.md`, so scheduling lives outside the
repository (host cron, Windows Task Scheduler, Jenkins, GitLab schedules, etc.).
See [Weekly scheduling](#weekly-scheduling).

## Requirements

| Tool | Needed for | Notes |
|---|---|---|
| `trivy` | both scripts | Script exits `127` with install hints when missing |
| `bash` 4+ | `cve-scan.sh` | Uses arrays and `${var//pattern/replacement}` |
| `jq` or `python3` | `cve-scan.sh` | Parses Trivy JSON into the summary |
| PowerShell 5.1+ | `cve-scan.ps1` | Uses built-in `ConvertFrom-Json` |

Install Trivy: <https://trivy.dev/latest/getting-started/installation/>
(`brew install trivy`, `apt-get install trivy`, `winget install AquaSecurity.Trivy`).

Trivy downloads its vulnerability database on first use. In air-gapped setups set
`TRIVY_CACHE_DIR` to a pre-populated cache and use Trivy's offline-scan options.

## Usage

Run from the repository root (the scripts verify `deploy/helm/opsmesh` exists).

```bash
# Weekly-style review run: HIGH findings are reported but do not block.
./scripts/security/cve-scan.sh

# Stricter gate: also fail on HIGH vulnerabilities / HIGH misconfigurations.
./scripts/security/cve-scan.sh --fail-on-high

# Scan deployed images too.
IMAGES="ghcr.io/opsmesh/opsmesh:0.14.0,ghcr.io/opsmesh/opsmesh-agent:0.14.0" \
  ./scripts/security/cve-scan.sh
```

```powershell
# Windows PowerShell equivalent.
.\scripts\security\cve-scan.ps1
.\scripts\security\cve-scan.ps1 -FailOnHigh
$env:IMAGES = 'ghcr.io/opsmesh/opsmesh:0.14.0,ghcr.io/opsmesh/opsmesh-agent:0.14.0'
.\scripts\security\cve-scan.ps1 -OutDir 'D:\security-reports\weekly'
```

Both scripts accept:

| Flag / variable | Meaning |
|---|---|
| `--fail-on-high` / `-FailOnHigh` | Also fail on HIGH vulnerabilities and HIGH misconfigurations |
| `IMAGES` | Comma-separated image references for `trivy image`; empty skips image scans |
| `SECURITY_REPORT_DIR` | Report directory; default `./security-reports/<UTC timestamp>` |
| `TRIVY_BIN` | Trivy executable path/name |
| `-OutDir <path>` | PowerShell-only report directory override |

Default report directory: `./security-reports/20261116T053728Z/` (UTC, second precision).

## What each scan covers

1. **Filesystem** — `trivy fs --scanners vuln,secret,misconfig --severity HIGH,CRITICAL .`
   - language/OS dependency vulnerabilities, committed secrets, and misconfigured
     IaC/config files in the working tree.
   - Severity is limited to HIGH/CRITICAL to match the PR gate and keep the report
     actionable.
2. **Images** — `trivy image` for each entry in `IMAGES`.
   - Scans the exact image reference (prefer digests; see
     [Images list convention](#images-list-convention)).
   - Empty `IMAGES` is not an error: the summary records that image scanning was
     skipped and the filesystem/Helm scans still run.
   - All severities are recorded so the weekly review can spot MEDIUM trends.
3. **Helm chart** — `trivy config --scanners misconfig deploy/helm/opsmesh`.
   - Kubernetes/Helm misconfiguration checks for the chart templates.
   - All severities are recorded; misconfigurations are advisory unless
     `--fail-on-high` is used.

The local filesystem scan intentionally does **not** pass `.trivyignore`. The CI
gate's suppressions are reviewed separately; the weekly report should still show
what CI is hiding so accepted risks stay visible and expirable.

## Output layout

```
security-reports/20261116T053728Z/
  fs.json                          # filesystem scan (vuln, secret, misconfig)
  image-1-ghcr.io_opsmesh_opsmesh_0.14.0.json
  image-2-ghcr.io_opsmesh_opsmesh-agent_0.14.0.json
  helm-config.json                 # deploy/helm/opsmesh misconfig scan
  findings.tsv                     # normalized rows (bash)
  findings.csv                     # normalized rows (PowerShell)
  summary.md                       # verdict, severity counts, per-scan table
```

Both scripts print the report directory as the final line, so schedulers can
capture it:

```bash
REPORT_DIR="$(./scripts/security/cve-scan.sh | tail -n 1 | sed 's/^Report: //')"
```

**Do not commit report directories.** Trivy JSON can contain matched secret
material and raw file context. Archive reports in the scheduler's artifact store
and restrict access.

## Verdict logic

`summary.md` starts with `PASS` or `FAIL`.

| Condition | Default | With `--fail-on-high` |
|---|---|---|
| CRITICAL vulnerability | FAIL | FAIL |
| Any secret finding (any severity) | FAIL | FAIL |
| HIGH vulnerability | PASS (reported) | FAIL |
| HIGH misconfiguration | PASS (reported) | FAIL |
| CRITICAL misconfiguration | PASS (reported) | PASS (reported) |
| Scan error / unparseable JSON | FAIL (results incomplete) | FAIL |

Exit codes:

| Code | Meaning |
|---|---|
| `0` | PASS |
| `1` | FAIL (blocking finding or incomplete scan) |
| `2` | Usage/prerequisite error (wrong cwd, missing `jq`/`python3`) |
| `127` | `trivy` not installed |

A scan error fails the run on purpose: "no findings" is not trustworthy when a
scanner failed. Fix the scan (registry auth, network, DB download) before
interpreting the result.

## Images list convention

`IMAGES` is a comma-separated list of image references:

```bash
IMAGES="ghcr.io/opsmesh/opsmesh:0.14.0,ghcr.io/opsmesh/opsmesh-agent:0.14.0"
```

Conventions:

- **Pin digests for reproducible weekly reports**:
  `ghcr.io/opsmesh/opsmesh@sha256:<digest>`. Tags can move; digests cannot.
- Keep the list aligned with what is actually deployed. Source it from
  `deploy/helm/opsmesh/values.yaml` / `values-production.yaml` and the release
  manifest rather than inventing a separate inventory.
- Private registries: authenticate before the scan (`docker login`, or set
  `TRIVY_USERNAME` / `TRIVY_PASSWORD`). The scripts do not add `--insecure`.
- Empty entries and surrounding whitespace are ignored. Duplicate refs produce
  separate JSON files (one per list position).
- If `IMAGES` is unset, image scanning is skipped explicitly in the output — it is
  never silently treated as "no vulnerabilities".

## Weekly scheduling

Pick one scheduler. Reports should be retained at least through the next review
cycle (90 days is a practical default for CVE triage).

### Linux/macOS cron

```cron
# Every Monday 03:17 UTC — adjust paths and image list.
17 3 * * 1 cd /srv/opsmesh && IMAGES="ghcr.io/opsmesh/opsmesh@sha256:...,ghcr.io/opsmesh/opsmesh-agent@sha256:..." ./scripts/security/cve-scan.sh >> /var/log/opsmesh-cve.log 2>&1
```

Add log rotation for `/var/log/opsmesh-cve.log`; reports accumulate under
`security-reports/` and should be copied to the artifact store before pruning.

### Windows Task Scheduler

```powershell
$repo = 'F:\Agent\kilo\workspace\OpsMesh'
$action = New-ScheduledTaskAction `
  -Execute 'powershell.exe' `
  -Argument '-NoProfile -ExecutionPolicy Bypass -File "F:\Agent\kilo\workspace\OpsMesh\scripts\security\cve-scan.ps1" -FailOnHigh' `
  -WorkingDirectory $repo
$trigger = New-ScheduledTaskTrigger -Weekly -DaysOfWeek Monday -At 3:17am
Register-ScheduledTask `
  -TaskName 'OpsMesh weekly CVE scan' `
  -Action $action `
  -Trigger $trigger `
  -Description 'Weekly Trivy report for CVE review (R7)'
```

Scheduled tasks do not inherit an interactive shell's `IMAGES`; set it as a
machine/user environment variable or add a wrapper `.ps1` that sets `$env:IMAGES`.

### External scheduler (GitHub Actions workflows are frozen)

Use any scheduler that can check out the repository and run a shell step. The
workflow file lives outside `.github/workflows/`, so the frozen CI is untouched.

**Jenkins** (declarative pipeline in Jenkins, not in this repo):

```groovy
pipeline {
  agent any
  triggers { cron('H 3 * * 1') }
  stages {
    stage('CVE scan') {
      steps {
        sh 'IMAGES="ghcr.io/opsmesh/opsmesh@sha256:..." ./scripts/security/cve-scan.sh --fail-on-high'
      }
    }
  }
  post {
    always {
      archiveArtifacts artifacts: 'security-reports/**', allowEmptyArchive: true
    }
  }
}
```

**GitLab CI** (`.gitlab-ci.yml`, separate from GitHub workflows):

```yaml
trivy-weekly:
  rules:
    - if: '$CI_PIPELINE_SOURCE == "schedule"'
  script:
    - IMAGES="ghcr.io/opsmesh/opsmesh@sha256:..." ./scripts/security/cve-scan.sh --fail-on-high
  artifacts:
    when: always
    paths:
      - security-reports/
    expire_in: 90 days
```

**Argo Workflows / Kubernetes CronJob**: run the same script in a job image that
contains `bash`, `trivy`, and `jq`, with the repository mounted or cloned, then
upload `security-reports/` to object storage. Use `--fail-on-high` if the job is
meant to be a gate; otherwise leave it off and review the summary weekly.

Because the scripts return `1` on blocking findings, any scheduler can treat a
non-zero exit as an alert trigger while still archiving `summary.md`.

## Baseline and triage process

### First run / baseline

1. Run the scanner on the current default branch and keep that `summary.md` as
   the baseline for the quarter.
2. From then on, review deltas (new CRITICAL/HIGH, new secrets, count changes)
   rather than re-reading the entire report.
3. Re-baseline after a release or a large dependency upgrade, and record the
   baseline commit/tag next to the archived report.

### Triage each blocking or notable finding

1. **Confirm** the finding applies: package/version present, code path reachable,
   secret is real (not a test fixture or documented placeholder).
2. **Fix** first: upgrade the dependency/base image, remove/rotate the secret, or
   correct the chart configuration.
3. **If risk is accepted**, add a row to `docs/tech-debt.md` following the existing
   ledger style, for example:

   | ID | Item | Decision / rationale | Owner | Accepted | Expires | Compensating control |
   |---|---|---|---|---|---|---|
   | TD-XXX | CVE-YYYY-NNNNN in `pkg@version` | Not reachable: code path disabled; no fixed release | team/owner | 2026-11-16 | 2027-02-14 | Network policy blocks exposure; WAF rule |
   | TD-XXX | Secret scanner hit in `path/file` | Test fixture, not a live credential | team/owner | 2026-11-16 | 2027-02-14 | File excluded from deploy artifacts |

4. **Expiry is mandatory** and should be at most 90 days out. An expired
   acceptance is an open finding again; re-review or fix before the date.
5. **Do not edit `.trivyignore` to silence the weekly report.** That file belongs
   to the CI gate and every entry there still needs a tech-debt row and rationale.
   The local scan deliberately ignores it so suppressed findings remain visible.
6. Re-run the scan after every fix or acceptance and archive the new
   `summary.md` with the triage note. Keep the previous report for audit history.

### Handling a failed scan

A scan error (`1` exit with `Scan errors` in the summary) is not a vulnerability
finding and must not be triaged as one. Check Trivy DB download/network, registry
credentials for `IMAGES`, and JSON parse errors, then rerun. Do not mark the week
as "clean" until all three scans complete.
