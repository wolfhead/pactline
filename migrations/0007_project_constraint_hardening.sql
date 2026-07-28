ALTER TABLE acceptance_checks
    ADD CONSTRAINT acceptance_checks_external_checker_ref_required
    CHECK (
        checker_type = 'user'
        OR (checker_ref IS NOT NULL AND btrim(checker_ref) <> '')
    );
