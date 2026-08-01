package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/agent/artifact"
)

func TestInspectArtifactRequiresGoalAndReturnsDescription(t *testing.T) {
	cleaned := false
	resolver := &artifactResolverStub{local: artifact.LocalFile{
		Reference: artifact.Reference{ID: "report", Kind: artifact.KindCSV, Name: "report.csv"},
		Path:      "/opaque/test/path",
		Cleanup: func() error {
			cleaned = true
			return nil
		},
	}}
	describer := &artifactDescriberStub{description: "The bounded sample contains five rows."}
	config := Config{
		Run: pactagent.Run{
			ID: uuid.New(), TenantID: "tenant", ConversationID: "conversation",
			TriggerOccurredAt: time.Now(),
		},
		Artifacts: resolver, ArtifactDescriber: describer,
	}

	_, err := inspectArtifact(context.Background(), config, InspectArtifactInput{
		ArtifactID: "report",
	})
	require.ErrorIs(t, err, ErrToolInput)
	require.False(t, resolver.called)

	description, err := inspectArtifact(context.Background(), config, InspectArtifactInput{
		ArtifactID: "report", AnalysisGoal: "Determine whether this is the full population.",
	})
	require.NoError(t, err)
	require.Equal(t, "The bounded sample contains five rows.", description)
	require.Equal(t, "Determine whether this is the full population.", describer.goal)
	require.True(t, cleaned)
}

func TestInspectArtifactReportsDescriptionAndCleanupFailures(t *testing.T) {
	descriptionErr := errors.New("description failed")
	cleanupErr := errors.New("cleanup failed")
	config := Config{
		Run: pactagent.Run{
			ID: uuid.New(), TenantID: "tenant", ConversationID: "conversation",
			TriggerOccurredAt: time.Now(),
		},
		Artifacts: &artifactResolverStub{local: artifact.LocalFile{
			Reference: artifact.Reference{ID: "report"}, Path: "/opaque/test/path",
			Cleanup: func() error { return cleanupErr },
		}},
		ArtifactDescriber: &artifactDescriberStub{err: descriptionErr},
	}

	_, err := inspectArtifact(context.Background(), config, InspectArtifactInput{
		ArtifactID: "report", AnalysisGoal: "Summarize the evidence.",
	})

	require.ErrorIs(t, err, descriptionErr)
	require.ErrorIs(t, err, cleanupErr)
}

type artifactResolverStub struct {
	local  artifact.LocalFile
	called bool
}

func (s *artifactResolverStub) Resolve(
	context.Context,
	artifact.Scope,
	string,
) (artifact.LocalFile, error) {
	s.called = true
	return s.local, nil
}

type artifactDescriberStub struct {
	description string
	goal        string
	err         error
}

func (s *artifactDescriberStub) Describe(
	_ context.Context,
	_ artifact.LocalFile,
	goal string,
) (string, error) {
	s.goal = goal
	return s.description, s.err
}
