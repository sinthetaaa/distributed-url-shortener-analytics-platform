# Distributed Rate Limiting: Correctness, Failure, and Performance

Date: 2026-10-07

## Purpose

Phase 8 added traffic protection to ShortScale and measured the design before and after introducing distributed coordination.

The experiments answered five questions:

1. Does a process-local limiter remain correct after horizontal scaling?
2. Can Redis provide one shared rate-limit state across replicas?
3. Does atomic Lua execution prevent concurrent over-consumption?
4. What happens when Redis becomes unavailable?
5. What latency and allocation cost does the distributed limiter add?

These are local development experiments, not production capacity claims.

## Policy Under Test

The protected endpoint is:

```text
POST /api/v1/urls
```

Initial policy:

```text
identity:        client IP
burst capacity:  5 requests
refill rate:     10 requests / minute
token refill:    ~1 token every 6 seconds
```

When limited, the API returns HTTP `429` with `Retry-After`.

## Architecture

The final Phase 8 topology is:

```text
                         ┌── API replica 1 ──┐
                         │                   │
Client ──> Nginx ────────┼── API replica 2 ──┼──> PostgreSQL
                         │                   │
                         └── API replica 3 ──┘
                                  |
                                  v
                                Redis
                                  |
                           atomic Lua bucket
```

All replicas use the same Redis key for the same client identity.

## 1. Process-Local Baseline

### Goal

Verify a token bucket in one Go process, then determine whether it remains a service-wide limit after adding replicas.

### One Replica

Six Immediate URL-creation requests were sent with the same identity.

Observed:

```text
201 Created:            5
429 Too Many Requests:  1
```

The process-local bucket correctly enforced its configured burst capacity.

### Three Replicas

Six requests were sent directly to each of three fresh API replicas using the same client identity.

Observed:

```text
API replica 1: 5 allowed, 1 limited
API replica 2: 5 allowed, 1 limited
API replica 3: 5 allowed, 1 limited

total requests: 18
allowed:        15
limited:        3
```

### Conclusion

The process-local limiter was correct only inside one process.

With three replicas:

```text
configured burst: 5
effective burst:  15
```

The effective capacity scaled approximately with replica count.

This established the need for shared state.

## 2. Shared Redis Bucket

### Goal

Verify that all API replicas consume one global bucket.

### Method

The process-local production limiter was replaced with a Redis-backed token bucket.

All three replicas used the same client identity and therefore the same Redis key.

### Result

```text
total requests: 18
201 Created:     5
429 responses:   13
```

Per-replica distribution was not required to be even.

The invariant was:

```text
allowed across all replicas <= global burst capacity
```

The experiment observed exactly five successful requests.

### Before / After

```text
process-local:
3 replicas
→ 15 burst requests allowed

shared Redis:
3 replicas
→ 5 burst requests allowed globally
```

## 3. Atomic Lua Concurrency Test

### Goal

Verify that concurrent limiter decisions cannot over-consume the shared bucket.

### Method

Three independent Redis limiter objects shared the same Redis instance and identity.

Sixty goroutines attempted limiter decisions concurrently.

The refill rate for this test was intentionally negligible so refill could not affect the initial burst measurement.

### Result

```text
concurrent attempts: 60
initial capacity:     5
allowed:              exactly 5
```

### Conclusion

The Lua state transition preserved the burst invariant under concurrent access.

The operation combines:

```text
read
refill
consume
persist
expire
```

inside one atomic Redis script invocation.

## 4. Redis Failure / Fail-Open Experiment

### Goal

Verify that Redis does not become a hard availability dependency for URL creation.

### Healthy Baseline

Before the outage:

```text
201 Created:            5
429 Too Many Requests:  1
```

### Redis Stopped

Redis was stopped while all three API replicas and PostgreSQL remained running.

Readiness during the outage:

```text
HTTP 200
```

Eight additional URL-creation requests were sent using the already exhausted identity.

Observed:

```text
201 Created:            8
429 Too Many Requests:  0
other statuses:         0
fail-open warnings:     8
```

Sample failure mode:

```text
distributed rate limiter unavailable; allowing request
```

The API continued serving writes through PostgreSQL.

### Redis Restarted

After Redis recovered and a fresh bucket was used:

```text
201 Created:            5
429 Too Many Requests:  1
```

No API restart was required.

### Conclusion

The limiter fails open.

This preserves write availability but temporarily removes distributed traffic protection during a Redis outage.

## 5. Traffic-Shaping Behavior

The final limiter was exercised through Nginx rather than directly against one API process.

### Test A: Concurrent Burst

Twenty requests were launched concurrently.

Observed:

```text
requests:   20
allowed:    5
limited:    15
other:      0
```

The initial burst was globally capped at five.

### Test B: Traffic at Refill Rate

Eight requests were sent approximately six seconds apart.

Observed:

```text
requests:   8
allowed:    8
limited:    0
other:      0
```

The workload approximately matched the configured one-token-per-six-seconds refill rate.

### Test C: Traffic Above Refill Rate

Twenty requests were sent approximately one second apart.

Observed:

```text
requests:   20
allowed:    8
limited:    12
other:      0
```

Request sequence:

