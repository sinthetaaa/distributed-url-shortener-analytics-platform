# Bottlenecks and Scaling Boundaries

## Purpose

This document interprets the consolidated benchmark evidence in `final-benchmark-evidence.md`.

It identifies measured bottlenecks, separates proven component boundaries from whole-system saturation, and records the scaling limits that should shape future architecture work.

This is a local engineering analysis. It is not a production capacity statement.

## Executive Bottleneck Summary

The strongest current bottleneck evidence is the API-side async analytics producer capacity.

At the tested 3,000 RPS profile, all 90,001 executed redirects were accepted by the analytics queue and persisted end to end.

At the tested approximately 10,000 RPS profile, redirect correctness remained intact while the bounded API-side analytics queue dropped 249,777 of 299,749 executed redirect events, an 83.3287% analytics drop rate.

The exact enqueue-boundary accounting was:

```text
299,749 = 49,972 + 249,777
executed = enqueued + dropped
```

Every one of the 49,972 events accepted by the queue then reconciled through publish, consume, raw persistence, and daily rollup persistence.

The completed redirect path also showed a separate high-load boundary in FINAL-1 R3. Across the three 10,000 RPS / 60 second samples, 10,513 total k6 iterations were dropped and tail latency became materially less stable.

That FINAL-1 result is not a proven single-component redirect bottleneck. The API, Nginx, Redis, PostgreSQL, Kafka, observability stack, Docker, and k6 shared one development machine, so the correct interpretation is local integrated-runtime/load-generator saturation.

## Boundary 1 — Completed Redirect Path Under Integrated Load

FINAL-1 measured the finished integrated redirect path rather than an isolated PostgreSQL-only or Redis-only implementation.

R1 at 500 RPS and R2 at 3,000 RPS completed cleanly:

```text
R1:
median actual RPS: 500.011
median latency:    0.371 ms
p95:               0.765 ms
p99:               1.410 ms
dropped:           0

R2:
median actual RPS: 2,999.935
median latency:    0.374 ms
p95:               0.900 ms
p99:               1.830 ms
dropped:           0
```

R3 exposed the local high-load boundary:

```text
offered rate:      10,000 RPS
duration:          60 s x 3
completed:         1,789,490
median sample RPS: 9,948.772
median latency:    1.661 ms
p95:               13.200 ms
p99:               42.129 ms
k6 dropped:        10,513 total
HTTP failures:     0
check failures:    0
```

The third R3 sample reached the configured 800 active-VU ceiling, while individual R3 samples varied materially.

Therefore the defensible boundary is:

- executed redirects remained correct under the tested high-load profile
- the local environment could not cleanly sustain every scheduled 10,000 RPS arrival
- tail latency became unstable
- the benchmark does not identify one service as the sole cause
- the benchmark must not be converted into a maximum production RPS claim

Future work on this boundary should begin with component-level profiling on independent hosts or a less resource-coupled load generator before changing application architecture.

## Boundary 2 — API-Side Async Analytics Producer

This is the clearest application-level saturation boundary measured in the final benchmark.

The current design is availability-first:

```text
redirect request
    |
    +----> HTTP 302
    |
    v
non-blocking bounded analytics queue
    |
    v
background publisher
    |
    v
Kafka
```

A1 at 3,000 RPS showed no loss:

```text
executed:   90,001
enqueued:   90,001
dropped:    0
published:  90,001
processed:  90,001
inserted:   90,001
raw delta:  90,001
rollup:     90,001
```

A2 at approximately 10,000 executed RPS showed saturation before Kafka:

```text
executed:            299,749
enqueued:             49,972
enqueue dropped:     249,777
analytics drop rate: 83.3287%

publish success:      49,972
publish failure:           0
processed:            49,972
inserted:             49,972
raw delta:            49,972
rollup delta:         49,972
```

This proves that the loss boundary is currently the API-side enqueue/publisher capacity, not downstream persistence for the events that were successfully accepted.

The current semantics are intentional:

- redirect availability has priority over analytics completeness
- the redirect path does not block waiting for Kafka
- queue saturation drops analytics instead of applying backpressure to redirect requests
- API-side analytics is best effort
- lossless analytics at all loads is not claimed

The next optimization target, if product requirements demand greater analytics completeness, is producer throughput rather than redirect correctness.

Candidate changes should be measured one at a time:

1. producer batching
2. a native asynchronous Kafka producer path
3. carefully bounded multiple publisher workers
4. queue sizing only when memory and drain-rate measurements justify it
5. explicit sampling or aggregation if full-fidelity events are not required

Increasing queue capacity alone would mostly delay saturation unless publisher drain throughput also improves.

