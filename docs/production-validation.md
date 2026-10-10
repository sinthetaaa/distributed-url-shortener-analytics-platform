# ShortScale Production Validation Record

This document consolidates the production evidence collected during Phase 15 after ShortScale was deployed.

It complements the local benchmark and failure-engineering reports. The measurements here describe the deployed portfolio-scale system only and must not be interpreted as a maximum-capacity or high-availability certification.

## Validated production topology

```text
Vercel frontend
      ↓
Railway API
   ↙       ↘
Upstash   Aiven Kafka
 Redis         ↓
         Railway analytics consumer
                  ↓
             Neon PostgreSQL

Railway API + consumer
          ↓ private OTLP
Railway Grafana Alloy
          ↓
     Grafana Cloud
```

The consumer and Alloy services have no public application domain.

The public API keeps Prometheus metrics off the public application listener.

## Production observability validation

Production metrics were verified for both application processes.

Observed application health included:

```text
API up = 1
consumer up = 1
Kafka consumer lag = 0
consumer failures = 0
```

A real redirect increased the consumer processed counter from 0 to 1.

A real distributed trace crossed:

```text
API request
→ Kafka publication
→ analytics consumer
→ PostgreSQL persistence
```

One verified cross-service trace contained:

```text
2 services
7 spans
```

This established that trace context crosses the asynchronous Kafka boundary in production.

Public metrics isolation was also verified:

```text
public API /metrics → 404
API readiness       → 200
```

## Production resource measurement

Idle process measurements:

| Service | RSS | Go heap |
| --- | ---: | ---: |
| API | ~29.1 MiB | ~7.88 MiB |
| Analytics consumer | ~31.3 MiB | ~5.01 MiB |

Idle goroutines were approximately:

```text
API       18
consumer  32
```

A controlled 100-redirect production exercise returned exactly 100 HTTP 302 responses.

Observed maxima during that short exercise were approximately:

| Metric | API | Consumer |
| --- | ---: | ---: |
| RSS | 32.4 MiB | 31.6 MiB |
| Go heap | 9.66 MiB | 7.40 MiB |
| Goroutines | 32 | 35 |
| 1-minute-smoothed CPU | 0.400% core | 0.444% core |

After recovery:

| Metric | API | Consumer |
| --- | ---: | ---: |
| RSS | 35.1 MiB | 30.4 MiB |
| Go heap | 8.24 MiB | 4.22 MiB |
| Goroutines | 20 | 34 |

The measurements support only a modest-resource-use conclusion for the tested workload.

They do not prove the absence of memory leaks, a maximum throughput boundary, or a production capacity ceiling.

## Production security audit

The production audit verified:

- frontend publicly exposed through Vercel
- API publicly exposed through Railway
- analytics consumer has no public domain
- observability service has no public domain
- public API `/metrics` returns 404
- public debug/admin-style probe paths return 404
- liveness/readiness remain available
- public HTTP redirects to HTTPS
- no permissive CORS policy was observed
- browser API access uses the same-origin frontend proxy
- production session cookie is Secure, HttpOnly, and SameSite=Lax
- application containers run non-root
- application request bodies and JSON parsing are bounded/strict
- Kafka uses TLS/SASL
- production Redis uses TLS
- URL validation accepts only HTTP/HTTPS destinations
- ShortScale does not fetch destination URLs as part of shortening
- analytics endpoints enforce authentication and ownership
- SQL access uses sqlc-generated parameterized queries
- repository secret scanning was clean
- production dependency audit reported no production npm vulnerabilities
- govulncheck reported no reachable/imported Go vulnerabilities in the audited application paths

The audit also deliberately records current limitations:

- no dedicated CSRF token mechanism
- duplicate-registration behavior can reveal that an email is already registered
- managed-provider availability is relied upon for the production database and other external dependencies

## Analytics-consumer outage and recovery

The analytics consumer was deliberately removed while the API remained available.

Independent private probes confirmed that the consumer metrics listener was unreachable during the outage.

A controlled short URL had an analytics baseline of 5.

Exactly five redirects were then accepted while the consumer was absent:

```text
5 / 5 redirects → HTTP 302
analytics count  → remained at 5 during outage
```

After the consumer was restored:

```text
analytics count → 10
```

The first catch-up check observed the complete five-event increase.

This is evidence that accepted Kafka events remained in the backlog and were processed after consumer recovery.

It is not a global exactly-once guarantee.

