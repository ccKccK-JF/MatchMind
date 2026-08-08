# Test plan

## Fast verification

```powershell
go test -count=1 ./...
go vet ./...
```

The suite covers Elo math, domain validation and state transitions, dynamic
candidate windows, deterministic team formation, role assignment, quality
scoring, reservation rollback and recovery, simulation reproducibility,
idempotent rating updates, HTTP mapping, Prometheus output, Agent audit/state
transitions, five-check risk gating, approval separation of duties,
activation/rollback, and the complete service gRPC flow.
Player coverage includes normalized regional-latency replacement in memory and
PostgreSQL, active-Ticket status lookup, and owner enforcement for Ticket
create/query/cancel operations.
Trace tests cover untrusted ID replacement, context/metadata preservation,
multi-hop client/server propagation, gRPC response metadata, correlated JSON
logs, and a real HTTP-to-Player-gRPC integration path.

Algorithm tests use a crafted candidate pool where Beam Search must improve
role coverage and total quality over greedy selection. They also verify stable
tie-breaking, anchor retention, party integrity, deterministic A/B assignment,
and the configured treatment distribution. Batch simulation compares 1,000
cases at concurrency 1 and 16 and requires byte-for-byte equivalent reports.
Analysis tests verify signed/absolute prediction error, per-policy aggregation,
Brier win-probability scoring, normalized filters, invalid bounds, deterministic
historical replay, and non-mutation of source Tickets. The complete in-memory
gRPC test finishes a Match, exercises analysis/replay, then runs the Agent over
the same real Matchmaking gRPC boundary and verifies its immutable tool audit.

With a real PostgreSQL instance, enable the isolated-schema persistence test:

```powershell
$env:MATCHMIND_POSTGRES_TEST_DSN = "postgres://matchmind:matchmind@localhost:5432/matchmind?sslmode=disable"
go test -count=1 .\tests\integration -run PostgreSQL
```

The test applies embedded migrations in a temporary schema and verifies Player
storage, idempotent Ticket creation, ten-row atomic reservation, durable Match
recovery, optimistic revision conflicts, and transactional Match/Ticket
assignment. The in-memory and PostgreSQL flows also verify that players cannot
requeue during an assigned Match but can create a new Ticket after it finishes.
The PostgreSQL path additionally queries the finished-Match history using
policy, mode, region, time-range, and limit filters. It also verifies
transactional Agent run/proposal completion and optimistic proposal review
updates.

With a real Redis instance, enable the Lua reservation/recovery test:

```powershell
$env:MATCHMIND_REDIS_TEST_ADDRESS = "localhost:6379"
go test -count=1 .\tests\integration -run Redis
```

Fast tests run the same Lua scripts against an isolated in-process Redis
implementation and inject missing metadata, competing reservations, expired
reservation sets, durable rejection, and complete Redis data loss.

## Concurrency and race detection

Concurrency tests explicitly cover:

- 100 goroutines creating Tickets for distinct players;
- 100 goroutines competing to create an active Ticket for one player;
- simultaneous Ticket creation and cancellation;
- reservation expiry racing with assignment confirmation;
- ten workers competing for one ten-player batch;
- atomic player uniqueness across a completed Match.

Install the pinned, checksum-verified portable MinGW-w64 GCC and run the race
detector:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install-race-tools.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\race.ps1
```

The race command is a mandatory release gate. A plain `go test` result is not
a substitute.

## Load test

Start the stack, then run the built-in HTTP generator:

```powershell
go run .\cmd\loadtest -rate 500 -duration 10m -max-tickets 100000 -concurrency 256
```

The JSON result records successful Ticket throughput, failures, P95/P99 Ticket
latency, and load-generator heap/goroutine counts. During the run, capture
service resource usage with:

```powershell
docker stats
```

For a queue-only 100,000 Ticket query test, set
`MATCHMAKING_WORKER_COUNT=0` so workers do not drain the queue. For the
multi-worker test, use `MATCHMAKING_WORKER_COUNT=10`, which is the Compose
default. Performance numbers must only be reported together with the exact
machine, configuration, commit, and raw output.