## Boundary 3 — Kafka, Consumer, and Persistence

FINAL-3 did not expose Kafka, consumer processing, or PostgreSQL persistence as the first loss boundary for accepted events.

For A1 and A2:

```text
publish failures:        0
consumer failures:       0
persistence errors:      0
persistence duplicates:  0 in the controlled runs
final queue depth:       0
final consumer lag:      0
```

In A2, all 49,972 accepted events reconciled through every downstream stage.

Historical forced-redelivery testing also established at-least-once delivery with idempotent PostgreSQL effects through `event_id` uniqueness and `ON CONFLICT DO NOTHING`.

The correct guarantee remains:

```text
Kafka delivery:         at least once
database side effect:   idempotent
global exactly once:    not claimed
```

Scaling the consumer or database is therefore not the first optimization justified by the final A2 evidence.

They remain future boundaries if accepted-event throughput grows enough to create persistent lag or persistence latency.

## Boundary 4 — Analytics Read Path

The earlier analytics implementation derived bounded daily data and lifetime summaries from raw redirect history.

With 100,000 events, latency grew materially with larger windows:

```text
1 day / 50 RPS:     median 7.26 ms
7 days / 50 RPS:    median 7.80 ms
30 days / 50 RPS:   median 9.77 ms
90 days / 50 RPS:   median 17.46 ms
90 days / 200 RPS:  median 19.56 ms
```

The later design introduced daily preaggregation and a lifetime summary.

FINAL-2, using the aligned 100,000-event workload shape, measured:

```text
1 day / 50 RPS:     median 3.411 ms
7 days / 50 RPS:    median 3.408 ms
30 days / 50 RPS:   median 3.444 ms
90 days / 50 RPS:   median 3.569 ms
90 days / 200 RPS:  median 2.228 ms
```

The required 90-day / 200 RPS / 30 second stability sample completed 6,001 requests with:

```text
median:             2.195 ms
p95:                2.962 ms
p99:                3.232 ms
HTTP failures:      0
check failures:     0
dropped iterations: 0
```

The optimized read path removed the strong window-size growth observed in the historical raw-event design.

No further analytics-read optimization is currently justified by the final fixed workloads.

The next analytics-read scaling work should be triggered by new measurements such as:

- materially larger per-URL histories
- higher query concurrency
- retention growth
- expensive cross-URL reporting
- production database resource pressure

Partitioning, read replicas, sharding, or additional caching should not be added solely because they are common distributed-system patterns.

## Boundary 5 — Redis and PostgreSQL on the Redirect Path

Historical controlled measurements justify Redis primarily for tail-latency control and PostgreSQL offload.

At 20,000 offered RPS in the earlier aligned comparison:

```text
PostgreSQL-only:
p95 = 8.82 ms
p99 = 23.17 ms

Redis:
p95 = 0.553 ms
p99 = 1.53 ms
```

Warm Redis runs recorded cache hits for executed benchmark redirects and near-zero PostgreSQL CPU during the monitored 10,000 RPS run.

PostgreSQL remains the source of truth.

Redis is not a correctness dependency:

- cache misses fall back to PostgreSQL
- Redis outage falls back to PostgreSQL
- cache failure latency is bounded
- cached redirects can continue when PostgreSQL is unavailable for an already-warm key

The current evidence therefore supports retaining Redis, but it does not justify treating Redis as durable state.

## Boundary 6 — Horizontal Replication

Horizontal replication was measured primarily as an availability mechanism.

Three replicas behind Nginx distributed a 1,500-request test almost evenly:

```text
501 / 499 / 500 requests
```

After Nginx failure-policy tuning, a replica crash during a 4,500-request workload produced:

```text
correct redirects: 4,500
HTTP failures:     0
```

Throughput scaling on the same physical machine was not monotonic.

At 10,000 RPS, three replicas improved some tail behavior relative to one replica.

At 15,000 and 20,000 RPS, three local replicas produced worse tail latency and more dropped iterations because the replicas competed for the same host resources.

Therefore:

```text
more local containers != proof of more scalable production throughput
```

The measured benefit of horizontal replication is stronger availability and failure tolerance.

Production throughput scaling would require independent application hosts, realistic network topology, and shared-service capacity testing.

Unlimited horizontal scalability is not claimed.

## Boundary 7 — Process-Local Singleflight

Go `singleflight` successfully collapses concurrent same-key cache misses inside one API process.

Historical measurement:

```text
20 concurrent callers
-> 1 process
-> 1 PostgreSQL lookup
```

With three API processes:

```text
20 callers per replica
60 total callers
-> 3 PostgreSQL lookups
```

