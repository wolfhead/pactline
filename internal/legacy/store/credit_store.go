package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	userdomain "github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/legacy/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreditStore reads and writes attribution records.
type CreditStore struct{ db *DB }

// NewCreditStore wires a CreditStore to the pool.
func NewCreditStore(db *DB) *CreditStore { return &CreditStore{db: db} }

const creditColumns = `id, bounty_id, user_id, role, nominated_by, evidence, status, confirmed_at, created_at`

// Nominate records a credit. Re-nominating the same (bounty, user, role) is a
// no-op that returns the existing row, so retries are safe.
func (s *CreditStore) Nominate(ctx context.Context, c domain.Credit) (domain.Credit, error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.Status == "" {
		c.Status = domain.CreditPending
	}

	tag, err := s.db.Pool.Exec(ctx, `
		INSERT INTO credits (id, bounty_id, user_id, role, nominated_by, evidence, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (bounty_id, user_id, role) DO NOTHING`,
		c.ID, c.BountyID, c.UserID, c.Role, c.NominatedBy, nullableString(c.Evidence), c.Status,
	)
	if err != nil {
		return domain.Credit{}, fmt.Errorf("insert credit: %w", err)
	}
	inserted := tag.RowsAffected() > 0

	row := s.db.Pool.QueryRow(ctx,
		`SELECT `+creditColumns+` FROM credits WHERE bounty_id=$1 AND user_id=$2 AND role=$3`,
		c.BountyID, c.UserID, c.Role)
	out, err := scanCredit(row)
	if err != nil {
		return domain.Credit{}, err
	}

	// A conflict means the row already existed. If what was just submitted
	// differs from what's stored, that data was silently discarded by
	// DO NOTHING — make that visible instead of logging an identical-looking
	// info line.
	if !inserted {
		evidenceDiffers := c.Evidence != out.Evidence
		nominatedByDiffers := !uuidPtrEqual(c.NominatedBy, out.NominatedBy)
		if evidenceDiffers || nominatedByDiffers {
			slog.Warn("credit nomination conflict discarded submitted data",
				"credit_id", out.ID, "bounty_id", out.BountyID, "user_id", out.UserID, "role", out.Role,
				"submitted_evidence", c.Evidence, "stored_evidence", out.Evidence,
				"submitted_nominated_by", c.NominatedBy, "stored_nominated_by", out.NominatedBy)
			return out, nil
		}
	}

	slog.Info("credit nominated",
		"credit_id", out.ID, "bounty_id", out.BountyID, "user_id", out.UserID,
		"role", out.Role, "nominated_by", out.NominatedBy, "status", out.Status)
	return out, nil
}

// uuidPtrEqual reports whether two possibly-nil uuid pointers refer to the
// same value.
func uuidPtrEqual(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// GetByID returns one credit, or userdomain.ErrNotFound.
func (s *CreditStore) GetByID(ctx context.Context, id uuid.UUID) (domain.Credit, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+creditColumns+` FROM credits WHERE id=$1`, id)
	c, err := scanCredit(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Credit{}, userdomain.ErrNotFound
	}
	return c, err
}

// ListByBounty returns every credit on a bounty regardless of status.
func (s *CreditStore) ListByBounty(ctx context.Context, bountyID uuid.UUID) ([]domain.Credit, error) {
	return s.query(ctx, `SELECT `+creditColumns+` FROM credits WHERE bounty_id=$1 ORDER BY created_at`, bountyID)
}

// ListPendingForUser returns credits awaiting this user's acknowledgement.
func (s *CreditStore) ListPendingForUser(ctx context.Context, userID uuid.UUID) ([]domain.Credit, error) {
	return s.query(ctx,
		`SELECT `+creditColumns+` FROM credits WHERE user_id=$1 AND status='PENDING' ORDER BY created_at DESC`,
		userID)
}

// Respond sets the credit's status, stamping confirmed_at on confirmation.
func (s *CreditStore) Respond(ctx context.Context, id uuid.UUID, status domain.CreditStatus) (domain.Credit, error) {
	var confirmedAt *time.Time
	if status == domain.CreditConfirmed {
		now := time.Now().UTC()
		confirmedAt = &now
	}
	row := s.db.Pool.QueryRow(ctx,
		`UPDATE credits SET status=$2, confirmed_at=$3 WHERE id=$1 RETURNING `+creditColumns,
		id, status, confirmedAt)
	c, err := scanCredit(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Credit{}, userdomain.ErrNotFound
	}
	if err != nil {
		return domain.Credit{}, err
	}
	slog.Info("credit responded",
		"credit_id", c.ID, "bounty_id", c.BountyID, "user_id", c.UserID, "status", c.Status)
	return c, nil
}

