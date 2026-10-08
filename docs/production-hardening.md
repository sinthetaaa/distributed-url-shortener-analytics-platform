# Production Hardening & CI

Phase 14 turns ShortScale's validated distributed-system implementation into a repeatable, security-scanned, CI-enforced production candidate.

The goal was not to add arbitrary process or infrastructure. Each hardening control was either derived from the existing architecture, validated locally, or enforced remotely through GitHub Actions.

## Phase Outcome

Phase 14 is complete.

ShortScale now has:

- deterministic lint and static-analysis checks
- automated tests and race detection
- build validation
- deterministic sqlc generation validation
- full migration round-trip validation
- production container build validation
- reachable Go vulnerability scanning
- repository vulnerability, secret, and misconfiguration scanning
- production image vulnerability scanning
- non-root application containers
- read-only application root filesystems
- all Linux capabilities dropped from custom application containers
- `no-new-privileges` runtime enforcement
- CI regression checks protecting the runtime-hardening configuration
- deliberately tested CI failure behavior

The main CI workflow contains eight blocking jobs:

1. Lint
2. Test
3. Race detector
4. Build
5. sqlc determinism
6. Migrations
7. Containers
8. Security

## 14A — Hardening Baseline Audit

Before introducing new gates, the existing repository was audited.

Validated successfully:

```text
go mod verify
go mod tidy -diff
gofmt
go vet ./...
go test ./...
go build ./...
sqlc generate determinism
Goose migrations
docker compose config
CGO_ENABLED=1 go test -race ./...
```

No source changes were required.

Coverage was measured rather than converted into an arbitrary blocking threshold.

Measured package coverage included approximately:

```text
analytics consumer    9.9%
API                  66.4%
analytics            70.9%
cache                91.1%
observability        76.2%
shortcode            92.3%
overall              61.8%
```

The project intentionally does not fail CI on a made-up repository-wide coverage percentage. Regression tests are added around behavior where they provide engineering value.

The existing production images were also inspected.

API image:

```text
~10.2 MB
scratch runtime
non-root 65532:65532
no shell
```

Analytics-consumer image:

```text
~8.8 MB
scratch runtime
non-root 65532:65532
no shell
```

## 14B — Lint & Static Analysis

ShortScale uses:

```text
golangci-lint v2.14.0
```

with the standard linter set:

```text
errcheck
govet
ineffassign
staticcheck
unused
```

and `gofmt` formatting validation.

No project-wide linter suppressions were introduced to force the repository green.

The initial lint pass found and corrected:

- unchecked Redis close errors
- non-idiomatic Kafka error strings
- dead wrapper functions

Commit:

```text
aae6e247c5d25dc28d5177578481899dee53cfaf
chore: add lint and static analysis gate
```

## 14C — Core GitHub Actions CI

The initial CI pipeline introduced four blocking jobs:

```text
Lint
Test
Race detector
Build
```

The workflow runs on:

```text
push to main
pull requests
```

and uses minimal read-only repository permissions.

Commit:

```text
195dfb00b80c1bd1d0882302548831a943d43bb6
ci: add core Go validation pipeline
```

The first remote four-job run completed successfully.

## 14D — Generated Code, Database & Container Validation

CI was extended with three additional jobs.

### sqlc determinism

CI installs the pinned sqlc version, regenerates database code, and fails if generation changes the tracked generated output.

This prevents stale generated database code from being merged.

### Migration round trip

CI starts an isolated PostgreSQL 16 service and performs:

```text
migrate up
inspect status
migrate down to zero
migrate up again
inspect status
```

This validates both forward and reverse migration behavior from a clean database.

### Production container artifacts

CI validates Compose configuration, builds both custom production images, and asserts:

```text
API user       = 65532:65532
API entrypoint = /shortscale-api

consumer user       = 65532:65532
consumer entrypoint = /shortscale-analytics-consumer
```

Commit:

```text
442934e3c7d366a3dc1b4de4a92c43fcb2a9b3f2
ci: validate database and container artifacts
```

All seven jobs passed remotely.

## 14E — Dependency & Vulnerability Scanning

Security scanning was first run locally before it was made blocking.

Baseline results:

```text
govulncheck                    0 reachable vulnerabilities
API image HIGH/CRITICAL        0 findings
consumer image HIGH/CRITICAL   0 findings
repository HIGH/CRITICAL       0 findings
Dockerfile misconfigurations   0 findings
```

The CI security job uses:

```text
govulncheck v1.8.0
Trivy v0.75.0
trivy-action v0.36.0
```

The repository scan covers:

```text
vulnerabilities
secrets
misconfigurations
```

Production images are scanned for HIGH and CRITICAL fixed vulnerabilities.

Security findings are blocking. There is no blanket ignore file and no `continue-on-error` behavior.

### Scanner compatibility failure

The first remote security run exposed a tooling problem rather than a ShortScale vulnerability.

`govulncheck v1.1.4` crashed under Linux with Go 1.27.1 inside its older `golang.org/x/tools` dependency.

