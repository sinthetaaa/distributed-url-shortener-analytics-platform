# ShortScale Final Engineering and Architecture Report

## Purpose

This document is the final engineering and architecture report for ShortScale.

It explains how the system evolved from a minimal URL shortener into a deployed distributed backend, which measurements justified each architectural change, which guarantees the final system provides, and where the measured scaling boundaries remain.

The report is intentionally evidence-driven.

ShortScale does not claim production capacity from local benchmarks. It documents measured behavior, explicit failure semantics, and the architectural trade-offs that were validated during development.

## Executive Summary

ShortScale began as:

```text
Client
  |
  v
Go API
  |
  v
PostgreSQL
```

The architecture was expanded only after a measured problem justified additional complexity.

The completed system includes:

- a Go API for authentication, URL creation, redirect resolution, and analytics reads
- PostgreSQL as the durable source of truth
- Redis for redirect caching and distributed rate-limit state
- Nginx for local multi-replica routing and failure experiments
- a bounded non-blocking analytics queue
- Kafka for durable asynchronous redirect-event transport
- a separate analytics consumer
- idempotent raw redirect-event persistence
- atomic daily analytics pre-aggregation
- optimized lifetime analytics summaries
- Prometheus metrics and distributed OpenTelemetry tracing
- production deployment through Vercel, Railway, Neon, Upstash, Aiven, and Grafana Cloud
- hardened CI, container security controls, automated deployment verification, and production smoke tests

The central engineering pattern was:

```text
Build
  ->
Validate
  ->
Measure
  ->
Identify bottleneck
  ->
Evolve architecture
  ->
Measure again
```

The strongest final application-level bottleneck is the API-side asynchronous analytics producer path under high redirect pressure.

At the tested 3,000 RPS FINAL-3 profile, all 90,001 executed redirects were accepted into the analytics pipeline and persisted.

At the tested approximately 10,000 RPS FINAL-3 profile:

```text
executed redirects: 299,749
enqueued:             49,972
dropped:             249,777
analytics drop rate: 83.3287%
```

The enqueue boundary reconciled exactly:

```text
299,749 = 49,972 + 249,777
```

Every accepted event then reconciled through publish, consume, raw persistence, and daily-rollup persistence.

This is an intentional availability-first design: redirect correctness is prioritized over analytics completeness when the producer queue saturates.

## Final Product Architecture

### Production topology

```text
Users
  |
  v
Vercel frontend
  |
  | same-origin API proxy
  v
Railway API ------------------------> Neon PostgreSQL
  |                                     ^ durable source of truth
  |                                     |
  +--> Upstash Redis                    |
  |    redirect cache                   |
  |    distributed rate-limit state     |
  |                                     |
  +--> Aiven Kafka                      |
          |                             |
          v                             |
  Railway analytics consumer -----------+
          |
          +--> raw redirect events
          +--> daily analytics rollups

API + consumer
      |
      v
Railway Grafana Alloy
      |
      v
Grafana Cloud
```

Public application services:

```text
frontend -> Vercel
API      -> Railway
```

Private application services:

```text
analytics consumer
observability collector
```

Managed dependencies:

```text
PostgreSQL -> Neon
Redis      -> Upstash
Kafka      -> Aiven
telemetry  -> Grafana Cloud
```

### Local integrated topology

The final benchmark used the completed local Compose stack:

```text
k6 on macOS host
      |
      v
Nginx :18080
      |
      v
1 ShortScale API replica
      |
      +------> PostgreSQL
      |
      +------> Redis
      |
      +------> Kafka
                   |
                   v
          1 analytics consumer
                   |
                   v
              PostgreSQL

API + consumer
      |
      v
OpenTelemetry collector
      |
      +------> Tempo

Prometheus scrapes API and consumer metrics
Grafana remains part of the integrated local stack
```

The final benchmark intentionally kept observability enabled because observability is part of the completed architecture.

## Core Data and Request Paths

### URL creation

