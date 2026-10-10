# ADR 004: Horizontal API Scaling and Load Balancing

- Status: Accepted
- Date: 2026-10-07

## Context

ShortScale originally ran as a single Go API process.

That architecture was enough to validate PostgreSQL persistence, Redis cache-aside behavior, hot-key protection, TTL jitter, and Redis failure handling, but a single API process is also a single application-level failure domain.

Phase 7 introduced horizontal API replication to answer four separate questions:

1. Can multiple stateless ShortScale API processes share the same PostgreSQL and Redis services?
2. Can requests be distributed across those replicas through a load balancer?
3. Can the service remain available if one replica crashes abruptly?
4. What happens to process-local mechanisms such as `singleflight` when multiple replicas exist?

Availability, throughput, and coordination behavior were measured separately.

## Decision

ShortScale runs multiple stateless Go API replicas behind Nginx.

The local Phase 7 topology is:

```text
                         ┌── API replica 1 ──┐
                         │                   │
Client ──> Nginx ────────┼── API replica 2 ──┼──> PostgreSQL
                         │                   │
                         └── API replica 3 ──┘
                                  │
                                  └────────────> Redis
```

PostgreSQL remains the source of truth.

Redis remains an optional acceleration layer.

API replicas contain no durable URL state.

## Container Model

The Go API uses a multi-stage Docker build.

The runtime image:

- contains only the compiled ShortScale binary
- runs without CGO
- uses a `scratch` runtime
- runs as a non-root user
- exposes port `8080`

Docker Compose is used locally to create multiple identical API replicas.

The API service has no host-published port. Only Nginx is exposed to the host.

## Shared Dependencies

All API replicas share:

- PostgreSQL
- Redis

The following state remains process-local:

- Go `singleflight.Group`
- Redis circuit-breaker state
- database connection pool
- HTTP server state

Horizontal replication therefore creates multiple independent copies of all in-memory coordination mechanisms.

## PostgreSQL Dependency

PostgreSQL is required for correctness.

The API therefore depends on PostgreSQL being healthy during Compose startup.

If PostgreSQL is unavailable, a cache miss cannot be recovered from the authoritative datastore.

## Redis Dependency

Redis is not required for correctness.

The API does not make Redis a Compose startup dependency.

If Redis is unavailable:

1. the API still starts
2. readiness remains based on PostgreSQL
3. redirects fall back to PostgreSQL
4. the cache circuit breaker limits repeated Redis failures

This preserves the earlier design principle that Redis is an optimization rather than a source of truth.

## Load Balancer

Nginx is the public entry point for the horizontally replicated API.

The local host endpoint is:

```text
localhost:18080
```

Nginx forwards traffic to:

```text
api:8080
```

using Docker's embedded DNS instead of hard-coded container names.

This lets the API replica count change without rewriting Nginx configuration.

## Load-Balancing Strategy

Nginx uses:

```nginx
least_conn;
```

The backend with the fewest active connections is preferred.

## Dynamic Service Discovery

Nginx uses Docker's DNS server:

```nginx
resolver 127.0.0.11 valid=2s ipv6=off;
```

and dynamically resolves:

```nginx
server api:8080 resolve;
```

The upstream is stored in a shared-memory zone so Nginx workers can observe updated backend membership.

## Replica Failure Policy

The tested upstream failure policy is:

```nginx
server api:8080 resolve max_fails=1 fail_timeout=10s;
```

The final timing relationship is:

```text
DNS refresh interval:      2 seconds
failed-peer quarantine:   10 seconds
```

This relationship is intentional.

An earlier configuration used:

```text
DNS refresh interval:      5 seconds
failed-peer quarantine:    5 seconds
```

During an abrupt replica crash, that created a race. A failed replica could become eligible again at approximately the same time Nginx was due to refresh Docker DNS.

The failure experiment produced one client-visible error under that configuration.

The final configuration disables a backend after the first failed attempt and keeps it unavailable long enough for DNS membership to converge.

## Retry Policy

Nginx retries eligible upstream failures:

```nginx
proxy_next_upstream error timeout http_502 http_503 http_504;
proxy_next_upstream_tries 3;
```

The experiment showed that Nginx could attempt the just-failed replica and transparently retry a healthy replica without exposing that failed upstream attempt to the client.

## Traffic Distribution

A controlled 300 RPS benchmark over 5 seconds produced 1,500 successful redirects.