The failure was reproduced and isolated.

`govulncheck v1.8.0` was then validated inside:

```text
golang:1.27.1-alpine
```

and returned:

```text
No vulnerabilities found.
```

The scanner was upgraded instead of weakening or removing the security gate.

Commits:

```text
fa1f378d1e5d9a54208eda0b39c370c3022827b7
ci: add vulnerability scanning

8cb0748b9b9dbb5a342a181ab8ee54c99ccec3bf
ci: update vulnerability scanner
```

The final eight-job pipeline passed remotely.

## 14F — Runtime Container Hardening

The custom ShortScale services already used minimal scratch images and non-root users, so the next useful security boundary was runtime isolation.

Both:

```text
api
analytics-consumer
```

now enforce:

```yaml
user: "65532:65532"
read_only: true
cap_drop:
  - ALL
security_opt:
  - no-new-privileges:true
```

### Runtime validation

The hardened system was started with:

```text
3 API replicas
1 analytics consumer
PostgreSQL
Redis
Kafka
Nginx
Tempo
OpenTelemetry Collector
```

All three API replicas reported:

```text
User: 65532:65532
ReadonlyRootfs: true
CapDrop: ["ALL"]
SecurityOpt: ["no-new-privileges:true"]
```

The analytics consumer reported the same controls.

Application behavior remained healthy:

```text
readiness         HTTP 200
liveness          HTTP 200
consumer metrics  HTTP 200
```

CI now parses the normalized Compose configuration and fails if either custom service loses any of these protections.

Commit:

```text
2da03faf407ae02865234c9c866368a684d7e620
chore: harden application containers
```

The corresponding eight-job GitHub Actions run completed successfully.

## 14G — Failure Experiments

Phase 14 ended by testing whether the new gates actually reject bad changes.

These experiments were temporary and the repository was restored after each one.

### Experiment 1 — Broken workflow syntax

A malformed GitHub Actions workflow was passed to `actionlint`.

Result:

```text
exit code 1
PASS — malformed workflow rejected
```

### Experiment 2 — Broken Go source

A temporary Go source file containing an unused variable was introduced.

`golangci-lint` rejected it with:

```text
declared and not used
exit code 1
```

Result:

```text
PASS — invalid source rejected
```

### Experiment 3 — Generated-code drift

An untracked file was introduced into the sqlc generated-code directory.

Git detected:

```text
?? internal/database/generated/phase14g_drift.go
```

Result:

```text
PASS — generated-code drift detected
```

### Experiment 4 — Runtime hardening regression

The API's read-only root filesystem was temporarily changed from:

```text
true
```

to:

```text
false
```

The same assertion used by CI failed.

Result:

```text
exit code 1
PASS — weakened runtime configuration rejected
```

After restoration, the hardening assertion passed again.

Final quality gates also passed:

```text
actionlint
golangci-lint
go test ./...
docker compose config
git diff --check
```

The repository ended at the same commit where the experiments started and with a clean worktree.

No experiment commit was created.

## Hardening Decisions

Several things were deliberately not added simply to make the project appear more complicated.

### No arbitrary coverage threshold

Coverage is measured, but a global threshold is not used as a vanity metric.

### No arbitrary CPU or memory limits

Resource limits should be based on deployment measurements and workload behavior. They were not invented during local hardening.

These can be finalized during deployment when the target runtime and resource envelope are known.

### No blanket vulnerability suppressions

Scanner failures or findings must be understood rather than globally ignored.

### No weakening of gates for tool failures

When the original govulncheck version failed in CI, the scanner was upgraded and validated under the target Go/Linux environment.

### Dependency update automation

Phase 14 focuses on deterministic validation and blocking vulnerability detection.

Automated dependency-update PR generation is not required for correctness of the current build and was not mixed into the security gate. Dependency versions remain explicit and scanner-enforced.

## Final Production-Readiness State

At the end of Phase 14:

```text
Source quality
  ✓ formatting
  ✓ lint/static analysis
  ✓ unit/regression tests
  ✓ race detector
  ✓ builds

Database
  ✓ deterministic sqlc output
  ✓ clean migration up/down/up round trip

Containers
  ✓ minimal scratch application images
  ✓ non-root runtime
  ✓ read-only root filesystem
  ✓ all capabilities dropped
  ✓ no-new-privileges
  ✓ CI regression enforcement

Security
  ✓ reachable Go vulnerability scan
  ✓ repository vulnerability scan
  ✓ secret scan
  ✓ misconfiguration scan
  ✓ API image scan
  ✓ analytics-consumer image scan

CI behavior
  ✓ eight blocking jobs
  ✓ deliberate failure experiments
  ✓ remote successful pipeline
```

ShortScale is therefore ready to move from local production hardening into deployment engineering.

## Next Phase

**Phase 15 — Deployment**

Deployment is a required project outcome.

The next phase will select and implement a deployable production topology, configure real environment/secrets handling, provision required infrastructure, deploy ShortScale, validate the public system, and preserve observability and operational behavior in the deployed environment.