Authenticated URL creation follows the durable write path:

```text
authenticated client
      |
      v
Go API
      |
      +--> distributed Redis rate limiter
      |
      v
validate request
      |
      v
PostgreSQL INSERT
      |
      v
short URL response
```

PostgreSQL is authoritative for URL ownership and URL records.

The Redis-backed rate limiter is a protection mechanism, not a data source.

### Redirect resolution

The redirect path is optimized for low latency while preserving a durable fallback:

```text
GET /{shortCode}
      |
      v
Redis GET
   |
   +-- hit --> HTTP 302
   |
   +-- miss --> PostgreSQL lookup
                  |
                  v
              Redis SET
                  |
                  v
               HTTP 302
```

PostgreSQL remains the source of truth.

Redis can fail without corrupting redirect correctness because cache failures fall back to PostgreSQL.

### Asynchronous analytics

Redirect analytics is deliberately outside the synchronous redirect completion path:

```text
redirect request
      |
      +------------------------> HTTP 302
      |
      v
non-blocking bounded queue
      |
      v
background publisher
      |
      v
Kafka
      |
      v
analytics consumer
      |
      v
atomic persistence
      |
      +--> redirect_events
      |
      +--> redirect_daily_counts
```

The queue is bounded to protect API memory.

If the producer cannot drain quickly enough, analytics events are dropped instead of forcing redirect requests to wait.

### Analytics reads

The original analytics API derived daily and lifetime values from raw event history.

The final read path separates historical event retention from common read operations:

```text
window/daily counts
  -> redirect_daily_counts

lifetime total
  -> compact rollup-derived summary path

first/last redirect
  -> indexed raw-event metadata
```

This change removed the strong growth in request latency that had been observed as analytics windows expanded.

## Architecture Evolution

### V1 — PostgreSQL-only URL shortener

The first useful implementation used:

```text
Client -> Go API -> PostgreSQL
```

This established correctness, durable persistence, short-code resolution, and a baseline before any cache was introduced.

The PostgreSQL-only warm local redirect benchmark sustained 10,000 offered RPS for 60 seconds with:

```text
completed requests: 600,001
actual RPS:         9,999.84
median:             0.163 ms
p95:                0.374 ms
p99:                1.23 ms
HTTP failures:      0%
dropped iterations: 0
```

At higher offered rates, tail latency and dropped scheduled iterations increased.

At 20,000 offered RPS:

```text
p95:                8.82 ms
p99:                23.17 ms
dropped iterations: 1,518
```

This baseline was important because it prevented Redis from being added by convention alone.

### V2 — Redis cache-aside redirect acceleration

Redis was introduced only after the PostgreSQL path had been measured.

The historical aligned 20,000 RPS comparison measured:

```text
PostgreSQL-only:
p95 = 8.82 ms
p99 = 23.17 ms

Redis:
p95 = 0.553 ms
p99 = 1.53 ms
```

Warm Redis benchmark redirects were cache hits, and the monitored 10,000 RPS Redis run reduced PostgreSQL CPU to approximately 0-1%.

The architectural conclusion was not merely that Redis reduced median latency.

Its larger value was:

- lower tail latency under load
- reduced repeated database reads for hot short codes
- continued use of PostgreSQL as the durable source of truth
- graceful fallback when Redis is unavailable

### V3 — Horizontal API replication

ShortScale then moved to multiple stateless API replicas behind Nginx.

A 1,500-request distribution experiment measured:

```text
replica 1: 501
replica 2: 499
replica 3: 500
```

After Nginx failure-policy tuning, abruptly killing one replica during a 4,500-request test produced:

```text
correct redirects: 4,500
HTTP failures:     0
```

The strongest measured benefit of replication was availability.

Same-host throughput did not scale monotonically because multiple API processes, Nginx, Redis, PostgreSQL, Docker, and the load generator competed for one physical machine.

ShortScale therefore does not claim linear horizontal throughput scaling from those experiments.

