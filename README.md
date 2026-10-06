# Distributed URL Shortener & Analytics Platform

**ShortScale** is a backend engineering project that progressively builds a production-style distributed URL shortening and analytics system.

The project explores backend engineering and distributed-systems concepts through implementation, measurement, failure testing, and architectural evolution rather than starting with unnecessary infrastructure.

## Engineering Approach

**Build → Validate → Measure → Identify Bottlenecks → Evolve → Measure Again**

The system begins with the smallest useful architecture:

```text
Client → Go API → PostgreSQL
```

Later phases progressively introduce Redis caching, horizontal scaling, load balancing, Kafka-based asynchronous analytics, observability, failure engineering, and production deployment.

## Initial Scope

The first version will support:

- creating short URLs
- redirecting short URLs to their original destinations
- persistent URL mappings in PostgreSQL
- URL validation and structured error handling
- liveness and readiness endpoints
- automated testing

## Planned Stack

**Go · PostgreSQL · Redis · Kafka · Docker · Nginx · Prometheus · Grafana · OpenTelemetry · k6**

Technologies are introduced only when the architecture reaches a problem that justifies them.

## Initial API

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/urls` | Create a short URL |
| `GET` | `/{shortCode}` | Redirect to the original URL |
| `GET` | `/health/live` | Liveness check |
| `GET` | `/health/ready` | Readiness check |

## Architecture Evolution

```text
V1: Go → PostgreSQL
V2: Go → Redis → PostgreSQL
V3: Load Balancer → Multiple Go Instances → Redis → PostgreSQL
V4: Asynchronous Analytics → Kafka → Analytics Worker
```

Each stage will be benchmarked and documented before the next major architectural change.

## Project Status

**Phase 0 — Project Definition & Engineering Plan**

Application implementation has not started yet.

## Documentation

Detailed architecture, engineering decisions, benchmarks, and experiments are maintained under [`docs/`](docs/).
