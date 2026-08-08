# System architecture

MatchMind uses four independently runnable processes. External clients use the
HTTP API; internal calls use versioned Protocol Buffers over gRPC.

```text
HTTP client
    |
    v
api-service :8080
    |--------------------|--------------------|
    v                    v                    v
player-service      matchmaking-service   simulation-service
:50051 / :8081      :50052 / :8082        :50053 / :8083
                         |                    |
                         |<-------------------|
                         |         completes matches
                         v
                  matching workers (1..N)
```

## Package boundaries

- `internal/*/domain` contains state and invariants without transport or
  persistence dependencies.
- `internal/*/application` coordinates use cases through interfaces.
- `internal/*/transport` adapts HTTP or gRPC requests.
- `internal/*/gateway` adapts calls to another service.
- `internal/*/repository` owns persistence implementations.
- `internal/platform` provides process infrastructure such as servers,
  structured logging, IDs, and metrics.
- `proto` is the source of truth for internal service contracts; `gen/go` is
  generated code.

## Match lifecycle

1. The player service validates and stores a player profile.
2. The matchmaking service creates an idempotent Ticket and places it in a
   pool partitioned by mode, client version, and region.
3. One or more workers select candidates using dynamic rating windows.
4. Party-safe team formation assigns exactly five unique players per side and
   covers all five roles.
5. Quality scoring combines skill, roles, latency, party symmetry, and wait
   time. A low-quality candidate is rejected with reasons.
6. All ten Tickets are atomically reserved. A Match is created only after the
   reservation succeeds.
7. Server allocation marks the Match READY and atomically assigns all Tickets.
8. The seeded simulator starts and finishes the Match, records process
   metrics, and applies one idempotent Elo batch.

## Concurrency guarantees

The memory repositories use locks around compound state changes, not only
individual map operations. Reservation and assignment update all ten tickets
or none. The active-player index prevents a player from owning two active
Tickets. Multiple workers may share the same stores; only one can reserve a
given batch.

The production persistence adapter keeps these application interfaces while
combining PostgreSQL transactions with Redis Lua scripts. PostgreSQL owns
durable state; Redis owns rebuildable queue order, snapshots, reservations,
and expiry indexes.
