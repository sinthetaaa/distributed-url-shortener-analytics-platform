# Database Scaling & Distributed-System Analysis

Phase 13 evaluates how ShortScale's PostgreSQL-backed storage behaves as analytics volume grows and which scaling techniques are actually justified by measured bottlenecks.

The phase deliberately avoids adding replicas, partitions, or shards only for architectural appearance.

The engineering rule remains:

**Measure → identify the real bottleneck → apply the smallest useful architectural change → measure again.**

## Phase 13 Decisions

| Technique | Decision | Reason |
| --- | --- | --- |
| Additional ordinary analytics indexes | Rejected | Hot-key aggregation still scales with matching event volume and extra indexes amplify writes/storage |
| Daily analytics pre-aggregation | Adopted | Converts large raw scans into tiny exact rollup reads |
| Lifetime-summary query rewrite | Adopted | Removes the remaining O(N) hot-key summary scan |
| Read replica | Not yet | Query optimization removed most measured read pressure |
| Time/range partitioning | Not yet | Useful for retention, but current reads do not need it and global event-id uniqueness becomes harder |
| Short-code sharding | Rejected for current scale | Analytics stay local but hot keys create severe shard imbalance |
| Event-ID sharding | Rejected for current scale | Writes balance well but per-code analytics fan out to every shard |
| Application-level database sharding | Not yet | Current primary has not reached a measured storage/write ceiling |

## 13A — PostgreSQL Baseline and Query-Plan Audit

Phase 13 began by measuring the existing production-style database rather than changing it.

At the time of the baseline:

- database size: approximately 54 MB
- `urls`: 109 rows
- `redirect_events`: 199,685 rows
- one hot short code contained 115,438 redirect events
- the hot code represented roughly 58% of the event table
- active and idle application connections remained far below PostgreSQL's configured `max_connections`

### URL lookup

The URL table was still tiny.

PostgreSQL chose a sequential scan for the representative short-code lookup because scanning approximately one hundred rows was cheaper than an additional index traversal.

Observed lookup execution was approximately:

```text
0.033 ms
```

This demonstrated an important query-planning principle:

> An index existing does not mean PostgreSQL should always use it.

### Analytics summary

The lifetime analytics summary for the hot short code scanned a large fraction of `redirect_events`.

PostgreSQL correctly chose a parallel sequential scan because the predicate was not selective enough for the existing index to be advantageous.

The query completed in single-digit milliseconds at the baseline dataset, but its cost was directly tied to the number of raw matching events.

### Daily aggregation

The 30-day daily aggregation used the existing:

```text
(short_code, occurred_at DESC)
```

index and performed an index-only scan.

However, it still had to process and group more than 100,000 matching rows.

Execution was approximately:

```text
43.6 ms
```

This identified the first meaningful database scaling pressure:

```text
event growth
+ hot-key skew
+ repeated raw aggregation
```

rather than URL lookup, connection count, or table maintenance.

## 13B — Controlled Dataset Growth

A disposable table using the production-relevant raw-event schema and indexes was grown through:

```text
100,000
500,000
1,000,000
```

rows.

The synthetic workload intentionally reproduced the observed hot-key skew:

```text
58% of events → one hot short code
```

### Storage growth

Approximate storage grew as follows:

| Rows | Table | Indexes | Total |
| ---: | ---: | ---: | ---: |
| 100k | 9.9 MB | 13 MB | 23 MB |
| 500k | 48 MB | 70 MB | 118 MB |
| 1M | 96 MB | 140 MB | 236 MB |

At one million events, index storage was already larger than table storage.

This made additional read-specific indexes a meaningful write/storage trade-off rather than a free optimization.

### Selective tail query

The existing composite index remained very effective for a selective short code.

Observed execution increased approximately:

```text
100k → 0.065 ms
500k → 0.377 ms
1M   → 2.616 ms
```

The database therefore did not have a generic indexing problem.

### Hot-key summary

The hot lifetime summary grew approximately:

```text
100k → 6.1 ms
500k → 15.8 ms
1M   → 29.0 ms
```

### Hot daily aggregation

The strongest scaling signal was the daily aggregation:

```text
100k → ~17.7 ms
500k → ~98.9 ms
1M   → ~332.8 ms
```

At one million rows, the query processed approximately 580,000 events for the hot code and spilled its sort to disk.

The experiment showed that hot-key raw aggregation was the dominant scaling problem.

## 13C — Query and Index Trade-Off Analysis

