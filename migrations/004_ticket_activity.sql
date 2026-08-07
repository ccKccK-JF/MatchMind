ALTER TABLE tickets
    ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT TRUE;

UPDATE tickets AS ticket
SET active = CASE
    WHEN ticket.state IN ('CANCELLED', 'EXPIRED', 'FAILED') THEN FALSE
    WHEN ticket.state = 'ASSIGNED' AND EXISTS (
        SELECT 1 FROM matches AS match
        WHERE match.id = ticket.match_id AND match.state IN ('FINISHED', 'FAILED')
    ) THEN FALSE
    ELSE TRUE
END;

DROP INDEX IF EXISTS tickets_one_active_per_player_idx;

CREATE UNIQUE INDEX tickets_one_active_per_player_idx
    ON tickets (player_id)
    WHERE active;

CREATE INDEX IF NOT EXISTS tickets_match_active_idx
    ON tickets (match_id, id)
    WHERE active AND state = 'ASSIGNED';
