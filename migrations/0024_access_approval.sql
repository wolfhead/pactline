ALTER TABLE users
    ADD COLUMN access_status text NOT NULL DEFAULT 'APPROVED'
        CHECK (access_status IN ('PENDING', 'APPROVED', 'REJECTED'));

-- Every pre-existing account was admitted through the old invitation or
-- bootstrap flow and therefore retains access during the cutover.
UPDATE users SET access_status = 'APPROVED';

CREATE INDEX users_access_requests
    ON users (access_status, created_at)
    WHERE platform_role = 'MEMBER' AND access_status <> 'APPROVED';
