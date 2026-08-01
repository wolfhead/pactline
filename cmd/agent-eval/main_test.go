package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wolfhead/pactline/internal/agent/evaluation"
)

func TestWriteMarkdownIncludesArtifactDescription(t *testing.T) {
	description, err := json.Marshal("The image shows 49.4% and says traffic release is blocked.\nThis is direct attachment evidence.")
	require.NoError(t, err)
	var output bytes.Buffer

	writeMarkdown(&output, []report{{
		Scenario: evaluation.Scenario{ID: "image", Name: "Image evidence"},
		Conversion: evaluation.ConversionArtifact{
			Model: "generator", PromptVersion: "prompt", Outcome: "task_created",
			ToolTrace: []evaluation.ToolTrace{{
				ToolName: "inspect_artifact", State: "completed", Result: description,
			}},
		},
	}})

	require.Contains(t, output.String(), "Artifact description:")
	require.Contains(t, output.String(), "> The image shows 49.4%")
	require.Contains(t, output.String(), "> This is direct attachment evidence.")
}
