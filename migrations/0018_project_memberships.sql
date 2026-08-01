CREATE TABLE project_memberships (
    id          uuid PRIMARY KEY,
    project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users(id),
    role        text NOT NULL CHECK (role IN ('admin', 'member')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, user_id)
);

CREATE INDEX idx_project_memberships_user
    ON project_memberships (user_id, project_id);
CREATE INDEX idx_project_memberships_project_role
    ON project_memberships (project_id, role, user_id);

-- Existing Projects were visible to every active user. Preserve that behavior
-- before Project-scoped authorization is enabled.
INSERT INTO project_memberships (id, project_id, user_id, role)
SELECT gen_random_uuid(), p.id, u.id, 'member'
FROM projects p
CROSS JOIN users u
WHERE u.active
ON CONFLICT (project_id, user_id) DO NOTHING;

-- The former owner and immutable creator become administrators. Keep inactive
-- historical identities as memberships so their role is not lost.
INSERT INTO project_memberships (id, project_id, user_id, role)
SELECT gen_random_uuid(), id, owner_id, 'admin' FROM projects
ON CONFLICT (project_id, user_id)
DO UPDATE SET role = 'admin', updated_at = now();

INSERT INTO project_memberships (id, project_id, user_id, role)
SELECT gen_random_uuid(), id, creator_id, 'admin' FROM projects
ON CONFLICT (project_id, user_id)
DO UPDATE SET role = 'admin', updated_at = now();

DROP INDEX IF EXISTS idx_projects_owner;
ALTER TABLE projects DROP COLUMN owner_id;