Phase 13C tested whether conventional SQL/index tuning could solve the hot daily aggregation before changing the data model.

Four approaches were compared.

### Current query with default memory

The baseline query continued to process the complete hot-key working set.

### Higher `work_mem`

Increasing PostgreSQL `work_mem` from 4 MB to 64 MB removed the sort spill and reduced latency from approximately:

```text
152.9 ms
→
120.1 ms
```

This was useful but did not change the underlying amount of data being scanned and aggregated.

### Day-range rewrite using the existing index

The query was rewritten into a series of daily range lookups over the existing `(short_code, occurred_at)` index.

Execution dropped to approximately:

```text
54.2 ms
```

with no new index.

### Date-expression index

A new expression index reduced the query slightly further to approximately:

```text
50.5 ms
```

but added roughly:

```text
39 MB
```

of index storage at only one million events.

Total index storage increased to approximately 190 MB in that benchmark.

### Decision

The expression index was rejected.

The approximately four-millisecond advantage over the day-range rewrite did not justify another large write-maintained index.

More importantly, both approaches still scaled with raw event volume.

The experiment established that the next improvement needed to change the read model rather than continuously optimize raw aggregation.

## 13D — Daily Analytics Pre-Aggregation

Phase 13D compared raw-event aggregation with a compact daily rollup.

The benchmark used:

```text
1,000,000 raw events
580,000 events for the hot code
```

### Raw daily query

Using the improved day-range raw query:

```text
72.678 ms
```

### Pre-aggregated daily query

Reading the compact rollup table:

```text
0.029 ms
```

This was roughly a:

```text
2,500×
```

reduction in database execution time for the benchmarked hot daily read.

### Storage

The raw event dataset occupied approximately:

```text
255 MB
```

The daily aggregate contained:

```text
5,473 rows
568 kB total
```

### Correctness

The rollup was verified against raw data:

```text
hot raw total       = 580,000
hot aggregate total = 580,000
mismatched days     = 0
```

### Incremental maintenance

A simulated batch of 10,000 events measured:

```text
raw insert          ≈ 41.8 ms
batched rollup      ≈ 5.2 ms
```

Post-update totals remained identical.

### Production design

The benchmark justified a production rollup table:

```text
redirect_daily_counts (
    short_code,
    day,
    redirects
)
```

The consumer now maintains raw events and daily counts in one atomic PostgreSQL statement:

```text
Kafka event
    ↓
INSERT raw event
    ↓
only if the event_id was newly inserted
    ↓
UPSERT daily count
```

This preserves Kafka redelivery idempotency.

### Production backfill

When migration `00003_create_redirect_daily_counts.sql` was applied, the existing production event history was backfilled.

Verification showed:

```text
raw events      = aggregate total
daily mismatches = 0
```

### Live validation

A fresh URL received exactly 100 redirects.

The system reached:

```text
raw rows       = 100
rollup total   = 100
analytics API  = 100
```

### Duplicate Kafka delivery

The exact same properly keyed Kafka event was published twice.

Observed result:

```text
Kafka deliveries          = 2
unique raw rows            = 1
daily rollup increment     = 1
duplicate metric increment = 1
analytics API total        = 1
```

The rollup therefore retained the existing idempotency guarantee.

Production commit:

`3803954` — `feat: pre-aggregate daily redirect analytics`

## 13E — Read Scaling and Replication Decision

After daily pre-aggregation, Phase 13E tested whether analytics reads still created enough primary-database contention to justify a PostgreSQL read replica.

### 13E1 — Single-primary mixed workload

The workload compared:

```text
5,000 redirects alone
```

against:

```text
5,000 redirects
+
1,000 concurrent analytics reads
```

Before lifetime-summary optimization, mixed analytics traffic caused:

```text
redirect throughput  -7.74%
redirect p50         +2.91%
redirect p95         +23.67%
redirect p99         +27.08%
```

Analytics latency under load was:

```text
p50  = 30.21 ms
p95  = 77.82 ms
p99  = 964.63 ms
```

The daily rollup query itself was only approximately:

```text
0.059 ms
```

The remaining expensive operation was the lifetime summary over 115,438 raw hot-key events:

```text
~11.3 ms
```

### 13E2 — Lifetime-summary rewrite

Instead of computing:

```text
COUNT(*)
MIN(occurred_at)
MAX(occurred_at)
```

over all raw matching rows, the candidate used:

