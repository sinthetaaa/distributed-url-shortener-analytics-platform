# Final Benchmark Methodology

## Purpose

This methodology defines a final, reproducible engineering benchmark for the completed ShortScale application without turning local measurements into production-capacity claims.

The application behavior under test is the runtime code present at:

```text
8102b79edaf7e6a40b1ad5bd0b88a5d7474dbaab
```

Benchmark scripts and reports may evolve independently, but application-runtime behavior must remain unchanged during the final measurement set unless a benchmark exposes a real correctness defect that requires revalidation.

The benchmark answers three questions:

1. How does the completed redirect path behave at fixed representative loads?
2. How does the optimized analytics read path behave under the same hot-key workload shape used by the earlier analytics API benchmark?
3. Under redirect load, how does the asynchronous analytics path account for enqueued, dropped, published, consumed, and persisted events?

The benchmark does **not** attempt to discover a maximum production RPS.

## Benchmark Principles

The final benchmark follows these rules:

- use fixed workloads rather than repeatedly increasing load until failure
- run on the local development machine, not against Vercel or Railway
- keep the runtime topology explicit and reproducible
- record exact commit and tool versions
- validate response correctness, not only latency
- keep redirect-path and analytics-read measurements logically separate
- record dropped k6 iterations rather than hiding them
- record asynchronous analytics drops rather than treating HTTP success as analytics completeness
- wait for the analytics queue and Kafka consumer lag to drain between relevant profiles
- distinguish local benchmark evidence from production validation
- do not claim exactly-once processing, multi-region availability, or a production capacity ceiling

## Environment

The benchmark runs on the existing macOS development machine.

At execution time, record:

```text
git commit
macOS version
machine model / CPU
logical CPU count
memory
Go version
Docker version
Docker Compose version
k6 version
```

Historical ShortScale local benchmarks were run on an Apple M4 machine with 10 CPU cores and 24 GiB RAM. The final report records the values observed during benchmark execution rather than assuming they are unchanged.

## Final Local Runtime Topology

Use the repository's current Docker Compose stack.

Explicitly run one API replica for the final benchmark.

```text
k6 on macOS host
      |
      v
Nginx :18080
      |
      v
1 ShortScale API container
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

Prometheus scrapes API + consumer metrics
Grafana remains part of the local integrated stack
```

Published local ports used by the benchmark environment:

```text
Nginx                 18080
PostgreSQL             5433
Redis                  6380
Kafka                  9094
API/Prometheus scrape 19090
Consumer metrics      19091
Grafana               13000
Tempo                  13200
```

The final benchmark intentionally keeps the observability stack running because it is part of the completed local integrated architecture. This also means absolute numbers must not be compared blindly with older experiments that ran fewer components.

## Common Run Discipline

Before each benchmark group:

1. verify `HEAD == origin/main`
2. verify the worktree is clean
3. build/start the current Compose stack with exactly one API replica
4. wait for service health
5. verify API liveness and readiness
6. verify Redis, PostgreSQL, Kafka, and the analytics consumer are healthy
7. ensure the benchmark fixture exists
8. warm the redirect cache when required
9. verify analytics queue depth is zero
10. verify Kafka consumer lag is zero

Between asynchronous-analytics profiles, wait until:

```text
shortscale_redirect_analytics_queue_depth = 0
shortscale_analytics_consumer_lag_records = 0
```

before taking the next baseline.

## Repository Benchmark Tooling

The reproducible final benchmark surface is tracked in the repository:

```text
benchmarks/k6/redirect.js
benchmarks/k6/analytics.js
benchmarks/final/fixture.sh
benchmarks/final/seed-analytics.sql
benchmarks/final/cleanup-fixtures.sql
benchmarks/final/metrics.py
```

The redirect workload accepts environment controls including `SHORT_CODE`, `EXPECTED_LOCATION`, `RATE`, `DURATION`, `PRE_ALLOCATED_VUS`, and `MAX_VUS`.

The authenticated analytics workload requires both `SHORT_CODE` and `SESSION_COOKIE`. It also accepts `DAYS`, `RATE`, `DURATION`, `PRE_ALLOCATED_VUS`, and `MAX_VUS`.

A non-executing static inspection can be performed with:

