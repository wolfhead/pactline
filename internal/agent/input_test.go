package agent

import (
	"testing"

	"github.com/wolfhead/pactline/internal/agent/artifact"

	"github.com/stretchr/testify/require"
)

func TestCommandEnvelopePreservesTriggerArtifactsAndLegacyCommands(t *testing.T) {
	encoded, err := EncodeCommandEnvelope("create a Task", []artifact.Reference{{
		ID: "artifact-1", Kind: artifact.KindImage, Name: "report.png",
		Availability: artifact.AvailabilityAvailable,
	}})
	require.NoError(t, err)

	envelope, err := DecodeCommandEnvelope(encoded)
	require.NoError(t, err)
	require.Equal(t, "create a Task", envelope.Text)
	require.Len(t, envelope.Artifacts, 1)
	require.Equal(t, "artifact-1", envelope.Artifacts[0].ID)

	legacy, err := DecodeCommandEnvelope([]byte("legacy command"))
	require.NoError(t, err)
	require.Equal(t, "legacy command", legacy.Text)
	require.Empty(t, legacy.Artifacts)
}
