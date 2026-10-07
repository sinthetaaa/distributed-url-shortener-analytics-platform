# Redirect Analytics API: Integration and Performance

Date: 2026-10-07

## Purpose

Phase 10 exposed persisted redirect analytics through a bounded HTTP API and validated the complete asynchronous analytics pipeline.

The API provides:

```text
GET /api/v1/urls/{shortCode}/analytics?days=N
```

where:

```text
default days: 7
minimum:      1
maximum:      90
```

The response contains:

- all-time redirect count
- first redirect timestamp
- last redirect timestamp
- bounded UTC analytics window
- total redirects inside that window
- zero-filled daily redirect counts

Analytics is eventually consistent because redirect events reach PostgreSQL asynchronously through Kafka.

## End-to-End Architecture

```text
Client redirect
      |
      v
ShortScale API
      |
      +------> HTTP 302
      |
      v
bounded analytics queue
      |
      v
Kafka
      |
      v
analytics consumer
      |
      v
redirect_events
      |
      v
analytics SQL queries
      |
      v
GET /api/v1/urls/{shortCode}/analytics
```

The analytics read API does not alter the redirect-path availability guarantees established in Phase 9.

## API Semantics

Example:

```json
{
  "short_code": "SDoysLP",
  "total_redirects": 5,
  "first_redirect_at": "2026-10-07T15:44:03.090118Z",
  "last_redirect_at": "2026-10-07T15:44:03.140317Z",
  "window": {
    "days": 3,
    "start_at": "2026-10-05T00:00:00Z",
    "end_at": "2026-10-08T00:00:00Z",
    "redirects": 5
  },
  "daily": [
    {
      "date": "2026-10-05",
      "redirects": 0
    },
    {
      "date": "2026-10-06",
      "redirects": 0
    },
    {
      "date": "2026-10-07",
      "redirects": 5
    }
  ]
}
```

Daily buckets use UTC calendar days.

The end timestamp is exclusive.

For an existing short URL with no persisted redirect events:

```text
total_redirects:   0
first_redirect_at: null
last_redirect_at:  null
daily buckets:     zero-filled
```

A nonexistent short code returns HTTP 404.

Invalid `days` values return HTTP 400.

## Real Pipeline Integration Test

A unique URL was created through the real API.

Before any redirects, the analytics endpoint returned:

```text
total_redirects: 0
window redirects: 0
first_redirect_at: null
last_redirect_at: null
```

Five real HTTP redirects were then issued.

Observed:

```text
HTTP 302 responses:          5 / 5
persisted redirect_events:   5
analytics total_redirects:   5
analytics window redirects:  5
```

The daily response contained the five redirects on the correct UTC date and zero-filled the other requested days.

The integration therefore validated:

```text
API redirect
    ->
async publication
    ->
Kafka
    ->
consumer
    ->
PostgreSQL
    ->
analytics SQL
    ->
HTTP analytics response
```

The same live API also returned the expected HTTP 404 for an unknown short code and HTTP 400 for an invalid analytics window.

## Existing Analytics Index

Phase 9 created:

```sql
CREATE INDEX redirect_events_short_code_occurred_at_idx
    ON redirect_events (short_code, occurred_at DESC);
```

The analytics API uses two query patterns.

### All-Time Summary

```sql
SELECT
    COUNT(*)::bigint,
    MIN(occurred_at)::timestamptz,
    MAX(occurred_at)::timestamptz
FROM redirect_events
WHERE short_code = $1;
```

### Bounded Daily Window

```sql
SELECT
    (occurred_at AT TIME ZONE 'UTC')::date,
    COUNT(*)::bigint
FROM redirect_events
WHERE short_code = $1
  AND occurred_at >= $2
  AND occurred_at < $3
GROUP BY 1
ORDER BY 1;
```

## Small-Table Planner Experiment

With 88 redirect events in the local database, PostgreSQL naturally selected sequential scans.

Observed execution:

```text
all-time summary:  0.030 ms
daily query:       0.058 ms
```

This was expected because the entire table occupied only a few pages.

With sequential scans disabled, PostgreSQL demonstrated that the existing composite index was eligible for both query shapes.

