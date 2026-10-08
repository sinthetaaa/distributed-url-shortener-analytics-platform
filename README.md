# Distributed URL Shortener & Analytics Platform

**ShortScale** is a backend engineering project that progressively builds a production-style distributed URL shortening and analytics system.

The project explores backend engineering and distributed-systems concepts through implementation, measurement, failure testing, and architectural evolution rather than starting with unnecessary infrastructure.

## Engineering Approach

**Build → Validate → Measure → Identify Bottlenecks → Evolve → Measure Again**

The system began with the smallest useful architecture:

```text
Client → Go API → PostgreSQL
```

It has since evolved to include Redis caching, horizontal API scaling behind Nginx, distributed rate limiting, Kafka-based asynchronous redirect analytics, Prometheus metrics, Grafana dashboards, and OpenTelemetry traces exported through an OTel Collector to Tempo.

## Current Capabilities

- create and persist short URLs
- redirect short URLs to their original destinations
- Redis-backed redirect caching with PostgreSQL fallback
- cache failure isolation, circuit breaking, and singleflight request coalescing
- horizontal API scaling behind Nginx
- distributed rate limiting
- asynchronous redirect analytics through Kafka
- analytics aggregation API
- consumer lag and persistence observability
- Prometheus metrics and a provisioned Grafana operations dashboard
- distributed tracing across HTTP, Redis/PostgreSQL resolution, Kafka, the analytics consumer, and PostgreSQL persistence
- liveness and readiness endpoints
- automated tests and runtime failure/load experiments

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
```

Each major stage is validated with tests, runtime experiments, and measured behavior before the next architectural change.

## Project Status

**Phase 11 — Observability: complete**

Completed through Phase 11:

- Prometheus metrics for HTTP, cache, rate limiting, async analytics, consumer processing, persistence, and Kafka lag
- direct scraping of horizontally scaled API replicas
- provisioned Grafana operations dashboard
- OpenTelemetry Collector and Tempo trace pipeline
- bounded HTTP and domain span names
- W3C trace-context propagation across Kafka
- cache miss → PostgreSQL fallback → Redis refill tracing
- cache-hit tracing
- telemetry-backend failure isolation experiment
- healthy-stack load and cross-component metric reconciliation

**Next:** Phase 12 — Failure Engineering & Resilience.

## Documentation

Detailed architecture, engineering decisions, benchmarks, and experiments are maintained under [`docs/`](docs/).

- [Observability architecture, metrics, tracing, and Phase 11 experiments](docs/observability.md)
- [Architecture notes](docs/architecture/)
- [Engineering decisions](docs/decisions/)
- [Performance experiments](docs/performance/)

The final portfolio-oriented README and interview package are planned for the final project phase after deployment and the final benchmark report.
