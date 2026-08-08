ALTER TABLE players
    ADD COLUMN IF NOT EXISTS hero_proficiency JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(hero_proficiency) = 'object');

ALTER TABLE tickets
    ADD COLUMN IF NOT EXISTS behavior_score DOUBLE PRECISION NOT NULL DEFAULT 100
    CHECK (behavior_score BETWEEN 0 AND 100),
    ADD COLUMN IF NOT EXISTS hero_proficiency JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(hero_proficiency) = 'object');
