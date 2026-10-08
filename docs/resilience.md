# Failure Engineering & Resilience

Phase 12 validates how ShortScale behaves when individual infrastructure components fail, when processes crash, and when services receive graceful termination signals.

The goal is not to claim perfect availability. The goal is to make failure behavior explicit, bounded, measurable, and recoverable.

## Resilience Principles

ShortScale follows several failure-engineering principles:

- the redirect request path should remain independent from asynchronous analytics where possible
- PostgreSQL remains the source of truth for URLs
- Redis is an optimization and must not become a hard redirect dependency
- Kafka provides durable buffering between redirect traffic and analytics persistence
- asynchronous queues must be bounded rather than grow without limit
- transient dependency failures should retry with bounded backoff
- Kafka offsets must not be committed before analytics persistence succeeds
- liveness and readiness represent different operational states
- graceful shutdown must preserve accepted work where practical
- process-local metrics must be interpreted carefully when processes are replaced or killed

## Resilience Matrix

| Failure | Redirect behavior | Analytics behavior | Recovery | Result |
| --- | --- | --- | --- | --- |
| API replica SIGKILL | Traffic continues through surviving replicas | Surviving replicas continue publishing | Killed replica can be restored | Zero redirect loss observed |
| Redis outage | PostgreSQL fallback preserves redirects | Redirect analytics continues independently | Circuit breaker probes Redis and closes after recovery | Redirect path survives cache loss |
| Kafka outage | Redirects continue through bounded local queues | Queue fills, then analytics is deliberately dropped | Publishers and consumer recover after Kafka returns | Request path protected; analytics degradation bounded |
| Analytics consumer crash | Redirects unaffected | Kafka retains backlog durably | Restarted consumer drains backlog | Zero analytics loss observed |
| PostgreSQL outage | Cached redirects continue; uncached/write paths fail | Consumer keeps Kafka offsets uncommitted and retries persistence | API pools and consumer recover automatically | Cached availability plus zero analytics loss |
| API SIGTERM | Accepted HTTP work completes | Buffered analytics queue drains before process exit | Replica can be recreated | Zero accepted-event loss observed |
| Consumer SIGTERM | Redirects unaffected | Unfinished Kafka work remains uncommitted | Restarted consumer redelivers and persists | Zero analytics loss observed |

## 12A — API Replica Crash

### Experiment

One API replica was terminated with `SIGKILL` while traffic continued through Nginx.

The first sustained run sent 3,100 redirect requests while the replica disappeared.

Observed client behavior:

- 3,100 / 3,100 responses were HTTP `302`
- zero client errors
- approximately 386 requests/second
- p50: 2.89 ms
- p95: 4.31 ms
- p99: 5.24 ms

Compose immediately showed two surviving API replicas.

### Metric accounting lesson

A naive Prometheus before/after comparison undercounted the requests because the killed API process took its in-memory counters with it.

The experiment was therefore repeated after the topology stabilized at two replicas.

Corrected verification:

- 1,000 / 1,000 redirects returned HTTP `302`
- approximately 5,092 requests/second
- p50: 4.11 ms
- p95: 9.04 ms
- p99: 16.38 ms
- Prometheus HTTP counter delta: exactly 1,000

The killed replica was restored afterward and the system returned to three healthy API replicas.

### Finding

Horizontal API replication plus Nginx removed a single API process as a redirect-availability dependency.

The experiment also demonstrated that process-local cumulative metrics cannot be naively compared across a changing replica set.

## 12B — Redis Outage

### Experiment

Redis was stopped after warming a redirect into cache.

API readiness remained HTTP `200` because PostgreSQL, not Redis, determines readiness.

During the outage:

- all redirect requests continued returning HTTP `302`
- requests fell back to PostgreSQL
- the API processes remained unchanged
- only 20 actual Redis GET failures were observed across 432 outage-period redirects

A trace captured the circuit breaker in the open state:

```text
redirect.resolve
├── redirect.cache.get
│   result = circuit_open
└── redirect.database.lookup
```

No cache refill was attempted while the cache dependency was considered unhealthy.

After Redis returned and the breaker recovery window elapsed, a later trace showed a normal cache hit with no PostgreSQL lookup.

### Finding

Redis is treated as an optimization rather than a redirect correctness dependency.

The circuit breaker substantially reduced repeated traffic toward an unavailable Redis instance while preserving PostgreSQL fallback.

## 12C — Kafka Outage and Bounded Analytics Degradation

### Initial failure discovery

Kafka was stopped while three API replicas handled redirect traffic.

A 13,000-request load completed with:

- 13,000 / 13,000 HTTP `302`
- approximately 5,628 requests/second in the first experiment
- bounded per-replica analytics queues
- combined queue capacity: 12,288 events

