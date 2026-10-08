# Distributed URL Shortener & Analytics Platform

**ShortScale** is a backend engineering project that progressively builds a production-style distributed URL shortening and analytics system.

The project explores backend engineering and distributed-systems concepts through implementation, measurement, failure testing, and architectural evolution rather than starting with unnecessary infrastructure.

## Engineering Approach

**Build → Validate → Measure → Identify Bottlenecks → Evolve → Measure Again**

The system began with the smallest useful architecture:

```text
Client → Go API → PostgreSQL
```

It has since evolved to include Redis caching, horizontal API scaling behind Nginx, distributed rate limiting, Kafka-based asynchronous redirect analytics, pre-aggregated analytics, Prometheus metrics, Grafana dashboards, distributed OpenTelemetry tracing, measured resilience behavior, and evidence-based database scaling decisions.

## Current Capabilities

- create and persist short URLs
- redirect short URLs to their original destinations
- Redis-backed redirect caching with PostgreSQL fallback
- cache failure isolation, circuit breaking, and singleflight request coalescing
- horizontal API scaling behind Nginx
- distributed rate limiting
- asynchronous redirect analytics through Kafka
- bounded local analytics queues
- raw redirect-event persistence with global event-id idempotency
- atomic daily analytics pre-aggregation
- optimized lifetime analytics summaries
- analytics aggregation API
- Kafka consumer lag and persistence observability
- transient Kafka polling retries with bounded exponential backoff
- transient analytics persistence retries without prematurely committing Kafka offsets
- Prometheus metrics and a provisioned Grafana operations dashboard
- distributed tracing across HTTP, Redis/PostgreSQL resolution, Kafka, analytics consumption, and PostgreSQL persistence
- liveness and PostgreSQL-backed readiness endpoints
- graceful API and analytics-consumer shutdown behavior
- explicit container shutdown grace periods
- measured failure, recovery, load, database-growth, partitioning, and sharding experiments
- automated Go tests and regression coverage
- eight-job GitHub Actions CI pipeline
- deterministic sqlc and migration round-trip validation
- reachable dependency and production-image vulnerability scanning
- repository secret and misconfiguration scanning
- non-root, read-only application containers with dropped Linux capabilities
- CI-enforced application runtime-hardening invariants

## Current Stack

**Go · PostgreSQL · Redis · Kafka · Docker · Nginx · Prometheus · Grafana · OpenTelemetry Collector · Tempo**

Technologies are introduced only when the architecture reaches a problem that justifies them.

## API

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/urls` | Create a short URL |
| `GET` | `/{shortCode}` | Redirect to the original URL |
| `GET` | `/api/v1/urls/{shortCode}/analytics` | Query redirect analytics |
| `GET` | `/health/live` | Liveness check |
| `GET` | `/health/ready` | Readiness check |
| `GET` | `/metrics` | Prometheus metrics for an API replica |

## Architecture Evolution

```text
V1: Go API → PostgreSQL

V2: Go API → Redis → PostgreSQL

V3: Nginx → Multiple Go API Replicas → Redis → PostgreSQL

V4: Redirect → Local Async Queue → Kafka → Analytics Consumer → PostgreSQL

V5: API + Consumer → Prometheus → Grafana
                   → OpenTelemetry Collector → Tempo → Grafana Explore

V6: Failure-tested runtime with circuit breaking, bounded degradation,
    durable Kafka recovery, dependency retries, and graceful shutdown

V7: Raw analytics events + compact daily rollups
    → hot analytics reads no longer repeatedly scan raw event history
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

See [Database Scaling & Distributed-System Analysis](docs/database-scaling.md) for the complete benchmark evidence and future trigger points.

## Failure Model

ShortScale deliberately separates dependency roles.

```text
Redis failure
→ PostgreSQL fallback
→ redirect remains available

Kafka failure
→ redirect remains available
→ bounded analytics degradation

Consumer failure
→ Kafka retains durable backlog
→ restart drains backlog

PostgreSQL failure
→ cached redirects remain available
→ uncached redirects and writes fail
→ readiness reports 503
→ analytics persistence retries without committing offsets
```

Failure testing has also validated graceful SIGTERM handling for both the API and analytics consumer.

See [Failure Engineering & Resilience](docs/resilience.md) for the full experimental matrix and measured results.

## Project Status

**Phase 14 — Production Hardening & CI: complete**

Production-readiness controls now include:

- golangci-lint and static-analysis enforcement
- tests, race detection, and build validation
- deterministic sqlc generation checks
- PostgreSQL migration up/down/up validation
- production container artifact validation
- govulncheck reachable-vulnerability scanning
- Trivy repository, secret, misconfiguration, and image scanning
- non-root scratch application images
- read-only application root filesystems
- all Linux capabilities dropped from custom application containers
- `no-new-privileges` enforcement
- CI regression assertions for runtime-hardening settings
- deliberate CI failure experiments proving the gates reject regressions

All eight GitHub Actions jobs pass on `main`.

See [Production Hardening & CI](docs/production-hardening.md) for the complete Phase 14 audit, security baseline, runtime validation, CI design, and failure experiments.

**Next:** Phase 15 — Deployment.

## Documentation

Detailed architecture, engineering decisions, benchmarks, observability, and failure experiments are maintained under [`docs/`](docs/).

- [Production hardening, CI, security scanning, and Phase 14 failure experiments](docs/production-hardening.md)
- [Database scaling benchmarks and distributed-system trade-offs](docs/database-scaling.md)
- [Failure engineering, resilience matrix, and Phase 12 experiments](docs/resilience.md)
- [Observability architecture, metrics, tracing, and Phase 11 experiments](docs/observability.md)
- [Architecture notes](docs/architecture/)
- [Engineering decisions](docs/decisions/)
- [Performance experiments](docs/performance/)

The final portfolio-oriented README and interview package are planned for the final project phase after deployment and the final benchmark report.
