# ADR 005: Distributed Rate Limiting with Redis and Atomic Lua

- Status: Accepted
- Date: 2026-10-07

## Context

ShortScale runs multiple stateless Go API replicas behind Nginx.

Phase 7 proved that process-local coordination remains process-local after horizontal scaling. Phase 8 applied that lesson to traffic protection.

The URL-creation endpoint performs durable work:

```text
POST /api/v1/urls
        |
        v
short-code generation
        |
        v
PostgreSQL INSERT
```

Without rate limiting, one client can create unbounded write pressure.

The redirect path is different. It is latency-sensitive, heavily cached, and designed for high read throughput. Adding a synchronous distributed limiter to every redirect would place another Redis operation directly on that critical path.

The first Phase 8 implementation therefore protected URL creation and deliberately used process-local state so its distributed limitation could be measured before introducing shared coordination.

## Decision

ShortScale rate-limits `POST /api/v1/urls` with a token bucket whose authoritative state is stored in Redis.

The initial policy is:

```text
refill rate:     10 requests / minute / client IP
burst capacity:  5 requests
```

The production request path is:

```text
Client
  |
  v
Nginx
  |
  v
API replica
  |
  v
rate-limit middleware
  |
  v
Redis Lua script
  |
  +-- allowed --> URL creation --> PostgreSQL
  |
  +-- limited --> HTTP 429
```

All API replicas use the same Redis key for the same client identity.

Token refill, token consumption, state persistence, and TTL refresh execute atomically inside one Redis Lua script.

## Why Token Bucket

A token bucket was chosen instead of a fixed-window counter.

A fixed window can permit boundary bursts. For example, a client can consume the full quota at the end of one window and immediately consume another full quota at the beginning of the next.

A token bucket provides two independent controls:

1. a sustained refill rate
2. a bounded burst capacity

For the current URL-creation policy:

```text
capacity:        5 tokens
refill:          1 token every ~6 seconds
```

This allows short legitimate bursts while constraining sustained write pressure.

## Why the First Implementation Was Process-Local

The first implementation stored one token bucket per client identity in each Go process.

That implementation was intentionally useful as a baseline because it made the distributed failure mode measurable.

One process correctly enforced:

```text
6 immediate requests
→ 5 allowed
→ 1 rejected
```

With three API replicas, however, the same client identity received three independent buckets:

```text
API 1: capacity 5
API 2: capacity 5
API 3: capacity 5
```

The controlled experiment observed:

```text
18 requests across 3 replicas
→ 15 allowed
→ 3 rejected
```

The intended service-wide burst capacity was 5, but the effective burst capacity became 15.

Therefore a process-local limiter does not provide a service-wide rate limit after horizontal scaling.

## Shared Redis State

The distributed limiter stores state under keys shaped as:

```text
shortscale:rate-limit:create-url:<client-ip>
```

The state contains:

```text
tokens
last_refill_ms
```

Every API replica reads and modifies the same bucket for the same client identity.

A three-replica experiment using the shared Redis limiter produced:

```text
18 requests across 3 replicas
→ 5 allowed
→ 13 rejected
```

This restored the intended global burst capacity.

## Atomic Lua Execution

A naive distributed implementation could perform:

```text
GET state
calculate refill
consume token
SET state
```

as separate Redis operations.

That would create a race:

```text
replica A reads 1 token
replica B reads 1 token
replica A consumes it
replica B also consumes it
```

ShortScale instead executes the complete decision inside one Redis Lua script.

The script:

1. reads Redis server time
2. reads the existing bucket
3. calculates elapsed refill
4. caps tokens at bucket capacity
5. decides whether one token can be consumed
6. calculates retry delay when rejected
7. stores the updated state
8. refreshes the state TTL
9. returns the decision

Redis executes one Lua script atomically with respect to other commands.

A concurrency integration test issued 60 simultaneous decisions through three independent limiter objects sharing one Redis instance.

Exactly five requests consumed the initial five-token burst.

## Redis Server Time

