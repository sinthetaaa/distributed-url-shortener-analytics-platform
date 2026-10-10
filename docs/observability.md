# ShortScale Observability

ShortScale uses metrics, dashboards, and distributed traces to observe the API, cache path, asynchronous redirect analytics pipeline, Kafka consumer, and PostgreSQL persistence without making telemetry a dependency of request availability.

> **Scope note:** the architecture and experiments in the main Phase 11 sections below record the local observability stack that was used when Phase 11 was completed. Production later evolved to Grafana Alloy on Railway forwarding private application metrics and OTLP traces to Grafana Cloud. The production topology is recorded in the **Production evolution after Phase 11** section below and in the deployment runbook.

## Architecture

```text
                         +----------------------+
                         |      Grafana         |
                         | dashboards + Explore |
                         +----------+-----------+
                                    |
                    +---------------+----------------+
                    |                                |
                    v                                v
             +-------------+                  +-------------+
             | Prometheus  |                  |    Tempo    |
             +------+------+                  +------+------+
                    ^                                ^
                    |                                |
         scrape API + consumer                 OTLP/gRPC
                    |                                |
         +----------+-----------+            +-------+--------+
         |                      |            | OTel Collector |
         v                      v            +-------+--------+
 +---------------+     +------------------+          ^
 | API replicas  |     | analytics        |          |
 | x3            |     | consumer         |----------+
 +-------+-------+     +---------+--------+
         |                       ^
         | redirect event        |
         v                       |
   local async queue -> Kafka ---+
```

PostgreSQL remains the source of truth. Redis accelerates redirect resolution, Kafka carries redirect analytics asynchronously, Prometheus scrapes metrics directly from the API replicas and consumer, and traces are exported through the OpenTelemetry Collector to Tempo.

Telemetry is intentionally fail-open: an unavailable trace backend must not make URL creation, redirects, cache fallback, Kafka publishing, or analytics persistence unavailable.

## Prometheus Metrics

### API

- `shortscale_http_requests_total{method,route,status}`
- `shortscale_http_request_duration_seconds{method,route}`
- `shortscale_http_requests_in_flight`
- `shortscale_cache_operations_total{operation,result}`
- `shortscale_rate_limit_decisions_total{result}`
- `shortscale_redirect_analytics_enqueues_total{result}`
- `shortscale_redirect_analytics_publishes_total{result}`
- `shortscale_redirect_analytics_queue_depth`

The HTTP route label is normalized. For example, redirects are recorded as `/{shortCode}` instead of using the actual short code as a label.

### Analytics consumer

- `shortscale_analytics_consumer_processed_total`
- `shortscale_analytics_consumer_failures_total{stage}`
- `shortscale_analytics_consumer_persistence_total{result}`
- `shortscale_analytics_consumer_lag_records`
- `shortscale_analytics_consumer_lag_collection_failures_total`

Consumer lag is collected from Kafka group offsets rather than inferred from processed-event counts.

## Grafana

The provisioned `ShortScale Overview` dashboard contains operational panels for:

- API replicas and consumer availability
- request rate
- p50, p95, and p99 HTTP latency
- HTTP error rate
- requests in flight
- cache operations
- distributed rate-limit decisions
- async analytics enqueue outcomes
- Kafka publish outcomes
- consumer throughput
- Kafka consumer lag
- PostgreSQL persistence outcomes
- consumer failures
- lag-collection failures

Grafana also provisions Tempo as a trace datasource for trace inspection through Explore.

## Distributed Tracing

The Go services use OpenTelemetry with W3C Trace Context and Baggage propagation. OTLP traces are sent to the OpenTelemetry Collector and exported to Tempo.

Health and metrics endpoints are excluded from HTTP tracing to avoid observability-generated trace noise.

HTTP root span names are bounded:

- `HTTP GET`
- `HTTP POST`

Short codes and original URLs are not embedded in span names.

### Redirect resolution path

A redirect can expose either the database fallback path:

```text
HTTP GET
└── redirect.resolve
    ├── redirect.cache.get          result=miss
    ├── redirect.database.lookup    result=success
    └── redirect.cache.set          result=success
```

or the cache-hit path:

```text
HTTP GET
└── redirect.resolve
    └── redirect.cache.get          result=hit
```

Low-cardinality attributes describe outcomes such as `hit`, `miss`, `success`, `error`, `circuit_open`, `cache`, and `database`.

### Asynchronous redirect analytics path

Trace context is propagated through Kafka record headers instead of being added to the analytics event JSON contract.

```text
HTTP GET
└── redirect.analytics.enqueue
    └── redirect.analytics.publish
        |
        | W3C trace context in Kafka headers
        v
    redirect.analytics.process
    └── redirect.analytics.persist
```

The consumer process span is a child of the API Kafka publish span, proving that the same trace crosses the asynchronous service boundary.

Historical Kafka records without trace headers remain compatible and can be processed normally.

## Runtime Validation

### Kafka consumer lag experiment

With the analytics consumer stopped, 30,000 real redirects were generated.

Observed behavior:

- 30,000 redirects returned HTTP 302.
- Kafka consumer lag reached 30,000 records.
- After the consumer restarted, processing reached 30,000 events.
- Lag drained back to 0.
- Lag collection failures remained 0.

This validated that `shortscale_analytics_consumer_lag_records` measures actual Kafka backlog rather than application-local processing counters.

### Trace propagation experiment

A known W3C `traceparent` was injected into a real redirect request.

