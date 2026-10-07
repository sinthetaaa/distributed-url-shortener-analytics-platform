# Redis Redirect Performance Comparison

## Purpose

This report evaluates the effect of adding Redis to ShortScale's redirect hot path.

Phase 4 established the PostgreSQL-only baseline.

Phase 5 introduced a resilient cache-aside layer:

~~~text
GET /{shortCode}
      |
      v
Redis GET
   |
   +-- hit --> HTTP 302
   |
   +-- miss --> PostgreSQL
                  |
                  v
              Redis SET
                  |
                  v
               HTTP 302
~~~

PostgreSQL remains the source of truth.

Redis is an acceleration layer and is not required for correctness.

## Benchmark Environment

The comparison was performed on the same local development machine used for the PostgreSQL baseline:

- Apple M4
- 10 CPU cores
- 24 GiB RAM
- macOS
- Go API running natively
- k6 running natively
- PostgreSQL running in Docker
- Redis running in Docker

Redis:

- Redis 8.0 Alpine
- host port 6380
- container port 6379

The same benchmark script was used:

`benchmarks/k6/redirect.js`

The workload repeatedly requests:

`GET /3ZB9CeC`

which resolves to:

`https://example.com/shortscale-phase3`

Redirect following is disabled so the benchmark measures ShortScale rather than the destination website.

## Cache Configuration

Redis keys use the form:

`shortscale:url:{shortCode}`

For the benchmark target:

`shortscale:url:3ZB9CeC`

The cached value is the original URL.

Cache TTL:

`1 hour`

Cache operations use a 50 ms application-level timeout.

The Redis client is configured to fail quickly so cache failures do not introduce multi-second delays into the redirect hot path.

## Workload Characteristics

These measurements represent a warm-cache hot-URL workload.

Before each Redis benchmark:

1. the target Redis key was deleted
2. one redirect was executed
3. PostgreSQL resolved the URL
4. Redis was populated
5. the benchmark began with the URL already cached

The measurements therefore evaluate the hot-cache redirect path rather than cold-cache behavior.

All benchmark components share the same physical machine.

Results should be interpreted as relative architectural measurements rather than production capacity guarantees.

## PostgreSQL vs Redis Results

### 10,000 RPS for 60 Seconds

| Metric | PostgreSQL Only | Redis |
|---|---:|---:|
| Completed requests | 600,001 | 599,958 |
| Actual RPS | 9,999.84 | 9,999.32 |
| Median | 0.163 ms | 0.141 ms |
| P95 | 0.374 ms | 0.212 ms |
| P99 | 1.23 ms | 0.445 ms |
| HTTP failures | 0% | 0% |
| Dropped iterations | 0 | 43 |
| Redis hits | N/A | 599,958 |
| Redis misses | N/A | 0 |

The Redis repeat run was used for latency comparison because it was performed without continuous resource sampling.

An earlier monitored Redis run at the same load completed 599,595 requests with:

- median = 0.141 ms
- P95 = 0.246 ms
- P99 = 0.715 ms
- 0 HTTP failures
- 405 dropped iterations

The repeat reduced dropped iterations from 405 to 43 while maintaining similar latency, showing that some variation came from the shared local benchmark environment.

## 15,000 RPS for 30 Seconds

| Metric | PostgreSQL Only | Redis |
|---|---:|---:|
| Completed requests | 449,973 | 449,582 |
| Actual RPS | 14,998.73 | 14,986.07 |
| Median | 0.289 ms | 0.205 ms |
| P95 | 1.07 ms | 0.363 ms |
| P99 | 8.23 ms | 0.733 ms |
| HTTP failures | 0% | 0% |
| Dropped iterations | 32 | 423 |
| Redis hits | N/A | 449,582 |
| Redis misses | N/A | 0 |

Redis significantly reduced tail latency at this load.

P99 decreased from:

`8.23 ms`

to:

`0.733 ms`

The Redis run had more dropped scheduled iterations than the PostgreSQL run despite much lower request latency.

Because the load generator and all services run on the same physical machine, dropped-iteration counts at these high rates are influenced by local scheduling and resource contention.

They should not be interpreted as an exact service-capacity boundary.

## 20,000 RPS for 30 Seconds

| Metric | PostgreSQL Only | Redis |
|---|---:|---:|
| Completed requests | 598,483 | 598,883 |
| Actual RPS | 19,948.68 | 19,961.45 |
| Median | 0.396 ms | 0.287 ms |
| P95 | 8.82 ms | 0.553 ms |
| P99 | 23.17 ms | 1.53 ms |
| HTTP failures | 0% | 0% |
| Dropped iterations | 1,518 | 1,118 |
| Redis hits | N/A | 598,883 |
| Redis misses | N/A | 0 |

This load shows the clearest difference between the two architectures.

PostgreSQL-only tail latency increased substantially:

~~~text
P95 = 8.82 ms
P99 = 23.17 ms
~~~

With Redis:

