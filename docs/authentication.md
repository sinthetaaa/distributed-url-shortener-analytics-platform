# ShortScale Authentication, Sessions & URL Ownership

This document records the authentication and authorization design that was added after ShortScale's original Phase 0 scope.

The initial architecture deliberately excluded authentication so the distributed URL-shortening core could be built and measured first. The deployed product now supports persistent user accounts, database-backed sessions, authenticated URL ownership, authenticated analytics access, and browser cookie sessions.

## Current authentication endpoints

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/auth/register` | Create a user account |
| `POST` | `/api/v1/auth/login` | Verify credentials and create a session |
| `GET` | `/api/v1/auth/me` | Return the authenticated user |
| `POST` | `/api/v1/auth/logout` | Revoke the current session |

Authenticated product endpoints include:

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/urls` | Create a URL owned by the current user |
| `GET` | `/api/v1/urls` | List URLs owned by the current user |
| `GET` | `/api/v1/urls/{shortCode}/analytics` | Read analytics only for a URL owned by the current user |

Public redirect resolution remains available through:

```text
GET /{shortCode}
```

A visitor does not need an account to follow a short link.

## Data model

Migration `00004_create_users_and_url_ownership.sql` introduced:

```text
users
user_sessions
urls.user_id
```

### `users`

Relevant fields:

```text
id
email
password_hash
created_at
```

Email is unique and constrained to the normalized lowercase representation.

### `user_sessions`

Relevant fields:

```text
token_hash
user_id
created_at
expires_at
```

The raw browser session token is not stored in PostgreSQL.

Only its SHA-256 hash is persisted.

### URL ownership

`urls.user_id` references the owning user.

The production API uses that ownership relationship for the authenticated URL list and analytics authorization.

## Registration

Registration performs:

```text
email
  ↓
trim + lowercase normalization
  ↓
email syntax validation
  ↓
password validation
  ↓
bcrypt password hashing
  ↓
PostgreSQL user insert
```

Current password constraints are:

```text
minimum length: 8 characters
maximum length: 72 bytes
```

The maximum matches bcrypt's password-length boundary.

Duplicate email registration is rejected through the database uniqueness constraint.

## Login

Login normalizes the submitted email, loads the matching user, verifies the bcrypt password hash, creates a new random session token, stores only the token hash, and returns the raw token only through the session cookie.

For nonexistent users, ShortScale still performs a bcrypt comparison against a dummy password hash before returning invalid credentials. This reduces the obvious timing difference between a missing-account path and an incorrect-password path.

The API returns the same public credential error for an invalid email/password combination:

```text
401 Unauthorized
invalid email or password
```

## Session design

Session tokens contain 32 bytes generated with Go's `crypto/rand` and are encoded with URL-safe Base64.

The database stores:

```text
SHA-256(session token)
```

rather than the bearer token itself.

The default session lifetime is:

```text
7 days
```

Authentication therefore remains server-side and revocable.

ShortScale does not store authentication bearer tokens in browser JavaScript storage.

## Session cookie

The browser session cookie is:

```text
shortscale_session
```

Properties:

```text
Path=/
HttpOnly=true
SameSite=Lax
Secure=<AUTH_COOKIE_SECURE>
```

Production requires:

```text
AUTH_COOKIE_SECURE=true
```

The cookie therefore travels only over HTTPS in production and is inaccessible to browser JavaScript.

`SameSite=Lax` is part of the current CSRF posture. ShortScale does not currently implement a separate CSRF token mechanism.

## Logout and revocation

Logout hashes the supplied session cookie token and deletes the corresponding database session row.

The response then clears the browser cookie.

Because sessions are database-backed, logout is server-side revocation rather than only a client-side cookie deletion.

## Authentication middleware

Protected routes read the `shortscale_session` cookie and authenticate it through the session store.

If no valid session exists, the API returns:

```text
401 Unauthorized
```

and invalid sessions are cleared from the client.

The authenticated user is attached to the request context for downstream ownership checks.

## URL ownership and analytics authorization

When authentication is enabled:

```text
authenticated user
      ↓
POST /api/v1/urls
      ↓
URL row stores user_id
```

`GET /api/v1/urls` returns the current user's owned links.

Analytics access adds an ownership check before returning redirect analytics.

The authorization invariant is:

```text
authenticated
AND
owns short code
→ analytics allowed
```

Authentication alone is not sufficient to read another user's analytics.

## Rate limiting

ShortScale has two different traffic-protection policies.

### Registration and login

Registration and login use a small process-local token bucket keyed by client IP.

Current policy:

```text
burst capacity: 5
refill rate:    5 requests/minute
```

This protects the authentication endpoints from simple repeated attempts.

It is intentionally separate from the distributed URL-creation limiter.

### URL creation

Authenticated URL creation uses the Redis-backed distributed token bucket documented in:

[ADR 005 — Distributed Rate Limiting](decisions/005-distributed-rate-limiting.md)

Current create policy:

```text
burst capacity: 5
refill rate:    10 requests/minute
```

The Redis limiter fails open if Redis is unavailable so Redis does not become a write-availability dependency.

## Request hardening

Authentication request bodies are capped at:

```text
4096 bytes
```

JSON decoding rejects unknown fields and trailing JSON values.

The API uses generic internal-server-error responses rather than returning database or implementation details.

## Production routing

The Vercel frontend uses same-origin route-handler proxying to the Railway API.

This allows the browser to use the HttpOnly cookie without storing a client token.

Production authentication was validated through:

```text
register
login
HttpOnly cookie session
/auth/me
logout/revocation
protected-route guards
auth-page guards
```

## Security boundaries and limitations

Current authentication is intentionally small.

ShortScale does not claim:

- OAuth or social login
- multi-factor authentication
- password-reset email flows
- account verification emails
- device/session management UI
- organization or team RBAC
- a dedicated CSRF token system
- distributed authentication-endpoint rate limiting
- protection from every account-enumeration side channel

The implemented design is a production-capable portfolio authentication model, not a complete identity platform.

## Related documentation

- [Production deployment and operations](production-deployment.md)
- [Production validation](production-validation.md)
- [Production hardening and CI](production-hardening.md)
- [Production database migrations](production-migrations.md)
- [ADR 005 — Distributed Rate Limiting](decisions/005-distributed-rate-limiting.md)
