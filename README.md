# Distributed URL Shortener & Analytics Platform

**ShortScale** is a production-deployed backend engineering project that progressively builds a distributed URL shortening and analytics system.

The project explores backend engineering and distributed-systems concepts through implementation, measurement, failure testing, and architectural evolution rather than starting with unnecessary infrastructure.

**Live application:** https://sscale.vercel.app

## Engineering Approach

**Build → Validate → Measure → Identify Bottlenecks → Evolve → Measure Again**

The system began with the smallest useful architecture:

```text
Client → Go API → PostgreSQL
```

It has since evolved to include Redis caching, horizontal API scaling, distributed rate limiting, Kafka-based asynchronous redirect analytics, pre-aggregated analytics, Prometheus metrics, distributed OpenTelemetry tracing, production failure testing, evidence-based database scaling decisions, hardened CI, and automated post-deployment verification.

## Current Capabilities

- create and persist short URLs
- redirect short URLs to their original destinations
- authenticated user accounts and server-side sessions
- Redis-backed redirect caching with PostgreSQL fallback
- cache failure isolation, circuit breaking, and singleflight request coalescing
- distributed rate limiting
- asynchronous redirect analytics through Kafka
- bounded local analytics queues
- raw redirect-event persistence with global event-id idempotency
- atomic daily analytics pre-aggregation
- optimized lifetime analytics summaries
- authenticated analytics aggregation API
- Kafka consumer lag and persistence observability
- transient Kafka polling retries with bounded exponential backoff
- transient analytics persistence retries without prematurely committing Kafka offsets
- Prometheus metrics and Grafana Cloud observability
- distributed tracing across HTTP, Redis/PostgreSQL resolution, Kafka, analytics consumption, and PostgreSQL persistence
- liveness and PostgreSQL-backed readiness endpoints
- graceful API and analytics-consumer shutdown behavior
- explicit container shutdown grace periods
- measured failure, recovery, load, database-growth, partitioning, and sharding experiments
- automated Go tests and regression coverage
- ten-job GitHub Actions CI pipeline
- deterministic sqlc and migration round-trip validation
- reachable dependency and production-image vulnerability scanning
- repository secret and misconfiguration scanning
- non-root production containers
- CI-enforced application runtime-hardening invariants
- production deployment on Railway and Vercel
- managed Neon PostgreSQL, Upstash Redis, and Aiven Kafka
- private Railway-to-Alloy telemetry flow into Grafana Cloud
- automated post-deployment verification for the exact deployed commit
- production smoke tests for health, readiness, frontend availability, metrics isolation, and unauthenticated access behavior

## Production

### Live application

```text
https://sscale.vercel.app
```

### Production topology

```text
Users
  |
  v
Vercel frontend
  |
  | same-origin API proxy
  v
Railway API
  |
  +-------------------+
  |                   |
  v                   v
Upstash Redis       Aiven Kafka
                        |
                        v
               Railway analytics consumer
                        |
                        v
                  Neon PostgreSQL

API + consumer
      |
      v
Railway Grafana Alloy
      |
      v
Grafana Cloud
```

Production services:

| Component | Provider | Exposure |
| --- | --- | --- |
| Frontend | Vercel | Public |
| API | Railway | Public |
| Analytics consumer | Railway | Private |
| Observability collector | Railway | Private |
| PostgreSQL | Neon | Managed |
| Redis | Upstash | Managed |
| Kafka | Aiven | Managed |
| Metrics and traces | Grafana Cloud | Managed |

The analytics consumer and observability collector do not expose public application domains.

See [Production Deployment & Operations](docs/production-deployment.md) for the deployment topology, environment-variable inventory, verification flow, rollback strategy, and operational runbook.

## Deployment Verification

Railway and Vercel deploy natively from GitHub.

GitHub Actions does not duplicate those deployment mechanisms. After the main CI workflow succeeds, the `Deployment Verification` workflow waits for the platform deployment statuses for the exact commit:

```text
celebrated-flow - shortscale-api
celebrated-flow - shortscale-analytics-consumer
celebrated-flow - shortscale-observability
Vercel
```

Once all four report success, production smoke checks verify:

| Check | Expected |
| --- | --- |
| API `/health/live` | `200` |
| API `/health/ready` | `200` |
| Frontend `/` | `200` |
| Public API `/metrics` | `404` |
| Unauthenticated `/api/v1/auth/me` | `401` |

The first automated production-verification chain was validated successfully on commit `adf4ba4`.

## Current Stack

**Backend:** Go · PostgreSQL · Redis · Kafka · sqlc · Goose

**Frontend:** Next.js · React · TypeScript

**Infrastructure:** Docker · Railway · Vercel · Neon · Upstash · Aiven

**Observability:** Prometheus · Grafana Alloy · Grafana Cloud · OpenTelemetry

**CI / Security:** GitHub Actions · golangci-lint · govulncheck · Trivy

Technologies are introduced only when the architecture reaches a problem that justifies them.

## API

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/auth/register` | Create an account |
| `POST` | `/api/v1/auth/login` | Create an authenticated session |
| `GET` | `/api/v1/auth/me` | Read the current session |
| `POST` | `/api/v1/auth/logout` | Revoke the current session |
| `POST` | `/api/v1/urls` | Create a short URL owned by the current user |
| `GET` | `/api/v1/urls` | List URLs owned by the current user |
| `GET` | `/{shortCode}` | Redirect to the original URL |
| `GET` | `/api/v1/urls/{shortCode}/analytics` | Query redirect analytics |
| `GET` | `/health/live` | Liveness check |
| `GET` | `/health/ready` | PostgreSQL-backed readiness check |

Prometheus metrics are served on a separate private metrics listener in production rather than through the public API listener.

## Architecture Evolution

```text
V1: Go API → PostgreSQL