The first experiment exposed an analytics-consumer resilience bug: a transient Kafka poll error caused the consumer process to exit.

The consumer previously treated every Kafka polling error as fatal.

### Fix

The consumer was changed to retry transient poll failures with bounded exponential backoff:

```text
100 ms → 200 ms → 400 ms → 800 ms → 1.6 s → 2 s maximum
```

Context cancellation still stops the retry immediately.

Decode and Kafka commit failures remain distinct fatal correctness stages.

### Runtime validation after the fix

Kafka was stopped again and 13,000 redirects were sent.

Observed redirect behavior:

- 13,000 / 13,000 HTTP `302`
- approximately 5,838 requests/second
- p50: 13.09 ms
- p95: 25.37 ms
- p99: 40.08 ms

The analytics path reconciled as:

- 12,294 events enqueued
- 706 queue-full drops
- 12,285 successful Kafka publishes
- 9 Kafka publish failures
- 12,285 consumer-processed events
- 12,285 PostgreSQL inserts
- 7 Kafka poll failures observed
- final API queue depth: 0
- final Kafka consumer lag: 0

Total analytics loss:

```text
706 queue drops
+ 9 publish failures
= 715 lost analytics events
```

Across 13,000 redirects, the measured analytics loss was approximately 5.50%.

### Finding

Kafka failure does not take down redirect traffic.

Analytics degradation is deliberately bounded by finite queues rather than unbounded memory growth.

The experiment also produced a concrete resilience improvement: transient Kafka polling failures no longer terminate the analytics consumer.

## 12D — Analytics Consumer Crash

### Experiment

Kafka remained healthy while the analytics consumer was terminated with `SIGKILL`.

Exactly 5,000 redirect requests were sent while the consumer was dead.

Observed behavior:

- 5,000 / 5,000 HTTP `302`
- approximately 5,309 requests/second
- PostgreSQL remained at the single warm-up analytics row
- Kafka consumer-group lag became exactly 5,000

This proved the events had reached Kafka but had not been processed.

The same consumer container was restarted.

Recovery completed with:

- Kafka lag: 5,000 → 0
- PostgreSQL rows: 1 → 5,001
- recovered outage events: exactly 5,000

### Finding

Kafka successfully acts as a durable decoupling layer between redirect traffic and analytics consumption.

A consumer crash caused backlog, not analytics loss.

## 12E — PostgreSQL Outage

### API behavior

PostgreSQL was stopped while Redis remained healthy.

Observed health semantics:

```text
GET /health/live   → 200
GET /health/ready  → 503
```

A previously cached redirect continued returning HTTP `302`.

A deterministic Redis miss returned HTTP `500` because PostgreSQL is the URL source of truth.

Creating a new short URL also returned HTTP `500`.

All three API processes remained alive.

When PostgreSQL returned:

- readiness recovered to HTTP `200`
- the previously uncached redirect returned HTTP `302`
- no API restart was required

### Consumer resilience bug

The first outage experiment exposed another real resilience gap.

A PostgreSQL persistence failure caused the analytics consumer to exit with code 1.

The Kafka record had not been safely completed, but the process did not stay alive to retry it.

### Fix

Processing failures are now retried with bounded exponential backoff while retaining the current Kafka record:

```text
Kafka record polled
        ↓
PostgreSQL write fails
        ↓
offset remains uncommitted
        ↓
100 ms → 200 ms → ... → 2 s retries
        ↓
PostgreSQL recovers
        ↓
persistence succeeds
        ↓
Kafka offset commits
```

The retry remains cancellation-aware.

Decode and Kafka commit errors remain fatal rather than being silently hidden.

### Runtime validation after the fix

With PostgreSQL down, exactly 1,000 cached redirects were sent.

Observed behavior:

- 1,000 / 1,000 HTTP `302`
- approximately 5,915 requests/second
- p50: 6.10 ms
- p95: 10.26 ms
- p99: 20.98 ms
- consumer remained alive
- consumer restart count remained 0
- persistence/process failures increased by 5
- Kafka lag became exactly 1,000

After PostgreSQL returned:

- the same consumer process recovered automatically
- Kafka lag returned to 0
- PostgreSQL reached exactly 1,001 rows including the warm-up event
- processed delta: 1,000
- inserted delta: 1,000

### Finding

ShortScale now separates availability according to dependency role:

- cached redirects can survive PostgreSQL loss
- uncached redirects cannot, because PostgreSQL remains authoritative
- readiness correctly advertises that the service cannot fully serve traffic
- analytics records remain durable in Kafka until PostgreSQL persistence succeeds

The experiment recovered all 1,000 analytics events with zero loss.

## 12F — Graceful Shutdown and In-Flight Work

### API graceful shutdown

A single API replica was isolated for the experiment.

