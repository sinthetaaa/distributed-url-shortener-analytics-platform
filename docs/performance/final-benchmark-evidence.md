# Consolidated Benchmark Evidence

## Purpose

This document consolidates the final benchmark evidence for ShortScale.

It brings together:

- the controlled final current-head benchmark results from FINAL-1, FINAL-2, and FINAL-3
- the earlier PostgreSQL, Redis, Kafka, and analytics measurements that motivated later architectural changes
- the comparison boundaries required by `final-benchmark-methodology.md`

This is an engineering evidence record, not a production-capacity statement.

## Runtime and Evidence Boundary

The application runtime behavior measured by the final benchmark is the runtime code at:

```text
8102b79edaf7e6a40b1ad5bd0b88a5d7474dbaab
```

The repository HEAD used while executing the final benchmark was:

```text
79478a5da1aa9d4ee3ebf3ed46bba02821952569
```

Commits after the runtime lock added benchmark methodology, benchmark tooling, and documentation without changing the application runtime under test.

The final benchmark ran locally through the completed integrated Compose topology with:

- k6 on the macOS host
- Nginx on port 18080
- exactly one ShortScale API replica
- PostgreSQL
- Redis
- Kafka
- one analytics consumer
- Prometheus, Grafana, Tempo, and OpenTelemetry components running

All capacity-like numbers in this document are therefore local benchmark measurements. They are not production RPS guarantees.

Raw transient k6 and metrics files remain outside Git in the local runtime evidence directory. The repository records only the summarized evidence.

## Evidence Inventory

The final evidence inventory found:

```text
27 k6 JSON result files
FINAL-1 summary files for R1, R2, and R3
FINAL-2 summary files for all required analytics profiles
FINAL-3 accounting summaries for A1 and A2
per-run Prometheus snapshots/deltas where required
plain-text k6 output for the final profiles
```

The worktree was clean and `HEAD == origin/main` before consolidation.

## FINAL-1 — Completed Redirect Hot Path

The completed current-head redirect path included Redis resolution, asynchronous Kafka analytics, tracing, metrics, the analytics consumer, and the observability stack.

Every executed request had to return:

```text
HTTP 302
Location = exact expected destination
```

### Consolidated Results

| Profile | Offered Rate | Duration | Samples | Completed Requests | Median Actual RPS | Median | P95 | P99 | HTTP Failures | Check Failures | Dropped Iterations |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| R1 | 500 RPS | 30 s | 3 | 45,003 | 500.011 | 0.371 ms | 0.765 ms | 1.410 ms | 0 | 0 | 0 |
| R2 | 3,000 RPS | 30 s | 3 | 270,003 | 2,999.935 | 0.374 ms | 0.900 ms | 1.830 ms | 0 | 0 | 0 |
| R3 | 10,000 RPS | 60 s | 3 | 1,789,490 | 9,948.772 | 1.661 ms | 13.200 ms | 42.129 ms | 0 | 0 | 10,513 |

R1 and R2 cleanly sustained their offered profiles in the tested environment with no dropped k6 iterations.

R3 preserved HTTP correctness for every executed request, but the three samples accumulated 10,513 dropped scheduled iterations and showed materially higher tail latency. The methodology therefore requires R3 to be described as local load-generator/runtime saturation evidence rather than as a clean 10,000 RPS capacity claim.

The individual R3 samples also varied substantially, reinforcing that high-rate results on the shared development machine are affected by local scheduling and resource contention.

## Historical Redirect Evolution

Earlier experiments intentionally measured simpler architectures before adding more components.

### PostgreSQL-Only Baseline

At 10,000 offered RPS for 60 seconds, the PostgreSQL-only hot redirect path measured:

```text
completed requests: 600,001
actual RPS:         9,999.84
median:             0.163 ms
p95:                0.374 ms
p99:                1.23 ms
HTTP failures:      0%
dropped iterations: 0
```

At 20,000 offered RPS, tail latency rose to:

