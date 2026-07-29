package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wolfhead/pactline/internal/legacy/domain"

	"github.com/google/uuid"
)

// CalibrationStore reads and writes quarterly value calibrations.
type CalibrationStore struct{ db *DB }

// NewCalibrationStore wires a CalibrationStore to the pool.
func NewCalibrationStore(db *DB) *CalibrationStore { return &CalibrationStore{db: db} }

const calibrationColumns = `id, bounty_id, quarter, original_value, original_score, calibrated_value, calibrated_score, note, created_by, created_at`

// Create records a calibration. Calibrations are append-only: there is no
// Update or Delete, because each row is itself a historical correction —
// correcting a calibration means recording a new one, not editing the old.
func (s *CalibrationStore) Create(ctx context.Context, c domain.Calibration) (domain.Calibration, error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO calibrations (id, bounty_id, quarter, original_value, original_score, calibrated_value, calibrated_score, note, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+calibrationColumns,
		c.ID, c.BountyID, c.Quarter, c.OriginalValue, c.OriginalScore, c.CalibratedValue, c.CalibratedScore, c.Note, c.CreatedBy)
	out, err := scanCalibration(row)
	if err != nil {
		return domain.Calibration{}, err
	}
	slog.Info("calibration recorded",
		"calibration_id", out.ID, "bounty_id", out.BountyID, "quarter", out.Quarter,
		"original_value", out.OriginalValue, "original_score", out.OriginalScore,
		"calibrated_value", out.CalibratedValue, "calibrated_score", out.CalibratedScore,
		"created_by", out.CreatedBy)
	return out, nil
}

// ListByBounty returns every calibration recorded against a bounty, oldest
// first, so the most recent correction is always the last element.
func (s *CalibrationStore) ListByBounty(ctx context.Context, bountyID uuid.UUID) ([]domain.Calibration, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+calibrationColumns+` FROM calibrations WHERE bounty_id=$1 ORDER BY created_at`, bountyID)
	if err != nil {
		return nil, fmt.Errorf("query calibrations: %w", err)
	}
	defer rows.Close()

	out := []domain.Calibration{}
	for rows.Next() {
		c, err := scanCalibration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanCalibration(s scanner) (domain.Calibration, error) {
	var c domain.Calibration
	if err := s.Scan(&c.ID, &c.BountyID, &c.Quarter, &c.OriginalValue, &c.OriginalScore, &c.CalibratedValue,
		&c.CalibratedScore, &c.Note, &c.CreatedBy, &c.CreatedAt); err != nil {
		return domain.Calibration{}, fmt.Errorf("scan calibration: %w", err)
	}
	return c, nil
}
