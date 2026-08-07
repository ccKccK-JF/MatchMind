CREATE TABLE IF NOT EXISTS tickets (
    id TEXT CONSTRAINT tickets_pkey PRIMARY KEY,
    player_id TEXT NOT NULL REFERENCES players(id),
    party_id TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    client_version TEXT NOT NULL,
    region TEXT NOT NULL,
    rating DOUBLE PRECISION NOT NULL CHECK (rating > 0),
    preferred_roles TEXT[] NOT NULL CHECK (cardinality(preferred_roles) BETWEEN 1 AND 5),
    region_latency JSONB NOT NULL,
    state TEXT NOT NULL CHECK (state IN (
        'CREATED', 'QUEUED', 'RESERVED', 'ASSIGNED',
        'CANCELLED', 'EXPIRED', 'FAILED'
    )),
    create_idempotency_key TEXT NOT NULL,
    reservation_id TEXT,
    reservation_expires_at TIMESTAMPTZ,
    match_id TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT tickets_player_create_key_unique UNIQUE (player_id, create_idempotency_key),
    CONSTRAINT tickets_reservation_consistency CHECK (
        (state = 'RESERVED' AND reservation_id IS NOT NULL AND reservation_expires_at IS NOT NULL AND match_id IS NULL)
        OR (state = 'ASSIGNED' AND reservation_id IS NOT NULL AND reservation_expires_at IS NOT NULL AND match_id IS NOT NULL)
        OR (state NOT IN ('RESERVED', 'ASSIGNED') AND reservation_id IS NULL AND reservation_expires_at IS NULL AND match_id IS NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS tickets_one_active_per_player_idx
    ON tickets (player_id)
    WHERE state IN ('CREATED', 'QUEUED', 'RESERVED', 'ASSIGNED');

CREATE INDEX IF NOT EXISTS tickets_pool_queue_idx
    ON tickets (mode, client_version, region, created_at, id)
    WHERE state = 'QUEUED';

CREATE INDEX IF NOT EXISTS tickets_reservation_idx
    ON tickets (reservation_id)
    WHERE state = 'RESERVED';

CREATE INDEX IF NOT EXISTS tickets_expired_reservation_idx
    ON tickets (reservation_expires_at)
    WHERE state = 'RESERVED';

CREATE TABLE IF NOT EXISTS ticket_cancel_idempotency (
    player_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (player_id, idempotency_key)
);
