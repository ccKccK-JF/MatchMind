# Database design

The local process demo uses concurrency-safe memory repositories by default.
Player/rating history can run on PostgreSQL through
`PLAYER_STORAGE_BACKEND=postgres`. `MATCHMAKING_TICKET_STORAGE_BACKEND=redis`
selects the PostgreSQL-durable/Redis-coordinated Ticket adapter, while
`MATCHMAKING_MATCH_STORAGE_BACKEND=postgres` stores Matches durably.

## PostgreSQL ownership

`players` stores profile, current rating, rating deviation, Glicko-2
volatility, behavior score, per-Hero proficiency JSON, and the current ban
flag/reason/operator/timestamp. A database
constraint requires complete ban metadata for banned players and no stale ban
metadata for active players. `rating_changes` is append-only and records the before/after value
of all three rating-state components plus the selected rating system. It
preserves response order with a per-Match sequence, and uses a unique
`(match_id, player_id)` key so result retries cannot update a rating twice.
Elo and Glicko-2 updates use a serializable transaction plus a Match-scoped PostgreSQL advisory
lock, making duplicate result delivery idempotent.

`tickets` now stores durable Ticket state, snapshotted behavior and Hero
proficiency, create/cancel idempotency keys, reservation metadata, Match ID,
and timestamps. A partial unique index on
`player_id` for active states prevents duplicate active Tickets even when
several API instances race. PostgreSQL row locks make ten-Ticket reservation,
assignment, release, and expiry recovery atomic.

The `active` flag separates an assigned Ticket's immutable history from player
eligibility. While a Match is READY or RUNNING, its assigned Tickets remain
active. Completing the Match stores its result and clears all ten active flags
in one transaction, allowing those players to queue again without deleting
historical Tickets.

`matches` stores policy version, quality sub-scores, team snapshots (including
assigned Hero, Hero proficiency, and behavior stability), server
allocation, state, prediction, result, actual quality, and a monotonic
revision. Updates reject stale revisions. The final READY Match update and all
reserved-to-assigned Ticket transitions share one database transaction.
Finished-Match quality analysis uses the existing policy/creation-time index;
historical replay joins the Match's immutable Ticket IDs back to durable Ticket
snapshots. Neither operation writes to Match, Ticket, or rating state.

`agent_runs` is an append-oriented audit record containing the Agent/model and
prompt versions, request and structured output, selected policy version,
status, timing, and the JSON transcript of allowlisted tool calls. A startup
recovery pass marks abandoned `RUNNING` rows failed rather than silently
rerunning them.

`policy_proposals` stores one proposal per Agent run: the candidate policy,
rationale, all five risk findings, requester/reviewer identities, state,
persisted rollout basis points and assignment salt, experiment ID, activation,
rollback, and timestamps. State updates use compare-and-swap predicates. The
run completion and initial proposal insert share one transaction.

The planned `outbox_events` table will store messages in the same transaction
as business changes. Consumers will deduplicate by event ID before applying
side effects.

## Redis ownership

Redis stores only rebuildable active coordination data:

- sorted sets per `mode:version:region` pool, scored by creation time;
- Ticket snapshots for fast candidate reads;
- reservation keys with TTL;
- an active-player key used by the reservation Lua script;
- short-lived worker leases.

Lua scripts atomically verify all ten Ticket states, reserve the entire batch,
or change nothing. PostgreSQL remains the durable source of truth. At startup,
the service rebuilds missing Redis queue entries from active PostgreSQL
Tickets and reconciles expired reservations.

PostgreSQL is the durable correctness path for Player, Ticket, Match, rating, and
Agent audit state. Redis is disposable coordination state: reservation scripts validate
all Ticket metadata before the first write, retries with the same reservation
are idempotent, and PostgreSQL rejection rolls the Redis reservation back. A
startup pass recovers expired PostgreSQL reservations and recreates missing
queued/reserved Redis entries.
