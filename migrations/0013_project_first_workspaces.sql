-- Project is now a long-lived workspace. Milestone owns delivery lifecycle and
-- acceptance, and every task belongs to exactly one Project.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM tasks WHERE project_id IS NULL)
       AND NOT EXISTS (SELECT 1 FROM users WHERE active) THEN
        RAISE EXCEPTION 'cannot migrate projectless tasks without an active user';
    END IF;
END $$;

INSERT INTO projects (
    id,
    name,
    outcome,
    description,
    owner_id,
    creator_id,
    status
)
SELECT
    '4f1dbd69-534f-4c6f-9950-000000000013'::uuid,
    '待整理',
    'Legacy tasks are assigned to a visible workspace',
    'Tasks migrated from the former project-optional model.',
    selected_owner.id,
    selected_owner.id,
    'active'
FROM LATERAL (
    SELECT id
    FROM users
    WHERE active
    ORDER BY
        CASE WHEN platform_role = 'ADMIN' THEN 0 ELSE 1 END,
        CASE WHEN id = '00000000-0000-0000-0000-000000000001'::uuid THEN 0 ELSE 1 END,
        created_at,
        id
    LIMIT 1
) AS selected_owner
WHERE EXISTS (SELECT 1 FROM tasks WHERE project_id IS NULL)
ON CONFLICT (id) DO NOTHING;

UPDATE tasks
SET project_id = '4f1dbd69-534f-4c6f-9950-000000000013'::uuid,
    updated_at = now()
WHERE project_id IS NULL;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM tasks WHERE project_id IS NULL) THEN
        RAISE EXCEPTION 'projectless tasks remain after Project-first migration';
    END IF;
END $$;

ALTER TABLE milestones
    ADD COLUMN owner_id uuid REFERENCES users(id);

UPDATE milestones AS milestone
SET owner_id = project.owner_id
FROM projects AS project
WHERE project.id = milestone.project_id;

ALTER TABLE milestones
    ALTER COLUMN owner_id SET NOT NULL,
    DROP CONSTRAINT milestones_status_check,
    ALTER COLUMN status DROP DEFAULT;

UPDATE milestones
SET status = 'active',
    updated_at = now()
WHERE status = 'open';

ALTER TABLE milestones
    ADD CONSTRAINT milestones_status_check
        CHECK (status IN ('planned', 'active', 'completed', 'cancelled')),
    ALTER COLUMN status SET DEFAULT 'planned';

CREATE INDEX idx_milestones_owner
    ON milestones (owner_id, status, target_date);

DELETE FROM acceptance_checks
WHERE criterion_id IN (
    SELECT id
    FROM acceptance_criteria
    WHERE project_id IS NOT NULL
);

DELETE FROM acceptance_criteria
WHERE project_id IS NOT NULL;

DROP INDEX idx_acceptance_criteria_project;

ALTER TABLE acceptance_criteria
    DROP CONSTRAINT acceptance_criteria_exactly_one_owner,
    DROP COLUMN project_id,
    ADD CONSTRAINT acceptance_criteria_exactly_one_owner
        CHECK (
            (milestone_id IS NOT NULL)::integer
            + (task_id IS NOT NULL)::integer = 1
        );

ALTER TABLE tasks
    DROP CONSTRAINT tasks_milestone_requires_project,
    ALTER COLUMN project_id SET NOT NULL;

DROP INDEX idx_projects_status;

ALTER TABLE projects
    DROP COLUMN outcome,
    DROP COLUMN status,
    DROP COLUMN target_date,
    DROP COLUMN completed_at,
    DROP COLUMN cancelled_at;

CREATE INDEX idx_projects_active_name
    ON projects (lower(name), number)
    WHERE archived_at IS NULL;
