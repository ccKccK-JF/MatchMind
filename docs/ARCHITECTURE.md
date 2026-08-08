# System architecture

MatchMind uses five independently runnable processes. External clients use the
HTTP API; internal calls use versioned Protocol Buffers over gRPC.

```text
HTTP client
    |
    v
api-service :8080
    |-------------|--------------------|-------------------|
    v             v                    v                   v
player-service  matchmaking-service  simulation-service  agent-service
:50051/:8081    :50052/:8082         :50053/:8083        :50054/:8084
                    ^                    |                   |
                    | completes matches  |                   |
                    |<-------------------|                   |
                    | allowlisted read/approved rollout      |
                    |<---------------------------------------|
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
3. One or more workers select candidates using bounded, wait-driven rating and
   latency windows. The hard latency cap is never relaxed.
4. A policy selector chooses greedy or Beam Search formation. A/B mode hashes
   a stable player key, experiment salt, and experiment ID into a deterministic
   control/treatment bucket. Both algorithms keep parties intact, assign five
   unique players per side, and cover all five roles. Non-preferred-role scores
   begin increasing only after a configured wait and stop at a policy cap.
5. Quality scoring combines skill, roles, latency, party symmetry, and wait
   time. A low-quality candidate is rejected with reasons.
6. All ten Tickets are atomically reserved. A Match is created only after the
   reservation succeeds.
7. Server allocation marks the Match READY and atomically assigns all Tickets.
8. The seeded simulator starts and finishes the Match, records process
   metrics, and applies one idempotent Elo batch.
9. Quality analysis reads finished Match snapshots and groups prediction
   errors and process outcomes by persisted policy version.
10. Historical replay rebuilds queued copies of durable Ticket snapshots and
    runs selected policies without reservations or writes.
11. The Agent reads only operational snapshots, quality analysis, and replay
    APIs. It proposes but does not directly activate a policy.
12. A different reviewer approves a proposal after five mandatory risk checks;
    an administrator may then start a persisted A/B rollout or roll it back.

The chosen policy version is stored on each Match. This makes later replay and
predicted-versus-actual quality analysis possible without reconstructing the
active experiment. Offline batch simulation uses the same deterministic
simulation model but deliberately bypasses Match completion and Elo updates.
Historical replay is also read-only. It evaluates counterfactual formation
quality, while the historical actual-quality result remains attached only to
the source Match because an unplayed counterfactual has no real outcome.

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

The Agent is outside the real-time matchmaking path. Its gateway exposes a
compile-time allowlist instead of a generic RPC, Shell, or SQL interface.
Mutation calls require both an approved proposal and an internal control token
sent as gRPC metadata. Run input/output, model and prompt versions, every tool
call, reviewer identity, rollout parameters, activation, and rollback are
durable audit data. Transitional `ACTIVATING` and `ROLLING_BACK` states make
external calls retry-safe after process interruption.
