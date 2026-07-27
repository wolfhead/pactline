package domain_test

import (
	"testing"

	"bountyboard/internal/domain"

	"github.com/stretchr/testify/require"
)

func TestValidateCalibration(t *testing.T) {
	cases := []struct {
		name    string
		c       domain.Calibration
		wantErr error
	}{
		{"valid", domain.Calibration{OriginalValue: domain.ValueA, CalibratedValue: domain.ValueB, Quarter: "2026Q3"}, nil},
		{"same value both ways is still valid", domain.Calibration{OriginalValue: domain.ValueA, CalibratedValue: domain.ValueA, Quarter: "2026Q3"}, nil},
		{"bad original value", domain.Calibration{OriginalValue: "Z", CalibratedValue: domain.ValueB, Quarter: "2026Q3"}, domain.ErrInvalidValueLevel},
		{"bad calibrated value", domain.Calibration{OriginalValue: domain.ValueA, CalibratedValue: "Z", Quarter: "2026Q3"}, domain.ErrInvalidValueLevel},
		{"missing quarter", domain.Calibration{OriginalValue: domain.ValueA, CalibratedValue: domain.ValueB}, domain.ErrQuarterRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.ValidateCalibration(tc.c)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
