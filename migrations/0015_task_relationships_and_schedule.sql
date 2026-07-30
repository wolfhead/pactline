-- Add the scheduling and relationship primitives shared by List and Gantt.
-- Existing development tasks remain unscheduled and unrelated.

ALTER TABLE tasks
    ADD COLUMN start_date date,
    ADD COLUMN parent_task_id uuid REFERENCES tasks(id),
    ADD CONSTRAINT tasks_schedule_order
        CHECK (start_date IS NULL OR due_date IS NULL OR start_date <= due_date),
    ADD CONSTRAINT tasks_parent_not_self
        CHECK (parent_task_id IS NULL OR parent_task_id <> id);

CREATE INDEX idx_tasks_start_date ON tasks (start_date, number);
CREATE INDEX idx_tasks_parent ON tasks (parent_task_id, number)
    WHERE parent_task_id IS NOT NULL;

CREATE TABLE task_dependencies (
    task_id            uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on_task_id uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, depends_on_task_id),
    CHECK (task_id <> depends_on_task_id)
);

CREATE INDEX idx_task_dependencies_predecessor
    ON task_dependencies (depends_on_task_id, task_id);

CREATE FUNCTION validate_task_relationships()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    parent_parent_id uuid;
    parent_project_id uuid;
    parent_milestone_id uuid;
BEGIN
    IF NEW.parent_task_id IS NOT NULL THEN
        SELECT parent_task_id, project_id, milestone_id
        INTO parent_parent_id, parent_project_id, parent_milestone_id
        FROM tasks
        WHERE id = NEW.parent_task_id;

        IF NOT FOUND THEN
            RAISE EXCEPTION 'task parent does not exist';
        END IF;
        IF parent_parent_id IS NOT NULL THEN
            RAISE EXCEPTION 'task relationships support exactly one parent-child level';
        END IF;
        IF EXISTS (
            SELECT 1 FROM tasks child WHERE child.parent_task_id = NEW.id
        ) THEN
            RAISE EXCEPTION 'a task with children cannot become a child';
        END IF;
        IF parent_project_id <> NEW.project_id
           OR parent_milestone_id IS DISTINCT FROM NEW.milestone_id THEN
            RAISE EXCEPTION 'parent and child must share project and milestone';
        END IF;
    ELSE
        IF EXISTS (
            SELECT 1
            FROM tasks child
            WHERE child.parent_task_id = NEW.id
              AND (
                  child.project_id <> NEW.project_id
                  OR child.milestone_id IS DISTINCT FROM NEW.milestone_id
              )
        ) THEN
            RAISE EXCEPTION 'parent and children must share project and milestone';
        END IF;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM task_dependencies dependency
        JOIN tasks related
          ON related.id = CASE
              WHEN dependency.task_id = NEW.id
                  THEN dependency.depends_on_task_id
              ELSE dependency.task_id
          END
        WHERE (dependency.task_id = NEW.id OR dependency.depends_on_task_id = NEW.id)
          AND related.project_id <> NEW.project_id
    ) THEN
        RAISE EXCEPTION 'task dependencies must stay within one project';
    END IF;

    IF NEW.parent_task_id IS NOT NULL
       AND EXISTS (
           SELECT 1
           FROM task_dependencies dependency
           WHERE (dependency.task_id = NEW.id
                  AND dependency.depends_on_task_id = NEW.parent_task_id)
              OR (dependency.task_id = NEW.parent_task_id
                  AND dependency.depends_on_task_id = NEW.id)
       ) THEN
        RAISE EXCEPTION 'direct parent and child cannot also be dependencies';
    END IF;

    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER tasks_relationship_invariants
AFTER INSERT OR UPDATE OF parent_task_id, project_id, milestone_id
ON tasks
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION validate_task_relationships();

CREATE FUNCTION validate_task_dependency()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    task_project_id uuid;
    task_parent_id uuid;
    predecessor_project_id uuid;
    predecessor_parent_id uuid;
BEGIN
    SELECT project_id, parent_task_id
    INTO task_project_id, task_parent_id
    FROM tasks
    WHERE id = NEW.task_id;

    SELECT project_id, parent_task_id
    INTO predecessor_project_id, predecessor_parent_id
    FROM tasks
    WHERE id = NEW.depends_on_task_id;

    IF task_project_id <> predecessor_project_id THEN
        RAISE EXCEPTION 'task dependencies must stay within one project';
    END IF;
    IF task_parent_id = NEW.depends_on_task_id
       OR predecessor_parent_id = NEW.task_id THEN
        RAISE EXCEPTION 'direct parent and child cannot also be dependencies';
    END IF;
    IF EXISTS (
        WITH RECURSIVE reachable(id) AS (
            SELECT dependency.depends_on_task_id
            FROM task_dependencies dependency
            WHERE dependency.task_id = NEW.depends_on_task_id
            UNION
            SELECT dependency.depends_on_task_id
            FROM task_dependencies dependency
            JOIN reachable ON reachable.id = dependency.task_id
        )
        SELECT 1 FROM reachable WHERE id = NEW.task_id
    ) THEN
        RAISE EXCEPTION 'task dependency cycle is not allowed';
    END IF;

    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER task_dependency_invariants
AFTER INSERT OR UPDATE
ON task_dependencies
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION validate_task_dependency();
