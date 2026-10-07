# Hot-Key and Cache Failure Resilience Report

## Purpose

This report records the measurements used to design Phase 6 of ShortScale.

Phase 5 demonstrated that Redis materially improved the hot redirect path.

Phase 6 asks what happens when the cache is stressed by:

- many concurrent requests for one uncached hot URL
- synchronized cache expiry
- Redis unavailability

The phase follows the same engineering sequence used elsewhere in ShortScale:

~~~text
reproduce
   |
   v
measure
   |
   v
implement smallest justified mechanism
   |
   v
validate
   |
   v
measure again
~~~

## Test Environment

The experiments were performed in the existing local development environment:

- Apple M4
- 10 CPU cores
- 24 GiB RAM
- Go API running natively
- k6 running natively
- PostgreSQL running in Docker
- Redis running in Docker

PostgreSQL:

- host port 5433
- container port 5432

Redis:

- host port 6380
- container port 6379

These results describe behavior in this local environment.

They are engineering comparisons rather than production capacity guarantees.

## Experiment 1: Same-Key Cache Stampede

### Problem

The original cache-aside implementation independently performed PostgreSQL fallback after every Redis miss.

A concurrent cache miss on a highly popular URL could therefore create duplicate database work.

### Characterization

A controlled unit test released 50 callers for the same short code simultaneously.

The fake cache always returned a miss.

The database source was intentionally delayed so all callers overlapped.

Result before request coalescing:

~~~text
concurrent requests = 50
database lookups    = 50
~~~

This confirmed the cache-stampede vulnerability.

## Singleflight Implementation

ShortScale added per-short-code request coalescing with:

`golang.org/x/sync/singleflight`

The short code is the coalescing key.

### Unit Result

After the change:

~~~text
concurrent same-key misses = 50
database lookups           = 1
~~~

A second test used:

~~~text
25 requests -> hot1234
25 requests -> hot5678
~~~

Result:

~~~text
total requests    = 50
different keys    = 2
database lookups  = 2
~~~

This verifies that request coalescing is per key rather than global.

## Context and Cancellation Validation

The shared database lookup is detached from one caller's cancellation.

Tests verified:

- canceling one request returns `context.Canceled` to that caller
- the shared lookup continues for other callers
- the shared lookup has its own database deadline

Shared database lookup timeout:

`2 seconds`

## Real-Service Stampede Validation

A real integration experiment used:

- the running ShortScale HTTP API
- real Redis
- real PostgreSQL
- 50 simultaneous HTTP redirects

The Redis key was removed before the burst.

PostgreSQL's `urls` table was temporarily held with an `ACCESS EXCLUSIVE` lock so the fallback query remained visible long enough to inspect through `pg_stat_activity`.

Observed while the requests were blocked:

~~~text
active ShortScale URL lookups waiting on PostgreSQL lock = 1
~~~

Final result:

~~~text
HTTP requests          = 50
HTTP 302 responses     = 50
Redis GET operations   = 50
Redis cache misses     = 50
Redis SET operations   = 1
PostgreSQL lookups     = 1
~~~

This proves that the coalescing behavior exists in the real request path rather than only in mocks.

## Experiment 2: Synchronized TTL Expiry

### Problem

ShortScale originally assigned every redirect cache entry exactly the configured base TTL.

For the current configuration:

`TTL = 1 hour`

A characterization test performed 50 cache writes.

Result:

~~~text
cache writes         = 50
distinct TTL values  = 1
TTL used             = 1h0m0s
~~~

Keys populated in the same interval could therefore also expire in the same interval.

Singleflight only combines requests for the same key, so synchronized expiry across many different popular keys can still produce many independent PostgreSQL fallbacks.

## Deterministic TTL Jitter

ShortScale now applies:

`±10%`

per-key TTL jitter.

For a one-hour base TTL, the intended window is approximately:

~~~text
minimum ≈ 54 minutes
maximum ≈ 66 minutes
~~~

The offset is deterministically derived from the short code.

### Unit Result

Fifty cache writes after the change produced:

~~~text
writes                 = 50
distinct TTL values    = 50
observed minimum       = 54m25.104942719s
observed maximum       = 1h5m52.453408381s
~~~

Tests also verified:

- the same short code receives the same TTL
- generated TTLs remain inside the ±10% window
- non-positive TTL values are preserved

## Real Redis TTL Validation

Fifty temporary short URLs were inserted into PostgreSQL.

Each URL was requested through the real HTTP API exactly once, causing normal cache-aside population in Redis.

The real Redis `PTTL` values were then inspected.

Result:

~~~text
keys inspected       = 50
distinct PTTLs       = 50
minimum PTTL         = 3,248,875 ms
maximum PTTL         = 3,938,081 ms
observed spread      = 689,206 ms
~~~

The measured expiry spread was approximately:

`11 minutes 29 seconds`

All 50 HTTP requests returned:

`302`

Temporary database rows and Redis keys were removed after the experiment.

## Experiment 3: Redis Outage Without Circuit Breaker

Phase 5 had already configured Redis to fail quickly rather than performing long internal retry sequences.

The remaining question was whether repeated Redis failures under traffic were still expensive.

The benchmark used:

~~~text
offered rate = 1,000 RPS
duration     = 15 seconds
hot URL      = 3ZB9CeC
~~~

### Healthy Redis

Result:

~~~text
requests       = 15,001
actual RPS     ≈ 999.99
HTTP failures  = 0

median         = 0.665 ms
P95            = 0.766 ms
P99            = 1.52 ms
~~~

Redis failure warnings:

`0`

### Redis Stopped

Readiness remained:

`HTTP 200`

Result:

~~~text
requests       = 15,001
actual RPS     ≈ 1,000.01
HTTP failures  = 0

