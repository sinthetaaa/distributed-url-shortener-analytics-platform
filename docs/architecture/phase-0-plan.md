# Phase 0 — Project Definition & Engineering Plan

## 1. Project Identity

**Project name:** ShortScale
**Repository:** `distributed-url-shortener-analytics-platform`
**Public title:** Distributed URL Shortener & Analytics Platform

ShortScale is a backend and distributed-systems engineering project that progressively evolves a URL shortening service from a simple PostgreSQL-backed application into a scalable, observable, and resilient distributed system.

The goal is engineering depth rather than building a feature-heavy Bitly-style product clone.

---

## 2. Engineering Philosophy

ShortScale follows this development cycle:

```text
Understand
    ↓
Design
    ↓
Discuss Trade-offs
    ↓
Implement
    ↓
Test
    ↓
Measure
    ↓
Review
    ↓
Commit
    ↓
Push
```

Architectural complexity should be introduced only when a real problem or learning objective justifies it.

Performance claims must come from reproducible measurements. Benchmark numbers will never be invented.

---

## 3. Functional Scope

### Initial MVP

The first functional version will support:

- creating a short URL from a valid HTTP/HTTPS URL
- resolving a short code
- redirecting to the original URL
- persistent URL mappings in PostgreSQL
- URL validation
- structured error handling
- handling unknown short codes
- liveness checks
- readiness checks
- automated testing

### Later Phases

The architecture will progressively introduce:

- URL expiration behavior
- custom aliases
- Redis caching
- hot-key and cache-stampede handling
- horizontal scaling
- load balancing
- rate limiting
- asynchronous analytics
- Kafka
- analytics retrieval
- observability
- failure experiments
- production deployment

### Explicitly Out of Scope

The project does not initially include:

- authentication or user registration
- OAuth
- teams or workspaces
- subscriptions or payments
- QR-code generation
- link-in-bio functionality
- marketing features
- an elaborate frontend

---

## 4. Non-Functional Requirements

### Performance

Redirect resolution is the primary performance-sensitive path.

Performance will eventually be measured using:

- requests per second
- P50 latency
- P95 latency
- P99 latency
- error rate

No performance target will be claimed before measurement.

### Scalability

The application tier should eventually support horizontal scaling.

The service should not rely on process-local memory for durable application state.

### Read-Heavy Optimization

Redirect requests are expected to significantly outnumber URL creation requests.

The architecture will therefore evolve with particular attention to the redirect path.

### Availability and Graceful Degradation

The project will investigate how critical functionality behaves when dependencies fail.

Precise degradation behavior will be defined as those dependencies are introduced.

### Durability

URL mappings must survive application, container, and cache restarts.

PostgreSQL is the authoritative source of truth for URL mappings.

### Consistency

URL mapping correctness is critical.

Analytics may become eventually consistent when asynchronous processing is introduced.

### Concurrency

The implementation must behave correctly under simultaneous requests.

Go concurrency mechanisms will be introduced when they solve actual problems rather than being added artificially.

### Hot URLs

The project will explicitly investigate highly skewed traffic where a single short URL receives disproportionately high request volume.

### Observability

Operational visibility will be introduced progressively through:

- structured logging
- metrics
- dashboards
- distributed tracing

### Reproducibility

Tests, failure experiments, and performance benchmarks should be automated and reproducible whenever practical.

---

## 5. Initial Architecture

The first functional architecture is intentionally small:

```text
Client
  |
  | HTTP
  v
Go API
  |
  | pgx
  v
PostgreSQL
```

The initial Go application handles both URL creation and redirects.

The implementation should maintain lightweight conceptual separation between:

- HTTP transport concerns
- application/domain behavior
- PostgreSQL data access

Unnecessary enterprise-style layering and premature microservices should be avoided.

---

## 6. Initial Request Flows

### Create URL

```text
POST /api/v1/urls
        ↓
Router
        ↓
Handler
        ↓
Parse + Validate
        ↓
Application Logic
        ↓
Short-Code Generation
        ↓
Repository
        ↓
PostgreSQL
        ↓
201 Created
```

The short-code generation strategy is intentionally unresolved.

Before implementation, reasonable alternatives will be compared, including:

- random identifiers
- UUID-derived identifiers
- database sequence + Base62
- distributed ID approaches such as Snowflake-style IDs

The final choice will be documented as an Architecture Decision Record.

### Redirect

```text
GET /{shortCode}
        ↓
Go API
        ↓
Lookup short code
        ↓
PostgreSQL
        ↓
Original URL
        ↓
302 Redirect
```

Every redirect initially reaches PostgreSQL deliberately.

This provides a measurable baseline before Redis caching is introduced.

---

## 7. Initial PostgreSQL Data Model

The initial conceptual `urls` table contains:

| Column | Purpose |
| --- | --- |
| `id` | Internal numeric primary key |
| `short_code` | Unique public short-code identifier |
| `original_url` | Destination URL |
| `created_at` | Creation timestamp |
| `expires_at` | Optional expiration timestamp |

Initial decisions:

- `short_code` must be unique.
- `original_url` is not unique.
- Multiple short URLs may point to the same destination.
- `original_url` should use storage appropriate for URLs longer than 255 characters.
- timestamps should use timezone-aware PostgreSQL timestamps.
- analytics fields do not belong in the initial `urls` table.
- the redirect path will not synchronously increment a click counter in the URL row.