```text
1   201
2   201
3   201
4   201
5   201
6   429
7   201
8   429
9   429
10  429
11  429
12  429
13  201
14  429
15  429
16  429
17  429
18  201
19  429
20  429
```

After the five-token initial burst was exhausted, later requests were admitted near refill boundaries while excess traffic continued to be throttled.

## 6. Client Identity Observation

The public-path experiment discovered the actual Redis limiter key after a probe request through Nginx.

Observed:

```text
shortscale:rate-limit:create-url:172.217.26.49
```

The important conclusion is not the specific development-machine address.

It is that the application identity depends on the trusted proxy/network path and should not be assumed from the originating host.

This validates keeping client-IP extraction explicit and testing it through the actual ingress topology.

## 7. Middleware Performance Benchmark

### Goal

Measure the isolated cost of adding the limiter to an otherwise minimal successful HTTP handler.

This benchmark does not execute URL creation or PostgreSQL work.

It compares the baseline handler

Local token bucket middleware
Redis + Lua token bucket middleware

```

The benchmark uses an intentionally huge capacity and refill rate so no request is rejected during the measurement.

### Environment


```text
OS:          darwin
architecture: arm64
CPU:         Apple M4
Redis:       local Docker container
Go benchmark CPU setting: 1
benchtime:   2 seconds
samples:     5 per implementation
```

### Raw Samples

#### Baseline

```text
2.488 ns/op
2.487 ns/op
2.520 ns/op
2.481 ns/op
2.481 ns/op
```

Median:

```text
2.487 ns/op
```

Allocations:

```text
0 B/op
0 allocs/op
```

#### Process-Local Token Bucket

```text
133.1 ns/op
132.0 ns/op
132.4 ns/op
131.7 ns/op
132.8 ns/op
```

Median:

```text
132.4 ns/op
```

Allocations:

```text
32 B/op
2 allocs/op
```

#### Redis + Lua Token Bucket

```text
100,967 ns/op
104,486 ns/op
104,506 ns/op
104,883 ns/op
101,962 ns/op
```

Median:

```text
104,486 ns/op
104.486 µs/op
0.104486 ms/op
```

Allocations:

```text
936 B/op
23 allocs/op
```

## Middleware Overhead

Subtracting the baseline median:

```text
local limiter:
132.4 - 2.487
≈ 129.9 ns/request

Redis limiter:
104,486 - 2.487
≈ 104,483.5 ns/request
≈ 104.48 µs/request
≈ 0.1045 ms/request
```

The Redis limiter is roughly 789 times slower than the local limiter in this microbenchmark.

That ratio is not the most useful operational interpretation because the local limiter is only about 0.00013 ms.

The more relevant measured cost is the Redis limiter's absolute local overhead:

```text
~0.10 ms per allowed request
```

## Performance Interpretation

The Redis-backed limiter intentionally trades a small synchronous network operation for service-wide correctness.

The local benchmark shows:

```text
process-local state:
very cheap
but incorrect as a global limit after scaling

Redis + Lua:
~104 µs median local middleware cost
and one globally shared atomic limit
```

The added cost is currently acceptable for the write path being protected.

This result does not justify putting the same limiter on the latency-sensitive redirect path without separate measurement and abuse requirements.

## Important Benchmark Limitations

The benchmark is intentionally narrow.

It excludes:

- Nginx
- external network latency
- PostgreSQL
- URL validation
- JSON decoding
- short-code generation
- PostgreSQL INSERT latency
- TLS
- remote Redis
- multi-host networking
- production concurrency patterns

Redis was running locally in Docker on the same development machine.

Therefore:

```text
104.486 µs/op
```

is not a production Redis latency estimate.

The useful comparison is:

```text
same Go benchmark
same machine
same middleware boundary
one controlled implementation change
```

## Overall Phase 8 Findings

Phase 8 demonstrated that:

1. a token bucket can enforce both burst and sustained-rate behavior
2. a process-local limiter does not provide a global limit after horizontal scaling
3. the effective local limit can multiply with replica count
4. Redis provides shared rate-limit state across API replicas
5. Lua preserves atomic token consumption under concurrent access
6. Redis server time avoids API-process clock disagreement for refill calculations
7. stale rate-limit state can expire automatically
8. fail-open behavior preserves URL-creation availability during Redis failure
9. fail-open behavior temporarily sacrifices traffic protection
10. rate limiting by client IP depends on a correct trusted-proxy boundary
11. the final token bucket behaves correctly under burst, refill-rate, and overload traffic
12. the Redis/Lua middleware adds about 104 µs median overhead in the local microbenchmark
13. the current distributed limiter belongs on the URL-creation write path, while redirect-path limiting remains deferred

## Reproducibility

The middleware benchmark is kept in:

```text
cmd/api/rate_limiter_benchmark_test.go
```

The Redis integration tests cover:

- shared state across limiter instances
- atomic Lua behavior under concurrency
- Redis result decoding

The process-local tests cover:

- initial burst behavior
- gradual refill
- independent client identities
- HTTP 429 behavior
- `Retry-After`
- client-IP extraction
- route-level enforcement

Together, these tests preserve both the local baseline and the distributed implementation behavior.
