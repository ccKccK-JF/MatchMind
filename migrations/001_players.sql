CREATE TABLE IF NOT EXISTS players (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    rating DOUBLE PRECISION NOT NULL CHECK (rating > 0),
    rating_deviation DOUBLE PRECISION NOT NULL CHECK (rating_deviation > 0),
    preferred_roles TEXT[] NOT NULL CHECK (cardinality(preferred_roles) BETWEEN 1 AND 5),
    home_region TEXT NOT NULL,
    region_latency JSONB NOT NULL,
    behavior_score DOUBLE PRECISION NOT NULL CHECK (behavior_score BETWEEN 0 AND 100),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS rating_changes (
    match_id TEXT NOT NULL,
    player_id TEXT NOT NULL REFERENCES players(id),
    sequence SMALLINT NOT NULL CHECK (sequence BETWEEN 0 AND 9),
    rating_before DOUBLE PRECISION NOT NULL CHECK (rating_before > 0),
    rating_after DOUBLE PRECISION NOT NULL CHECK (rating_after > 0),
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (match_id, player_id),
    UNIQUE (match_id, sequence)
);

CREATE INDEX IF NOT EXISTS rating_changes_player_history_idx
    ON rating_changes (player_id, created_at, match_id);
