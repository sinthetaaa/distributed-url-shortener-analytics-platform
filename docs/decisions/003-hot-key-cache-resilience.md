# ADR 003: Hot-Key and Cache Failure Resilience

## Status

Accepted

## Context

ADR 002 introduced Redis as a cache-aside acceleration layer for redirect lookups while keeping PostgreSQL as the source of truth.

That design reduced database pressure and improved high-load tail latency, but it introduced additional failure and traffic-shaping concerns:

1. many concurrent requests for one expired hot key could all miss Redis and query PostgreSQL simultaneously
2. many different cache entries created around the same time could expire around the same time
3. a Redis outage could cause every request to repeatedly attempt a dependency already known to be unavailable

Phase 6 measured each of these behaviors before deciding how to address them.

The goal is not to make Redis authoritative or required for correctness.

The goal is to keep the redirect path stable when:

- one URL becomes extremely hot
- cache entries expire
- Redis becomes unavailable

## Decision

ShortScale will use three complementary resilience mechanisms:

1. per-short-code request coalescing with `singleflight`
2. deterministic per-key TTL jitter
3. a small Redis circuit breaker

Each mechanism addresses a different failure mode.

## 1. Per-Key Request Coalescing

Concurrent database fallbacks for the same short code are coalesced with:

`golang.org/x/sync/singleflight`

The short code is used as the singleflight key.

This means requests for:

`3ZB9CeC`

can share one PostgreSQL lookup while requests for unrelated short codes continue independently.

### Before

A characterization test demonstrated:

~~~text
50 concurrent requests
        |
        v
50 Redis misses
        |
        v
50 PostgreSQL lookups
~~~

### After

The same concurrent workload produced:

~~~text
50 concurrent requests
        |
        v
50 Redis misses
        |
        v
singleflight("shortCode")
        |
        v
1 PostgreSQL lookup
        |
        v
1 Redis SET
        |
        v
50 successful results
~~~

A real HTTP + Redis + PostgreSQL experiment confirmed:

~~~text
HTTP requests       = 50
HTTP 302 responses  = 50
Redis GETs          = 50
Redis misses        = 50
PostgreSQL lookups  = 1
Redis SETs          = 1
~~~

### Per-Key Rather Than Global Locking

The coalescing scope is deliberately the short code.

Two different short codes must be able to perform independent database lookups concurrently.

A global mutex would unnecessarily serialize unrelated traffic.

### Shared Lookup Context

A shared database lookup must not be owned by the lifecycle of the first HTTP caller.

If the initiating client disconnects while other requests are waiting for the same result, its cancellation should not terminate work needed by the remaining callers.

The shared operation therefore detaches from the initiating request cancellation.

Each individual caller can still stop waiting on its own context.

### Shared Lookup Timeout

Detaching from caller cancellation must not create unbounded work.

Shared PostgreSQL fallbacks therefore have their own timeout:

`2 seconds`

This bounds the lifetime of a coalesced database lookup if PostgreSQL stalls.

## 2. Deterministic TTL Jitter

A fixed TTL causes entries created around the same time to also expire around the same time.

Before jitter, a characterization test performed 50 cache writes using the one-hour TTL.

Result:

~~~text
cache writes        = 50
distinct TTL values = 1
TTL                 = 1 hour
~~~

ShortScale now applies deterministic ±10% TTL jitter.

With a one-hour base TTL, the possible window is approximately:

~~~text
54 minutes to 66 minutes
~~~

The jitter is derived from the short code rather than from a runtime random-number generator.

### Why Deterministic Jitter

The goal is to spread expirations across keys, not to require cryptographic randomness.

Deterministic per-key jitter provides:

- different expiration offsets for different short codes
- stable behavior for the same short code
- no random-number synchronization
- deterministic tests
- no runtime entropy dependency

### Real Redis Validation

Fifty temporary URLs were inserted and loaded through the real ShortScale redirect path.

Redis reported:

~~~text
keys inspected        = 50
distinct TTL values   = 50
minimum PTTL          = 3,248,875 ms
maximum PTTL          = 3,938,081 ms
observed spread       = 689,206 ms
~~~

The observed expiry spread was approximately 11 minutes 29 seconds.

TTL jitter complements singleflight:

- jitter reduces the probability of many different keys expiring together
- singleflight protects PostgreSQL when many requests still arrive for one expired key

## 3. Redis Circuit Breaker

Phase 5 already configured Redis operations to fail quickly.

A Redis outage therefore did not make ShortScale unavailable.

However, Phase 6 measured a different problem: failure amplification.

### Before Circuit Breaker

At an offered load of 1,000 RPS for 15 seconds with Redis stopped:

~~~text
completed requests           = 15,001
actual throughput            ≈ 1,000 RPS
HTTP failures                = 0
Redis-read failure warnings  = 15,001
~~~

Every request independently attempted Redis before falling back to PostgreSQL.

The service remained correct, but a known Redis outage still generated approximately one failed Redis operation and one warning per request.

### Circuit States

ShortScale now uses three states:

~~~text
CLOSED
   |
   | 3 consecutive dependency failures
   v
OPEN
   |
   | 1 second cooldown
   v
HALF-OPEN
   |
   +-- probe succeeds --> CLOSED
   |
   +-- probe fails ----> OPEN