For the bounded query, the index condition included:

```text
short_code equality
+
occurred_at lower bound
+
occurred_at upper bound
```

No additional index was justified.

## 100,000-Event Hot-Key Experiment

A temporary short code was populated with 100,000 redirect events spread across approximately 90 days.

The synthetic data was removed after measurement.

### All-Time Summary Plan

PostgreSQL selected a sequential scan because nearly the entire 100,000-row synthetic dataset belonged to the tested short code.

Observed:

```text
rows matched:       100000
execution time:     10.210 ms
```

This demonstrates an important scaling property:

```text
all-time COUNT + MIN + MAX
cost grows with historical event volume
for the requested short code
```

An additional ordinary index does not eliminate the aggregation work.

### Seven-Day Window Plan

The bounded query matched 7,777 events.

PostgreSQL selected:

```text
Index Only Scan
using redirect_events_short_code_occurred_at_idx
```

Observed:

```text
matched rows:       7777
heap fetches:       0
execution time:     2.087 ms
```

The index condition contained the short code and both timestamp bounds.

This validates the current index for bounded time-series analytics.

## HTTP Performance

The live ShortScale topology used three API replicas behind Nginx.

The hot short code contained 100,000 historical redirect events.

Each workload used a constant arrival rate for 10 seconds.

### Results

| Window | Target Rate | Completed | Median | p95 | p99 | Max | HTTP Failures |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 day | 50 req/s | 500 | 7.26 ms | 8.45 ms | 10.00 ms | 12.93 ms | 0.00% |
| 7 days | 50 req/s | 501 | 7.80 ms | 9.06 ms | 10.70 ms | 21.69 ms | 0.00% |
| 30 days | 50 req/s | 500 | 9.77 ms | 10.68 ms | 11.25 ms | 13.53 ms | 0.00% |
| 90 days | 50 req/s | 501 | 17.46 ms | 18.96 ms | 19.55 ms | 43.94 ms | 0.00% |
| 90 days | 200 req/s | 2000 | 19.56 ms | 25.51 ms | 31.78 ms | 44.19 ms | 0.00% |

These are local comparative measurements, not production capacity claims.

## Interpretation

Latency increased as the requested analytics window grew.

This is expected because a larger window requires scanning and grouping more matching redirect events.

The 90-day endpoint remained stable at approximately 200 requests per second during the local workload:

```text
completed requests: 2000
HTTP failures:      0
p95:                25.51 ms
p99:                31.78 ms
```

The current design is therefore sufficient for the present project scope.

## Identified Scaling Boundary

The bounded daily query already has an appropriate index.

The more important future scaling boundary is the all-time summary:

```text
COUNT(*)
MIN(occurred_at)
MAX(occurred_at)
```

These aggregates operate over the complete redirect history for one short code.

For extremely hot URLs with millions or billions of events, repeatedly deriving these values from raw events would eventually become unnecessarily expensive.

The appropriate future evolution would be pre-aggregation rather than adding another similar raw-event index.

Possible structures include:

```text
url_analytics_summary
---------------------
short_code
total_redirects
first_redirect_at
last_redirect_at
```

and:

```text
url_analytics_daily
-------------------
short_code
date
redirect_count
```

These could be maintained asynchronously by the analytics consumer.

That optimization is deliberately not implemented yet because current measurements do not justify the extra consistency and operational complexity.

## Phase 10 Conclusions

Phase 10 established that:

- analytics can be queried through a stable HTTP contract
- nonexistent URLs are distinguished from URLs with zero redirects
- daily buckets are bounded and zero-filled
- all calendar semantics are UTC-based
- analytics remains eventually consistent
- the real Kafka-to-PostgreSQL pipeline feeds the API correctly
- the existing composite analytics index supports bounded time queries
- PostgreSQL correctly avoids unnecessary index use on tiny datasets
- a 100,000-event hot key remains practical under the tested local workload
- larger analytics windows cost more than smaller windows
- all-time aggregation is the principal future scaling boundary
- pre-aggregation is the preferred future evolution when measurements justify it

No claim of production-scale capacity is made from these local experiments.
