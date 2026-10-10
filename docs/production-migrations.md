# ShortScale Production Database Migrations

ShortScale uses Goose for ordered PostgreSQL schema migrations.

Production PostgreSQL is hosted on Neon. Production migrations are an explicit deployment operation and must never run automatically from application startup.

## Current production baseline

As verified on 10 October 2026, production has the following migrations applied:

1. `00001_create_urls_table.sql`
2. `00002_create_redirect_events_table.sql`
3. `00003_create_redirect_daily_counts.sql`
4. `00004_create_users_and_url_ownership.sql`

No migration was applied during the Phase 15AS verification because production was already fully up to date.

## Principles

- Migration files are append-only after they have reached production.
- Never edit an already-applied migration to change production schema.
- Create a new numbered migration for every future schema change.
- Review migration SQL before running it against production.
- Never store or commit the production `DATABASE_URL`.
- Never paste production credentials directly into shell history.
- Check migration status before and after every production migration.
- Validate application health immediately after migration.
- Prefer a forward-fix migration over an unsafe production rollback.
- Destructive migrations require explicit data-loss and compatibility review.

## 1. Pre-migration checks

Before touching production, inspect the repository and migration inventory:

    git status --short
    git log --oneline -5
    goose --version
    find migrations -maxdepth 1 -type f -name '*.sql' -print | sort

The worktree should be clean.

The intended migration files must already be reviewed and committed before they are applied to production.

The application version being deployed should remain compatible with both the pre-migration and post-migration schema whenever a rolling deployment can expose multiple application versions concurrently.

## 2. Review pending migration SQL

Inspect every new migration before execution:

    sed -n '1,240p' migrations/<migration-file>.sql

Review:

- schema changes
- indexes
- constraints
- potentially expensive table operations
- table locks
- backfills
- destructive statements
- compatibility with the currently deployed application
- `Up` behavior
- `Down` behavior

A migration must not be run merely because it exists in the repository.

## 3. Load the production connection string securely

Use the Neon production pooled PostgreSQL connection string.

Do not place the connection string directly in a shell command because that can expose it through shell history.

Load it interactively:

    printf 'Paste Neon production DATABASE_URL: '
    read -s DATABASE_URL
    printf '\n'
    export DATABASE_URL

The value is entered without being echoed to the terminal.

## 4. Check production migration state

Always inspect the production state before applying anything:

    goose -dir migrations postgres "$DATABASE_URL" status

Confirm which migrations are already applied and which migrations, if any, are pending.

If production is already current, do not run `goose up`.

If the production migration state differs unexpectedly from the intended deployment state, stop and investigate before making any mutation.

## 5. Apply pending migrations

Only after the SQL and current production state have been reviewed:

    goose -dir migrations postgres "$DATABASE_URL" up

Goose applies pending migrations in version order.

Do not interrupt an in-progress production migration unless there is a clear operational reason to do so.

## 6. Verify migration state

Immediately after the migration command completes:

    goose -dir migrations postgres "$DATABASE_URL" status

Confirm that:

- every intended migration is recorded as applied
- no unexpected migration was executed
- migration ordering remains correct

## 7. Validate application readiness

After migration, verify the production API readiness endpoint.

The expected result is:

    HTTP 200
    {"status":"ok"}

The readiness check confirms that the deployed API can successfully reach the production PostgreSQL database.

A readiness check is necessary but does not replace functional production smoke testing.

## 8. Run production smoke tests

After readiness passes, validate the product paths affected by the schema change.

Depending on the migration, this can include:

- registration
- login
- authenticated session restoration
- URL creation
- My Links retrieval
- public short-link redirect
- analytics event publishing
- analytics processing
- analytics retrieval
- logout

For analytics-related schema changes, allow for asynchronous Kafka processing before validating the final redirect count.

## 9. Remove the production credential from the shell

After migration work is complete:

    unset DATABASE_URL

Do not leave production credentials exported longer than necessary.

## Rollback policy

Production rollback is not an automatic response to a failed deployment.

Before executing a Goose `Down` migration, verify:

- the `Down` operation is safe for existing production data
- no data written after the migration depends on the new schema
- the application version being restored is compatible with the rolled-back schema
- the rollback does not drop data that cannot be reconstructed
- application rollback and database rollback have been considered independently

For destructive or data-transforming migrations, prefer a new forward-fix migration unless a rollback has been explicitly reviewed and proven safe.

Do not run `goose down` blindly in production.

## Failure procedure

If a migration fails:

1. Stop further deployment actions.
2. Preserve the Goose output and database error.
3. Re-run `goose status` to determine the recorded migration state.
4. Check production database connectivity and application readiness.
5. Determine whether the failed migration executed partially.
6. Do not manually edit `goose_db_version`.
7. Do not rerun destructive SQL blindly.
8. Decide between a safe retry, a forward-fix migration, or a reviewed rollback.
9. Re-run production readiness and smoke tests after recovery.

## Adding a future migration

Future migrations must use the next ordered migration number.

For example, after `00004`, the next migration should use version `00005`.

The migration must contain both Goose sections:

    -- +goose Up

and:

    -- +goose Down

Before production execution:

1. implement the migration
2. review its SQL
3. validate it locally
4. run relevant automated tests
5. commit and push the migration
6. wait for CI to pass
7. inspect production `goose status`
8. apply only the pending migration
9. inspect `goose status` again
10. run production readiness and smoke tests

## Phase 15AS verification record

Production migration state was audited on 10 October 2026.

The following migrations were already applied:

- `00001_create_urls_table.sql`
- `00002_create_redirect_events_table.sql`
- `00003_create_redirect_daily_counts.sql`
- `00004_create_users_and_url_ownership.sql`

`goose status` reported all four migrations as applied.

No migration mutation was necessary during Phase 15AS.

After the migration-state audit:

- production API `/health/ready` returned HTTP 200
- response body was `{"status":"ok"}`
- the repository worktree remained clean before creation of this runbook

This establishes the production migration baseline and the procedure to be used for future ShortScale schema deployments.
