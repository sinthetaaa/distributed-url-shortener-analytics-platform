# Horizontal Scaling and Failure Benchmark

Date: 2026-10-07

## Purpose

Phase 7 measured ShortScale after moving from one API process to multiple stateless Go API replicas behind Nginx.

The experiments separated four concerns:

1. request distribution
2. availability during replica failure
3. local throughput scaling
4. process-local versus distributed request coalescing

These are local development benchmarks, not production capacity claims.

## Environment

The experiments ran locally on an Apple M4 development machine using Docker.

The main runtime topology was:

```text
Client / k6
     |
     v
Nginx :18080
     |
     +---- API replica 1
     |
     +---- API replica 2
     |
     +---- API replica 3
               |
          +----+----+
          |         |
          v         v
     PostgreSQL   Redis
```

PostgreSQL and Redis were shared by all API replicas.

## 1. Replica Distribution Test

### Goal

Verify that Nginx sends real traffic to every running API replica.

### Workload

```text
Rate:       300 RPS
Duration:   5 seconds
Requests:   1,500
Endpoint:   GET /3ZB9CeC
Expected:   HTTP 302
```

### Result

```text
HTTP failures: 0

API replica 1: 501 requests
API replica 2: 499 requests
API replica 3: 500 requests
```

Percentage distribution:

```text
API replica 1: 33.40%
API replica 2: 33.27%
API replica 3: 33.33%
```

Observed latency:

```text
median: 1.19 ms
p95:    1.76 ms
p99:    2.28 ms
```

### Conclusion

All three replicas received real application traffic.

The local workload was distributed almost evenly across the backend pool.

## 2. Replica Crash Experiment

### Goal

Determine whether ShortScale remains available when one API replica disappears abruptly.

### Workload

```text
Rate:       300 RPS
Duration:   15 seconds
Failure:    SIGKILL one API replica after 5 seconds
```

### Initial Configuration

```text
DNS refresh:       5 seconds
max_fails:         2
fail_timeout:      5 seconds
connect timeout:   500 ms
```

### Initial Result

```text
HTTP requests:     4,501
HTTP failures:     1
failure rate:      0.02%
max latency:       ~501 ms
```

Nginx logs showed that requests immediately after the crash could be retried successfully to surviving replicas.

Approximately six seconds later, the dead backend became eligible again while service discovery was also converging.

One request attempted the dead backend, waited approximately the configured 500 ms connection timeout, and produced a client-visible failure.

### Revised Configuration

```text
DNS refresh:       2 seconds
max_fails:         1
fail_timeout:      10 seconds
connect timeout:   500 ms
```

The failed-peer quarantine is therefore longer than the DNS refresh interval.

### Retest Result

```text
HTTP requests:     4,500
correct 302s:      4,500
HTTP failures:     0

average:           1.07 ms
median:            1.09 ms
p95:               1.41 ms
p99:               2.02 ms
maximum:           2.94 ms
```

At least one request was observed attempting the failed replica and then successfully retrying a healthy backend.

After DNS convergence:

```text
failed-replica references: 0

survivor A requests: 1,020
survivor B requests: 1,019
```

Readiness remained HTTP `200` while only two replicas were running.

The third replica was later restarted successfully.

### Conclusion

The final Nginx policy tolerated an abrupt API-process failure without exposing a failure to clients during the measured run.

## 3. One Replica vs Three Replicas

### Goal

Measure whether three API processes improve local redirect-path performance compared with one API process.

Both configurations used:

- the same machine
- the same Nginx instance
- the same PostgreSQL instance
- the same Redis instance
- the same hot URL
- the same k6 workload
- the same Docker environment

The Redis entry was warm.

### 10,000 RPS

#### One Replica

```text
actual throughput:      9,977.24 req/s
average latency:        0.557 ms
median:                 0.348 ms
p95:                    0.834 ms
p99:                    3.07 ms
HTTP failures:          0
dropped iterations:     328
```

#### Three Replicas

```text
actual throughput:      9,990.35 req/s
average latency:        0.476 ms
median:                 0.413 ms
p95:                    0.661 ms
p99:                    1.74 ms
HTTP failures:          0
dropped iterations:     138
```

At 10,000 RPS, the three-replica topology reduced tail latency and dropped iterations.

### 15,000 RPS

#### One Replica

```text
actual throughput:      14,985.04 req/s
average latency:        0.607 ms
median:                 0.508 ms
p95:                    1.09 ms
p99:                    2.54 ms
HTTP failures:          0
dropped iterations:     218
```

