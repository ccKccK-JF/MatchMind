# Requirements traceability

This matrix records implementation evidence against `需求文档.md` and
`游戏内容文档.md`. A row is marked verified only when both production code and
an automated check exist. Planned items are not counted as complete.

## Functional requirements

| Requirement | Status | Evidence |
|---|---|---|
| FR-PLAYER-001 create player | Verified | Player domain/application, memory and PostgreSQL repositories, REST/gRPC and tests |
| FR-PLAYER-002 rating, uncertainty, recent Matches, matchmaking status | Verified | Rating view returns rating/deviation/history, recent Match IDs and active Ticket status; HTTP and end-to-end tests |
| FR-PLAYER-003 update regional latency | Verified | Validated immutable domain update, memory/PostgreSQL persistence, owner-gated REST, gRPC and tests |
| FR-RATING-001..004 Elo prediction/update/config/history | Verified | Configurable Elo calculator, atomic idempotent ten-player update and rating history tests |
| FR-TICKET-001..005 lifecycle, expiry and idempotency | Verified | Domain state machine, active-player uniqueness, timeout recovery and repository/API tests |
| FR-QUEUE-001..005 partition, ordering and concurrency | Verified | Mode/version/region pools, oldest-first snapshots, validity filtering, memory/Redis tests and race suite |
| FR-CANDIDATE-001..005 candidate generation/explanation | Verified | Dynamic rating window, bounded candidate set, party grouping, deterministic decisions and tests |
| FR-TEAM-001..006 Greedy/Beam and role assignment | Verified | Party-safe deterministic 5v5 formation, preference scoring, Beam comparison metrics and tests |
| FR-QUALITY-001..005 five scores, weights, threshold, reasons | Verified | MatchPolicy validation, quality engine, worker threshold and tests |
| FR-EXPAND-001 rating expansion | Verified | Time-based bounded rating range and tests |
| FR-EXPAND-002 latency expansion | Verified | Anchor wait time expands the admissible latency from a strict initial value to an immutable hard maximum; decision-reason and boundary tests exist |
| FR-EXPAND-003 role relaxation | Verified | Non-preferred-role score remains zero until a policy delay, then increases by wait time to a configured cap; assignment tests cover early, relaxed and capped states |
| FR-EXPAND-004 immutable hard constraints | Partial | Version, active Ticket and party integrity are hard constraints; explicit banned-player state is not modeled |
| FR-RESERVE-001..005 atomic reservation/recovery/uniqueness | Verified | Memory, PostgreSQL and Redis all-or-nothing paths, TTL recovery and concurrent worker tests |
| FR-MATCH-001,003..005 lifecycle, rollback, connection | Verified | Match state machine, allocation rollback, address/token and persistence tests |
| FR-MATCH-002 latency/capacity server selection | Verified | Every live-capacity region is evaluated using average/max latency, team difference and variance; local capacity lifecycle, cross-region Worker flow and Agones API adapter have automated tests |
| FR-SIM-001..004 deterministic and batch simulation | Verified | Seeded simulator, statistical rating test, complete result fields and bounded offline batch tests |
| FR-AGENT-001..006 allowlist/advice/replay/risk/approval/audit | Verified | Separate Agent service, five-tool gateway, five risk checks, approval state machine, persistence and integration tests |

## Non-functional and delivery requirements

| Requirement | Status | Evidence or remaining work |
|---|---|---|
| Context cancellation, timeouts and graceful shutdown | Verified | Process signal contexts, gRPC/HTTP shutdown and bounded downstream calls |
| Structured logs and required Prometheus metrics | Verified | Shared JSON logging and metric registry tests |
| End-to-end Trace ID | Verified | Validated `X-Trace-ID` is stored in context, propagated by shared gRPC client/server interceptors, returned as metadata and recorded with method/status/duration in JSON logs; unit and HTTP-to-gRPC integration tests exist |
| Race safety | Verified | `scripts/race.ps1` runs `go test -race -count=1 ./...` and passes on Windows |
| Real PostgreSQL/Redis integration | Implemented, environment-gated | Isolated-schema PostgreSQL and isolated Redis tests exist; live external services are required to execute the gated variants |
| 500 requests/s, 100k queue, P95/P99 targets | Pending verification | Load generator exists, but no current machine/run artifact proves the targets |
| Reproducible complete demo | Implemented | Five-process PowerShell demo covers Match, Elo, analysis, replay and Agent; runtime execution remains an acceptance gate while services are intentionally kept off |
| Glicko rating | Pending optional phase-two item | Elo is complete; Glicko has not been implemented |
| Agones allocation | Implemented, environment-gated | Kubernetes adapter reads Fleet ready replicas and creates v1 `GameServerAllocation` resources with RBAC and HTTP contract tests; a live Agones cluster is required for deployment verification |
| NATS/outbox | Pending extension | Architecture keeps the boundary open; no message broker is required by the first-version acceptance flow |

## Next acceptance order

1. Decide and implement the documented Glicko option without changing ranked
   Elo compatibility.
2. Run the full local demo and environment-gated PostgreSQL/Redis/Agones/load
   gates,
   preserving raw results before declaring the project complete.
