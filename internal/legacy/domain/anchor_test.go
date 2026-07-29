package domain_test

import (
	"testing"

	"github.com/wolfhead/pactline/internal/legacy/domain"

	"github.com/stretchr/testify/require"
)

func TestValidateAnchorExample(t *testing.T) {
	cases := []struct {
		name    string
		a       domain.AnchorExample
		wantErr error
	}{
		{"value dimension with value level", domain.AnchorExample{Dimension: domain.AnchorDimensionValue, Level: "A"}, nil},
		{"difficulty dimension with difficulty level", domain.AnchorExample{Dimension: domain.AnchorDimensionDifficulty, Level: "L"}, nil},
		{"value dimension with difficulty level is rejected", domain.AnchorExample{Dimension: domain.AnchorDimensionValue, Level: "XL"}, domain.ErrInvalidAnchorLevel},
		{"difficulty dimension with value level is rejected", domain.AnchorExample{Dimension: domain.AnchorDimensionDifficulty, Level: "B"}, domain.ErrInvalidAnchorLevel},
		{"unknown dimension is rejected", domain.AnchorExample{Dimension: "WEIRD", Level: "A"}, domain.ErrInvalidAnchorDimension},
		{"unknown level is rejected", domain.AnchorExample{Dimension: domain.AnchorDimensionValue, Level: "Q"}, domain.ErrInvalidAnchorLevel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.ValidateAnchorExample(tc.a)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