#### Three Replicas

```text
actual throughput:      14,942.70 req/s
average latency:        1.02 ms
median:                 0.559 ms
p95:                    3.00 ms
p99:                    10.38 ms
HTTP failures:          0
dropped iterations:     851
```

Compared with one replica:

```text
p95 latency:          ~175% higher
p99 latency:          ~309% higher
dropped iterations:   ~290% higher
```

At 15,000 RPS, three replicas were less efficient in the local single-host environment.

### 20,000 RPS

#### One Replica

```text
actual throughput:      19,958.25 req/s
average latency:        1.81 ms
median:                 0.788 ms
p95:                    7.73 ms
p99:                    16.29 ms
HTTP failures:          0
dropped iterations:     605
```

#### Three Replicas

```text
actual throughput:      19,883.77 req/s
average latency:        3.10 ms
median:                 1.10 ms
p95:                    13.17 ms
p99:                    25.70 ms
HTTP failures:          0
dropped iterations:     1,703
```

Compared with one replica:

```text
p95 latency:          ~70% higher
p99 latency:          ~58% higher
dropped iterations:   ~181% higher
```

At 20,000 offered RPS, the three-replica topology again produced worse tail latency and more dropped iterations.

## Throughput Conclusion

Horizontal replication did not provide a consistent throughput advantage in the local benchmark.

Its clearest measured benefit was availability.

The experiment ran every API replica, Nginx, Redis, PostgreSQL, Docker, and the load generator on the same physical development machine.

Additional replicas therefore competed for shared host resources.

These results do not predict how independent application hosts would scale in production.

The `docker stats` snapshots were captured after each benchmark, so their near-zero CPU readings are not valid evidence of peak CPU utilization and are not used to identify a bottleneck.

## 4. Process-Local Singleflight Experiment

### Goal

Determine whether Go `singleflight` continues to collapse same-key cache misses after the service is horizontally replicated.

### Method

The Redis entry for the benchmark short code was removed.

An exclusive PostgreSQL table lock was briefly used only as a test instrument.

The lock held each process's first PostgreSQL lookup in-flight long enough to inspect `pg_stat_activity`.

The lock is not representative of normal ShortScale operation.

### Experiment A: One Go Process

Twenty concurrent requests were sent directly to one API replica.

Observed:

```text
concurrent callers:       20
PostgreSQL SELECTs:       1
successful responses:     20 / 20
```

Result:

```text
20 callers
   |
   v
singleflight
   |
   v
1 PostgreSQL lookup
```

### Experiment B: Three Go Processes

Twenty concurrent requests were sent directly to each of three replicas.

Observed:

```text
API replica 1 -> 1 PostgreSQL lookup
API replica 2 -> 1 PostgreSQL lookup
API replica 3 -> 1 PostgreSQL lookup

total PostgreSQL SELECTs: 3
successful responses:     60 / 60
```

Result:

```text
20 callers -> API 1 -> singleflight -> 1 SELECT
20 callers -> API 2 -> singleflight -> 1 SELECT
20 callers -> API 3 -> singleflight -> 1 SELECT
```

### Conclusion

Go `singleflight` protects a single process.

It does not provide distributed coordination across API replicas.

Horizontal replication still bounded amplification by replica count in this experiment:

```text
without coalescing:
60 concurrent callers
could produce up to 60 DB lookups

with process-local singleflight:
60 concurrent callers across 3 replicas
produced 3 DB lookups
```

ShortScale therefore keeps process-local `singleflight` and does not currently add a distributed locking mechanism.

## Overall Phase 7 Findings

Phase 7 demonstrated that:

1. the API can run as multiple stateless replicas
2. PostgreSQL and Redis can be shared by those replicas
3. Nginx can dynamically discover and distribute requests across scaled containers
4. one replica can crash without necessarily producing client-visible failure
5. failover timing must be coordinated with service-discovery timing
6. horizontal scaling improves availability independently of throughput
7. more local replicas do not guarantee more local throughput
8. process-local coordination mechanisms remain process-local after scaling
9. cross-replica cache-miss amplification is bounded by replica count with the current design

## Important Benchmark Limitations

The results were obtained on one development machine.

They are not production capacity claims.

The tests do not model:

- independent application hosts
- multiple availability zones
- production network latency
- production load balancers
- remote PostgreSQL
- remote Redis
- noisy-neighbor effects
- autoscaling
- long-duration workloads
- production traffic distributions

The value of the benchmarks is comparative:

```text
same environment
same workload
one controlled architectural change
measured before/after behavior
```