```text
total count
→ SUM(daily rollups)

first redirect
→ existing index
→ ORDER BY occurred_at ASC LIMIT 1

last redirect
→ existing index
→ ORDER BY occurred_at DESC LIMIT 1
```

The candidate was checked against every existing short code:

```text
summary mismatches = 0
```

Hot-key execution improved approximately:

```text
10.33 ms
→
0.10 ms
```

or about a 100× improvement.

### 13E3 — Production optimization

The lifetime-summary rewrite was deployed and the exact mixed workload was repeated.

The production query plan executed in approximately:

```text
0.110 ms
```

The analytics load improved to:

```text
p50 = 5.68 ms
p95 = 22.50 ms
p99 = 31.56 ms
```

Relative to the original mixed workload:

```text
analytics p50 improvement ≈ 81.20%
analytics p95 improvement ≈ 71.09%
analytics p99 improvement ≈ 96.73%
```

Redirect isolation also improved significantly:

```text
throughput change = -9.05%
p50 change        = +6.82%
p95 change        = +11.17%
p99 change        = +2.57%
```

The remaining throughput variation showed that shared resources are not free, but database tail-latency interference was dramatically reduced.

All redirect analytics still persisted exactly and the raw/rollup totals remained synchronized.

### Read-replica decision

A read replica was **not deployed**.

At the measured workload:

- optimized analytics already sustained approximately 3,050 requests/second
- redirect p99 interference was only about +2.6%
- connection capacity was not saturated
- correctness remained intact

Adding a replica now would introduce:

- replication lag
- read-after-write consistency questions
- primary/replica routing logic
- replica health/failover behavior
- additional deployment and operational cost

without solving a current measured bottleneck.

Production commit:

`b978b99` — `perf: optimize lifetime analytics summary`

### Future read-replica triggers

A replica should be revisited when one or more are measured:

- sustained analytics traffic materially degrades redirect tail latency after query optimization
- primary PostgreSQL CPU or I/O becomes saturated by reads
- database connection pressure approaches pool/server limits
- analytics availability requires isolation from primary write workloads
- a documented stale-read tolerance exists for analytics endpoints

## 13F — Partitioning vs Sharding

### 13F1 — Time/range partitioning

A one-million-event unpartitioned table was compared with weekly range partitions on `occurred_at`.

#### Storage

```text
unpartitioned = 255 MB
partitioned   = 263 MB
```

Partitioning slightly increased storage.

#### Current first/last lookup

The current API needs indexed first/last redirect timestamps.

Unpartitioned:

```text
first ≈ 0.031 ms
last  ≈ 0.018 ms
```

Partitioned:

```text
first ≈ 0.093 ms
last  ≈ 0.073 ms
```

Partitioning made these reads slower because PostgreSQL had to merge index results across partitions.

#### One-day hot-key raw scan

```text
unpartitioned ≈ 9.03 ms
partitioned   ≈ 7.05 ms
```

A modest improvement.

#### One-day all-event raw scan

```text
unpartitioned ≈ 17.67 ms
partitioned   ≈ 7.73 ms
```

Partition pruning produced a substantial improvement for broad time-bounded raw scans.

However, ShortScale's daily analytics API no longer performs this raw scan because it reads the compact rollup table.

#### Retention

Removing 193,600 historical events from the unpartitioned table using `DELETE` took approximately:

```text
92.7 ms
```

Dropping the equivalent old partitions took approximately:

```text
5.6 ms
```

This is the strongest future reason to adopt time partitioning.

#### Global idempotency problem

The production raw table currently guarantees:

```text
PRIMARY KEY(event_id)
```

The unpartitioned duplicate experiment therefore produced:

```text
same event_id
different occurred_at
→ 1 row
```

The range-partitioned design required uniqueness to include the partition key:

```text
PRIMARY KEY(event_id, occurred_at)
```

The same experiment produced:

```text
same event_id
different occurred_at
→ 2 rows
```

That is weaker than ShortScale's current global event-id deduplication guarantee.

### Range-partitioning decision

Range partitioning was **not adopted**.

It should be reconsidered when retention, vacuuming, index maintenance, or historical raw-event storage becomes an operational bottleneck large enough to justify redesigning global idempotency.

### 13F2 — Logical sharding

The sharding experiment evaluated two candidate routing keys:

```text
short_code
event_id
```

#### Short-code sharding

Short-code routing keeps all analytics for one URL on one shard.

But the hot key remains entirely on one machine.

Synthetic one-million-event distribution:

