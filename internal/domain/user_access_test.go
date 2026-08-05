package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccessApprovalTransitions(t *testing.T) {
	require.True(t, CanChangeAccessStatus(AccessStatusPending, AccessStatusApproved))
	require.True(t, CanChangeAccessStatus(AccessStatusPending, AccessStatusRejected))
	require.True(t, CanChangeAccessStatus(AccessStatusRejected, AccessStatusApproved))
	require.False(t, CanChangeAccessStatus(AccessStatusApproved, AccessStatusRejected))
	require.False(t, CanChangeAccessStatus(AccessStatusRejected, AccessStatusPending))

	user := User{Active: true, AccessStatus: AccessStatusApproved}
	require.True(t, user.CanUseApplication())
	user.AccessStatus = AccessStatusPending
	require.False(t, user.CanUseApplication())
	user.AccessStatus = AccessStatusApproved
	user.Active = false
	require.False(t, user.CanUseApplication())
}