### V4 — Shared distributed rate limiting

The initial token bucket was process-local.

With one replica, a burst capacity of five behaved correctly:

```text
5 allowed
1 limited
```

With three independent replicas, the same logical policy became:

```text
15 allowed
3 limited
```

The effective burst multiplied with replica count.

Moving rate-limit state to Redis and using an atomic Lua operation restored the service-wide invariant:

```text
18 distributed attempts
5 allowed
13 limited
```

A separate 60-goroutine concurrency test allowed exactly five operations from a five-token bucket.

The local middleware microbenchmark measured approximately:

```text
process-local limiter: ~132 ns/op
Redis + Lua limiter:   ~104.486 us/op
```

The final design accepts a small synchronous Redis cost on the URL-creation write path in exchange for cross-replica correctness.

The limiter intentionally fails open if Redis is unavailable:

```text
Redis unavailable
  ->
URL creation remains available
  ->
distributed abuse protection is temporarily absent
```

### V5 — Kafka-based asynchronous redirect analytics

Redirect analytics was moved off the synchronous redirect path.

The design uses:

```text
HTTP redirect
  ->
bounded local queue
  ->
background publisher
  ->
Kafka
  ->
analytics consumer
  ->
PostgreSQL
```

Kafka is not synchronously required for a redirect response.

Failure experiments established that Kafka outages did not break redirect correctness.

The API-side analytics policy is best effort.

Kafka consumer processing is at least once.

Database side effects are idempotent through event-ID uniqueness and conflict-safe inserts.

Global exactly-once processing is not claimed.

### V6 — Resilience and failure isolation

The runtime was hardened around dependency roles rather than treating every dependency as equally critical.

The final failure model is:

```text
Redis failure
  -> PostgreSQL redirect fallback
  -> rate limiter fails open where configured

Kafka failure
  -> redirect path remains available
  -> analytics publication may be lost or delayed according to queue behavior

analytics consumer failure
  -> Kafka retains durable backlog
  -> consumer restart drains backlog

telemetry failure
  -> application continues serving
  -> metrics/traces may be lost or delayed

PostgreSQL failure
  -> already-cached redirects may remain temporarily available
  -> uncached redirects and database-backed operations fail
  -> readiness fails
```

Graceful shutdown, bounded retry behavior, circuit breaking, and dependency recovery were tested separately during the project.

### V7 — Pre-aggregated analytics

The original raw-event analytics benchmark showed a clear read-scaling problem.

With 100,000 redirect events, historical API latency grew as the requested window expanded:

```text
1 day / 50 RPS:     median 7.26 ms
7 days / 50 RPS:    median 7.80 ms
30 days / 50 RPS:   median 9.77 ms
90 days / 50 RPS:   median 17.46 ms
90 days / 200 RPS:  median 19.56 ms
```

Later database experiments showed approximately:

```text
~2,500x reduction
for the benchmarked hot daily query after pre-aggregation

~100x reduction
for the hot lifetime-summary query after its rewrite
```

The consumer was evolved to preserve raw events while also maintaining compact daily aggregates.

This avoided prematurely adding read replicas, sharding, or partitioning to solve a query-shape problem.

### V8 — Production deployment and verification

The completed application was deployed using:

```text
frontend              -> Vercel
API                   -> Railway
analytics consumer    -> Railway
observability service -> Railway
PostgreSQL            -> Neon
Redis                 -> Upstash
Kafka                 -> Aiven
metrics/traces        -> Grafana Cloud
```

Railway and Vercel deploy natively from GitHub.

GitHub Actions then performs post-deployment verification for the exact commit after provider deployment statuses succeed.

Production smoke checks include:

```text
API /health/live                 -> 200
API /health/ready                -> 200
frontend /                       -> 200
public API /metrics              -> 404
unauthenticated /api/v1/auth/me  -> 401
```

Capacity benchmarking is deliberately not run against the public production deployment.

## Data Consistency and Delivery Semantics

