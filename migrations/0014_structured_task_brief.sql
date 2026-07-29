ALTER TABLE tasks
    ADD COLUMN context text NOT NULL DEFAULT '',
    ADD COLUMN expected_result text NOT NULL DEFAULT '';