```text
k6 inspect benchmarks/k6/redirect.js

k6 inspect \
  -e SHORT_CODE=benchmark-inspect \
  -e SESSION_COOKIE=benchmark-inspect \
  benchmarks/k6/analytics.js
```

`fixture.sh` owns disposable fixture creation/cleanup. Session cookies and other runtime secrets must remain outside Git.

`metrics.py` records and reconciles API enqueue/publish counters, consumer processing/persistence counters, queue depth, and consumer lag for the asynchronous analytics profiles.

Raw k6 outputs and disposable fixture state remain transient evidence outside Git; the repository retains the scripts, methodology, and summarized results required to understand and reproduce the benchmark design.

## Metrics to Record

### k6 request metrics

For every HTTP profile record:

```text
target rate
duration
completed requests
actual requests/second
median
p95
p99
maximum
HTTP failure rate
failed checks
dropped iterations
```

### API metrics

Record counter deltas where relevant:

```text
shortscale_cache_operations_total
shortscale_redirect_analytics_enqueues_total{result="enqueued"}
shortscale_redirect_analytics_enqueues_total{result="dropped"}
shortscale_redirect_analytics_publishes_total{result="success"}
shortscale_redirect_analytics_publishes_total{result="failure"}
shortscale_redirect_analytics_queue_depth
```

### Analytics-consumer metrics

Record:

```text
shortscale_analytics_consumer_processed_total
shortscale_analytics_consumer_failures_total
shortscale_analytics_consumer_persistence_total
shortscale_analytics_consumer_lag_records
```

## FINAL-1 — Completed Redirect Hot Path

### Goal

Measure the completed current-head redirect path with Redis, asynchronous Kafka analytics, tracing, metrics, and the analytics consumer all enabled.

This is not a PostgreSQL-only or Redis-only comparison. Those controlled before/after experiments are documented in earlier benchmark reports.

### Fixture

Use a dedicated benchmark short URL.

The target must:

- belong to the disposable benchmark user
- resolve to a fixed `https://example.com/...` destination
- be warmed into Redis before measurement
- remain unexpired for the benchmark

The k6 script must disable redirect following and validate:

```text
HTTP status = 302
Location = exact expected destination
```

### Warm-up

Before every recorded sample:

```text
100 redirect requests
```

Then verify the Redis entry is present.

### Profiles

Run three samples for each profile.

| Profile | Offered rate | Duration |
| --- | ---: | ---: |
| R1 | 500 RPS | 30 s |
| R2 | 3,000 RPS | 30 s |
| R3 | 10,000 RPS | 60 s |

The final report should use the median of the three sample values for latency/throughput summaries while retaining the individual samples in the benchmark evidence record.

### Success criteria

For each sample:

- redirect checks must pass for every executed request
- HTTP failure rate must be 0%
- API must remain live and ready
- no application container may unexpectedly restart

Dropped k6 iterations are **recorded evidence**, not automatically hidden or treated as HTTP failures.

If dropped iterations become material, the report must describe the profile as load-generator/runtime saturation rather than presenting the offered rate as cleanly sustained capacity.

## FINAL-2 — Optimized Analytics Read Path

### Goal

Measure the final authenticated analytics endpoint after daily pre-aggregation and lifetime-summary optimization.

### Dataset

Use one dedicated analytics URL owned by the benchmark user.

Create a controlled synthetic history containing:

```text
100,000 raw redirect events
approximately 90 UTC days
```

Then generate the corresponding `redirect_daily_counts` rows from that exact synthetic raw history.

Validate before benchmarking:

```text
raw total = 100,000
daily rollup SUM(redirects) = 100,000
daily mismatches = 0
```

Using 100,000 events deliberately matches the earlier raw-event hot-key workload shape so the final HTTP results can be discussed against that historical benchmark.

The separate one-million-row database experiments remain the stronger database-scaling evidence and are not replaced by this final HTTP test.

### Authentication

The endpoint is benchmarked through the real authenticated API route.

Use a disposable benchmark account and a real server-side session cookie.

Do not bypass authentication middleware for the benchmark.

### Profiles

Use the historical raw-event analytics request shapes for comparability:

| Window | Offered rate | Duration |
| --- | ---: | ---: |
| 1 day | 50 RPS | 10 s |
| 7 days | 50 RPS | 10 s |
| 30 days | 50 RPS | 10 s |
| 90 days | 50 RPS | 10 s |
| 90 days | 200 RPS | 10 s |