Kafka was paused so redirect analytics accumulated in the API's bounded asynchronous queue.

After 200 successful redirect requests:

- 199 events were still visibly queued when SIGTERM was sent
- Kafka was immediately resumed
- API process exited with code 0
- shutdown lifecycle completed without a graceful-shutdown error
- PostgreSQL ultimately contained exactly 200 new analytics events

Result:

```text
200 accepted redirects
→ 200 persisted analytics events
→ zero loss
```

This validates the production shutdown sequence:

```text
SIGTERM
→ stop accepting new HTTP work
→ wait for in-flight HTTP requests
→ close analytics queue
→ drain buffered analytics
→ flush tracing
→ exit
```

### Consumer graceful shutdown

PostgreSQL was stopped and 100 cached redirects were published.

Before SIGTERM:

- consumer was alive
- persistence retry was active
- Kafka lag was exactly 100

After SIGTERM:

- consumer exited with code 0
- Kafka lag remained exactly 100
- no partial analytics rows were persisted while PostgreSQL was unavailable

After PostgreSQL recovery and consumer restart:

- Kafka lag returned to 0
- PostgreSQL gained exactly 100 events
- zero analytics events were lost

### Container shutdown budgets

Application-level graceful shutdown was already correct, but Compose had no explicit grace periods.

The API can theoretically spend up to:

```text
10 s HTTP shutdown
+ 2 s analytics drain
+ 5 s trace flush
= 17 s
```

Explicit Compose shutdown budgets were therefore added:

```yaml
api:
  stop_grace_period: 25s

analytics-consumer:
  stop_grace_period: 15s
```

Docker inspection confirmed the containers received stop timeouts of 25 and 15 seconds respectively.

## Resilience Changes Produced by Failure Testing

Phase 12 was not only observational.

Two runtime experiments uncovered real defects and produced code changes.

### Kafka consumer poll recovery

Before:

```text
transient Kafka poll failure
→ consumer process exits
```

After:

```text
transient Kafka poll failure
→ bounded exponential retry
→ same process recovers
```

Commit:

`b21756b` — `fix: retry transient Kafka consumer failures`

### PostgreSQL persistence recovery

Before:

```text
transient PostgreSQL persistence failure
→ consumer process exits
```

After:

```text
transient PostgreSQL persistence failure
→ Kafka offset remains uncommitted
→ bounded processing retry
→ same process recovers
→ offset commits only after persistence succeeds
```

Commit:

`1cdbf66` — `fix: retry transient analytics persistence failures`

### Graceful shutdown orchestration

Explicit Compose stop budgets prevent the container runtime's termination window from being shorter than the application's own graceful-shutdown sequence.

Commit:

`b6c38c2` — `chore: harden graceful shutdown timing`

## Failure Semantics

ShortScale intentionally does not treat every failure the same way.

### Redis unavailable

```text
cache fails
→ circuit breaker opens
→ PostgreSQL fallback
→ redirect remains available
```

### Kafka unavailable

```text
redirect succeeds
→ local analytics queue buffers temporarily
→ queue reaches fixed capacity
→ excess analytics are dropped
```

Request availability is prioritized over analytics completeness during a Kafka outage.

### Analytics consumer unavailable

```text
redirect succeeds
→ event remains durable in Kafka
→ consumer restart drains backlog
```

### PostgreSQL unavailable

For cached URLs:

```text
Redis hit
→ redirect succeeds
```

For uncached URLs and writes:

```text
PostgreSQL unavailable
→ operation fails
→ readiness = 503
```

For analytics:

```text
Kafka record remains uncommitted
→ persistence retries
→ PostgreSQL returns
→ persistence succeeds
→ offset commits
```

## What Phase 12 Demonstrated

The failure experiments established that ShortScale now has explicit behavior for:

- API process loss
- Redis failure
- Kafka failure
- analytics-consumer process loss
- PostgreSQL failure
- graceful API termination
- graceful consumer termination

The system does not claim that every request or analytics event survives every possible infrastructure failure.

Instead, its guarantees are intentionally scoped:

- redirect availability is protected from cache, analytics, telemetry, and individual API-process failures
- PostgreSQL remains required for uncached redirect correctness and URL writes
- analytics are durable after Kafka accepts them
- analytics before Kafka acceptance may be lost during sustained Kafka failure because API queues are intentionally bounded
- Kafka offsets are committed only after successful processing
- graceful shutdown preserves accepted work under the tested healthy-dependency conditions

## Phase 12 Result

**Failure Engineering & Resilience: complete.**

Phase 12 converted several implicit assumptions into measured guarantees and turned two discovered dependency failures into tested recovery behavior.

The next phase focuses on database scaling and distributed-system trade-off analysis rather than adding infrastructure without evidence.
