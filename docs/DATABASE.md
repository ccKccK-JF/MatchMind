# Database design

The local process demo uses concurrency-safe memory repositories by default.
The Player and rating-history repository can now run on PostgreSQL through
`PLAYER_STORAGE_BACKEND=postgres`. Ticket/Match and Redis adapters remain the
next persistence increment.

## PostgreSQL ownership

`players` stores profile and current rating. `rating_changes` is append-only,
preserves response order with a per-Match sequence, and uses a unique
`(match_id, player_id)` key so result retries cannot update Elo twice. Elo
updates use a serializable transaction plus a Match-scoped PostgreSQL advisory
lock, making duplicate result delivery idempotent.

`tickets` stores the durable Ticket state, idempotency keys, reservation
metadata, Match ID, timestamps, and a numeric version for optimistic checks. A
partial unique index on `player_id` for active states prevents duplicate active
Tickets even when several API instances race.

`matches` stores policy version, quality sub-scores, teams as normalized child
rows, server allocation, state, prediction, result, and actual quality. Match
creation and durable Ticket assignment share one database transaction.

`outbox_events` stores messages in the same transaction as business changes.
Consumers deduplicate by event ID before applying side effects.

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

The concrete migrations and integration tests are delivered with the
persistence implementation rather than declaring this design complete in
advance.
