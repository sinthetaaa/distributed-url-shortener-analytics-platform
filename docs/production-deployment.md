# ShortScale Production Deployment & Operations Runbook

This document is the production deployment and operations entry point for ShortScale.

It documents the deployed topology, provider responsibilities, environment configuration, deployment flow, verification procedure, rollback strategy, failure expectations, and operational limitations.

Detailed subsystem procedures remain in:

- [Production database migrations](production-migrations.md)
- [Production hardening and CI](production-hardening.md)
- [Observability](observability.md)
- [Failure engineering and resilience](resilience.md)
- [Database scaling](database-scaling.md)

## Production topology

```text
Users
  |
  v
Vercel
sscale.vercel.app
  |
  | same-origin API proxy
  v
Railway: shortscale-api
  |
  +-------------------+
  |                   |
  v                   v
Upstash Redis       Aiven Kafka
(cache + limiter)      |
                       v
              Railway analytics consumer
                       |
                       v
                  Neon PostgreSQL

shortscale-api ------------------+
                                 |
shortscale-analytics-consumer ---+--> private OTLP
                                      |
                                      v
                           Railway Grafana Alloy
                           shortscale-observability
                                      |
                                      v
                                Grafana Cloud
```

### Production services

| Component | Provider | Service / URL | Exposure |
| --- | --- | --- | --- |
| Frontend | Vercel | `https://sscale.vercel.app` | Public |
| API | Railway | `shortscale-api` | Public |
| Analytics consumer | Railway | `shortscale-analytics-consumer` | Private |
| Observability | Railway | `shortscale-observability` | Private |
| PostgreSQL | Neon | managed database | Provider-managed |
| Redis | Upstash | managed Redis | Provider-managed |
| Kafka | Aiven | managed Kafka | Provider-managed |
| Metrics/traces | Grafana Cloud | managed telemetry | Provider-managed |

Production API hostname:

```text
shortscale-api-production.up.railway.app
```

The analytics consumer and Alloy services must not expose public application domains.

## Deployment model

ShortScale uses platform-native Git deployments.

A push to `main` triggers:

```text
push main
   |
   +--> GitHub Actions CI
   |
   +--> Railway
   |     +--> shortscale-api
   |     +--> shortscale-analytics-consumer
   |     +--> shortscale-observability
   |
   +--> Vercel frontend
```

Railway and Vercel deploy through their GitHub integrations.

GitHub Actions does not directly deploy either platform, so the deployment-verification workflow does not need Railway or Vercel deployment credentials.

After CI succeeds, the `Deployment Verification` workflow waits for these commit status contexts:

```text
celebrated-flow - shortscale-api
celebrated-flow - shortscale-analytics-consumer
celebrated-flow - shortscale-observability
Vercel
```

After all four succeed, production smoke checks run.

This is post-deployment verification, not a replacement deployment mechanism.

## Production build artifacts

### API

Railway service: `shortscale-api`

Build artifact: `Dockerfile`

Runtime characteristics:

- Go binary built with `CGO_ENABLED=0`
- scratch final image
- CA certificates included
- non-root `USER 65532:65532`
- application port `8080`
- private Prometheus metrics listener on `9090`

### Analytics consumer

Railway service: `shortscale-analytics-consumer`

Build artifact: `Dockerfile.analytics-consumer`

Runtime characteristics:

- scratch final image
- CA certificates included
- non-root `USER 65532:65532`
- private metrics listener on `9091`
- no public domain

### Observability collector

Railway service: `shortscale-observability`

Build artifact: `Dockerfile.alloy`

Grafana Alloy runs as the non-root `alloy` user.

Private listeners:

```text
4317   OTLP gRPC
12345  Alloy HTTP/admin
```

The service has no public domain.

### Frontend

Vercel deploys the application under `web/`.

Production URL:

```text
https://sscale.vercel.app
```

Authenticated frontend API calls use same-origin route-handler proxying to the Railway API.

Public short links are also served through the Vercel domain.

## Production environment variables

Never commit production secrets or credential-bearing URLs.

Store secret values only in the relevant provider environment-variable store.

### API

Production configuration includes:

```text
DATABASE_URL
REDIS_URL

KAFKA_BROKERS
KAFKA_CLIENT_ID
KAFKA_REDIRECT_TOPIC
KAFKA_TLS_ENABLED
KAFKA_SASL_USERNAME
KAFKA_SASL_PASSWORD

AUTH_COOKIE_SECURE

OTEL_EXPORTER_OTLP_ENDPOINT
OTEL_EXPORTER_OTLP_INSECURE
DEPLOYMENT_ENVIRONMENT

PORT
API_METRICS_ADDR
```