Repeat each profile three times.

Add one longer stability sample:

```text
90 days
200 RPS
30 seconds
```

### Response correctness

Every executed request must return HTTP 200 and the response must identify the expected short code and requested window.

The benchmark must not treat a fast HTTP error response as successful performance.

### Success criteria

- HTTP failure rate = 0%
- response checks = 100%
- API remains live and ready
- no unexpected application restarts

Latency is compared with the historical raw-event API measurements only because the synthetic event volume and endpoint shape are deliberately aligned.

## FINAL-3 — Async Analytics Integrity Under Redirect Load

### Goal

Measure the completed availability-first asynchronous analytics design under representative redirect pressure.

This test is about accounting and degradation semantics, not only HTTP latency.

### Fixture

Use a separate benchmark URL with no synthetic redirect history.

Record baseline values for:

```text
API analytics enqueue counters
API analytics publish counters
API analytics queue depth
consumer processed counter
consumer persistence counters
consumer failure counters
consumer lag
raw redirect-event count for the test short code
daily rollup total for the test short code
```

### Profiles

Run:

| Profile | Offered rate | Duration |
| --- | ---: | ---: |
| A1 | 3,000 RPS | 30 s |
| A2 | 10,000 RPS | 30 s |

One controlled run per profile is sufficient because these are behavioral/accounting experiments rather than latency-estimation samples.

Before A2, wait for A1 queue depth and consumer lag to return to zero.

After each profile, wait for the queue and consumer lag to drain before taking the final counters.

### Accounting

Record deltas for:

```text
executed HTTP redirects
enqueue enqueued
enqueue dropped
publish success
publish failure
consumer processed
consumer persistence inserted
consumer persistence duplicate
consumer failures
final raw rows for benchmark short code
final daily rollup total
```

The report must explain any difference between successful redirects and persisted analytics using the measured enqueue-drop or publish-failure counters.

### Core invariant

For the API enqueue boundary:

```text
executed redirect events
=
enqueued
+
dropped
```

subject to the exact counter sampling boundaries used by the run.

After the system drains, persisted analytics should align with successfully delivered unique events, not with total HTTP redirects if the API intentionally dropped analytics events.

### Success criteria

The redirect path succeeds independently of analytics completeness:

- executed redirect checks = 100%
- HTTP failure rate = 0%
- API remains live and ready
- queue/lag eventually recover after the profile

Analytics drops, if observed, are **not hidden**. They are the measured consequence of the current best-effort API-side analytics policy.

## Repetition and Comparison Rules

Use direct comparisons only when the workload and topology are sufficiently aligned.

Valid examples:

- historical 100k-event raw analytics endpoint vs FINAL-2 100k-event analytics endpoint
- current-head sample 1 vs sample 2 vs sample 3 under the same FINAL-1 profile

Use caution when comparing:

- old standalone API benchmarks with the final full Compose stack
- one-replica and three-replica tests
- monitored runs and unmonitored runs
- production resource measurements and local throughput measurements

The report must clearly label these as different environments rather than presenting them as one continuous capacity curve.

## Production Boundary

Do not run a capacity benchmark against:

```text
https://sscale.vercel.app
```

or the public Railway API.

Production evidence remains limited to the controlled smoke, observability, resource, security, and failure/recovery validation performed against the deployed system.

The final benchmark is a local engineering benchmark.

## Result Storage

Raw benchmark output should be kept outside Git unless a small artifact is intentionally selected for reproducibility.

The repository should ultimately commit:

- benchmark scripts
- fixture/setup scripts
- the summarized final benchmark evidence
- the final engineering report

Do not commit:

- secrets
- session tokens
- large transient k6 output files
- disposable synthetic database dumps

## Claim Rules

The final report may claim measured facts such as:

- fixed local workload completed with a stated latency distribution
- Redis reduced historical redirect tail latency in the earlier controlled comparison
- pre-aggregation reduced historical database read cost in the earlier controlled comparison
- the final current-head analytics endpoint completed the defined fixed workload
- asynchronous analytics dropped or preserved events exactly as measured

The final report must **not** claim:

- maximum production RPS
- unlimited horizontal scalability
- lossless API-side analytics at all loads
- global exactly-once processing
- multi-region failover
- absence of all memory leaks
- universal cloud-provider performance