| Shards | Busiest shard |
| ---: | ---: |
| 4 | 69.70% |
| 8 | 63.80% |
| 16 | 60.80% |

The hot code alone contained 580,000 events and remained on exactly one shard regardless of shard count.

Real production data simulated across four logical short-code shards produced:

```text
shard 0 = 56.81%
shard 1 = 11.51%
shard 2 = 28.61%
shard 3 = 3.07%
```

The hottest production code represented:

```text
52.52%
```

of all events and landed on the busiest shard.

Adding more short-code shards therefore does not solve hot-key skew.

#### Event-ID sharding

Hashing by event ID produced excellent distribution.

Synthetic results:

| Shards | Busiest shard |
| ---: | ---: |
| 4 | 25.05% |
| 8 | 12.56% |
| 16 | 6.32% |

This is close to ideal write balancing.

However, events for one short code are distributed across every shard.

Hot-code analytics fanout became:

```text
4 shards  → query 4 shards
8 shards  → query 8 shards
16 shards → query 16 shards
```

This trades write balance for expensive distributed reads.

#### Global uniqueness under short-code sharding

A per-shard unique constraint cannot enforce a global `event_id` guarantee.

The experiment inserted the same event ID into two different short-code shards and successfully produced:

```text
globally duplicate rows = 2
shards containing event = 2
```

Global idempotency would therefore require another distributed coordination mechanism or globally routed deduplication store.

### Sharding decision

Application/database sharding was **not adopted**.

No evaluated shard key simultaneously provided:

```text
balanced writes
+ per-short-code analytics locality
+ global event-id idempotency
```

At current scale, introducing shard routing, cross-shard analytics, rebalance operations, global deduplication, and multi-shard failure handling would add significantly more complexity than value.

## Current Database Architecture

After Phase 13:

```text
                         ┌───────────────┐
                         │     Redis     │
                         │ redirect cache│
                         └───────┬───────┘
                                 │
Client → Nginx → Go API ─────────┼────────→ PostgreSQL primary
                                 │               │
                                 │               ├─ urls
                                 │               ├─ redirect_events
                                 │               └─ redirect_daily_counts
                                 │
                                 └─ redirect path

Redirect
   ↓
bounded local analytics queue
   ↓
Kafka
   ↓
analytics consumer
   ↓
atomic PostgreSQL write
   ├─ raw event
   └─ daily rollup increment
```

Analytics reads now use:

```text
Daily/window counts
→ redirect_daily_counts

Lifetime total
→ SUM(redirect_daily_counts.redirects)

First/last redirect
→ (short_code, occurred_at) index
```

Raw events remain available for:

- durable analytics history
- audit/debugging
- exact event idempotency
- future reprocessing/backfill
- future analytical models that require event-level history

## Scaling Triggers Going Forward

### Read replica

Revisit when:

- optimized analytics materially harms redirect latency
- primary read CPU/I/O becomes a sustained bottleneck
- connection pressure becomes significant
- read availability requires independent infrastructure

### Time partitioning

Revisit when:

- retention deletes become operationally expensive
- vacuum/index maintenance becomes significant
- raw historical storage reaches a level where partition lifecycle management provides clear benefit

Before adoption, global `event_id` idempotency must be redesigned deliberately.

### Sharding

Revisit only when:

- a single PostgreSQL primary reaches a measured write/storage ceiling
- vertical scaling and query/data-model optimization are insufficient
- routing and rebalance requirements are understood
- a deliberate solution exists for global event deduplication
- cross-shard analytics semantics are defined

## What Phase 13 Demonstrated

Phase 13 changed the system where measurements justified change and explicitly rejected infrastructure where they did not.

Implemented:

- exact daily analytics pre-aggregation
- atomic raw-event + rollup persistence
- historical rollup backfill
- duplicate-safe Kafka redelivery behavior
- approximately 2,500× faster benchmarked daily hot-key reads
- approximately 100× faster hot lifetime-summary query

Measured and rejected for now:

- another large analytics expression index
- PostgreSQL read replica
- range partitioning
- short-code sharding
- event-ID sharding
- application-level database sharding

This is the intended ShortScale engineering philosophy:

> Distributed-system complexity is introduced only after simpler designs fail under measured load.

## Phase 13 Result

**Database Scaling & Distributed-System Analysis: complete.**

The database architecture now matches the measured workload rather than an imagined future scale.

The next phase focuses on production hardening and continuous integration.
