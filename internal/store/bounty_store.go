package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
	value_level, difficulty, commitment, completion, status,
	sponsor_id, claimed_by, claimed_at,
	person_days, retrospective, settled_score, settled_at,
	completed_at, created_at, updated_at`

// BountyFilter narrows a List query. Zero value means "everything" — in
// particular, a nil Viewer applies no draft-visibility restriction, which
// keeps every store-level test and internal call site that has no notion of
// a requesting user working unchanged.
type BountyFilter struct {
	Statuses           []domain.Status
	Type               *domain.BountyType
	BusinessTag        string
	ClaimedBy          *uuid.UUID
	SponsorID          *uuid.UUID
	OrderByCompletedAt bool

	// CompletedFrom and CompletedTo bound completed_at as the half-open
	// interval [CompletedFrom, CompletedTo): CompletedFrom is inclusive,
	// CompletedTo is exclusive. Both nil means unbounded. This backs
	// settlement (spec §7.2's "settle a period"): scanning every terminal
	// bounty whose completed_at falls in a range. Half-open, not inclusive at
	// both ends, so that two adjacent periods (e.g. back-to-back months) do
	// not both claim the boundary instant — a bounty completed at exactly
	// that instant would otherwise be a candidate in both runs.
	CompletedFrom *time.Time
	CompletedTo   *time.Time

	// Viewer scopes DRAFT visibility per spec §5 ("DRAFT 仅出题人可见"): unless
	// Viewer.IsSteward, a DRAFT bounty is included only when Viewer.ID equals
	// its sponsor_id. Every other status is unaffected. Callers reachable
	// from the untrusted HTTP API (bounty_handler.list) MUST set this to the
	// authenticated caller; leaving it nil is only safe for trusted,
	// non-user-facing call sites.
	Viewer *DraftViewer
}

// DraftViewer is the identity a List call is scoped against for DRAFT
// visibility. See BountyFilter.Viewer.
type DraftViewer struct {
	ID        uuid.UUID
	IsSteward bool
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
			value_level, difficulty, commitment, completion, status,
			sponsor_id, claimed_by, claimed_at,
			person_days, retrospective, settled_score, settled_at, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		RETURNING `+bountyColumns,
		b.ID, b.Type, b.ParentID, b.Title, b.Goal, b.AcceptanceCriteria,
		b.Visibility, nullableString(b.Restriction), b.DirectedTo, lines,
		nullableString(string(b.ValueLevel)), nullableString(string(b.Difficulty)), b.Commitment, nullableString(string(b.Completion)), b.Status,
		b.SponsorID, b.ClaimedBy, b.ClaimedAt,
		b.PersonDays, nullableString(b.Retrospective), b.SettledScore, b.SettledAt, b.CompletedAt)

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
//
// SponsorID is immutable: it is who opened the bounty, a fact about its
// origin rather than a mutable attribute. The SET clause below deliberately
// never touches sponsor_id, so any change to b.SponsorID made by the caller
// before calling Update is silently ignored — do not "fix" this by adding
// sponsor_id to the UPDATE.
//
// SettledScore and SettledAt are likewise immutable here, for a related but
// distinct reason: spec §7.2 requires that reading a settled score always
// reads the snapshot, never recomputes it, and that changing scoring
// constants must not rewrite history. Routing every ordinary write (claim,
// deliver, amend, value-level and difficulty edits) through this one method
// while keeping it structurally unable to touch those two columns means no
// future call site can accidentally clobber a settlement snapshot merely by
// round-tripping a fetched Bounty through Update. The only writer of those
// columns is Settle.
func (s *BountyStore) Update(ctx context.Context, b domain.Bounty) (domain.Bounty, error) {
	lines, err := json.Marshal(nonNilLines(b.BusinessLines))
	if err != nil {
		return domain.Bounty{}, fmt.Errorf("marshal business_lines: %w", err)
	}

	row := s.db.Pool.QueryRow(ctx, `
		UPDATE bounties SET
			type=$2, parent_id=$3, title=$4, goal=$5, acceptance_criteria=$6,
			visibility=$7, restriction=$8, directed_to=$9, business_lines=$10,
			value_level=$11, difficulty=$12, commitment=$13, completion=$14, status=$15,
			claimed_by=$16, claimed_at=$17,
			person_days=$18, retrospective=$19, completed_at=$20, updated_at=now()
		WHERE id=$1
		RETURNING `+bountyColumns,
		b.ID, b.Type, b.ParentID, b.Title, b.Goal, b.AcceptanceCriteria,
		b.Visibility, nullableString(b.Restriction), b.DirectedTo, lines,
		nullableString(string(b.ValueLevel)), nullableString(string(b.Difficulty)), b.Commitment, nullableString(string(b.Completion)), b.Status,
		b.ClaimedBy, b.ClaimedAt,
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
	if f.CompletedFrom != nil {
		add("completed_at >= $%d", *f.CompletedFrom)
	}
	if f.CompletedTo != nil {
		add("completed_at < $%d", *f.CompletedTo)
	}
	if f.Viewer != nil && !f.Viewer.IsSteward {
		// A DRAFT row is included only when the viewer is its sponsor; every
		// other status passes through untouched. Doing this in SQL (rather
		// than filtering the Go slice after Query) means a large draft
		// population never leaks through a paginated read later.
		add("(status <> 'DRAFT' OR sponsor_id = $%d)", f.Viewer.ID)
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

// Settle snapshots a computed score onto a terminal bounty exactly once.
// The WHERE clause's "AND settled_at IS NULL" makes the write conditional at
// the database level, not just in application logic: if two settlement runs
// somehow race on the same bounty, only the first commits and the second
// gets domain.ErrAlreadySettled back instead of silently overwriting the
// snapshot spec §7.2 requires to be immutable history.
func (s *BountyStore) Settle(ctx context.Context, id uuid.UUID, score float64, settledAt time.Time) (domain.Bounty, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE bounties SET settled_score=$2, settled_at=$3
		WHERE id=$1 AND settled_at IS NULL
		RETURNING `+bountyColumns,
		id, score, settledAt)
	out, err := scanBounty(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Bounty{}, domain.ErrAlreadySettled
	}
	if err != nil {
		return domain.Bounty{}, err
	}
	slog.Info("bounty settled",
		"bounty_id", out.ID, "value_level", out.ValueLevel, "difficulty", out.Difficulty,
		"completion", out.Completion, "commitment", out.Commitment, "status", out.Status,
		"settled_score", score, "settled_at", settledAt)
	return out, nil
}

func scanBounty(s scanner) (domain.Bounty, error) {
	var (
		b           domain.Bounty
		lines       []byte
		restriction *string
		valueLevel  *string
		difficulty  *string
		completion  *string
		retro       *string
	)
	err := s.Scan(
		&b.ID, &b.Type, &b.ParentID, &b.Title, &b.Goal, &b.AcceptanceCriteria,
		&b.Visibility, &restriction, &b.DirectedTo, &lines,
		&valueLevel, &difficulty, &b.Commitment, &completion, &b.Status,
		&b.SponsorID, &b.ClaimedBy, &b.ClaimedAt,
		&b.PersonDays, &retro, &b.SettledScore, &b.SettledAt,
		&b.CompletedAt, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return domain.Bounty{}, fmt.Errorf("scan bounty: %w", err)
	}
	if restriction != nil {
		b.Restriction = *restriction
	}
	if valueLevel != nil {
		b.ValueLevel = domain.ValueLevel(*valueLevel)
	}
	if difficulty != nil {
		b.Difficulty = domain.Difficulty(*difficulty)
	}
	if completion != nil {
		b.Completion = domain.Completion(*completion)
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