The token bucket uses Redis `TIME` as the clock for shared state.

This avoids using each API process's local wall clock for refill calculations.

Without a shared clock, clock skew between replicas could cause inconsistent refill behavior.

Redis therefore provides both:

- shared bucket state
- shared time authority for that state

## State Expiration

Rate-limit keys are temporary.

The TTL is long enough for the bucket to refill fully and is currently at least 60 seconds for the configured policy.

After an identity becomes inactive, its bucket expires automatically.

This prevents inactive client identities from accumulating indefinitely in Redis.

## Client Identity

The current limiter key is based on client IP.

In the deployed local topology, Nginx is the trusted ingress and overwrites:

```text
X-Real-IP
```

with the address it observes.

The application prefers:

1. `X-Real-IP`
2. the right-most valid `X-Forwarded-For` address
3. `RemoteAddr`

This trust boundary matters.

Client-supplied forwarding headers must not be trusted when the API is directly exposed to untrusted traffic.

The Phase 8 public-path experiment also showed that the identity observed through Docker/Nginx was not necessarily loopback even when the request originated from the local development machine.

Rate limiting by IP is therefore correct only when proxy ownership and header rewriting are understood and controlled.

## HTTP Rejection Behavior

When the token bucket is exhausted, ShortScale returns:

```text
HTTP 429 Too Many Requests
Retry-After: <seconds>
Content-Type: application/json
```

with:

```json
{"error":"rate limit exceeded"}
```

`Retry-After` is derived from the amount of time required for enough token credit to become available.

## Redis Failure Policy

The distributed limiter fails open.

If Redis cannot execute the limiter operation:

```text
request
   |
   v
Redis limiter error
   |
   +--> warning log
   |
   +--> allow request
```

The URL-creation request is still allowed to proceed to PostgreSQL.

This is intentional.

PostgreSQL remains the correctness dependency for URL creation. Redis should not become a new availability dependency merely because it also stores rate-limit coordination state.

## Failure Experiment

Redis was deliberately stopped after exhausting one client's bucket.

Before the outage:

```text
5 requests allowed
1 request limited
```

During the outage:

```text
8 requests attempted
8 requests allowed
0 requests limited
readiness: HTTP 200
fail-open warnings: 8
```

After Redis restarted:

```text
5 requests allowed
1 request limited
```

The distributed limiter resumed automatically.

## Fail-Open Trade-off

Fail-open prioritizes availability over traffic protection.

### Benefit

A Redis outage does not make core URL creation unavailable.

### Cost

During the outage, abusive clients are temporarily not constrained by the distributed limiter.

ShortScale accepts this trade-off for the current architecture.

Potential future mitigations include:

- a process-local emergency fallback limiter
- a dedicated rate-limit Redis deployment
- multi-node Redis
- different fail-open/fail-closed policies by endpoint
- edge or load-balancer rate limiting

These are not added until measurements justify the operational complexity.

## Redirect Endpoint Decision

Phase 8 does not add the same synchronous Redis limiter to `GET /{shortCode}`.

The redirect path is the service's critical high-throughput read path.

Adding a Redis rate-limit decision to every redirect would:

- add another network dependency to every request
- increase latency on cache hits
- couple redirect availability to limiter failure handling
- add Redis load even when redirect caching is healthy

The current decision is therefore:

```text
POST /api/v1/urls
→ distributed Redis rate limiting

GET /{shortCode}
→ no distributed per-request limiter yet
```

A redirect limiter can be revisited when abuse requirements, production traffic, or edge infrastructure justify it.

## Traffic-Shaping Validation

The final Redis-backed token bucket was tested through the public Nginx entry point.

### Concurrent Burst

```text
20 simultaneous requests
→ 5 allowed
→ 15 limited
```

### Traffic at the Refill Rate

Eight requests were sent approximately six seconds apart.

```text
8 allowed
0 limited
```

### Traffic Above the Refill Rate

Twenty requests were sent approximately one second apart.

```text
8 allowed
12 limited
```