## Telemetry outage isolation

The API and consumer OTLP endpoint was temporarily changed to an unreachable private endpoint.

While telemetry export was unavailable:

```text
API liveness      → 200
API readiness     → 200
frontend          → 200
authenticated me  → 200
redirect          → 302
```

Analytics also advanced during the telemetry failure window, proving that the consumer continued processing.

The original OTLP configuration was restored and both application services returned to their normal telemetry path.

This establishes fail-open application behavior for an unreachable telemetry destination.

It does not prove telemetry-delivery durability; telemetry generated during an exporter outage may be lost.

## Redis outage and recovery

The API's Redis endpoint was temporarily changed to an unreachable private endpoint.

Startup logs explicitly showed:

```text
redis unavailable at startup; continuing without cache
```

During the Redis outage:

```text
API liveness       → 200
API readiness      → 200
frontend           → 200
authenticated me   → 200
redirect           → 302
analytics advanced → yes
```

A URL-creation request with an invalid scheme returned the normal validation error after passing the unavailable Redis-backed create limiter.

That provides runtime evidence that the distributed URL-creation limiter fails open when Redis is unavailable.

The original TLS Redis configuration was then restored and the API returned to a successful deployment state.

This production test supports these claims:

```text
Redis is non-critical to API startup.
Redirects can fall back to PostgreSQL.
The Redis-backed create limiter fails open.
Kafka analytics can continue while Redis is unavailable.
```

It does not mean every authenticated path is Redis-backed; sessions are stored in PostgreSQL.

## PostgreSQL production fault policy

PostgreSQL was not intentionally broken in production.

It is the authoritative database and a critical dependency for authentication, URL writes, uncached redirects, and analytics persistence.

The stronger PostgreSQL failure experiments remain the controlled Phase 12 local resilience tests.

Production documentation therefore does not claim that a destructive database-outage experiment was performed against Neon.

## Deployment automation validation

ShortScale uses provider-native deployments from GitHub.

The separate `Deployment Verification` workflow waits for the exact commit's provider status contexts:

```text
celebrated-flow - shortscale-api
celebrated-flow - shortscale-analytics-consumer
celebrated-flow - shortscale-observability
Vercel
```

It then runs production smoke checks.

Initial automated verification:

```text
commit adf4ba4
CI #47                    SUCCESS
Deployment Verification  #1 SUCCESS
production smoke          PASS
```

Phase 15 documentation verification:

```text
commit 91c1912
CI #48                    SUCCESS
Deployment Verification  #2 SUCCESS
production smoke          PASS
```

README deployment update verification:

```text
commit 4db6584
CI #49                    SUCCESS
Deployment Verification  #3 SUCCESS
production smoke          PASS
```

Phase 15 lock commit:

```text
commit 29e5c34
CI #50                    SUCCESS
Deployment Verification  #4 SUCCESS
provider statuses         SUCCESS
production smoke          PASS
```

## Final Phase 15 repository validation

Before the lock commit, the repository passed:

```text
Go tests
Go build
Go race detector
Docker Compose validation
frontend tests
frontend lint
frontend typecheck
frontend production build
production npm audit
sqlc determinism
git diff --check
```

Frontend test result:

```text
5 test files
18 tests
18 passed
```

Production npm audit:

```text
0 vulnerabilities
```

The worktree was clean and `HEAD == origin/main`.

## Final production smoke contract

The release smoke suite checks:

| Check | Expected |
| --- | --- |
| API `/health/live` | `200` |
| API `/health/ready` | `200` |
| Frontend `/` | `200` |
| Public API `/metrics` | `404` |
| Unauthenticated `/api/v1/auth/me` | `401` |

These checks are intentionally small and safe enough to run after ordinary deployments.

## Claims Phase 15 does not make

Production validation does not establish:

- multi-region API failover
- multi-primary PostgreSQL
- zero-downtime database failover
- global exactly-once analytics processing
- lossless API-side analytics under every Kafka failure
- unlimited managed-provider capacity
- a maximum sustainable production RPS
- absence of all memory leaks from short resource samples
- production behavior under a deliberately induced Neon outage

## Related documentation

- [Production deployment and operations](production-deployment.md)
- [Authentication, sessions and ownership](authentication.md)
- [Production hardening and CI](production-hardening.md)
- [Observability](observability.md)
- [Failure engineering and resilience](resilience.md)
- [Database scaling](database-scaling.md)
