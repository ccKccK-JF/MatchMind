ALTER TABLE players
    ADD COLUMN IF NOT EXISTS rating_volatility DOUBLE PRECISION NOT NULL DEFAULT 0.06
    CHECK (rating_volatility > 0);

ALTER TABLE rating_changes
    ADD COLUMN IF NOT EXISTS deviation_before DOUBLE PRECISION NOT NULL DEFAULT 350
    CHECK (deviation_before > 0),
    ADD COLUMN IF NOT EXISTS deviation_after DOUBLE PRECISION NOT NULL DEFAULT 350
    CHECK (deviation_after > 0),
    ADD COLUMN IF NOT EXISTS volatility_before DOUBLE PRECISION NOT NULL DEFAULT 0.06
    CHECK (volatility_before > 0),
    ADD COLUMN IF NOT EXISTS volatility_after DOUBLE PRECISION NOT NULL DEFAULT 0.06
    CHECK (volatility_after > 0),
    ADD COLUMN IF NOT EXISTS rating_system TEXT NOT NULL DEFAULT 'elo'
    CHECK (rating_system IN ('elo', 'glicko2'));
