# Architecture Decision Records

This directory contains Architecture Decision Records (ADRs) for significant engineering decisions made during ShortScale's development.

ADRs preserve the context and trade-offs at the point a decision was made. Later architecture can evolve without rewriting the historical decision; when that happens, the original ADR receives an evolution note or is superseded by a new ADR.

## ADR index

| ADR | Decision | Status |
| --- | --- | --- |
| [001](001-random-base62-short-codes.md) | Random seven-character Base62 short-code generation | Accepted |
| [002](002-redis-cache-aside.md) | Redis cache-aside for redirect lookups | Accepted |
| [003](003-hot-key-cache-resilience.md) | Singleflight, TTL jitter, and Redis circuit breaking | Accepted |
| [004](004-horizontal-scaling.md) | Horizontal API scaling and Nginx load balancing in the Phase 7 local topology | Accepted; production evolution documented |
| [005](005-distributed-rate-limiting.md) | Redis-backed distributed URL-creation rate limiting with atomic Lua | Accepted; authentication-era evolution documented |
| [006](006-asynchronous-kafka-analytics.md) | Bounded asynchronous redirect analytics through Kafka | Accepted; resilience evolution documented |

## ADR structure

Each ADR should document:

1. **Context** — what problem or decision is being addressed
2. **Alternatives** — reasonable approaches considered
3. **Decision** — the selected approach
4. **Reasoning** — why it was selected
5. **Trade-offs** — advantages and disadvantages
6. **Consequences** — how the decision affects the system
7. **Evolution** — later production changes when the original decision remains historically useful but no longer describes the complete current state

## Policy

Create a new ADR when a new decision has meaningful architectural consequences.

Do not create speculative ADRs only to populate the directory.

When a later phase changes operational topology without invalidating the original engineering lesson, preserve the original ADR and append a clearly labeled evolution section.