// InheritDefineCredits gives the plan bounty's author a DEFINE credit on the
// delivery bounty that descends from it, so the defining half of the work is
// never lost just because nobody remembered to name it.
//
// The inherited credit is PENDING like any other: the system ensures nobody is
// forgotten, it does not confirm on anyone's behalf.
func (s *CreditStore) InheritDefineCredits(ctx context.Context, child domain.Bounty) (int, error) {
	if child.ParentID == nil {
		return 0, nil
	}

	authors, err := s.confirmedLeadHolders(ctx, *child.ParentID)
	if err != nil {
		return 0, err
	}
	source := "parent_confirmed_lead"

	if len(authors) == 0 {
		var claimedBy *uuid.UUID
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT claimed_by FROM bounties WHERE id=$1`, *child.ParentID,
		).Scan(&claimedBy); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, userdomain.ErrNotFound
			}
			return 0, fmt.Errorf("read parent claimer: %w", err)
		}
		if claimedBy == nil {
			slog.Warn("parent plan has neither confirmed lead nor claimer; no DEFINE credit inherited",
				"child_id", child.ID, "parent_id", *child.ParentID)
			return 0, nil
		}
		authors = []uuid.UUID{*claimedBy}
		source = "parent_claimed_by"
	}

	for _, a := range authors {
		if _, err := s.Nominate(ctx, domain.Credit{
			BountyID: child.ID,
			UserID:   a,
			Role:     domain.CreditRoleDefine,
			Status:   domain.CreditPending,
		}); err != nil {
			return 0, err
		}
	}
	slog.Info("define credits inherited",
		"child_id", child.ID, "parent_id", *child.ParentID, "count", len(authors), "source", source)
	return len(authors), nil
}

func (s *CreditStore) confirmedLeadHolders(ctx context.Context, bountyID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT user_id FROM credits WHERE bounty_id=$1 AND role=$2 AND status='CONFIRMED'`,
		bountyID, domain.CreditRoleLead)
	if err != nil {
		return nil, fmt.Errorf("query parent leads: %w", err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan parent lead: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListConfirmedBountyIDsForUser maps each bounty the user is credited on to the
// roles they hold there. Only CONFIRMED credits are counted — this is the
// mechanism's one hard constraint (spec 6.2).
func (s *CreditStore) ListConfirmedBountyIDsForUser(ctx context.Context, userID uuid.UUID) (map[uuid.UUID][]domain.CreditRole, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT bounty_id, role FROM credits WHERE user_id=$1 AND status='CONFIRMED'`, userID)
	if err != nil {
		return nil, fmt.Errorf("query confirmed credits: %w", err)
	}
	defer rows.Close()

	out := map[uuid.UUID][]domain.CreditRole{}
	for rows.Next() {
		var (
			id   uuid.UUID
			role domain.CreditRole
		)
		if err := rows.Scan(&id, &role); err != nil {
			return nil, fmt.Errorf("scan confirmed credit: %w", err)
		}
		out[id] = append(out[id], role)
	}
	return out, rows.Err()
}

func (s *CreditStore) query(ctx context.Context, sql string, args ...any) ([]domain.Credit, error) {
	rows, err := s.db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query credits: %w", err)
	}
	defer rows.Close()

	out := []domain.Credit{}
	for rows.Next() {
		c, err := scanCredit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanCredit(s scanner) (domain.Credit, error) {
	var (
		c        domain.Credit
		evidence *string
	)
	err := s.Scan(&c.ID, &c.BountyID, &c.UserID, &c.Role, &c.NominatedBy,
		&evidence, &c.Status, &c.ConfirmedAt, &c.CreatedAt)
	if err != nil {
		return domain.Credit{}, fmt.Errorf("scan credit: %w", err)
	}
	if evidence != nil {
		c.Evidence = *evidence
	}
	return c, nil
}
