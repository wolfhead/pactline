-- Phase 2: scoring's two append-only side tables.
--
-- bounties.value_level, difficulty, completion, settled_score and settled_at
-- already exist from migrations/0001_init.sql; they were created there and
-- deliberately left unused until this phase. Nothing about the bounties
-- table changes here.

-- calibrations is spec §4.6's quarterly value-versus-reality correction.
-- Stored as its own row rather than mutating bounties.settled_score, so the
-- settlement snapshot stays the historical fact and the calibration is a
-- separate, attributable correction layered on top (spec §7.2: changing
-- constants, or later judgement, must never rewrite history).
CREATE TABLE calibrations (
    id                uuid PRIMARY KEY,
    bounty_id         uuid NOT NULL REFERENCES bounties(id) ON DELETE CASCADE,
    quarter           text NOT NULL,
    original_value    text NOT NULL,
    calibrated_value  text NOT NULL,
    calibrated_score  numeric NOT NULL,
    note              text NOT NULL DEFAULT '',
    created_by        uuid NOT NULL REFERENCES users(id),
    created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_calibrations_bounty ON calibrations (bounty_id);

-- anchor_examples is spec §4.7's precedent list: a handful of already-graded
-- bounties kept per level so that leveling arguments converge on a specific
-- prior example instead of restarting from first principles every quarter.
-- Plain CRUD, steward-managed — no suggestion logic, no auto-promotion.
CREATE TABLE anchor_examples (
    id         uuid PRIMARY KEY,
    dimension  text NOT NULL,
    level      text NOT NULL,
    bounty_id  uuid NOT NULL REFERENCES bounties(id) ON DELETE CASCADE,
    note       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_anchor_examples_dimension_level ON anchor_examples (dimension, level);
