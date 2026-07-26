package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// BountyStore reads and writes bounties (which become works once terminal).
type BountyStore struct{ db *DB }

// NewBountyStore wires a BountyStore to the pool.
func NewBountyStore(db *DB) *BountyStore { return &BountyStore{db: db} }

const bountyColumns = `
	id, type, parent_id, title, goal, acceptance_criteria,
	visibility, restriction, directed_to, business_lines,
	commitment, status, sponsor_id, claimed_by, claimed_at,
	person_days, retrospective, completed_at, created_at, updated_at`

// BountyFilter narrows a List query. Zero value means "everything".
type BountyFilter struct {
	Statuses           []domain.Status
	Type               *domain.BountyType
	BusinessTag        string
	ClaimedBy          *uuid.UUID
	SponsorID          *uuid.UUID
	OrderByCompletedAt bool
}

// Create inserts a bounty, assigning an ID when absent.
func (s *BountyStore) Create(ctx context.Context, b domain.Bounty) (domain.Bounty, error) {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	lines, err := json.Marshal(nonNilLines(b.BusinessLines))
	if err != nil {
		return domain.Bounty{}, fmt.Errorf("marshal business_lines: %w", err)
	}

	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO bounties (
			id, type, parent_id, title, goal, acceptance_criteria,
			visibility, restriction, directed_to, business_lines,
			commitment, status, sponsor_id, claimed_by, claimed_at,
			person_days, retrospective, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING `+bountyColumns,
		b.ID, b.Type, b.ParentID, b.Title, b.Goal, b.AcceptanceCriteria,
		b.Visibility, nullableString(b.Restriction), b.DirectedTo, lines,
		b.Commitment, b.Status, b.SponsorID, b.ClaimedBy, b.ClaimedAt,
		b.PersonDays, nullableString(b.Retrospective), b.CompletedAt)

	out, err := scanBounty(row)
	if err != nil {
		return domain.Bounty{}, err
	}
	slog.Info("bounty created",
		"bounty_id", out.ID, "type", out.Type, "status", out.Status,
		"sponsor_id", out.SponsorID, "title", out.Title,
		"business_lines", string(lines))
	return out, nil
}

// GetByID returns one bounty, or domain.ErrNotFound.
func (s *BountyStore) GetByID(ctx context.Context, id uuid.UUID) (domain.Bounty, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+bountyColumns+` FROM bounties WHERE id = $1`, id)
	b, err := scanBounty(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Bounty{}, domain.ErrNotFound
	}
	return b, err
}

// Update overwrites every mutable column and refreshes updated_at.
func (s *BountyStore) Update(ctx context.Context, b domain.Bounty) (domain.Bounty, error) {
	lines, err := json.Marshal(nonNilLines(b.BusinessLines))
	if err != nil {
		return domain.Bounty{}, fmt.Errorf("marshal business_lines: %w", err)
	}

	row := s.db.Pool.QueryRow(ctx, `
		UPDATE bounties SET
			type=$2, parent_id=$3, title=$4, goal=$5, acceptance_criteria=$6,
			visibility=$7, restriction=$8, directed_to=$9, business_lines=$10,
			commitment=$11, status=$12, claimed_by=$13, claimed_at=$14,
			person_days=$15, retrospective=$16, completed_at=$17, updated_at=now()
		WHERE id=$1
		RETURNING `+bountyColumns,
		b.ID, b.Type, b.ParentID, b.Title, b.Goal, b.AcceptanceCriteria,
		b.Visibility, nullableString(b.Restriction), b.DirectedTo, lines,
		b.Commitment, b.Status, b.ClaimedBy, b.ClaimedAt,
		b.PersonDays, nullableString(b.Retrospective), b.CompletedAt)

	out, err := scanBounty(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Bounty{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Bounty{}, err
	}
	slog.Info("bounty updated",
		"bounty_id", out.ID, "status", out.Status, "claimed_by", out.ClaimedBy)
	return out, nil
}

// List returns bounties matching the filter.
func (s *BountyStore) List(ctx context.Context, f BountyFilter) ([]domain.Bounty, error) {
	var (
		where []string
		args  []any
	)
	add := func(clause string, arg any) {
		args = append(args, arg)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}

	if len(f.Statuses) > 0 {
		statuses := make([]string, len(f.Statuses))
		for i, s := range f.Statuses {
			statuses[i] = string(s)
		}
		add("status = ANY($%d)", statuses)
	}
	if f.Type != nil {
		add("type = $%d", *f.Type)
	}
	if f.BusinessTag != "" {
		add(`business_lines @> jsonb_build_array(jsonb_build_object('tag', $%d::text))`, f.BusinessTag)
	}
	if f.ClaimedBy != nil {
		add("claimed_by = $%d", *f.ClaimedBy)
	}
	if f.SponsorID != nil {
		add("sponsor_id = $%d", *f.SponsorID)
	}

	q := `SELECT ` + bountyColumns + ` FROM bounties`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	if f.OrderByCompletedAt {
		q += " ORDER BY completed_at DESC NULLS LAST, created_at DESC"
	} else {
		q += " ORDER BY created_at DESC"
	}

	rows, err := s.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query bounties: %w", err)
	}
	defer rows.Close()

	out := []domain.Bounty{}
	for rows.Next() {
		b, err := scanBounty(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func scanBounty(s scanner) (domain.Bounty, error) {
	var (
		b           domain.Bounty
		lines       []byte
		restriction *string
		retro       *string
	)
	err := s.Scan(
		&b.ID, &b.Type, &b.ParentID, &b.Title, &b.Goal, &b.AcceptanceCriteria,
		&b.Visibility, &restriction, &b.DirectedTo, &lines,
		&b.Commitment, &b.Status, &b.SponsorID, &b.ClaimedBy, &b.ClaimedAt,
		&b.PersonDays, &retro, &b.CompletedAt, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return domain.Bounty{}, err
	}
	if restriction != nil {
		b.Restriction = *restriction
	}
	if retro != nil {
		b.Retrospective = *retro
	}
	if err := json.Unmarshal(lines, &b.BusinessLines); err != nil {
		return domain.Bounty{}, fmt.Errorf("unmarshal business_lines for %s: %w", b.ID, err)
	}
	return b, nil
}

func nonNilLines(l []domain.BusinessLine) []domain.BusinessLine {
	if l == nil {
		return []domain.BusinessLine{}
	}
	return l
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
