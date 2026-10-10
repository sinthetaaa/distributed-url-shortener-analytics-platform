# ADR 006: Asynchronous Redirect Analytics with Kafka

- Status: Accepted
- Date: 2026-10-07

## Context

ShortScale has two paths with different correctness requirements. Redirects are latency-sensitive and correctness-critical; analytics is useful but must not become a correctness dependency.

```text
GET /{shortCode}
      |
      v
resolve destination
      |
      v
HTTP 302
```

Waiting synchronously for Kafka or PostgreSQL would couple redirect latency and availability to analytics infrastructure.

The governing invariant is:

```text
redirect correctness
must not depend on
analytics availability
```

## Decision

ShortScale records successful redirect events through a bounded asynchronous pipeline:

```text
Client
  |
  v
API redirect handler
  |
  +---- critical path ----> resolve URL ----> HTTP 302
  |
  +---- analytics path ---> bounded in-process queue
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
                             PostgreSQL
```

The HTTP handler never synchronously waits for Kafka. ShortScale prioritizes redirect availability and latency over lossless analytics ingestion.

## Event Contract

The current event is intentionally small:

```json
{
  "event_id": "uuid",
  "event_type": "redirect",
  "short_code": "3ZB9CeC",
  "occurred_at": "2026-10-07T11:45:00.123Z"
}
```

`event_id` is the downstream idempotency key.

## Topic and Partitioning

Redirect events use:

```text
shortscale.redirect-events.v1
```

The local development topic uses three partitions and replication factor 1. Replication factor 1 is a development constraint, not a production durability recommendation.

Kafka records are keyed by `short_code`:

```text
same short_code
      |
      v
same partition
      |
      v
partition ordering
```

Events for different short codes can be processed in parallel. Global ordering across all redirect events is neither required nor claimed.

### Partitioning Experiment

Twelve sequential events for one short code all landed on partition 1 with offsets 1 through 12.

Ninety distinct keys distributed as:

```text
partition 0: 32 keys
partition 1: 33 keys
partition 2: 25 keys
```

This verified same-key affinity, per-key ordering, and cross-key partition parallelism.

## Bounded API-Side Queue

Each API process owns one bounded analytics queue with capacity 4096.

The redirect handler performs a non-blocking enqueue. If the queue is full, the event is dropped, a counter is incremented, and the redirect still succeeds.

```text
queue has capacity -> enqueue -> return redirect
queue full         -> drop event -> return redirect
```

An unbounded queue was rejected because prolonged Kafka slowdown could cause unbounded memory growth. The explicit overload policy is:

```text
availability > analytics completeness
```

## Background Publishing

At the end of Phase 9, one background worker drained the queue per API process with a 500 ms per-publish timeout. The later production implementation retained one bounded worker/queue per API process but increased the application publish timeout to 5 seconds.

Failed publishes are logged and are not synchronously retried by the redirect handler. The worker model also avoids creating one goroutine per request.

## Kafka Failure Policy

Kafka is not a readiness dependency for the redirect API. If Kafka is unavailable, URL resolution still succeeds and HTTP 302 is returned.

If producer configuration is missing or invalid, analytics is disabled and redirects continue.

### Kafka Outage Experiment

Kafka was deliberately stopped while three API replicas remained running. A workload sent 3000 redirects using 150 VUs.

Observed:

```text
302 responses:         3000 / 3000
HTTP failures:         0
readiness:             HTTP 200
API process restarts:  0
```

After Kafka restarted, the same API processes resumed publishing new events without being restarted.

This demonstrated that Kafka failure is isolated from redirect correctness. Events whose publish attempts fail are not guaranteed to be recovered.

## Consumer Delivery Semantics

The analytics consumer disables automatic offset commits and processes records as:

```text
poll event
   |
   v
validate event
   |
   v
persist to PostgreSQL
   |
   v
commit Kafka offset
```

Offsets are committed only after successful processing. This provides at-least-once processing semantics.

## Why This Is Not Exactly Once

There is no atomic transaction spanning PostgreSQL persistence and Kafka offset commit. A crash can occur after the INSERT succeeds but before the offset commits, causing Kafka to redeliver the event.

ShortScale therefore does not claim exactly-once processing.

## Idempotent PostgreSQL Persistence

Redirect events are stored in `redirect_events`. `event_id` is the primary key and insertion uses:

```sql
INSERT ...
ON CONFLICT (event_id) DO NOTHING;
```

Therefore:

```text
first delivery     -> 1 row inserted
duplicate delivery -> 0 rows inserted -> processing still succeeds
```

