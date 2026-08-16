package domain_test

import (
	"testing"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestFleetHiddenNullableSchedulePatchPresence(t *testing.T) {
	t.Parallel()
	require.True(t, (domain.TaskPatch{}).IsEmpty())
	require.False(t, (domain.TaskPatch{StartDateSet: true}).IsEmpty())
	require.False(t, (domain.TaskPatch{DueDateSet: true}).IsEmpty())
}
