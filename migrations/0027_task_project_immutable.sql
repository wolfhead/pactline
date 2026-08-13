-- A Task's Project is its permanent workspace boundary. Moving work between
-- Projects creates authorization and history ambiguity, so new and historical
-- Tasks may change Milestone within the Project but never Project itself.

CREATE FUNCTION prevent_task_project_change() RETURNS trigger AS $$
BEGIN
    IF OLD.project_id IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'Task Project is immutable'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'tasks_project_immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER tasks_prevent_project_change
    BEFORE UPDATE OF project_id ON tasks
    FOR EACH ROW EXECUTE FUNCTION prevent_task_project_change();