```text
p95:                8.82 ms
p99:                23.17 ms
dropped iterations: 1,518
```

Those measurements established the original warm local PostgreSQL baseline.

### Redis Comparison

With the same historical hot-URL workload, Redis reduced redirect tail latency and database read pressure.

At 10,000 offered RPS for 60 seconds:

| Metric | PostgreSQL Only | Redis |
| --- | ---: | ---: |
| Median | 0.163 ms | 0.141 ms |
| P95 | 0.374 ms | 0.212 ms |
| P99 | 1.230 ms | 0.445 ms |
| HTTP failures | 0% | 0% |

At 20,000 offered RPS:

| Metric | PostgreSQL Only | Redis |
| --- | ---: | ---: |
| Median | 0.396 ms | 0.287 ms |
| P95 | 8.820 ms | 0.553 ms |
| P99 | 23.170 ms | 1.530 ms |
| HTTP failures | 0% | 0% |

The Redis benchmark also observed every executed warm redirect as a cache hit with zero cache misses, while PostgreSQL CPU was approximately 0-1% during the monitored 10,000 RPS run.

These historical comparisons justify Redis as a hot-path acceleration and PostgreSQL-offload layer. They are not direct capacity comparisons with FINAL-1 because FINAL-1 intentionally runs the completed full stack and observability topology.

## FINAL-2 — Optimized Analytics Read Path

FINAL-2 used a dedicated authenticated URL with exactly:

```text
100,000 raw redirect events
100,000 redirects represented by daily rollups
0 daily mismatches
```

The API was exercised through the real server-side session authentication path.

Every executed request had to return HTTP 200 and validate both the expected short code and requested window.

### Final Current-Head Results

| Window | Offered Rate | Duration | Samples | Completed | Median Actual RPS | Median | P95 | P99 | HTTP/Check Failures | Dropped |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 day | 50 RPS | 10 s | 3 | 1,501 | 49.999 | 3.411 ms | 4.556 ms | 4.875 ms | 0 | 0 |
| 7 days | 50 RPS | 10 s | 3 | 1,502 | 50.076 | 3.408 ms | 4.290 ms | 4.569 ms | 0 | 0 |
| 30 days | 50 RPS | 10 s | 3 | 1,502 | 50.078 | 3.444 ms | 4.233 ms | 4.790 ms | 0 | 0 |
| 90 days | 50 RPS | 10 s | 3 | 1,501 | 49.999 | 3.569 ms | 4.386 ms | 4.702 ms | 0 | 0 |
| 90 days | 200 RPS | 10 s | 3 | 6,002 | 200.089 | 2.228 ms | 2.978 ms | 3.329 ms | 0 | 0 |

The required longer stability sample measured:

```text
window:             90 days
offered rate:       200 RPS
duration:           30 s
completed requests: 6,001
actual RPS:         200.009
median:             2.195 ms
p95:                2.962 ms
p99:                3.232 ms
maximum:            16.724 ms
HTTP failures:      0
check failures:     0
dropped iterations: 0
unexpected restarts: 0
```

Latency across the 1-day through 90-day 50 RPS profiles remained nearly flat in the final implementation.

The lower latency observed in the 200 RPS samples must not be interpreted as proof that increasing load improves request latency. Warm runtime state, connection reuse, host scheduling, and ordinary local-run variance can affect absolute measurements.

## Historical Analytics API vs FINAL-2

The earlier raw-event analytics benchmark deliberately used the same 100,000-event hot-key workload shape, making it the intended historical comparison for FINAL-2.

### Historical Raw-Event Analytics Baseline

| Window | Offered Rate | Median | P95 | P99 | HTTP Failures |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 day | 50 RPS | 7.26 ms | 8.45 ms | 10.00 ms | 0% |
| 7 days | 50 RPS | 7.80 ms | 9.06 ms | 10.70 ms | 0% |
| 30 days | 50 RPS | 9.77 ms | 10.68 ms | 11.25 ms | 0% |
| 90 days | 50 RPS | 17.46 ms | 18.96 ms | 19.55 ms | 0% |
| 90 days | 200 RPS | 19.56 ms | 25.51 ms | 31.78 ms | 0% |