The mechanism therefore bounds miss amplification approximately by replica count but does not provide cross-replica coordination.

This is currently an acceptable trade-off.

A distributed lock or distributed singleflight layer is not justified by the existing measurements because it would add coordination latency and failure modes to solve a boundary that has not shown operational harm.

## Boundary 8 — Distributed Rate Limiting

The original process-local token bucket became incorrect as a service-wide policy after horizontal replication:

```text
configured burst: 5
three replicas:   15 requests allowed
```

Moving the bucket to Redis with an atomic Lua operation restored the global invariant:

```text
18 distributed attempts
5 allowed
13 limited
```

A 60-goroutine concurrency test also allowed exactly five requests from a five-token bucket.

The local middleware benchmark measured approximately:

```text
process-local limiter: ~132 ns/op
Redis + Lua limiter:   ~104.486 us/op
```

The distributed limiter deliberately trades a small synchronous Redis operation for cross-replica correctness on the URL-creation write path.

Its failure policy is availability-first:

```text
Redis unavailable
-> limiter fails open
-> URL creation remains available
-> distributed abuse protection is temporarily absent
```

In short, fail-open preserves writes but removes protection during outage.

That is a conscious correctness-versus-availability trade-off and should remain explicit in production documentation.

The measurement does not justify adding this synchronous limiter to the latency-sensitive redirect hot path without a separate abuse requirement and benchmark.

## Bottleneck Priority Matrix

| Boundary | Evidence Strength | Current Impact | Correct Next Action |
| --- | --- | --- | --- |
| API async producer queue | Direct final end-to-end accounting | High analytics loss near tested 10k profile | Optimize producer only if analytics completeness requirements demand it |
| Completed redirect path at R3 | Direct final integrated benchmark, but host-coupled | Tail-latency instability and k6 drops | Profile with decoupled load generation before assigning blame |
| Analytics read path | Direct final benchmark | No current measured problem at defined workload | No change |
| Kafka / consumer / persistence | Direct final accepted-event accounting | Accepted events fully reconciled | Monitor; scale only if lag/persistence pressure appears |
| Redis hot path | Historical controlled comparison | Positive latency and DB-offload benefit | Retain cache-aside design |
| PostgreSQL redirect source of truth | Historical controlled comparison | Correct fallback; not current hot-read bottleneck | Retain as durable source |
| Horizontal replicas | Direct historical same-host comparison | Availability benefit; inconsistent local throughput benefit | Scale for availability; benchmark independent hosts for capacity |
| Process-local singleflight | Direct concurrency experiments | Amplification bounded by replica count | Retain; no distributed lock yet |
| Redis rate limiter | Direct correctness and microbenchmark evidence | Correct global write limiting; fail-open outage trade-off | Retain and document policy |

## Architectural Scaling Order

If future measurements show real pressure, the evidence supports this order of investigation:

1. improve API-side analytics producer throughput if event completeness is required above the current tested moderate profile
2. remeasure redirect saturation with load generation separated from the application host
3. scale analytics consumers only if accepted-event lag becomes persistent
4. scale or partition analytics storage only when database evidence shows it is necessary
5. validate multi-host API scaling rather than inferring it from same-host replicas
6. preserve Redis as an acceleration layer rather than a source of truth
7. avoid distributed coordination mechanisms unless measured contention justifies them

This keeps ShortScale measurement-driven rather than complexity-driven.

## Claims That Remain Out of Scope

The final benchmark evidence does not establish:

- maximum production RPS
- a precise 3k-to-10k analytics producer capacity threshold
- lossless API-side analytics at arbitrary load
- global exactly-once processing
- unlimited horizontal scaling
- multi-region failover
- production autoscaling behavior
- cloud-provider performance equivalence
- absence of all long-duration memory or resource leaks

## Conclusions

The completed ShortScale architecture now has distinct, evidence-backed boundaries.

The strongest application bottleneck is the bounded API-side analytics producer path under high redirect pressure.

That boundary is intentionally isolated from redirect availability: at the tested high-load A2 profile, redirects remained correct while analytics completeness degraded.

The completed redirect path itself also reaches a high-load local saturation region, but the shared-machine benchmark cannot attribute that region to one component.

The analytics read path is no longer a measured bottleneck under the final fixed workload after preaggregation and lifetime-summary optimization.

Redis remains justified for hot-path latency and database offload.

Horizontal replication is proven useful for availability, but same-host experiments do not establish linear throughput scaling.

Kafka, the analytics consumer, and PostgreSQL persistence correctly drained and reconciled all events that crossed the API enqueue boundary in the final controlled tests.

The resulting architecture should therefore evolve only when a measured boundary and a product requirement justify the additional complexity.