### Source of truth

PostgreSQL is the durable source of truth for:

- users
- sessions
- URL ownership
- short URL records
- raw redirect events
- daily analytics aggregates

Redis is not treated as durable business state.

### Redirect correctness

Redirect correctness depends on resolving a valid short code to its original URL.

A Redis hit can satisfy the read.

On a cache miss or recoverable cache failure, PostgreSQL provides the durable fallback.

### Analytics delivery

The analytics path separates API-side acceptance from downstream durability.

The API enqueue boundary is:

```text
executed redirect event
  =
enqueued
  +
dropped
```

Once an event crosses that boundary successfully, the final controlled benchmark observed exact reconciliation through:

```text
enqueue
  ->
Kafka publish
  ->
consumer processing
  ->
raw insert
  ->
daily rollup
```

The consumer-side semantics are:

```text
delivery:             at least once
database effect:      idempotent
global exactly once:  not claimed
```

### Daily aggregate atomicity

Raw-event insertion and daily-rollup maintenance are coupled so a duplicate event does not double-count the rollup.

The design preserves:

- raw event history
- event-level idempotency
- compact daily query state

## Final Benchmark Results

The final benchmark measured runtime code locked at:

```text
8102b79edaf7e6a40b1ad5bd0b88a5d7474dbaab
```

Later benchmark and documentation commits added methodology, tooling, evidence, and reporting without changing the application runtime under test.

### FINAL-1 — Completed redirect path

R1 and R2 were clean in the final integrated local topology:

```text
R1 — 500 RPS x 30 s x3
median actual RPS: 500.011
median latency:    0.371 ms
p95:               0.765 ms
p99:               1.410 ms
HTTP failures:     0
check failures:    0
dropped:           0
```

```text
R2 — 3,000 RPS x 30 s x3
median actual RPS: 2,999.935
median latency:    0.374 ms
p95:               0.900 ms
p99:               1.830 ms
HTTP failures:     0
check failures:    0
dropped:           0
```

R3 exposed a local high-load saturation region:

```text
R3 — 10,000 offered RPS x 60 s x3
completed requests: 1,789,490
median sample RPS:  9,948.772
median latency:     1.661 ms
p95:                13.200 ms
p99:                42.129 ms
HTTP failures:      0
check failures:     0
k6 dropped:         10,513 total
```

The third sample reached the configured 800-VU ceiling.

This is local integrated-runtime/load-generator saturation evidence, not a maximum production-RPS claim and not proof that one specific service caused the boundary.

### FINAL-2 — Optimized analytics read path

The controlled dataset contained:

```text
100,000 raw redirect events
100,000 redirects represented by daily rollups
0 daily mismatches
```

The final authenticated endpoint measured:

| Window | Offered rate | Median | P95 | P99 |
| --- | ---: | ---: | ---: | ---: |
| 1 day | 50 RPS | 3.411 ms | 4.556 ms | 4.875 ms |
| 7 days | 50 RPS | 3.408 ms | 4.290 ms | 4.569 ms |
| 30 days | 50 RPS | 3.444 ms | 4.233 ms | 4.790 ms |
| 90 days | 50 RPS | 3.569 ms | 4.386 ms | 4.702 ms |
| 90 days | 200 RPS | 2.228 ms | 2.978 ms | 3.329 ms |

All required samples completed with zero HTTP/check failures and zero dropped iterations.

The additional 90-day, 200 RPS, 30-second stability run measured:

```text
completed requests: 6,001
actual RPS:         200.009
median:             2.195 ms
p95:                2.962 ms
p99:                3.232 ms
max:                16.724 ms
HTTP failures:      0
check failures:     0
dropped iterations: 0
unexpected restarts: 0
```

The final implementation removed the strong window-size latency growth seen in the historical raw-event design.

The lower latency of the 200 RPS samples is not interpreted as evidence that more load improves latency.

### FINAL-3 — Async analytics integrity

