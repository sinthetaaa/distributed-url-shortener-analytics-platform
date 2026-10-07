# ADR 002: Redis Cache-Aside for Redirect Lookups

## Status

Accepted

## Context

ShortScale initially resolved every redirect directly from PostgreSQL.

The PostgreSQL-only implementation was intentionally benchmarked before introducing a cache.

The baseline demonstrated that PostgreSQL performed well for a warm local hot-row workload, but tail latency increased under higher offered request rates and every redirect still consumed database read capacity.

A URL shortener can also experience highly skewed traffic where a small number of short URLs receive a large fraction of requests.

The architecture therefore needs a way to serve hot redirect reads without making PostgreSQL handle every request.

## Decision

ShortScale will use Redis as a cache-aside acceleration layer for redirect lookups.

PostgreSQL remains the source of truth.

The redirect lookup sequence is:

~~~text
Redis GET
   |
   +-- hit --> return cached URL
   |
   +-- miss --> query PostgreSQL
                  |
                  +-- found --> Redis SET --> return URL
                  |
                  +-- missing --> return not found
~~~

Redis is not authoritative and is not required for application correctness.

## Cache Key

URL redirects use the key format:

`shortscale:url:{shortCode}`

Example:

`shortscale:url:3ZB9CeC`

The value contains only the original URL required by the redirect hot path.

## TTL

Redirect cache entries use a one-hour TTL.

The TTL:

- limits stale cache lifetime
- naturally removes unused entries
- avoids treating Redis as permanent storage

ShortScale currently has no URL-update operation, so complex invalidation behavior is not required yet.

If mutation semantics are added later, invalidation must be revisited.

## Cache Miss Behavior

A Redis key miss is represented inside ShortScale as:

`cache.ErrMiss`

The application does not expose Redis-specific `redis.Nil` semantics outside the cache implementation.

On a cache miss:

1. PostgreSQL is queried
2. a successful result is written to Redis
3. the redirect is returned

Missing PostgreSQL rows are not cached.

Negative caching may be reconsidered later if repeated invalid-code traffic becomes measurable.

## Cache Failure Behavior

A Redis infrastructure failure is different from a normal cache miss.

If Redis GET fails:

1. a warning is logged
2. ShortScale immediately falls back to PostgreSQL
3. the request does not fail because Redis is unavailable
4. the cache write is skipped for that request

Skipping the write avoids performing a second operation against a cache that has already demonstrated failure.

If Redis SET fails after a normal cache miss:

1. a warning is logged
2. the successful PostgreSQL result is still returned

Cache population is therefore best-effort.

## Failure Latency

Cache operations have a 50 ms application-level timeout.

The Redis client is configured to fail quickly with:

- command retries disabled
- one connection dial attempt
- short dial timeout
- short read timeout
- short write timeout
- context timeout handling enabled

This policy was adopted after failure testing showed that default Redis retry behavior could increase a redirect to approximately 3.5 seconds during a Redis outage.

After fail-fast tuning, the same local failure test returned the redirect through PostgreSQL in approximately 3 ms.

## Readiness

Redis is not included in `/health/ready`.

Application readiness depends on PostgreSQL because PostgreSQL is the durable source of truth.

If Redis fails while PostgreSQL remains healthy, the ShortScale instance remains ready and continues serving redirects using PostgreSQL.

## Persistence

The ShortScale Redis container does not use a persistent data volume.

Cache loss is safe because entries can be reconstructed from PostgreSQL.

This reinforces the rule that Redis is an optimization rather than durable storage.

## Alternatives Considered

### PostgreSQL Only

Advantages:

- simplest architecture
- no additional service
- strong consistency
- already fast for moderate warm local workloads

Disadvantages:

- every redirect consumes database read capacity
- hot URLs directly increase PostgreSQL load
- higher-load tests showed substantial tail-latency growth

Rejected as the final hot-read architecture because measured Redis results materially reduced tail latency and database load.

### Redis as Source of Truth

Advantages:

- very fast lookup path
- simple redirect reads

Disadvantages:

- changes durability requirements
- increases operational risk
- cache eviction or Redis loss could remove authoritative mappings
- conflicts with the existing relational persistence model

Rejected.

PostgreSQL remains authoritative.

### Write-Through Cache

A write-through design would synchronously write both PostgreSQL and Redis during URL creation.

This was not selected because the redirect cache can be populated lazily on demand.

Cache-aside keeps creation correctness independent of Redis availability.

### Read-Through Abstraction Managed Entirely by Redis Infrastructure

A generic read-through system could hide database fallback from the application.

This was not selected because ShortScale currently benefits from explicit control over:

- miss semantics
- PostgreSQL fallback
- timeout policy
- logging
- cache population
- failure behavior

The current cache-aside implementation is small and observable.

## Consequences

### Positive

- hot redirects can avoid PostgreSQL reads
- high-load tail latency is significantly reduced
- PostgreSQL remains the durable authority
- Redis outages do not make ShortScale unavailable
- cache failure latency is bounded
- Redis persistence is unnecessary
- the HTTP handler remains decoupled from Redis details

### Negative

- Redis adds infrastructure and operational complexity
- cache entries can become stale if URL mutation is added later
- every hot redirect still performs a network operation to Redis
- high traffic can move the hot-key problem from PostgreSQL to Redis
- cache stampedes are possible when popular entries expire simultaneously

The hot-key and cache-failure behavior will be explored further in later phases.

## Measured Evidence

The decision is supported by measured results.

At 20,000 offered RPS:

~~~text
PostgreSQL-only
P95 = 8.82 ms
P99 = 23.17 ms

Redis
P95 = 0.553 ms
P99 = 1.53 ms
~~~

During the Redis run:

~~~text
executed redirects = 598,883
Redis GETs          = 598,883
cache hits          = 598,883
cache misses        = 0
HTTP failures       = 0
~~~

During the monitored 10,000 RPS Redis run, PostgreSQL CPU was approximately 0-1%, compared with approximately 25-34% in the PostgreSQL-only baseline.

This demonstrates that Redis both reduced tail latency and removed repeated hot-read pressure from PostgreSQL.

## Follow-Up

Future phases should evaluate:

- hot-key behavior
- cache stampedes
- TTL jitter if needed
- multiple API instances sharing Redis
- Redis failure under concurrent load
- rate limiting
- observability for cache hit/miss/error rates
- whether Redis itself becomes a scaling bottleneck