median         = 0.761 ms
P95            = 0.874 ms
P99            = 1.15 ms
~~~

The service therefore remained correct and maintained the requested throughput.

However:

~~~text
Redis-read failure warnings = 15,001
~~~

Every benchmark request attempted Redis even though Redis was already known to be unavailable.

This was classified as failure amplification rather than an availability failure.

## Experiment 4: Redis Outage With Circuit Breaker

ShortScale added a small Redis circuit breaker.

Configuration:

~~~text
failure threshold = 3 consecutive dependency failures
open duration     = 1 second
half-open probes  = 1 concurrent probe
~~~

A normal cache miss does not count as a dependency failure.

Caller cancellation does not count as a Redis failure.

The identical 1,000 RPS / 15 second outage benchmark was repeated.

### Healthy Redis With Breaker

Result:

~~~text
requests       = 15,000
actual RPS     ≈ 999.98
HTTP failures  = 0

median         = 0.679 ms
P95            = 1.03 ms
P99            = 2.65 ms
~~~

Redis failure warnings:

`0`

### Redis Down With Breaker

Readiness remained healthy.

Result:

~~~text
requests       = 15,000
actual RPS     ≈ 999.98
HTTP failures  = 0

median         = 0.743 ms
P95            = 1.42 ms
P99            = 4.09 ms
~~~

Redis-read failure warnings:

`17`

## Failure Amplification Comparison

Without breaker:

~~~text
15,001 requests
15,001 Redis failures/warnings
~~~

With breaker:

~~~text
15,000 requests
17 Redis failures/warnings
~~~

Reduction:

`99.89%`

The observed warnings also matched the intended breaker behavior:

1. the first failures opened the circuit
2. while open, requests bypassed Redis
3. approximately one failed probe occurred per cooldown interval

This changes failed Redis activity from approximately proportional to request volume to approximately proportional to breaker probe frequency during a sustained outage.

## Latency Interpretation

The circuit breaker should not be described as a latency optimization based on these measurements.

Redis-down results before the breaker were:

~~~text
median = 0.761 ms
P95    = 0.874 ms
P99    = 1.15 ms
~~~

Redis-down results with the breaker were:

~~~text
median = 0.743 ms
P95    = 1.42 ms
P99    = 4.09 ms
~~~

The runs contain normal local benchmark variation, and the later run had higher tail latency.

The defensible conclusion is therefore:

> The circuit breaker preserved correctness and throughput while reducing repeated Redis failures and warning amplification by approximately 99.89%.

The benchmark does not demonstrate that the circuit breaker reduced request latency.

## Circuit Recovery Validation

After the outage benchmark:

1. Redis was restarted
2. Redis became healthy
3. the breaker cooldown elapsed
4. the next redirect acted as the half-open probe
5. the probe returned HTTP 302
6. a second redirect also returned HTTP 302

Redis command statistics recorded:

~~~text
Redis GETs across recovery requests = 2
~~~

This demonstrates that a successful half-open probe closes the circuit and restores Redis to the redirect read path automatically.

## Combined Phase 6 Behavior

The three resilience mechanisms address different dimensions:

### Same Key, Many Requests

~~~text
many requests
     |
     v
same missing key
     |
     v
singleflight
     |
     v
1 PostgreSQL lookup
~~~

### Many Keys Expiring

~~~text
base TTL
   |
   v
deterministic ±10% jitter
   |
   v
expirations spread across time
~~~

### Redis Dependency Failure

~~~text
Redis failures
      |
      v
3 consecutive failures
      |
      v
circuit open
      |
      v
skip Redis
      |
      v
PostgreSQL fallback
      |
      v
periodic half-open probe
~~~

## Engineering Outcome

Phase 6 changed the redirect cache from a simple performance layer into a more resilient dependency boundary.

Measured results:

| Scenario | Before | After |
|---|---:|---:|
| Same-key 50-request burst: PostgreSQL lookups | 50 | 1 |
| 50 cache writes: distinct TTLs | 1 | 50 |
| Redis outage: failure warnings | 15,001 | 17 |
| Redis outage: HTTP failures | 0 | 0 |
| Redis outage: throughput | ~1,000 RPS | ~1,000 RPS |

The primary wins are:

- duplicate database work suppression
- expiration desynchronization
- dependency-failure amplification control
- automatic Redis recovery

## Limitations

### Local Benchmark

All components share one physical machine.

The measurements should not be interpreted as production throughput guarantees.

### Process-Local Singleflight

Singleflight coordination exists only inside one Go process.

With multiple API instances, each instance can independently perform one PostgreSQL fallback for the same short code.

### Complete Outage vs Slow Redis

The circuit-breaker experiment stopped Redis completely.

A future experiment should evaluate Redis that remains reachable but becomes slow.

### Static Breaker Policy

The current breaker uses:

- threshold = 3
- cooldown = 1 second

Those values are initial engineering defaults supported by the current experiment rather than universally optimal production values.

### Observability

Current proof relies on logs, Redis statistics, PostgreSQL activity, and benchmark output.

Later observability work should expose dedicated metrics for:

- cache hits
- cache misses
- cache errors
- circuit state
- circuit opens
- half-open probes
- singleflight sharing
- PostgreSQL fallback count

## Conclusion

Phase 6 demonstrates a measurement-driven progression:

~~~text
observe duplicate DB work
        |
        v
add singleflight
        |
        v
prove 50 -> 1

observe synchronized TTLs
        |
        v
add deterministic jitter
        |
        v
prove real Redis expiry spread

observe Redis outage amplification
        |
        v
add circuit breaker
        |
        v
prove 15,001 -> 17 failures
~~~

The resulting mechanisms are retained because they address measured behavior rather than hypothetical complexity.
