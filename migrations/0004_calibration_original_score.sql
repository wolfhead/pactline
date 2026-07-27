-- I3 fix: a calibration row only stored one side of the comparison a reader
-- actually needs (settled-then vs calibrated-now). original_value was
-- already captured; original_score was not, forcing a reader to go re-fetch
-- the bounty and hope its settled_score still matched what was true at
-- calibration time. Persist it here instead, copied from the bounty's
-- settled_score at calibration time, so each row is a self-contained
-- before/after.
ALTER TABLE calibrations ADD COLUMN original_score numeric NOT NULL DEFAULT 0;