~~~

### Failure Threshold

The breaker opens after:

`3 consecutive Redis dependency failures`

A normal cache miss is not a Redis failure.

A successful Redis operation resets the failure count.

Caller cancellation is not considered evidence that Redis is unhealthy.

### Open Duration

The initial open interval is:

`1 second`

While the circuit is open:

- Redis calls are skipped
- requests go directly to PostgreSQL fallback
- open-circuit fallbacks do not emit one Redis warning per request

After the cooldown, exactly one request is permitted as the half-open probe.

Other concurrent callers continue bypassing Redis until the probe resolves.

### After Circuit Breaker

The identical 1,000 RPS / 15 second Redis-outage experiment produced:

~~~text
completed requests           = 15,000
actual throughput            ≈ 1,000 RPS
HTTP failures                = 0
Redis-read failure warnings  = 17
~~~

Failure warnings fell from:

`15,001`

to:

`17`

which is approximately a:

`99.89% reduction`

The remaining failures consisted of the initial failures required to open the breaker plus approximately one failed half-open probe per cooldown interval.

### Recovery

After Redis was restarted and the breaker cooldown elapsed:

1. the half-open request successfully contacted Redis
2. the redirect returned HTTP 302
3. the breaker closed
4. the following request also used Redis

Two Redis GET operations were directly observed across the two recovery requests.

This demonstrates automatic recovery without restarting ShortScale.

## Readiness Behavior

Redis remains excluded from application readiness.

During the Redis-outage benchmark:

`/health/ready`

continued to return:

`HTTP 200`

This is intentional.

PostgreSQL is the durable source of truth.

Redis failure should degrade performance characteristics rather than make the API unavailable.

## Alternatives Considered

### No Singleflight

This preserves the simplest cache-aside implementation.

Rejected because the current implementation was directly measured producing 50 PostgreSQL lookups from 50 simultaneous misses for one key.

### Global Mutex Around Database Fallback

This would also suppress duplicate lookups.

Rejected because unrelated short codes should not block each other.

Per-key singleflight provides narrower coordination.

### Fixed TTL

Simpler and predictable.

Rejected because measured cache writes all received exactly the same expiry interval, allowing synchronized expiration waves.

### Runtime Random TTL Jitter

Would also spread expiry times.

Not selected because deterministic hashing achieves the required desynchronization while keeping behavior and tests stable.

### No Circuit Breaker

The fail-fast Redis configuration already kept outage latency low and preserved availability.

However, measurements showed every request continued attempting the unavailable dependency.

Rejected because the circuit breaker reduced this failure amplification by approximately 99.89%.

### Large or Adaptive Circuit-Breaker Framework

A third-party resilience framework could provide more policies and configuration.

Not selected.

The current requirement is small:

- consecutive failure threshold
- open interval
- one half-open probe
- automatic recovery

The local implementation is intentionally limited to the measured problem.

## Consequences

### Positive

- concurrent same-key misses collapse into one PostgreSQL lookup
- unrelated keys continue independently
- hot-key expiration bursts place less pressure on PostgreSQL
- cache expiry is spread across time
- Redis outages no longer cause one failed Redis attempt per request
- log amplification during Redis outages is substantially reduced
- Redis recovery is detected automatically
- PostgreSQL remains the source of truth
- Redis remains optional for correctness

### Negative

- the redirect path now contains additional concurrency state
- singleflight coordination is process-local
- multiple API instances can each perform one fallback lookup for the same key
- deterministic jitter does not guarantee perfectly uniform expiration distribution
- the circuit-breaker thresholds are currently static
- while the circuit is open, Redis may have recovered before the next probe occurs
- PostgreSQL receives redirect traffic during Redis outages

The multi-instance consequence is important.

Once ShortScale runs multiple API replicas, singleflight protects each process independently rather than globally.

A burst against one missing key could therefore produce approximately one PostgreSQL lookup per API instance.

That behavior should be considered during horizontal-scaling phases.

## Measured Decision Summary

The Phase 6 resilience mechanisms were adopted because each solved an observed problem:

~~~text
same-key stampede:
50 PostgreSQL lookups
        ↓
1 PostgreSQL lookup

fixed expirations:
50 writes / 1 TTL value
        ↓
50 writes / 50 TTL values

Redis outage amplification:
15,001 failed Redis attempts/warnings
        ↓
17 failed Redis attempts/warnings
~~~

The resulting architecture is:

~~~text
request
   |
   v
circuit breaker
   |
   +-- OPEN ----------> PostgreSQL fallback
   |
   +-- Redis available
           |
           v
       Redis GET
           |
      +----+----+
      |         |
     HIT       MISS
      |         |
      |         v
      |    singleflight(shortCode)
      |         |
      |         v
      |     PostgreSQL
      |         |
      |         v
      |      Redis SET
      |         |
      +---------+
           |
           v
        HTTP 302
~~~

TTL jitter operates when cached values are written so different keys naturally expire at different times.

## Follow-Up

Later phases should evaluate:

- behavior across multiple API replicas
- Redis latency degradation rather than complete outage
- circuit-breaker observability
- cache hit/miss/error metrics
- breaker state metrics
- PostgreSQL pressure during prolonged Redis outages
- whether centralized coordination is ever justified for extremely hot keys
