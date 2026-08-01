// Package evaluation runs Pactline's production first-party Agent against
// synthetic, non-mutating conversation scenarios.
package evaluation

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/agent/artifact"
)

const ScenarioVersion = 1

//go:embed testdata/scenarios/*.json testdata/artifacts/*
var scenarioFiles embed.FS

type Scenario struct {
	Version         int               `json:"version"`
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	BusinessContext string            `json:"business_context"`
	JudgeContext    string            `json:"judge_context,omitempty"`
	Trigger         ScenarioTrigger   `json:"trigger"`
	Messages        []ScenarioMessage `json:"messages"`
	Projects        []ScenarioProject `json:"projects"`
	Users           []ScenarioUser    `json:"users,omitempty"`
}

type ScenarioTrigger struct {
	MessageID           string    `json:"message_id"`
	ReplyToMessageID    string    `json:"reply_to_message_id,omitempty"`
	ThreadRootMessageID string    `json:"thread_root_message_id,omitempty"`
	Text                string    `json:"text"`
	Sender              string    `json:"sender"`
	At                  time.Time `json:"at"`
}

type ScenarioMessage struct {
	MessageID string             `json:"message_id"`
	Sender    string             `json:"sender"`
	Role      string             `json:"role,omitempty"`
	Text      string             `json:"text"`
	Artifacts []ScenarioArtifact `json:"artifacts,omitempty"`
	At        time.Time          `json:"at"`
}

type ScenarioArtifact struct {
	ID        string        `json:"id"`
	Kind      artifact.Kind `json:"kind"`
	Name      string        `json:"name"`
	MediaType string        `json:"media_type,omitempty"`
	Fixture   string        `json:"fixture"`
}

type ScenarioProject struct {
	Number int64  `json:"number"`
	Name   string `json:"name"`
}

type ScenarioUser struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
}

func (s Scenario) Validate() error {
	if s.Version != ScenarioVersion || strings.TrimSpace(s.ID) == "" ||
		strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.BusinessContext) == "" ||
		strings.TrimSpace(s.Trigger.MessageID) == "" || strings.TrimSpace(s.Trigger.Text) == "" ||
		strings.TrimSpace(s.Trigger.Sender) == "" || s.Trigger.At.IsZero() ||
		len(s.Messages) == 0 || len(s.Projects) == 0 {
		return errors.New("conversation evaluation scenario is incomplete")
	}
	messageIDs := map[string]struct{}{s.Trigger.MessageID: {}}
	artifactIDs := make(map[string]struct{})
	for _, message := range s.Messages {
		if strings.TrimSpace(message.MessageID) == "" || strings.TrimSpace(message.Sender) == "" ||
			strings.TrimSpace(message.Text) == "" || message.At.IsZero() ||
			!message.At.Before(s.Trigger.At) {
			return fmt.Errorf("scenario %s contains an invalid preceding message", s.ID)
		}
		if _, duplicate := messageIDs[message.MessageID]; duplicate {
			return fmt.Errorf("scenario %s contains duplicate message ID %s", s.ID, message.MessageID)
		}
		messageIDs[message.MessageID] = struct{}{}
		for _, attached := range message.Artifacts {
			if strings.TrimSpace(attached.ID) == "" || strings.TrimSpace(attached.Name) == "" ||
				strings.TrimSpace(attached.Fixture) == "" {
				return fmt.Errorf("scenario %s contains an invalid artifact", s.ID)
			}
			if _, duplicate := artifactIDs[attached.ID]; duplicate {
				return fmt.Errorf("scenario %s contains duplicate artifact ID %s", s.ID, attached.ID)
			}
			artifactIDs[attached.ID] = struct{}{}
			if _, err := scenarioFiles.ReadFile("testdata/artifacts/" + attached.Fixture); err != nil {
				return fmt.Errorf("scenario %s artifact fixture %s is unavailable", s.ID, attached.Fixture)
			}
		}
	}
	projectNumbers := make(map[int64]struct{}, len(s.Projects))
	for _, project := range s.Projects {
		if project.Number <= 0 || strings.TrimSpace(project.Name) == "" {
			return fmt.Errorf("scenario %s contains an invalid Project", s.ID)
		}
		if _, duplicate := projectNumbers[project.Number]; duplicate {
			return fmt.Errorf("scenario %s contains duplicate Project number %d", s.ID, project.Number)
		}
		projectNumbers[project.Number] = struct{}{}
	}
	return nil
}

func LoadScenarios() ([]Scenario, error) {
	paths, err := fs.Glob(scenarioFiles, "testdata/scenarios/*.json")
	if err != nil {
		return nil, fmt.Errorf("list embedded evaluation scenarios: %w", err)
	}
	var scenarios []Scenario
	for _, path := range paths {
		encoded, readErr := scenarioFiles.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read evaluation scenario %s: %w", path, readErr)
		}
		var scenario Scenario
		if decodeErr := json.Unmarshal(encoded, &scenario); decodeErr != nil {
			return nil, fmt.Errorf("decode evaluation scenario %s: %w", path, decodeErr)
		}
		if validateErr := scenario.Validate(); validateErr != nil {
			return nil, validateErr
		}
		scenarios = append(scenarios, scenario)
	}
	slices.SortFunc(scenarios, func(left, right Scenario) int {
		return strings.Compare(left.ID, right.ID)
	})
	return scenarios, nil
}

func FindScenario(id string) (Scenario, error) {
	scenarios, err := LoadScenarios()
	if err != nil {
		return Scenario{}, err
	}
	for _, scenario := range scenarios {
		if scenario.ID == id {
			return scenario, nil
		}
	}
	return Scenario{}, fmt.Errorf("evaluation scenario %q was not found", id)
}
