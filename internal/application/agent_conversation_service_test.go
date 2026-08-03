package application

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectUpdatePreservesExplicitInactiveBinding(t *testing.T) {
	inactive := false
	require.False(t, *bindingActiveForProjectUpdate(&inactive))
	require.True(t, *bindingActiveForProjectUpdate(nil))
}
