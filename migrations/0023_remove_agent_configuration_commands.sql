UPDATE agent_runs
SET command_kind = 'direct'
WHERE command_kind = 'configuration';

ALTER TABLE agent_runs
    DROP CONSTRAINT agent_runs_command_kind_check,
    ADD CONSTRAINT agent_runs_command_kind_check
        CHECK (command_kind IN ('direct', 'discussion'));
