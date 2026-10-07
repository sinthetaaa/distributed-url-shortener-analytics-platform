# Kafka Redirect Analytics: Failure, Ordering, Delivery, and Performance

Date: 2026-10-07

## Purpose

Phase 9 introduced asynchronous redirect analytics and tested failure isolation, partitioning, restart behavior, duplicate delivery, and redirect-path overhead.

These are local architecture experiments and comparative measurements, not production capacity claims.

## Architecture Under Test

```text
                               critical path
Client ──> API ──> resolve URL ──────────────> HTTP 302
            |
            | non-blocking enqueue
            v
       bounded queue
            |
            v
     background worker
            |
            v
          Kafka
            |
            v
    analytics consumer
            |
            v
       PostgreSQL
```

The redirect path does not synchronously wait for Kafka.

## Event Contract and Topic

```json
{
  "event_id": "uuid",
  "event_type": "redirect",
  "short_code": "3ZB9CeC",
  "occurred_at": "2026-10-07T11:45:00.123Z"
}
```

```text
topic:          shortscale.redirect-events.v1
partition key:  short_code
partitions:     3
replication:    1 (local development)
```

## 1. Kafka Outage Isolation

Kafka was deliberately stopped while three API replicas remained running. A shared-iterations k6 workload issued 3000 redirects with 150 VUs.

Observed:

```text
redirect checks:     3000 / 3000 passed
HTTP failures:       0.00%
redirect status:     100% HTTP 302
readiness:           HTTP 200
API restarts:        0
```

Observed latency for the short burst:

```text
average:  7.55 ms
median:   3.97 ms
p90:      17.78 ms
p95:      23.04 ms
```

The run completed at about 18085 requests/s because it was a short shared-iterations burst; that figure is not treated as sustained capacity.

Seven Kafka publish-failure log entries were observed. No queue-full summary appeared because the 3000 events fit within aggregate queue capacity.

After Kafka restarted, the same API processes resumed publishing new events without restart.

**Conclusion:** Kafka failure is isolated from redirect correctness. API-side analytics publication is best effort.

## 2. Kafka Partitioning and Ordering

Twelve redirect events were published sequentially through the real ShortScale producer using the same short code.

All landed on partition 1 at offsets 1 through 12, preserving publication order.

Ninety distinct keys distributed as:

```text
partition 0: 32 distinct keys
partition 1: 33 distinct keys
partition 2: 25 distinct keys
```

Results:

```text
same-key partition affinity: PASS
per-key Kafka ordering:      PASS
all three partitions used:   PASS
```

ShortScale therefore claims per-short-code ordering, not global ordering.

## 3. Consumer Restart Behavior

Controlled event:

```text
event_id:   10cb715b-1e41-49a7-97a5-4de53c2cac31
short_code: QPlGh1d
partition:  2
offset:     3013
```

After persistence the consumer group committed:

```text
CURRENT-OFFSET: 3014
LOG-END-OFFSET: 3014
LAG:            0
```

After stopping and restarting the same group, it resumed at 3014 and the database still contained exactly one row for the event.

Result: normal committed-offset restart **PASS**.

## 4. Forced Duplicate Delivery

To reproduce the effective state of a crash after database persistence but before Kafka commit, the group was stopped and rewound from offset 3014 to 3013.

Before replay:

```text
CURRENT-OFFSET: 3013
LOG-END-OFFSET: 3014
LAG:            1
```

After restarting the same group, Kafka redelivered the record at 3013.

After duplicate processing:

```text
CURRENT-OFFSET: 3014
LOG-END-OFFSET: 3014
LAG:            0
rows for event_id: 1
ingested_at: unchanged
```

This validates:

```text
Kafka at-least-once delivery
+
event_id primary key
+
ON CONFLICT DO NOTHING
=
idempotent PostgreSQL side effect
```

This is not an exactly-once processing claim.

## 5. Redirect Performance with Analytics

The same current API binary was benchmarked in two modes:

```text
CONTROL:   analytics disabled
ANALYTICS: analytics enabled with healthy Kafka
```

One standalone API process was used for both modes. Nginx and the normal three API containers were stopped during measurement. Each profile used a hot redirect URL, 100 warm-up redirects, and a 15-second k6 constant-arrival-rate workload.