### FINAL-2 Median-of-Three

| Window | Offered Rate | Median | P95 | P99 |
| --- | ---: | ---: | ---: | ---: |
| 1 day | 50 RPS | 3.411 ms | 4.556 ms | 4.875 ms |
| 7 days | 50 RPS | 3.408 ms | 4.290 ms | 4.569 ms |
| 30 days | 50 RPS | 3.444 ms | 4.233 ms | 4.790 ms |
| 90 days | 50 RPS | 3.569 ms | 4.386 ms | 4.702 ms |
| 90 days | 200 RPS | 2.228 ms | 2.978 ms | 3.329 ms |

### Approximate Latency Reduction

| Window | Rate | Median Reduction | P95 Reduction | P99 Reduction |
| --- | ---: | ---: | ---: | ---: |
| 1 day | 50 RPS | 53.0% | 46.1% | 51.3% |
| 7 days | 50 RPS | 56.3% | 52.7% | 57.3% |
| 30 days | 50 RPS | 64.7% | 60.4% | 57.4% |
| 90 days | 50 RPS | 79.6% | 76.9% | 76.0% |
| 90 days | 200 RPS | 88.6% | 88.3% | 89.5% |

This comparison supports the measured effect of the later pre-aggregation and lifetime-summary work: final analytics latency no longer grows with the requested window in the same way observed when the earlier implementation derived results from raw event history.

The topology and complete application state changed between benchmark generations, so the percentages are local architectural comparison evidence rather than a universal speedup guarantee.

## FINAL-3 — Async Analytics Integrity Under Redirect Load

FINAL-3 used the dedicated async fixture and measured end-to-end accounting rather than only latency.

The critical enqueue-boundary invariant was:

```text
executed redirects = enqueued + dropped
```

After the pipeline drained, published, processed, and persisted counts were reconciled against PostgreSQL raw rows and daily rollups.

### A1 — 3,000 RPS for 30 Seconds

```text
executed redirects:        90,001
actual RPS:                2,999.946
k6 dropped iterations:    0

enqueue enqueued:          90,001
enqueue dropped:           0
publish success:           90,001
publish failure:           0
consumer processed:        90,001
persistence inserted:      90,001
persistence duplicate:     0
persistence error:         0
consumer failures:         0

raw-row delta:             90,001
daily-rollup delta:        90,001
final queue depth:         0
final consumer lag:        0
```

A1 preserved every executed analytics event end to end.

### A2 — 10,000 RPS for 30 Seconds

```text
executed redirects:        299,749
actual RPS:                9,991.172
k6 dropped iterations:    252
HTTP failures:             0
check failures:            0

enqueue enqueued:          49,972
enqueue dropped:           249,777
analytics drop rate:       83.3287%

publish success:           49,972
publish failure:           0
consumer processed:        49,972
persistence inserted:      49,972
persistence duplicate:     0
persistence error:         0
consumer failures:         0

raw-row delta:             49,972
daily-rollup delta:        49,972
final queue depth:         0
final consumer lag:        0
```

The A2 enqueue-boundary invariant reconciled exactly:

```text
299,749 = 49,972 + 249,777
```

Every event accepted by the API-side analytics queue then reconciled through Kafka, the consumer, raw persistence, and the daily rollup.

The measured analytics persistence ratio relative to executed redirects was approximately 16.67%, while approximately 83.33% of analytics events were intentionally dropped at the API-side enqueue boundary under this profile.

Redirect correctness remained independent of analytics completeness.

## Historical Async Analytics Evidence

Earlier asynchronous-analytics testing had already established the qualitative behavior of the availability-first design:

