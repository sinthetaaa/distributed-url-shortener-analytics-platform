# PostgreSQL Redirect Performance Baseline

## Purpose

This benchmark establishes the performance baseline for ShortScale's redirect hot path before introducing Redis or any other caching layer.

The architecture under test is:

~~~text
k6
  |
  v
Go HTTP API
  |
  v
pgx connection pool
  |
  v
PostgreSQL
  |
  v
urls.short_code lookup
  |
  v
HTTP 302 redirect
~~~

Every redirect in this phase performs a PostgreSQL lookup.

The goal is not to claim production capacity. The goal is to measure the current single-node PostgreSQL-backed implementation, identify where latency begins to degrade, and create a baseline that later architectural changes can be compared against.

## Environment

Benchmark date: 2026-10-07

Machine:

- Apple M4
- 10 CPU cores
- 24 GiB RAM
- macOS
- API and k6 running natively on the same machine
- PostgreSQL running in Docker

Software:

- Go 1.27.1
- k6 2.3.0
- PostgreSQL 16 Alpine
- pgx v5 connection pool

PostgreSQL configuration observed before the benchmark:

- `max_connections = 100`
- `shared_buffers = 128MB`
- `effective_cache_size = 4GB`

The API uses `pgxpool.New(...)`.

No custom pgx pool sizing or performance tuning was applied.

## Workload

The benchmark targets:

`GET /3ZB9CeC`

The database row resolves to:

`https://example.com/shortscale-phase3`

k6 does not follow the returned redirect.

The request is considered correct only when:

1. the response status is `302`
2. the `Location` header matches the expected original URL

The workload uses k6's `constant-arrival-rate` executor.

## Important Benchmark Characteristics

This is a warm-cache local benchmark.

The same short code is requested repeatedly, which models a hot URL and means the relevant PostgreSQL table and index pages are expected to remain in memory.

The benchmark therefore does not represent cold disk access or geographically distributed production traffic.

The API, load generator, Docker runtime, and PostgreSQL also share the same physical machine. At high request rates, resource contention on the benchmark host can affect results.

For these reasons, the results are useful for relative architectural comparison, not as a production capacity guarantee.

## Results

| Offered RPS | Duration | Completed Requests | Median | P95 | P99 | HTTP Failures | Dropped Iterations |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 100 | 30s | 3,000 | 0.961 ms | 1.61 ms | 1.85 ms | 0% | 0 |
| 500 | 30s | 15,001 | 0.809 ms | 1.15 ms | 1.69 ms | 0% | 0 |
| 1,000 | 30s | 30,000 | 0.725 ms | 1.07 ms | 2.42 ms | 0% | 0 |
| 2,500 | 30s | 75,001 | 0.215 ms | 0.341 ms | 0.786 ms | 0% | 0 |
| 5,000 | 30s | 150,001 | 0.145 ms | 0.183 ms | 1.11 ms | 0% | 0 |
| 10,000 | 60s | 600,001 | 0.163 ms | 0.374 ms | 1.23 ms | 0% | 0 |
| 11,250 | 30s | 337,482 | 0.175 ms | 0.393 ms | 1.53 ms | 0% | 22 |
| 12,000 | 30s | 360,001 | 0.193 ms | 0.477 ms | 2.19 ms | 0% | 1 |
| 12,500 | 30s | 374,799 | 0.212 ms | 0.586 ms | 8.03 ms | 0% | 202 |
| 15,000 | 30s | 449,973 | 0.289 ms | 1.07 ms | 8.23 ms | 0% | 32 |
| 20,000 | 30s | 598,483 | 0.396 ms | 8.82 ms | 23.17 ms | 0% | 1,518 |

A repeated 2,500 RPS run produced almost identical latency results, confirming that the low latency at that load was reproducible after the system was warmed.

## 10,000 RPS Sustained Run

A 60-second run at 10,000 RPS completed:

~~~text
600,001 requests
9,999.84 requests/second
0% HTTP failures
median = 0.163 ms
P95 = 0.374 ms
P99 = 1.23 ms
~~~

This is the primary PostgreSQL-only reference point for future comparisons.

## Resource Observations at 10,000 RPS

### Go API

Observed CPU usage was approximately:

`62% - 67%`

Memory usage remained around:

`40 MiB RSS`

### PostgreSQL

Observed PostgreSQL container CPU usage was approximately:

`25% - 34%`

Memory usage remained around:

`39 MiB`

PostgreSQL generally reported:

~~~text
1-3 active connections
8-10 idle connections
~~~

The dominant observed PostgreSQL wait event was:

`ClientRead`

This indicates that PostgreSQL was frequently waiting for the client to issue another command rather than being blocked on locks or storage.

No evidence of PostgreSQL saturation was observed at 10,000 RPS.

## Interpretation

The PostgreSQL-backed redirect path comfortably handled 10,000 RPS in the warm local benchmark.

At 12,000 RPS, performance remained strong:

~~~text
P95 = 0.477 ms
P99 = 2.19 ms
HTTP failures = 0%
~~~

Only one k6 iteration was dropped during that run.

Above this range, tail latency became noticeably less stable.

At 12,500 RPS:

~~~text
P99 = 8.03 ms
dropped iterations = 202
~~~

At 15,000 RPS:

~~~text
P99 = 8.23 ms
~~~

At 20,000 RPS:

~~~text
P95 = 8.82 ms
P99 = 23.17 ms
dropped iterations = 1,518
~~~

The service still returned correct redirects for every executed HTTP request, but the load generator could no longer maintain the requested arrival rate without dropping scheduled iterations.

Because high-load tests are running on a shared local machine, individual dropped-iteration counts varied between runs.

The defensible conclusion is:

> The PostgreSQL-only ShortScale redirect path reliably sustained at least 10,000 RPS in the tested warm local environment. Performance remained strong around 12,000 RPS, while tail-latency instability became increasingly visible between roughly 12,500 and 15,000 RPS. At 20,000 RPS, significant tail-latency growth and dropped arrivals demonstrated clear saturation of the local benchmark setup.

## Why This Baseline Matters

Redis will not be added simply because URL shorteners commonly use caches.

The PostgreSQL implementation has now been measured first.

When Redis is introduced, the same workload can be repeated and compared against this baseline to determine:

- whether redirect latency improves
- whether P95 and P99 improve
- whether the clean sustainable request rate increases
- whether PostgreSQL load decreases
- how the system behaves for hot URLs
- whether the added architectural complexity is justified by measured results

This preserves the ShortScale engineering cycle:

~~~text
Build
  ->
Validate
  ->
Measure
  ->
Identify bottleneck
  ->
Evolve architecture
  ->
Measure again
~~~