Required production semantics:

```text
AUTH_COOKIE_SECURE=true
DEPLOYMENT_ENVIRONMENT=production
OTEL_EXPORTER_OTLP_INSECURE=true
```

The insecure OTLP setting applies only to the private Railway-to-Alloy network hop. Alloy forwards telemetry to Grafana Cloud using TLS.

### Analytics consumer

Configuration includes:

```text
DATABASE_URL

KAFKA_BROKERS
KAFKA_CONSUMER_CLIENT_ID
KAFKA_CONSUMER_GROUP
KAFKA_CONSUMER_RESET
KAFKA_REDIRECT_TOPIC
KAFKA_TLS_ENABLED
KAFKA_SASL_USERNAME
KAFKA_SASL_PASSWORD

OTEL_EXPORTER_OTLP_ENDPOINT
OTEL_EXPORTER_OTLP_INSECURE
DEPLOYMENT_ENVIRONMENT

ANALYTICS_METRICS_ADDR
```

Production consumer group:

```text
shortscale-analytics-v1
```

Production reset policy:

```text
earliest
```

### Grafana Alloy

Required provider-managed secrets:

```text
GRAFANA_CLOUD_PROMETHEUS_URL
GRAFANA_CLOUD_PROMETHEUS_USERNAME
GRAFANA_CLOUD_API_KEY
GRAFANA_CLOUD_OTLP_ENDPOINT
GRAFANA_CLOUD_OTLP_USERNAME
```

### Frontend

Vercel configuration includes:

```text
SHORTSCALE_API_ORIGIN
NEXT_PUBLIC_SHORTSCALE_REDIRECT_ORIGIN
```

Production redirect origin:

```text
https://sscale.vercel.app
```

## Fresh environment provisioning order

Recommended order:

```text
1. Neon PostgreSQL
2. Upstash Redis
3. Aiven Kafka
4. Apply database migrations
5. Grafana Cloud
6. Railway shortscale-observability
7. Railway shortscale-api
8. Railway shortscale-analytics-consumer
9. Vercel frontend
10. Production verification
```

Deploying Alloy before the applications allows telemetry collection from application startup.

## Database migrations

Production database changes use Goose.

Do not embed ad-hoc migration commands into ordinary application deployment.

Follow [Production database migrations](production-migrations.md).

The migration procedure covers:

- pre-migration checks
- secure credential loading
- migration-state inspection
- applying migrations
- readiness validation
- post-migration smoke testing
- rollback and failure handling

Prefer backward-compatible schema evolution.

For multi-step incompatible changes, use expand/migrate/contract rather than a single destructive migration.

## Routine deployment procedure

For an ordinary code change:

```text
1. Validate locally.
2. Review the diff.
3. Commit one logical change.
4. Push main.
5. GitHub CI runs.
6. Railway and Vercel perform native Git deployments.
7. CI must succeed.
8. Deployment Verification starts.
9. It waits for all four deployment status contexts.
10. Production smoke checks execute.
11. Treat production as verified only after that workflow succeeds.
```

Workflow:

```text
.github/workflows/deployment-verification.yml
```

Supporting scripts:

```text
.github/scripts/wait-for-platform-deployments.sh
.github/scripts/production-smoke.sh
```

## Automated production verification

The post-deployment workflow verifies the exact commit that triggered CI.

Production smoke expectations:

| Check | Expected |
| --- | --- |
| API `/health/live` | `200` |
| API `/health/ready` | `200` |
| Frontend `/` | `200` |
| Public API `/metrics` | `404` |
| Unauthenticated `/api/v1/auth/me` | `401` |

The smoke script retries transiently during deployment convergence.

Manual execution:

```bash
.github/scripts/production-smoke.sh
```

For significant redirect or analytics changes, also verify:

```text
login
→ owned short URL
→ redirect returns 302
→ analytics eventually increases
```

This does not establish a global exactly-once delivery guarantee.

## Observability requirements

Application telemetry endpoints remain private.

```text
public API /metrics
→ 404

API metrics
→ private :9090

consumer metrics
→ private :9091

API + consumer OTLP
→ private Alloy :4317
```

Production traces identify:

```text
service.namespace=shortscale
deployment.environment.name=production
```

A distributed redirect trace can include:

```text
API
→ Kafka producer
→ analytics consumer
→ PostgreSQL persistence
```

See [Observability](observability.md).

## Failure and recovery expectations