### Results

| Target | Analytics | Actual req/s | Median | p95 | p99 | HTTP failures | k6 dropped iterations | Analytics drops |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 500 | disabled | 499.97 | 0.701 ms | 0.863 ms | 1.580 ms | 0.00% | 0 | 0 |
| 500 | enabled | 499.97 | 0.546 ms | 0.882 ms | 1.680 ms | 0.00% | 0 | 0 |
| 3000 | disabled | 2999.99 | 0.145 ms | 0.228 ms | 0.505 ms | 0.00% | 0 | 0 |
| 3000 | enabled | 2999.97 | 0.136 ms | 0.236 ms | 0.425 ms | 0.00% | 0 | 0 |
| 10000 | disabled | 9997.58 | 0.148 ms | 0.356 ms | 0.825 ms | 0.00% | 34 | 0 |
| 10000 | enabled | 9995.78 | 0.217 ms | 0.394 ms | 0.727 ms | 0.00% | 60 | 87304 observed |

### 500 RPS

Both modes sustained the target with 0% failures and zero analytics drops. The small latency differences are treated as run-to-run noise, not as evidence that analytics improves performance.

### 3000 RPS

Both modes sustained the target with 0% failures and zero analytics drops. The differences again fall within ordinary local benchmark variance.

### Approximately 10000 RPS

Control:

```text
actual:                9997.58 req/s
median:                0.148 ms
p95:                   0.356 ms
p99:                   0.825 ms
HTTP failures:         0%
k6 dropped iterations: 34
```

Analytics enabled:

```text
actual:                9995.78 req/s
median:                0.217 ms
p95:                   0.394 ms
p99:                   0.727 ms
HTTP failures:         0%
k6 dropped iterations: 60
```

Approximate observed latency delta:

```text
median: +0.069 ms
p95:    +0.038 ms
```

The lower p99 in the enabled run is treated as normal variance, not a performance improvement.

### Analytics Saturation

At this load the API emitted repeated queue-full summaries. The latest observed periodic counter reached:

```text
dropped_total: 87304
```

That is the latest logged counter, not a claim that no additional drops could have occurred between periodic summaries.

The important behavior was:

```text
analytics queue saturated
      |
      v
analytics events dropped
      |
      v
redirect requests did not block
      |
      v
HTTP failures remained 0%
```

## Current Producer Bottleneck

Each API process currently has one bounded queue and one background publisher worker. The worker performs synchronous Kafka publish operations outside the HTTP request path.

At about 10000 redirect requests/s, event production exceeded that worker's drain rate, causing queue saturation and the defined drop policy to activate.

The correct conclusion is not that ShortScale supports lossless analytics at 10000 RPS. The supported conclusion is that the redirect path sustained approximately 10000 executed requests/s with 0% HTTP failures in this local benchmark while analytics saturated and dropped events instead of applying backpressure.

## Local Benchmark Limitations

The benchmark ran on one Apple M4 development machine, with load generation on the same machine, local or local-Docker PostgreSQL/Redis/Kafka, one standalone API process, 15-second profiles, one hot cached redirect, no production-like network distance, no analytics consumer during the API overhead comparison, and Kafka replication factor 1.

These measurements are useful for comparative architecture decisions, not production sizing.

## Phase 9 Conclusions

```text
Kafka outage
→ redirects remain available

same short code
→ same Kafka partition

sequential same-key publication
→ ordered partition offsets

consumer restart
→ resumes from committed offset

forced duplicate delivery
→ database row remains unique

moderate analytics load
→ no observed event drops

analytics saturation
→ events drop instead of blocking redirects
```

Current guarantees:

```text
redirect availability: strong priority
API-side analytics:     best effort
consumer processing:    at least once
database side effects:  idempotent
Kafka ordering:         per short code
exactly once:           not claimed
```

## Next Optimization Targets

Potential future work, only when measurements justify it:

1. producer batching
2. native asynchronous Kafka publishing
3. carefully designed multiple publisher workers
4. producer queue metrics
5. end-to-end analytics lag metrics
6. consumer-group lag monitoring
7. multiple analytics consumer instances
8. poison-event and dead-letter handling
9. multi-broker Kafka replication
10. explicit event-schema compatibility strategy