A1 at 3,000 RPS for 30 seconds was lossless at the API analytics boundary:

```text
executed redirects: 90,001
enqueued:           90,001
dropped:            0
published:          90,001
processed:          90,001
inserted:           90,001
raw-row delta:      90,001
rollup delta:       90,001
final queue depth:  0
final consumer lag: 0
```

A2 at approximately 10,000 RPS for 30 seconds exposed the producer boundary:

```text
executed redirects: 299,749
actual RPS:         9,991.172
k6 dropped:         252
HTTP failures:      0
check failures:     0

enqueued:           49,972
enqueue dropped:    249,777
analytics drop rate: 83.3287%

published:          49,972
processed:          49,972
inserted:           49,972
raw-row delta:      49,972
rollup delta:       49,972
final queue depth:  0
final consumer lag: 0
```

The result establishes that, for this tested profile, the first measured analytics-loss boundary was before Kafka rather than in consumer persistence.

## Database Scaling Decisions

ShortScale intentionally does not currently use:

- a PostgreSQL read replica
- time partitioning
- application-level sharding

These are measured non-decisions.

### Read replicas

Analytics query optimization removed the read pressure that had originally motivated consideration of a replica.

A read replica would add replication lag, routing complexity, operational cost, and consistency considerations without solving a currently measured problem.

### Time partitioning

Partitioning may become valuable for large retention windows and maintenance operations.

It was not adopted because the current scale did not require it and partitioned uniqueness complicates the global event-ID idempotency model.

### Sharding

Two major sharding strategies were evaluated conceptually.

Short-code sharding preserves per-URL analytics locality but can concentrate a hot URL on one shard.

Event-ID sharding distributes writes more evenly but makes per-short-code analytics fan out across shards.

Neither trade-off is justified by current measurements.

## Horizontal Scaling Interpretation

Horizontal replication is supported architecturally because the API is stateless with shared durable and distributed state where required.

Measured local benefits include:

- real load distribution across replicas
- successful failure tolerance after Nginx policy tuning
- continued availability after one replica disappears
- shared distributed rate-limit correctness

However:

```text
more local replicas != proven linear production throughput
```

The same-host throughput experiments were resource-coupled.

A meaningful production-capacity scaling experiment would require:

- independent application hosts
- a separate load generator
- realistic network latency
- shared-service capacity measurement
- production-like load-balancer behavior

## Observability

ShortScale treats observability as part of the architecture rather than an external debugging afterthought.

The system includes:

- Prometheus metrics
- Grafana dashboards / Grafana Cloud
- OpenTelemetry traces
- API metrics
- analytics-consumer metrics
- redirect-cache observations
- analytics enqueue/drop counters
- publish success/failure counters
- consumer processed/failure counters
- persistence counters
- queue depth
- consumer lag
- liveness and readiness endpoints

Distributed tracing covers important boundaries across HTTP, Redis/PostgreSQL resolution, Kafka publication/consumption, and persistence.

Telemetry is fail-open: exporter or collector problems do not intentionally block application traffic.

## Security and Runtime Hardening

The final production posture includes:

- non-root custom application containers
- dropped Linux capabilities where configured
- `no-new-privileges`
- read-only root filesystems where enforced by runtime configuration
- bounded request bodies
- strict JSON handling
- secure HttpOnly authentication cookies
- no client-side authentication token storage
- TLS-backed managed dependencies
- private metrics listeners
- no public analytics-consumer domain
- no public Alloy domain
- reachable dependency vulnerability scanning
- production-image vulnerability scanning
- repository secret scanning
- configuration/misconfiguration scanning
- production dependency auditing
- CI-enforced runtime-hardening invariants

Authentication uses persistent server-side sessions rather than exposing bearer tokens to browser JavaScript.

## CI and Delivery

The repository uses a multi-job GitHub Actions pipeline rather than a single build step.

Validation includes application tests, linting, database/migration checks, deterministic generated-code verification, vulnerability/security checks, and production-image checks.