~~~text
P95 = 0.553 ms
P99 = 1.53 ms
~~~

P95 and P99 were both reduced by roughly 94%.

The Redis path also completed slightly more requests and dropped fewer scheduled iterations.

## Redis Hit Evidence

Redis command statistics were measured immediately before and after each benchmark.

### 10,000 RPS repeat

~~~text
Redis GETs   = 599,958
cache hits   = 599,958
cache misses = 0
~~~

### 15,000 RPS

~~~text
Redis GETs   = 449,582
cache hits   = 449,582
cache misses = 0
~~~

### 20,000 RPS

~~~text
Redis GETs   = 598,883
cache hits   = 598,883
cache misses = 0
~~~

For every executed request in these warm-cache runs, Redis recorded one successful cache hit.

## PostgreSQL Offload

During the monitored 10,000 RPS Redis run, PostgreSQL CPU usage was generally approximately:

`0% - 1%`

Redis CPU usage during the same active load samples was approximately:

`13% - 14%`

The Go API remained roughly around:

`60% - 66% CPU`

By comparison, the PostgreSQL-only 10,000 RPS baseline observed PostgreSQL CPU around:

`25% - 34%`

This demonstrates the main architectural benefit of the cache:

Redis removes repeated hot redirect reads from the source-of-truth database.

PostgreSQL statistics still changed slightly during the monitored run because other database activity and monitoring were present.

Therefore the defensible statement is not that PostgreSQL executed literally zero queries.

The directly measured statement is:

> Every executed benchmark redirect was a Redis cache hit with zero cache misses, while PostgreSQL CPU remained approximately 0-1% during the monitored Redis run.

## Redis Failure Behavior

Redis is not part of application correctness.

A failure experiment was performed with PostgreSQL healthy and Redis stopped.

Before fail-fast tuning:

~~~text
redirect status = 302
redirect latency ≈ 3.50 seconds
~~~

The redirect remained correct, but go-redis connection retries caused unacceptable latency.

The Redis client was then configured with:

- no command retries
- one dial attempt
- short dial/read/write timeouts
- context timeouts enabled

Cache operations were also given a 50 ms application-level timeout.

After tuning:

~~~text
readiness status = 200
redirect status = 302
redirect latency ≈ 0.003 seconds
~~~

The redirect fell back to PostgreSQL immediately.

When a Redis read fails, ShortScale also skips the cache write for that request instead of attempting another operation against an already failing cache.

## Cache Miss Behavior

Real-service testing also verified the cache-aside path.

With the Redis key deleted:

~~~text
Redis GET
  -> miss
PostgreSQL lookup
  -> success
Redis SET
  -> success
HTTP 302
~~~

The resulting Redis key had:

~~~text
TTL = 3600 seconds
~~~

After stopping PostgreSQL while leaving the cached value present, the same redirect still returned HTTP 302.

This proves the hot redirect was served from Redis without requiring PostgreSQL.

After PostgreSQL was restarted and the Redis key was deleted again, the next request successfully loaded the URL from PostgreSQL and repopulated Redis.

## Interpretation

Redis improved ShortScale in two separate ways.

### 1. Lower Tail Latency

The largest improvement appeared under higher load.

At 20,000 offered RPS:

~~~text
PostgreSQL P99 = 23.17 ms
Redis P99      = 1.53 ms
~~~

Redis prevented the large tail-latency growth seen in the PostgreSQL-only path.

### 2. Database Offload

For warm cached redirects, Redis becomes the read path while PostgreSQL remains the durable source of truth.

This protects PostgreSQL from repeated reads generated by hot short URLs.

That benefit is more important than the relatively small median-latency improvement seen at lower loads.

## Dropped Iterations

Dropped iterations were not monotonic across the benchmark runs.

For example:

- PostgreSQL 15k: 32 dropped
- Redis 15k: 423 dropped
- PostgreSQL 20k: 1,518 dropped
- Redis 20k: 1,118 dropped

At the same time, executed Redis requests maintained low latency and zero HTTP failures.

Because k6, the API, Redis, PostgreSQL, Docker, and monitoring tools share one machine, high-rate dropped-iteration counts include load-generator and operating-system scheduling effects.

For this reason, the benchmark does not claim a precise maximum RPS.

## Engineering Conclusion

The measured evidence supports retaining Redis in the ShortScale architecture.

The cache provides:

- substantially lower P95 and P99 latency under high load
- reduced PostgreSQL read pressure
- successful service of hot URLs without database access
- graceful fallback when Redis is unavailable
- bounded cache failure latency
- no correctness dependency on Redis persistence

The engineering cycle for this phase was:

~~~text
Measure PostgreSQL
        |
        v
Introduce Redis
        |
        v
Validate cache correctness
        |
        v
Test Redis failure
        |
        v
Measure identical workloads
        |
        v
Compare latency and database load
~~~

The Redis layer is therefore justified by measured behavior rather than by assumption.
