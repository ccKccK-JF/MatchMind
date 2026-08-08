ALTER TABLE players
    ADD COLUMN IF NOT EXISTS banned BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS ban_reason TEXT,
    ADD COLUMN IF NOT EXISTS banned_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS banned_by TEXT;

ALTER TABLE players
    ADD CONSTRAINT players_ban_state_check CHECK (
        (NOT banned AND ban_reason IS NULL AND banned_at IS NULL AND banned_by IS NULL)
        OR
        (banned AND length(trim(ban_reason)) > 0 AND banned_at IS NOT NULL AND length(trim(banned_by)) > 0)
    );