### Redis unavailable

Redis is a non-critical dependency.

Expected behavior:

```text
Redis unavailable
→ cache access fails
→ fallback/circuit-breaker behavior
→ PostgreSQL resolves redirect
→ serving path remains available
```

The Redis-backed distributed limiter fails open so Redis failure does not make the entire API unavailable.

### Analytics consumer unavailable

Expected behavior:

```text
consumer unavailable
→ redirect API continues
→ Kafka retains accepted events
→ consumer restored
→ backlog catches up
```

Production failure testing confirmed this behavior with a controlled five-event outage.

This is not a global exactly-once claim.

### Telemetry unavailable

Telemetry is fail-open.

Expected behavior:

```text
OTLP destination unreachable
→ exporter errors/retries
→ API continues serving
→ consumer continues processing
```

Application availability must not depend on Alloy or Grafana Cloud.

### PostgreSQL unavailable

PostgreSQL is a critical dependency.

Expected behavior:

```text
PostgreSQL unavailable
→ readiness fails
→ database-backed auth/writes fail
→ uncached redirects fail
```

Cached redirects may continue temporarily while Redis contains valid entries.

ShortScale does not claim database high availability beyond the managed database provider.

See [Failure engineering and resilience](resilience.md).

## Rollback and recovery

### Application regression

Preferred rollback:

```text
identify bad commit
→ git revert
→ validate locally
→ push main
→ platform-native redeployment
→ Deployment Verification
```

Do not force-push `main`.

Provider-level rollback can be used for an emergency, but Git must then be reconciled so repository state again represents intended production state.

### Environment-variable regression

```text
restore previous provider-managed value
→ redeploy affected service
→ verify service SUCCESS
→ run production smoke checks
```

Never expose secret values in logs, documentation, issues, or commits.

### Database migration failure

Follow [Production database migrations](production-migrations.md).

Do not automatically run destructive down migrations just because an application deployment fails.

### Consumer recovery

```text
restore/redeploy consumer
→ verify metrics endpoint
→ verify service SUCCESS
→ verify Kafka backlog convergence
```

### Telemetry recovery

```text
restore telemetry configuration
→ redeploy affected application if needed
→ verify application health
→ verify Alloy health
→ confirm telemetry resumes
```

## Security deployment invariants

Production must preserve:

- secrets outside Git
- API container non-root
- consumer container non-root
- Alloy non-root
- public `/metrics` closed
- consumer metrics private
- Alloy without a public domain
- Secure production auth cookie
- HTTPS for public traffic
- TLS Redis connection
- Kafka TLS/SASL
- no production-provider secrets required by deployment verification

See [Production hardening and CI](production-hardening.md).

## Operational limitations

Current production deployment is portfolio-scale.

Do not claim that current evidence proves:

- multi-region API failover
- multi-primary PostgreSQL
- zero-downtime database failover
- global exactly-once delivery
- unlimited managed-provider capacity
- a maximum sustainable production throughput

Failure tests and resource measurements describe the tested scenarios only.

## Deployment checklist

Before push:

```text
[ ] relevant tests pass
[ ] lint/static checks pass
[ ] migration impact reviewed
[ ] no secrets added
[ ] diff reviewed
[ ] working tree contains only intended changes
```

After push:

```text
[ ] CI successful
[ ] Railway API successful
[ ] Railway analytics consumer successful
[ ] Railway observability successful
[ ] Vercel successful
[ ] Deployment Verification successful
[ ] production smoke successful
[ ] main matches origin/main
```

Database changes additionally require:

```text
[ ] migration procedure followed
[ ] production migration state verified
[ ] readiness verified
[ ] post-migration smoke successful
```

## Initial deployment-verification record

The first automated production verification was validated for:

```text
commit:
adf4ba4054e4c7c6761606134d85de1a74499ffd

message:
ci: verify production deployments
```

Observed chain:

```text
GitHub CI #47
→ SUCCESS

Railway shortscale-api
→ SUCCESS

Railway shortscale-analytics-consumer
→ SUCCESS

Railway shortscale-observability
→ SUCCESS

Vercel
→ SUCCESS

Deployment Verification #1
→ SUCCESS

Production smoke
→ PASS
```

This is historical evidence for the initial automation run. Every future deployment must be verified against its own commit.

## Related documentation

- [Production database migrations](production-migrations.md)
- [Production hardening and CI](production-hardening.md)
- [Observability](observability.md)
- [Failure engineering and resilience](resilience.md)
- [Database scaling](database-scaling.md)