Analytics storage will be designed when the analytics architecture is introduced.

---

## 8. Initial API Contract

### Create URL

```text
POST /api/v1/urls
```

Successful creation:

```text
201 Created
```

Invalid input:

```text
400 Bad Request
```

Initially accepted URL schemes:

- HTTP
- HTTPS

### Redirect

```text
GET /{shortCode}
```

Known short code:

```text
302 Found
Location: <original URL>
```

Unknown short code:

```text
404 Not Found
```

### Liveness

```text
GET /health/live
```

Answers whether the Go application is alive and capable of serving HTTP.

### Readiness

```text
GET /health/ready
```

Answers whether the service can currently perform its required work.

PostgreSQL is an essential dependency for initial readiness.

---

## 9. Development Roadmap

### Phase 0 — Project Definition & Engineering Plan
Define scope, requirements, architecture, data model, API contract, roadmap, development environment, repository, and foundational documentation.

### Phase 1 — Go Foundation & Service Bootstrap
Establish the Go service, routing, configuration, logging foundation, health handling, graceful shutdown, and initial tests.

### Phase 2 — PostgreSQL Persistence Foundation
Introduce Docker-based PostgreSQL, pgx, connection pooling, Goose migrations, sqlc, readiness checks, and database integration tests.

### Phase 3 — Core URL Shortener MVP
Select the short-code strategy, implement URL creation and redirects, handle failures, and validate the complete PostgreSQL-backed MVP.

### Phase 4 — Baseline Performance Engineering
Establish reproducible PostgreSQL-only performance measurements with k6 and inspect relevant PostgreSQL query plans.

### Phase 5 — Redis & Distributed Caching
Introduce Redis, cache-aside behavior, TTL strategy, cache failure handling, and comparative benchmarks.

### Phase 6 — Hot Keys & Cache Failure Engineering
Investigate hot URLs, cache stampedes, cache expiry under load, request coalescing, and Redis failure scenarios.

### Phase 7 — Horizontal Scaling & Load Balancing
Run multiple stateless Go instances behind a load balancer and measure scaling behavior.

### Phase 8 — Rate Limiting & Traffic Protection
Design and implement justified distributed traffic protection.

### Phase 9 — Asynchronous Analytics with Kafka
Move analytics away from the critical redirect path using Kafka and a separate analytics worker.

### Phase 10 — Analytics API
Expose useful analytics after the analytics processing and storage architecture exists.

### Phase 11 — Observability
Introduce Prometheus metrics, Grafana dashboards, and OpenTelemetry tracing alongside structured logs.

### Phase 12 — Failure Engineering & Resilience
Test dependency failures, application-instance failures, traffic spikes, slow consumers, duplicate events, timeouts, and recovery behavior.

### Phase 13 — Database Scaling & Distributed-System Analysis
Study query plans, connection limits, replication, partitioning, sharding, consistent hashing, and production-scale database architecture.

### Phase 14 — Production Hardening & CI
Harden configuration, containers, timeouts, graceful shutdown, security, CI, linting, testing, and deployment preparation.

### Phase 15 — Deployment
Deploy ShortScale and verify the actual public service and required infrastructure.

### Phase 16 — Final Benchmark & Architecture Report
Run controlled final benchmarks and compare the major architectural stages.

### Phase 17 — Portfolio, README & Interview Package
Complete repository documentation, architecture diagrams, measured results, resume bullets, and technical interview preparation.

---

## 10. Expected Architecture Evolution

### V1 — PostgreSQL Baseline

```text
Client → Go API → PostgreSQL
```

### V2 — Cached Redirects

```text
Client → Go API → Redis
                    ↓ cache miss
                 PostgreSQL
```

### V3 — Horizontally Scaled Application Tier

```text
                 ┌── Go API
Client → LB ─────┼── Go API
                 └── Go API
                       ↓
                     Redis
                       ↓
                  PostgreSQL
```

### V4 — Asynchronous Analytics

```text
Redirect Request
       ↓
Application Tier
       │
       ├── Resolve URL → Redirect
       │
       └── Analytics Event
                  ↓
                Kafka
                  ↓
          Analytics Worker
                  ↓
          Analytics Storage
```

Each major transition should be justified, tested, and measured.

---

## 11. Architecture Decision Records

Significant architectural decisions will be recorded under:

```text
docs/decisions/
```

Expected topics include:

- short-code generation strategy
- caching strategy
- analytics processing architecture
- Kafka partitioning strategy
- hot-key mitigation strategy

Each ADR should document the context, alternatives, decision, reasoning, trade-offs, and consequences.

ADRs should only be created when the corresponding decision is actually made.

---

## 12. Git Workflow

Development should use small, logical commits.

Before meaningful commits:

1. inspect the changes
2. run relevant tests, builds, or linting
3. review the diff
4. review Git status
5. create a descriptive Conventional Commit-style commit
6. push
7. verify the remote state

The `main` branch should remain stable whenever practical.

---

## 13. Phase 0 Completion Criteria

Phase 0 is complete when:

- project identity and scope are established
- non-functional requirements are documented
- MVP boundaries are established
- initial architecture is documented
- initial data model is documented
- initial API contract is documented
- the complete development roadmap exists
- local development prerequisites are verified
- the local Git repository exists
- the GitHub repository exists
- Phase 0 documentation is reviewed, committed, and pushed

No application code is required for Phase 0.