Traffic distribution was:

```text
API replica 1: 501 requests
API replica 2: 499 requests
API replica 3: 500 requests
```

All requests returned the expected HTTP `302`.

This proved that all three replicas were receiving real traffic through Nginx.

## Crash Resilience

A 300 RPS benchmark was run for 15 seconds.

After five seconds, one API replica was terminated abruptly with `SIGKILL`.

The initial configuration exposed one client-visible failure and revealed the DNS/failure-timeout race described above.

After adjusting the service-discovery and failed-peer timing:

```text
requests:       4,500
correct 302s:   4,500
HTTP failures:  0
```

After Docker DNS refreshed, the failed replica no longer appeared in upstream traffic.

The two surviving replicas continued serving requests, and the third replica later rejoined successfully.

## Horizontal Scaling and Throughput

Horizontal replication improved availability, but it did not consistently improve throughput in the single-machine benchmark environment.

At 10,000 RPS, three replicas slightly improved tail latency and reduced dropped iterations.

At 15,000 and 20,000 RPS, three replicas produced worse tail latency and more dropped iterations than one replica.

This does not imply that horizontal scaling is inherently slower.

All replicas, Nginx, Redis, PostgreSQL, Docker, and the load generator were competing for resources on the same development machine.

The Phase 7 benchmark therefore describes local process/container behavior, not production multi-host scaling efficiency.

## Process-Local Singleflight

ShortScale uses Go `singleflight` to coalesce simultaneous cache misses for the same short code.

Within one process:

```text
20 concurrent same-key cache misses
→ 1 PostgreSQL lookup
```

With three independent API processes:

```text
20 requests → API 1 → 1 PostgreSQL lookup
20 requests → API 2 → 1 PostgreSQL lookup
20 requests → API 3 → 1 PostgreSQL lookup
```

The experiment observed exactly three simultaneous PostgreSQL lookups.

This confirms that `singleflight` is process-local.

It does not provide distributed request coalescing.

## Distributed Lock Decision

ShortScale does not currently add a distributed lock for cross-replica cache-miss coalescing.

The measured amplification is bounded approximately by replica count rather than by concurrent caller count.

For example:

```text
60 simultaneous callers across 3 replicas
→ 3 PostgreSQL lookups
```

rather than 60 lookups.

Adding distributed coordination would introduce:

- extra Redis operations
- lock ownership and expiry concerns
- additional failure-mode complexity
- extra latency on cold misses
- another distributed mechanism to operate and reason about

The current tradeoff is accepted.

This can be revisited if future measurements show meaningful database pressure from cross-replica cold-miss duplication.

## Consequences

### Positive

- no single API-process failure domain
- stateless API replicas
- dynamic traffic distribution
- abrupt replica failure can be hidden from clients
- API processes can be scaled independently
- Redis remains optional for correctness
- Docker service discovery avoids hard-coded replica identities

### Negative

- Nginx is another runtime component
- service-discovery timing must be configured carefully
- circuit-breaker state is independent per replica
- `singleflight` state is independent per replica
- local replication can increase single-host resource contention
- local replica count is not equivalent to independent-machine capacity

## Revisit When

Revisit this decision if:

- ShortScale is deployed across multiple hosts
- PostgreSQL cold-miss traffic becomes a bottleneck
- cross-replica cache stampedes become measurable
- an orchestrator replaces Docker Compose
- active health checks become necessary
- Nginx becomes a measurable bottleneck
- per-replica circuit-breaker divergence becomes operationally significant

## Production evolution after Phase 7

This ADR records the Phase 7 **local horizontal-scaling experiment**.

Its Nginx topology remains important evidence that the API is stateless, that multiple replicas can share PostgreSQL/Redis, and that process-local coordination such as `singleflight` does not become distributed automatically.

It is not the current production ingress topology.

Phase 15 production uses:

```text
Vercel frontend
      ↓
same-origin API proxy
      ↓
Railway shortscale-api
```

The deployed production architecture does not place the Phase 7 Nginx container in front of the Railway API.

Therefore, claims from the Phase 7 three-replica/Nginx experiments must remain scoped to that controlled local environment.

Current production documentation does not claim multi-host or multi-region API failover merely because the earlier local topology demonstrated replica-level failure tolerance.

See:

- [Production deployment and operations](../production-deployment.md)
- [Production validation](../production-validation.md)
