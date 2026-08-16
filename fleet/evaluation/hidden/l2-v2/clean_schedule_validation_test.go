package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wolfhead/pactline/internal/domain"
)

func TestFleetHiddenScheduleValidationBoundaryTable(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	before := start.Add(-time.Nanosecond)
	after := start.Add(time.Nanosecond)
	for _, test := range []struct {
		name string
		start, due *time.Time
		valid bool
	}{
		{name: "none", valid: true},
		{name: "due only", due: &start, valid: true},
		{name: "start only", start: &start, valid: true},
		{name: "equal", start: &start, due: &start, valid: true},
		{name: "forward instant", start: &start, due: &after, valid: true},
		{name: "reversed instant", start: &start, due: &before},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := domain.ValidateSchedule(test.start, test.due)
			if test.valid { require.NoError(t, err) } else { require.ErrorIs(t, err, domain.ErrInvalidInput) }
		})
	}
}
