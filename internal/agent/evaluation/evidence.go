package evaluation

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	maxJudgeEvidenceBytes      = 16 << 10
	maxJudgeEvidenceTotalBytes = 48 << 10
)

// JudgeSourceEvidence is an evaluation-only reference derived from a synthetic
// fixture. It lets the Judge verify an artifact description without prescribing
// the Task that should be created.
type JudgeSourceEvidence struct {
	ArtifactID string `json:"artifact_id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated"`
}

func buildJudgeSourceEvidence(scenario Scenario) ([]JudgeSourceEvidence, error) {
	remaining := maxJudgeEvidenceTotalBytes
	var evidence []JudgeSourceEvidence
	for _, message := range scenario.Messages {
		for _, attached := range message.Artifacts {
			if remaining == 0 {
				return evidence, nil
			}
			encoded, err := scenarioFiles.ReadFile("testdata/artifacts/" + attached.Fixture)
			if err != nil {
				return nil, fmt.Errorf("read Judge source fixture %s: %w", attached.Fixture, err)
			}
			content, err := judgeEvidenceContent(attached.Fixture, encoded)
			if err != nil {
				return nil, err
			}
			limit := min(maxJudgeEvidenceBytes, remaining)
			bounded, truncated := truncateUTF8(content, limit)
			evidence = append(evidence, JudgeSourceEvidence{
				ArtifactID: attached.ID,
				Name:       attached.Name,
				Kind:       string(attached.Kind),
				Content:    bounded,
				Truncated:  truncated,
			})
			remaining -= len(bounded)
		}
	}
	return evidence, nil
}

func judgeEvidenceContent(fixture string, encoded []byte) (string, error) {
	switch {
	case strings.HasSuffix(fixture, ".image.json"):
		var source struct {
			Lines []string `json:"lines"`
		}
		if err := json.Unmarshal(encoded, &source); err != nil || len(source.Lines) == 0 {
			return "", fmt.Errorf("decode Judge image source fixture %s", fixture)
		}
		return "Visible text encoded in the synthetic image:\n" + strings.Join(source.Lines, "\n"), nil
	case strings.HasSuffix(fixture, ".workbook.json"):
		var source struct {
			Sheets []struct {
				Name string     `json:"name"`
				Rows [][]string `json:"rows"`
			} `json:"sheets"`
		}
		if err := json.Unmarshal(encoded, &source); err != nil || len(source.Sheets) == 0 {
			return "", fmt.Errorf("decode Judge workbook source fixture %s", fixture)
		}
		var output strings.Builder
		output.WriteString("Cells encoded in the synthetic workbook:")
		for _, sheet := range source.Sheets {
			output.WriteString("\n\nSheet: ")
			output.WriteString(sheet.Name)
			for _, row := range sheet.Rows {
				output.WriteByte('\n')
				output.WriteString(strings.Join(row, " | "))
			}
		}
		return output.String(), nil
	default:
		return "Content of the synthetic source file:\n" + string(encoded), nil
	}
}

func truncateUTF8(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	end := limit
	for end > 0 && value[end]&0xc0 == 0x80 {
		end--
	}
	return value[:end], true
}