V2: Go API → Redis → PostgreSQL

V3: Nginx → Multiple Go API Replicas → Redis → PostgreSQL

V4: Redirect → Local Async Queue → Kafka → Analytics Consumer → PostgreSQL

V5: API + Consumer → Prometheus → Grafana
                   → OpenTelemetry → distributed traces

V6: Failure-tested runtime with circuit breaking, bounded degradation,
    durable Kafka recovery, dependency retries, and graceful shutdown

V7: Raw analytics events + compact daily rollups
    → hot analytics reads no longer repeatedly scan raw event history

V8: Production deployment
    → Vercel frontend
    → Railway API + consumer + Alloy
    → Neon PostgreSQL + Upstash Redis + Aiven Kafka
    → Grafana Cloud
    → post-CI deployment verification + production smoke checks
```

Each major stage is validated with tests, runtime experiments, measured behavior, and failure analysis before the next architectural change.

## Analytics Storage Model

ShortScale retains raw redirect events while maintaining a compact daily read model.

```text
Kafka redirect event
        ↓
Analytics consumer
        ↓
Atomic PostgreSQL statement
        ├─ insert raw redirect event
        └─ increment daily rollup only for a newly inserted event
```

This preserves raw history and event-level idempotency while making common analytics reads inexpensive.

The API uses:

```text
daily/window counts
→ redirect_daily_counts

lifetime total
→ SUM(daily rollups)

first/last redirect
→ existing (short_code, occurred_at) index
```

Phase 13 benchmarks showed approximately a **2,500× reduction** for the benchmarked hot daily query after pre-aggregation and approximately a **100× reduction** for the hot lifetime-summary query after its rewrite.

## Database Scaling Decisions

ShortScale does not currently use a PostgreSQL read replica, table partitioning, or application-level sharding.

Those are intentional measured decisions rather than missing features.

```text
Read replica
→ not justified after analytics query optimization removed most read pressure

Time partitioning
→ useful for future retention
→ currently complicates global event-id idempotency

Short-code sharding
→ preserves analytics locality
→ fails under hot-key skew

Event-ID sharding
→ balances writes
→ forces per-short-code analytics to fan out across shards
```

See [Database Scaling & Distributed-System Analysis](docs/database-scaling.md) for the benchmark evidence and future trigger points.

## Failure Model

ShortScale deliberately separates dependency roles.

```text
Redis failure
→ PostgreSQL fallback
→ redirect remains available

Consumer failure
→ Kafka retains durable backlog
→ restart drains backlog

Telemetry failure
→ application keeps serving
→ telemetry export may be lost or delayed

PostgreSQL failure
→ cached redirects may remain temporarily available
→ uncached redirects and database-backed operations fail
→ readiness fails
```

Production fault testing has validated Redis fail-open behavior, consumer backlog recovery, and telemetry failure isolation. Earlier resilience testing separately validated graceful shutdown behavior.

These experiments do not establish global exactly-once delivery, multi-region failover, or unlimited production capacity.

See [Failure Engineering & Resilience](docs/resilience.md) for the detailed experiments and failure semantics.

## Production Security

Production hardening includes:

- non-root application containers
- read-only application root filesystems where enforced by runtime configuration
- dropped Linux capabilities for custom application containers
- `no-new-privileges` enforcement
- secure HttpOnly authentication cookies
- bounded request bodies and strict JSON handling
- TLS-backed production dependencies
- private metrics listeners
- no public consumer or Alloy domain
- reachable-vulnerability scanning
- repository secret and misconfiguration scanning
- production dependency auditing
- fail-open observability behavior
- no client-side authentication token storage

See [Production Hardening & CI](docs/production-hardening.md).

## Project Status

**Phase 15 — Deployment: complete**

Completed deployment milestones include:

- production frontend deployment on Vercel
- production API deployment on Railway
- production analytics consumer deployment on Railway
- production Alloy deployment on Railway
- managed Neon PostgreSQL, Upstash Redis, and Aiven Kafka integration
- Grafana Cloud production metrics and distributed tracing
- production resource measurement
- production security audit
- consumer outage and Kafka backlog recovery testing
- telemetry failure-isolation testing
- Redis outage and fallback testing
- CD/deployment verification automation
- production deployment and operations runbook

Final Phase 15 milestones:

```text
15BB — README product update              COMPLETE
15BC — Final validation                   COMPLETE
15BD — Final commit & Phase 15 lock       COMPLETE
```

Next:

```text
Phase 16 — Benchmark/report
Phase 17 — Portfolio/README/interview presentation
```

The final portfolio-oriented presentation package remains a later phase; this README update reflects the currently deployed product and production architecture.

## Documentation

Detailed architecture, engineering decisions, benchmarks, observability, deployment procedures, and failure experiments are maintained under [`docs/`](docs/).

- [Production deployment and operations runbook](docs/production-deployment.md)
- [Production validation record](docs/production-validation.md)
- [Authentication, sessions, and URL ownership](docs/authentication.md)
- [Production database migration procedure](docs/production-migrations.md)
- [Production hardening and CI](docs/production-hardening.md)
- [Database scaling benchmarks and distributed-system trade-offs](docs/database-scaling.md)
- [Failure engineering and resilience](docs/resilience.md)
- [Observability architecture, metrics, and tracing](docs/observability.md)
- [Architecture notes](docs/architecture/)
- [Engineering decisions](docs/decisions/)
- [Performance experiments](docs/performance/)