Deployment itself remains provider-native:

```text
GitHub
  ->
Railway / Vercel deployments
```

After CI succeeds, deployment verification waits for provider deployment contexts associated with the exact commit and then runs production smoke tests.

This separation avoids duplicating provider deployment logic inside GitHub Actions while still verifying that the committed revision became healthy production software.

## Failure Semantics

The final architecture makes failure policy explicit.

| Dependency / boundary | Failure behavior |
| --- | --- |
| Redis redirect cache | fall back to PostgreSQL |
| Redis distributed limiter | fail open for URL creation |
| Kafka publish path | redirect remains independent; analytics may degrade |
| API analytics queue | drop analytics on saturation rather than block redirect |
| analytics consumer | Kafka backlog retained; recovery drains backlog |
| duplicate Kafka delivery | idempotent DB side effect |
| telemetry pipeline | application continues serving |
| PostgreSQL | readiness fails; uncached DB-backed work unavailable |
| one API replica | load balancer routes to surviving replicas after failure handling |

These policies intentionally distinguish availability, durability, and completeness rather than hiding them behind a generic "high availability" claim.

## Measured Bottlenecks and Scaling Boundaries

### 1. API-side analytics producer

This is the strongest current application bottleneck.

Evidence:

```text
3k RPS profile:
90,001 / 90,001 analytics events accepted and persisted

~10k RPS profile:
49,972 accepted
249,777 dropped
83.3287% drop rate
```

If future product requirements demand higher-fidelity analytics at this load, the first justified optimization target is producer throughput.

Candidate experiments include:

- producer batching
- native asynchronous Kafka production
- carefully bounded multiple publisher workers
- explicit sampling/aggregation policy if full event fidelity is unnecessary

Increasing queue capacity alone is not sufficient if publisher drain throughput remains unchanged.

### 2. Integrated redirect high-load region

FINAL-1 R3 showed materially higher tail latency and k6 dropped arrivals.

Because the complete stack and k6 shared one physical machine, this cannot be attributed to one application component.

The next correct experiment is decoupled profiling, not speculative architecture replacement.

### 3. Analytics read path

The final defined workload does not expose the optimized analytics read path as a current bottleneck.

Future work should be triggered only by measurements showing larger histories, higher concurrency, retention pressure, expensive reporting, or database resource pressure.

### 4. Kafka / consumer / persistence

All events that crossed the final A1/A2 API enqueue boundary reconciled successfully downstream.

Consumer or database scaling is therefore not the first optimization justified by the final evidence.

### 5. Cross-replica coordination

Process-local `singleflight` reduces same-key miss amplification within each process.

Across three processes, one concurrent miss group can still produce one database lookup per process.

A distributed lock is deliberately not added because the measured behavior has not justified the latency and failure complexity of global coordination.

## What the Project Demonstrates

ShortScale demonstrates more than the final set of technologies.

It demonstrates an engineering method:

### Measure before adding infrastructure

Redis was evaluated against a PostgreSQL-only baseline.

Horizontal replication was evaluated for both availability and throughput rather than assumed to improve both.

Distributed rate limiting was introduced only after process-local state became incorrect under replication.

Pre-aggregation was introduced after raw-event analytics showed measurable window-size growth.

Read replicas, partitioning, and sharding were evaluated but not adopted because measurements did not justify them.

### Make failure semantics explicit

The project distinguishes:

- correctness
- availability
- analytics completeness
- durability
- idempotency
- observability
- abuse protection

For example, a successful redirect does not imply that its analytics event was durably persisted if the best-effort producer queue was saturated.

### Preserve evidence

Benchmark scripts, methodology, summarized evidence, scaling analysis, failure experiments, and deployment validation are kept as repository documentation.

Raw transient benchmark output remains outside Git where appropriate.

### Avoid unsupported claims

The project does not turn a local benchmark into a production capacity guarantee.

It also does not label at-least-once delivery with idempotent persistence as global exactly once.