Tempo showed one trace containing both:

- `shortscale-api`
- `shortscale-analytics-consumer`

The verified parent relationships were:

```text
incoming W3C parent
        |
        v
HTTP GET
        |
        v
redirect.analytics.publish
        |
        v
redirect.analytics.process
        |
        v
redirect.analytics.persist
```

The consumer process span's parent ID matched the Kafka publish span's span ID, and the persistence span's parent ID matched the consumer process span's span ID.

### Redirect cache-path experiment

A newly created short URL was explicitly removed from Redis before its first redirect.

The first request produced:

- HTTP 302
- cache miss
- successful PostgreSQL lookup
- successful Redis refill
- resolution source `database`

A second request to the same short URL produced:

- HTTP 302
- cache hit
- no PostgreSQL lookup span
- no cache refill span
- resolution source `cache`

## Telemetry Failure Isolation

The OpenTelemetry Collector was deliberately stopped while the API, Redis, Kafka, PostgreSQL, Prometheus, Tempo, and analytics consumer remained available.

A load of 500 redirects with 25 concurrent workers produced:

- 500 / 500 HTTP 302 responses
- approximately 4,069.99 requests/second from the local client
- client p50: 5.25 ms
- client p95: 11.05 ms
- client p99: 16.74 ms
- Prometheus redirect counter delta: exactly 500
- async analytics queue depth after the run: 0
- no API container restarts

All three API container IDs remained identical before, during, and after the Collector outage.

After the Collector restarted, a new known trace appeared in Tempo without restarting any API replica.

This demonstrates that trace-export infrastructure is not on the availability path.

## Healthy-Stack Load Experiment

With the complete observability stack healthy and 100% local trace sampling enabled, 2,000 redirects were generated with 50 concurrent workers.

Client-observed result:

- 2,000 / 2,000 HTTP 302 responses
- approximately 4,841.93 requests/second
- client p50: 8.96 ms
- client p95: 21.45 ms
- client p99: 32.32 ms

Prometheus server-side histogram for exactly those 2,000 requests:

- p50: 0.65 ms
- p95: 5.12 ms
- p99: 20.17 ms

Cross-component reconciliation after the run:

- HTTP 302 delta: 2,000
- analytics enqueue delta: 2,000
- analytics drops: 0
- Kafka publish-success delta: 2,000
- Kafka publish failures: 0
- cache operations: 2,000 `get/hit`
- consumer processed delta: 2,000
- PostgreSQL inserted delta: 2,000
- HTTP error delta: 0
- consumer failure delta: 0
- final async queue depth: 0
- final Kafka consumer lag: 0

A known post-load trace also reached Tempo successfully, proving that tracing remained functional after the burst.

The client latency includes local Nginx, TCP connection setup, scheduling, and client-side overhead. The Prometheus histogram measures server-side request handling and is therefore the more direct view of application latency.

The Collector-outage and healthy-stack runs used different request counts, so they are treated as separate resilience/load experiments and not as a controlled tracing-overhead benchmark.

## Production evolution after Phase 11

The Phase 11 local stack used Prometheus, Tempo, and the OpenTelemetry Collector to establish the observability model.

The deployed Phase 15 topology keeps the same application metrics and OpenTelemetry instrumentation while changing the collection backend:

```text
Railway API :9090 -----------+
                              |
Railway consumer :9091 -------+--> Railway Grafana Alloy
                              |         |
API + consumer OTLP :4317 ----+         +--> Grafana Cloud metrics
                                        +--> Grafana Cloud traces
```

Production invariants:

```text
public API /metrics → 404
API metrics          → private :9090
consumer metrics     → private :9091
application OTLP     → private Alloy :4317
Alloy public domain  → none
```

Production validation confirmed:

- API and consumer metrics were available through the private telemetry path
- consumer lag was zero in the healthy state
- consumer failures were zero in the healthy state
- a real redirect incremented the consumer processed counter
- a distributed trace crossed API → Kafka → consumer → PostgreSQL persistence
- an unreachable OTLP destination did not make the API or consumer unavailable

The production fault test establishes telemetry fail-open behavior. It does not establish lossless telemetry delivery while the exporter destination is unavailable.

See:

- [Production deployment and operations](production-deployment.md)
- [Production validation](production-validation.md)

## Operational Principles

ShortScale's observability implementation follows these rules:

1. Telemetry failure must not become application failure.
2. PostgreSQL remains the source of truth.
3. Redirect analytics remain asynchronous.
4. Prometheus metric labels use bounded cardinality.
5. Trace span names remain bounded and do not include short codes or URLs.
6. Trace context crosses asynchronous Kafka boundaries through headers, not domain JSON.
7. Health and metrics traffic is excluded from tracing.
8. Consumer lag is measured from Kafka group state.
9. Runtime experiments validate behavior instead of relying only on unit tests.
10. Load results are reported only from measurements actually produced by the local environment.

## Current Phase 11 Status

- 11A Prometheus metrics foundation — complete
- 11B HTTP metrics and `/metrics` — complete
- 11C domain metrics — complete
- 11D analytics-consumer metrics — complete
- 11E Prometheus scrape topology — complete
- 11F Grafana dashboard and Kafka lag — complete
- 11G OpenTelemetry Collector, Tempo, and tracing foundation — complete
- 11H important request and Kafka paths — complete
- 11I observability failure and load experiments — complete
- 11J documentation and final review — complete
