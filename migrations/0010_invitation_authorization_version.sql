ALTER TABLE authorization_transactions
    ADD COLUMN invitation_token_hash bytea;

-- Existing invitation transactions predate token-version binding and cannot
-- be safely backfilled after a token rotation. They are short-lived and must
-- restart through the public invitation-token validation flow.
DELETE FROM authorization_transactions
WHERE purpose = 'invitation';

ALTER TABLE authorization_transactions
    DROP CONSTRAINT authorization_transactions_check;

ALTER TABLE authorization_transactions
    ADD CONSTRAINT authorization_transactions_invitation_shape_check CHECK (
        (
            purpose = 'login'
            AND invitation_id IS NULL
            AND invitation_token_hash IS NULL
        )
        OR (
            purpose = 'invitation'
            AND invitation_id IS NOT NULL
            AND invitation_token_hash IS NOT NULL
        )
    );
