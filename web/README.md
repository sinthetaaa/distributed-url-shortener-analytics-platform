# ShortScale Web

Next.js frontend for the ShortScale distributed URL-shortener and analytics platform.

The production frontend is deployed on Vercel and uses the same-origin web application as the browser-facing boundary for authentication and API requests.

Production application:

```text
https://sscale.vercel.app
```

For the complete system architecture, deployment topology, and backend engineering documentation, see the repository [README](../README.md).

## Runtime architecture

Browser API requests use the Next.js catch-all route:

```text
/api/*
```

The route proxies to the configured ShortScale API origin:

```text
Browser
   ↓
Vercel / Next.js
   ↓ same-origin /api/*
Next.js route handler
   ↓
Railway ShortScale API
```

The proxy forwards the browser session cookie to the backend, which allows ShortScale to use an HttpOnly server-side session without storing bearer tokens in browser JavaScript storage.

Responses are returned with `Cache-Control: no-store`.

Public short links are also handled by the frontend route:

```text
/{shortCode}
```

That route requests the backend redirect with redirect following disabled and returns the backend redirect status and `Location` header to the client.

## Product flows

The frontend currently supports:

- account registration
- login
- authenticated session restoration
- logout
- protected application routes
- authenticated URL creation
- listing the current user's links
- copying generated short URLs
- public short-link redirects
- authenticated per-link analytics
- rate-limit and backend-unavailable error handling

## Environment variables

Copy `.env.example` to `.env.local` when local values need to be overridden.

### `SHORTSCALE_API_ORIGIN`

Server-side upstream used by the Next.js API proxy and public short-code route.

Local default:

```text
http://localhost:18080
```

Production points this variable at the Railway API origin.

This value is server-side configuration and should not contain credentials.

### `NEXT_PUBLIC_SHORTSCALE_REDIRECT_ORIGIN`

Public origin used when the UI builds a short URL for display and copy.

Local example:

```text
http://localhost:18080
```

If it is not configured in the browser bundle, the UI falls back to the current browser origin.

Production should use the public ShortScale web origin so generated links stay on the Vercel-facing URL.

## Development

Install dependencies:

```text
npm ci
```

Start the development server:

```text
npm run dev
```

The normal local frontend/backend topology expects the ShortScale API entry point at:

```text
http://localhost:18080
```

## Validation

Run the frontend test suite:

```text
npm test
```

Run linting:

```text
npm run lint
```

Run TypeScript checking:

```text
npm run typecheck
```

Build the production frontend:

```text
npm run build
```

Audit production dependencies:

```text
npm audit --omit=dev
```

The main repository CI runs the frontend tests, lint, typecheck, production build, and production dependency audit as one of the ten blocking CI jobs.

## Security model

The frontend does not persist an authentication bearer token in `localStorage` or `sessionStorage`.

Authentication uses the backend-issued `shortscale_session` HttpOnly cookie through same-origin requests.

The Next.js proxy uses a fixed configured backend origin rather than accepting an arbitrary upstream URL from the browser.

Production security headers are configured in `next.config.ts`.

See:

- [Authentication, sessions, and URL ownership](../docs/authentication.md)
- [Production hardening and CI](../docs/production-hardening.md)
- [Production deployment and operations](../docs/production-deployment.md)
- [Production validation](../docs/production-validation.md)

## Deployment

The frontend deploys natively from GitHub to Vercel.

GitHub Actions does not perform a second Vercel deployment. After CI succeeds, the repository's `Deployment Verification` workflow waits for the Vercel and Railway status contexts for the exact commit and then runs production smoke checks.

See [Production deployment and operations](../docs/production-deployment.md) for the complete release and rollback procedure.