- Kafka outage did not break redirect correctness
- API-side analytics publication was best effort
- at-least-once Kafka delivery combined with an event ID primary key and `ON CONFLICT DO NOTHING` produced idempotent PostgreSQL side effects
- same-short-code events preserved per-key Kafka partition ordering
- moderate 3,000 RPS analytics load showed no observed analytics drops
- approximately 10,000 RPS caused the bounded producer queue to saturate while redirect HTTP failures remained zero

The earlier 10,000 RPS asynchronous-analytics experiment relied on periodic queue-drop logging and observed a periodic drop counter reaching 87,304.

FINAL-3 strengthens that earlier evidence by using explicit Prometheus before/after snapshots and database deltas. It therefore gives exact accounting for the controlled A1 and A2 runs rather than inferring total loss from periodic logs.

## Consolidated Evidence Matrix

| Experiment | Architecture / Version | Workload | Measured Result | Engineering Interpretation |
| --- | --- | --- | --- | --- |
| PostgreSQL redirect baseline | PostgreSQL-only hot path | 10k RPS, 60 s | 9,999.84 RPS; p99 1.23 ms; 0 HTTP failures | Establish original redirect baseline |
| Redis redirect comparison | Redis cache-aside + PostgreSQL source of truth | 10k and 20k hot-path profiles | lower P95/P99; hot requests served from Redis; PostgreSQL load reduced | Justify Redis and preserve graceful DB fallback |
| Earlier async analytics | bounded queue + Kafka + consumer | 3k and ~10k redirect load | 3k no observed drops; ~10k queue saturation without HTTP failure | Identify producer queue as an availability-first loss boundary |
| Raw-event analytics API | raw-event analytics queries | 100k events; 1-90 day windows | latency rose materially with larger windows; 90d/200 RPS p99 31.78 ms | Motivate pre-aggregation and lifetime summary |
| FINAL-1 R1/R2 | completed integrated stack | 500 and 3k RPS | 0 failures, 0 drops, low tail latency | Confirm completed redirect path under representative moderate load |
| FINAL-1 R3 | completed integrated stack | 10k RPS, 60 s x3 | all executed redirects correct; median sample RPS 9,948.77; 10,513 scheduled iterations dropped | Establish local high-load saturation evidence |
| FINAL-2 | optimized authenticated analytics | 100k events; historical raw-event analytics windows | 90d/200 RPS median 2.228 ms, p99 3.329 ms; zero failures/drops | Confirm read-path optimization under aligned workload |
| FINAL-2 stability | optimized authenticated analytics | 90d, 200 RPS, 30 s | 6,001 requests; p99 3.232 ms; zero failures/drops/restarts | Confirm short-duration stability at defined profile |
| FINAL-3 A1 | completed async pipeline | 3k RPS, 30 s | 90,001/90,001 events accepted and persisted | Confirm lossless behavior at tested moderate profile |
| FINAL-3 A2 | completed async pipeline | 10k RPS, 30 s | redirects correct; 83.33% analytics enqueue drops; accepted 49,972 reconciled exactly to DB | Establish current analytics producer saturation boundary |

## Comparison Rules Preserved

The following distinctions must remain explicit in the scaling analysis and final engineering report:

- historical standalone or reduced-stack redirect benchmarks are not one continuous capacity curve with the completed full-stack FINAL-1 benchmark
- one-replica and three-replica measurements are not interchangeable
- monitored and unmonitored high-rate runs can differ
- k6 dropped scheduled iterations are separate from ShortScale analytics enqueue drops
- HTTP redirect success does not imply analytics completeness
- local development-machine measurements are not production sizing evidence
- exactly-once processing is not claimed
- unlimited horizontal scaling is not claimed
- the analytics A2 result is a measured best-effort degradation boundary, not a correctness failure of the redirect path

## Consolidated Result

The final benchmark evidence is now consolidated into one repository document with the historical comparison boundaries preserved.

This document intentionally stops at evidence consolidation.

Detailed bottleneck interpretation and scaling-boundary conclusions are documented separately in `bottlenecks-and-scaling-boundaries.md`.