The initial burst was consumed, later requests were admitted as tokens refilled, and excess traffic continued to receive HTTP 429.

## Performance Cost

A middleware microbenchmark compared:

1. terminal handler only
2. process-local token bucket
3. Redis-backed Lua token bucket

Median results across five runs on the local Apple M4 development machine were:

```text
baseline:          2.487 ns/op
local limiter:     132.4 ns/op
Redis + Lua:       104,486 ns/op
```

Approximate added cost:

```text
local limiter:     ~129.9 ns/request
Redis + Lua:       ~104.48 µs/request
```

The Redis-backed limiter also measured:

```text
936 B/op
23 allocs/op
```

This benchmark used local Docker Redis and deliberately excluded PostgreSQL, Nginx, JSON processing, and external network latency.

It is a comparative local middleware benchmark, not a production latency claim.

## Alternatives Considered

### Process-Local Token Bucket

Advantages:

- minimal latency
- no network dependency
- simple implementation

Rejected as the production design because the effective service-wide limit multiplied with replica count.

### Fixed-Window Redis Counter

Advantages:

- simple Redis representation
- easy to reason about

Rejected because boundary bursts do not match the desired controlled-burst behavior.

### Multiple Redis Commands with Optimistic Coordination

Advantages:

- avoids Lua
- individual commands are simple

Rejected because correctness would require additional transaction/watch logic and more round trips.

### Distributed Redis Token Bucket with Lua

Advantages:

- one shared bucket
- atomic state transition
- one Redis round trip
- shared Redis clock
- controlled burst and sustained rate

Selected.

### Fail Closed on Redis Failure

Advantages:

- traffic protection remains strict

Rejected for the current architecture because an optional coordination dependency would become a hard availability dependency for URL creation.

## Consequences

### Positive

- one service-wide limit across horizontally scaled API replicas
- atomic token consumption under concurrency
- controlled burst behavior
- sustained refill behavior
- automatic stale-state expiration
- clear HTTP 429 semantics
- Redis outage does not make URL creation unavailable
- reusable benchmark coverage for limiter overhead

### Negative

- every protected successful request adds a Redis round trip
- local benchmark overhead is roughly 104 µs per allowed request
- Redis failure temporarily disables distributed traffic protection
- IP identity depends on correct trusted-proxy configuration
- the limiter adds Redis memory and command load
- rejected requests still require the limiter round trip
- the current policy protects URL creation but not the redirect path

## Revisit When

Revisit this decision if:

- ShortScale moves behind a managed edge proxy or API gateway
- authenticated user/account identity becomes available
- NAT causes unacceptable false sharing between clients
- Redis limiter latency becomes meaningful
- Redis availability becomes insufficient for abuse protection
- redirect-path abuse requires protection
- different endpoints require different failure policies
- rate-limit policy needs to vary by customer or authentication tier
- production measurements justify a local emergency fallback limiter

## Authentication-era and production evolution

The original Phase 8 decision predates user authentication.

Authentication now exists, but the production URL-creation limiter deliberately remains keyed by client IP rather than user ID.

Current URL-creation policy remains:

```text
burst capacity: 5
refill rate:    10 requests/minute
coordination:   Redis + atomic Lua
failure mode:   fail open
```

Registration and login use a separate process-local client-IP token bucket:

```text
burst capacity: 5
refill rate:    5 requests/minute
```

The authentication limiter is intentionally not presented as a distributed global account-abuse system.

Production security testing also verified the deployed proxy/header path rather than assuming that an arbitrary client-supplied forwarding header could bypass the effective limiter identity.

The availability trade-off remains unchanged:

```text
Redis unavailable
→ distributed create limiter unavailable
→ request allowed
→ PostgreSQL remains the correctness dependency
```

Authentication therefore created a possible future identity for rate limiting, but it did not automatically justify changing the established production create policy.

A user-ID-based policy should be introduced only if product/abuse requirements justify how limits should behave across accounts, shared networks, unauthenticated endpoints, and multiple sessions.

See [Authentication, sessions and ownership](../authentication.md).