## Claims Supported by Evidence

The project can support statements such as:

- Redis materially reduced historical redirect tail latency and offloaded hot PostgreSQL reads in aligned local comparisons.
- The completed redirect path returned correct redirects for every executed request in the final fixed benchmark profiles.
- The final integrated 500 RPS and 3,000 RPS redirect profiles completed without k6 drops or HTTP/check failures.
- The final 10,000 offered-RPS redirect profile showed local saturation evidence and must not be presented as clean sustained capacity.
- Daily pre-aggregation and the lifetime-summary rewrite materially reduced analytics query cost in controlled benchmarks.
- The optimized authenticated analytics endpoint completed the final defined 100,000-event workloads with zero HTTP/check failures.
- The asynchronous pipeline preserved all analytics events at the tested 3,000 RPS FINAL-3 profile.
- At the tested approximately 10,000 RPS FINAL-3 profile, the API-side producer dropped analytics while redirects stayed correct.
- Every analytics event accepted by the API queue in FINAL-3 reconciled through Kafka, the consumer, raw persistence, and daily rollup persistence.
- Multi-replica experiments demonstrated load distribution and failure tolerance.
- Redis-backed rate limiting restored one global policy across replicas.
- Production deployment and automated post-deployment smoke verification were successfully validated.

## Claims Not Supported

The project does not establish:

- maximum production RPS
- a precise maximum local RPS
- a precise analytics-producer capacity threshold between 3,000 and 10,000 RPS
- lossless API-side analytics at arbitrary load
- global exactly-once processing
- unlimited horizontal scaling
- multi-region failover
- production autoscaling behavior
- zero-downtime behavior under every dependency failure
- universal cloud-provider performance
- absence of all memory leaks or long-duration resource issues
- production capacity equivalence with local benchmarks

## Future Evolution Triggers

Future architecture changes should remain trigger-based.

### Improve analytics producer throughput when

- analytics completeness becomes a stronger product requirement
- measured enqueue drops occur at required operating load
- producer drain rate becomes a contractual SLO concern

### Scale analytics consumers when

- accepted-event Kafka lag persists
- consumer throughput cannot recover after bursts
- persistence latency becomes the limiting stage

### Add database scaling mechanisms when

- PostgreSQL resource evidence shows sustained pressure
- retention volume changes maintenance requirements
- query concurrency or reporting workload exceeds the optimized read model

### Validate multi-host API scaling when

- capacity requirements exceed one application host
- independent-host deployment is available for controlled tests
- shared Redis/PostgreSQL/Kafka capacity can be measured separately

### Add distributed coordination when

- process-local coalescing creates measured harmful amplification
- the cost of duplicate upstream work exceeds coordination latency and failure risk

## Final Engineering Position

The final ShortScale architecture is intentionally not the most complex architecture that could be built.

It is the architecture justified by the project's measurements.

PostgreSQL remains durable truth.

Redis accelerates hot reads and carries explicitly disposable/shared coordination state.

Kafka decouples redirect availability from analytics persistence.

The analytics consumer preserves raw history while maintaining read-optimized aggregates.

The API remains stateless enough to replicate, while shared state is introduced only where process-local state would violate a service-wide invariant.

Observability, failure testing, CI hardening, and production deployment are treated as parts of the system rather than as afterthoughts.

The most important remaining application scaling boundary is the availability-first analytics producer.

The most important uncertainty in redirect capacity is the shared-host nature of the final high-load local benchmark.

The correct next evolution is therefore not "add more distributed systems components."

It is:

```text
measure the next real bottleneck
  ->
change one thing
  ->
validate correctness
  ->
measure again
```

## Final Engineering Result

This report consolidates the project into one final engineering narrative.

The report connects:

- architectural evolution
- controlled before/after measurements
- failure semantics
- data consistency guarantees
- deployment architecture
- security and observability
- final benchmark evidence
- current scaling boundaries
- intentionally deferred complexity
