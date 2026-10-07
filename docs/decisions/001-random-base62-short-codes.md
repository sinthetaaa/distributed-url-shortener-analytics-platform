# ADR 001: Random Base62 Short-Code Generation

## Status

Accepted

## Context

ShortScale needs to generate compact public identifiers for shortened URLs.

A short-code strategy affects several properties of the system:

- URL length
- predictability of generated codes
- database coupling
- horizontal scalability
- collision handling
- coordination requirements between API instances

The initial implementation uses seven-character short codes.

With a Base62 alphabet containing digits, lowercase letters, and uppercase letters, the available namespace is:

`62^7 = 3,521,614,606,208`

This provides approximately 3.52 trillion possible short codes.

Because ShortScale is intended to evolve into a horizontally scaled service, short-code generation should not require application instances to coordinate with each other.

## Alternatives

### 1. Database ID encoded as Base62

Insert the URL first, obtain its sequential database ID, and encode that ID using Base62.

Advantages:

- collision-free by construction
- compact identifiers
- straightforward implementation

Disadvantages:

- generated codes are predictable
- exposes information about insertion order and approximate system usage
- tightly couples public identifier generation to the database sequence
- requires the database-generated ID before the public code can be produced

### 2. UUID-derived short codes

Generate a UUID and derive a shorter public identifier from it.

Advantages:

- generation does not require database coordination
- straightforward distributed generation

Disadvantages:

- full UUIDs are too long for the desired short URL format
- truncating or encoding UUIDs still requires explicit collision reasoning
- adds unnecessary identifier size or transformation complexity for this use case

### 3. Centralized counter encoded as Base62

Maintain a globally increasing counter and encode its value using Base62.

Advantages:

- compact identifiers
- collision-free if the counter is strongly coordinated

Disadvantages:

- introduces a centralized coordination dependency
- the counter can become a scalability or availability concern
- identifiers remain predictable
- requires additional infrastructure or synchronization

### 4. Random Base62 generation

Generate each short code independently using cryptographically secure randomness from a Base62 alphabet.

Advantages:

- no coordination is required between application instances
- codes are not sequential or easily predictable
- simple generation model
- supports horizontal scaling naturally

Disadvantages:

- collisions are possible
- collision probability increases as the namespace fills
- correctness requires explicit collision handling

## Decision

ShortScale will generate random seven-character Base62 short codes.

The Base62 alphabet is:

`0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ`

Codes are generated using Go's `crypto/rand` package rather than `math/rand`.

PostgreSQL remains the source of truth and enforces uniqueness through the unique constraint on `urls.short_code`.

The creation algorithm is:

1. Generate a random seven-character Base62 code.
2. Attempt to insert the URL and short code into PostgreSQL.
3. If PostgreSQL reports a unique-constraint violation specifically for the short-code constraint, generate a new code and retry.
4. Retry for a bounded number of attempts.
5. Treat unrelated database errors as failures rather than collisions.

The initial implementation allows a maximum of five generation attempts.

ShortScale does not perform a separate `SELECT` before `INSERT` to check whether a generated code already exists.

## Reasoning

A pre-insert existence check would introduce a check-then-act race:

1. instance A checks whether a code exists
2. instance B checks the same code
3. both observe that it does not exist
4. both attempt to insert it

Therefore, application-level checking cannot provide the final uniqueness guarantee under concurrency.

The PostgreSQL unique constraint is the authoritative concurrency-safe mechanism. Competing inserts are serialized by the database constraint, allowing one insert to succeed and the other to receive a unique-violation error.

Random Base62 generation therefore provides a large, non-sequential identifier space, while PostgreSQL provides the correctness guarantee.

This approach also allows multiple ShortScale API instances to generate codes independently without introducing a distributed lock or centralized ID-generation service.

## Trade-offs

### Advantages

- simple generation logic
- approximately 3.52 trillion possible seven-character codes
- no application-level coordination between instances
- non-sequential public identifiers
- database-enforced correctness under concurrent inserts
- compatible with future horizontal scaling

### Disadvantages

- collisions remain theoretically possible
- URL creation may occasionally require another database insert attempt
- the fixed seven-character namespace is finite
- the application depends on recognizing the specific PostgreSQL uniqueness violation correctly

## Consequences

The `urls.short_code` unique constraint is part of ShortScale's correctness model and must not be removed without revisiting this decision.

URL creation must continue to treat the database insert as the authoritative collision check.

Future horizontally scaled API instances can generate short codes independently without coordinating with each other.

The seven-character length may need to be reconsidered if namespace utilization becomes sufficiently large. Such a change should be driven by measured system requirements rather than implemented speculatively.

Caching layers introduced later must not become the authority for short-code uniqueness. PostgreSQL remains the source of truth.

If the short-code generation strategy changes substantially in the future, this ADR should be superseded by a new architectural decision.
