ALTER TABLE acceptance_criteria
    ADD COLUMN task_id uuid REFERENCES tasks(id);

ALTER TABLE acceptance_criteria
    DROP CONSTRAINT acceptance_criteria_check,
    ADD CONSTRAINT acceptance_criteria_exactly_one_owner
        CHECK (
            (project_id IS NOT NULL)::integer
            + (milestone_id IS NOT NULL)::integer
            + (task_id IS NOT NULL)::integer = 1
        );

CREATE INDEX idx_acceptance_criteria_task
    ON acceptance_criteria (task_id, position) WHERE task_id IS NOT NULL;
