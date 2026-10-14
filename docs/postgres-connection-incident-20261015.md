# Financial PostgreSQL connection exhaustion

## Evidence

The production primary rejected even a local `psql` connection with
`sorry, too many clients already`. PostgreSQL process and socket inspection
at 2026-10-14 21:05 UTC showed almost all 100 connections belonged to the
financial API container. Groups of nine idle connections accumulated at
five-minute intervals, matching the nine companies scanned by the supervisor.

`SweepSupervisorAgentForCompany` created a fresh `APIHandler` and operational
database pool per company on every sweep, without closing it. Idle pools
remained registered with `database/sql` and retained their connections.
The preceding pool patch also configured the URL branch twice while leaving
the environment-variable branch unbounded.

## Correction

- Reuse the worker's handler for all company sweeps.
- Close the worker's operational pool when its context ends.
- Close the operational pool on every return from the standalone sweep.
- Route both PostgreSQL configuration paths through the same bounded opener.
- Check database connectivity in API health responses instead of always
  returning success.

Connection limits control concurrent database sessions per pool. They do not
limit the number of records, invoices, or writes. Requests wait for a pooled
connection when its concurrency limit is reached.

## Regression coverage

The PostgreSQL-backed regression tests verify both configuration paths have
the configured limit and repeatedly execute real supervisor sweeps while
asserting their operational sessions are released.
The tests require `TEST_POSTGRES_DSN` and run
against the disposable CI database, never the production financial database.

Before publication, isolated PostgreSQL 16 tests reproduced the old code's
leak on its first sweep and detected an unlimited pool on the environment
configuration path. With the correction, thirty real sweeps left zero
operational sessions after every invocation, both pool configuration paths
honored a test limit of three, and disconnected database health returned 503.
The financial Go suite, all five PostgreSQL integration tests, handler tests,
bridge tests, code firewall, Linux API build, and financial web production
build (including 47 UI unit tests) passed locally or in the isolated test
container. Production acceptance must still verify the deployed image and
authenticated UI after publication.

## Deployment and rollback

No schema, financial records, or database volumes change. Publish and deploy
the immutable financial API release through the existing production workflow.
Verify authenticated workspace reads, supervisor reports, and PostgreSQL
connection counts across several five-minute sweeps. An API restart releases
the old process's leaked sessions. Rolling back to the previous image
reintroduces the leak; it is only an emergency rollback, not a lasting fix.
