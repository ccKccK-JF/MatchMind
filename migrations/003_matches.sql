CREATE TABLE IF NOT EXISTS matches (
    id TEXT PRIMARY KEY,
    mode TEXT NOT NULL,
    team_a JSONB NOT NULL,
    team_b JSONB NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('CREATED', 'ALLOCATING', 'READY', 'RUNNING', 'FINISHED', 'FAILED')),
    server_region TEXT NOT NULL,
    server_address TEXT,
    connection_token TEXT,
    policy_version TEXT NOT NULL,
    quality JSONB NOT NULL,
    result JSONB,
    revision BIGINT NOT NULL CHECK (revision >= 1),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK ((server_address IS NULL) = (connection_token IS NULL)),
    CHECK ((state = 'FINISHED') = (result IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS matches_state_updated_idx
    ON matches (state, updated_at, id);

CREATE INDEX IF NOT EXISTS matches_policy_created_idx
    ON matches (policy_version, created_at, id);