There is intentionally no foreign key from analytics `short_code` to the URL table. Analytics events are historical facts and should remain ingestible even if URL lifecycle rules later change the source row.

### Duplicate-Delivery Experiment

A controlled event was persisted from partition 2 at offset 3013 and the group committed 3014. The consumer restarted normally and resumed from 3014 without replay.

The group was then manually rewound to 3013 to force redelivery.

After duplicate processing:

```text
committed offset: 3014
database rows:    1
ingested_at:      unchanged
```

This experimentally verified duplicate-safe side effects under at-least-once delivery.

## Phase 9 Consumer Error Policy

At the end of Phase 9, malformed events, processing failures, and commit failures stopped the consumer loop. Poison records were not silently skipped and there was no dead-letter topic.

## Redirect Performance

Phase 9 compared the same current API binary with analytics disabled and enabled.

At 500 and 3000 target requests per second, there were no analytics drops and no HTTP failures.

At approximately 10000 requests per second, the single background publisher could not drain the queue as quickly as events were produced. The latest observed periodic counter reached:

```text
dropped_total: 87304
```

Despite analytics saturation:

```text
HTTP redirect failures: 0%
```

Detailed measurements are in `docs/performance/kafka-redirect-analytics.md`.

## Alternatives Considered

### Synchronous Kafka Publish
Rejected because Kafka latency and outages would enter the redirect critical path.

### Direct PostgreSQL Write from Redirect Handler
Rejected because every redirect would add synchronous write pressure and analytics storage would become a redirect dependency.

### Unbounded In-Process Queue
Rejected because sustained downstream failure could cause unbounded memory growth.

### Goroutine Per Redirect
Rejected because concurrency would grow with request volume and downstream slowdown.

### Drop Redirects When Analytics Is Full
Rejected because analytics is not important enough to make a valid redirect unavailable.

### Partition by Event ID
Rejected because events for one short code could land on different partitions and lose per-short-code ordering.

### Exactly-Once Claim
Rejected because the Kafka-to-PostgreSQL boundary is not one atomic transaction. Idempotent writes solve duplicate side effects without making a stronger guarantee than the architecture provides.

## Consequences

### Positive
- Kafka failure does not break redirects
- analytics work is removed from the synchronous redirect path
- queue capacity bounds in-process buffering
- per-short-code ordering is preserved within Kafka
- different short codes can use partition parallelism
- offsets commit only after successful processing
- duplicate deliveries are safe at the PostgreSQL boundary
- API processes resume publishing after Kafka recovery
- overload behavior has been measured rather than assumed

### Negative
- analytics events can be lost when the queue is full
- analytics events can be lost when Kafka publishing fails
- the current single publisher worker becomes a throughput bottleneck at high event rates
- one queue exists independently in each API process
- replication factor 1 is not production-grade durability
- poison events currently stop the consumer
- PostgreSQL and Kafka offset commits are not atomic
- no exactly-once guarantee exists

## Revisit When

Revisit this decision if analytics loss becomes unacceptable, normal traffic causes sustained drops, producer batching or native async publishing is required, consumer throughput needs multiple instances, partition count must increase, hot short codes create skew, dead-letter handling becomes necessary, multi-broker durability is introduced, or event schema compatibility needs stronger management.

## Resilience evolution after Phase 9

Phase 12 failure testing changed the consumer behavior after real dependency failures exposed two resilience gaps.

### Transient Kafka polling failures

Before:

```text
transient Kafka poll failure
→ consumer exits
```

After:

```text
transient Kafka poll failure
→ bounded exponential backoff
→ same consumer process retries
```

The retry delay grows from 100 ms up to a 2-second maximum and remains cancellation-aware.

### Transient PostgreSQL persistence failures

Before:

```text
persistence failure
→ consumer exits
```

After:

```text
persistence failure
→ Kafka offset remains uncommitted
→ bounded processing retry
→ PostgreSQL recovers
→ persistence succeeds
→ offset commits
```

Decode errors and Kafka commit failures remain distinct fatal correctness stages rather than being silently acknowledged.

### Production consumer-outage validation

Phase 15 production testing removed the analytics consumer while redirects continued.

Five accepted redirects were not reflected while the worker was absent, then appeared after the worker was restored and drained the Kafka backlog.

This reinforces the intended semantics:

```text
API-side publication accepted by Kafka
→ durable backlog can survive consumer absence
→ restored consumer catches up
```

It still does **not** create a global exactly-once guarantee.

The API-side bounded queue and drop policy also remain in place before Kafka acceptance, so sustained Kafka failure can still lose analytics events while preserving redirect availability.

See:

- [Failure engineering and resilience](../resilience.md)
- [Production validation](../production-validation.md)
