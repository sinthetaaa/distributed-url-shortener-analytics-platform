# Distributed URL Shortener & Analytics Platform

**ShortScale** is a backend engineering project that progressively builds a production-style distributed URL shortening and analytics system.

The project explores backend engineering and distributed-systems concepts through implementation, measurement, failure testing, and architectural evolution rather than starting with unnecessary infrastructure.

## Engineering Approach

**Build → Validate → Measure → Identify Bottlenecks → Evolve → Measure Again**

The system began with the smallest useful architecture:

```text
Client → Go API → PostgreSQL
```

It has since evolved to include Redis caching, horizontal API scaling behind Nginx, distributed rate limiting, Kafka-based asynchronous redirect analytics, Prometheus metrics, Grafana dashboards, distributed OpenTelemetry tracing, and measured resilience behavior under infrastructure and process failures.

## Current Capabilities

- create and persist short URLs
- redirect short URLs to their original destinations
- Redis-backed redirect caching with PostgreSQL fallback
- cache failure isolation, circuit breaking, and singleflight request coalescing
- horizontal API scaling behind Nginx
- distributed rate limiting
- asynchronous redirect analytics through Kafka
- analytics aggregation API
- bounded local analytics queues
- Kafka consumer lag and persistence observability
- transient Kafka polling retries with bounded exponential backoff
- transient analytics persistence retries without prematurely committing Kafka offsets
- Prometheus metrics and a provisioned Grafana operations dashboard
- distributed tracing across HTTP, Redis/PostgreSQL resolution, Kafka, analytics consumption, and PostgreSQL persistence
- liveness and PostgreSQL-backed readiness endpoints
- graceful API and analytics-consumer shutdown behavior
- explicit container shutdown grace periods
- runtime failure, recovery, and load experiments
- automated Go tests and regression coverage

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
```

Each major stage is validated with tests, runtime experiments, measured behavior, and failure analysis before the next architectural change.

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

**Phase 12 — Failure Engineering & Resilience: complete**

Completed through Phase 12:

- Redis outage containment through PostgreSQL fallback and circuit breaking
- API replica crash recovery behind Nginx
- bounded analytics degradation during Kafka failure
- Kafka poll retry recovery without consumer termination
- durable analytics backlog during consumer crashes
- PostgreSQL outage semantics for cached and uncached traffic
- persistence retries that preserve uncommitted Kafka records
- zero-loss Kafka backlog recovery in tested consumer and PostgreSQL outage scenarios
- graceful API SIGTERM with asynchronous analytics drain
- graceful consumer SIGTERM with uncommitted Kafka work preserved
- explicit Docker Compose shutdown grace periods
- Prometheus, Grafana, OpenTelemetry Collector, and Tempo observability across these behaviors

**Next:** Phase 13 — Database Scaling & Distributed-System Analysis.

## Documentation

Detailed architecture, engineering decisions, benchmarks, observability, and failure experiments are maintained under [`docs/`](docs/).

- [Failure engineering, resilience matrix, and Phase 12 experiments](docs/resilience.md)
- [Observability architecture, metrics, tracing, and Phase 11 experiments](docs/observability.md)
- [Architecture notes](docs/architecture/)
- [Engineering decisions](docs/decisions/)
- [Performance experiments](docs/performance/)

The final portfolio-oriented README and interview package are planned for the final project phase after deployment and the final benchmark report.
