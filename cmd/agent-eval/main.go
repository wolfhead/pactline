// Command agent-eval runs the production first-party Agent against embedded,
// non-mutating conversation scenarios.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/agent/artifact"
	"github.com/wolfhead/pactline/internal/agent/evaluation"
	"github.com/wolfhead/pactline/internal/integrations/deepseek"
)

const commandTimeout = 30 * time.Minute

type report struct {
	Scenario   evaluation.Scenario           `json:"scenario"`
	Conversion evaluation.ConversionArtifact `json:"conversion"`
	Judge      *evaluation.JudgeArtifact     `json:"judge,omitempty"`
	SameModel  bool                          `json:"generation_and_judge_use_same_model"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "agent-eval:", err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agent-eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	list := flags.Bool("list", false, "list embedded scenarios")
	scenarioID := flags.String("scenario", "all", "scenario ID or all")
	withJudge := flags.Bool("judge", true, "run the LLM judge")
	format := flags.String("format", "markdown", "markdown or json")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	scenarios, err := evaluation.LoadScenarios()
	if err != nil {
		return err
	}
	if *list {
		for _, scenario := range scenarios {
			fmt.Fprintf(stdout, "%s\t%s\n", scenario.ID, scenario.Name)
		}
		return nil
	}
	if *scenarioID != "all" {
		scenario, findErr := evaluation.FindScenario(*scenarioID)
		if findErr != nil {
			return findErr
		}
		scenarios = []evaluation.Scenario{scenario}
	}
	if *format != "markdown" && *format != "json" {
		return errors.New("format must be markdown or json")
	}
	apiKey, err := configurationValue("DEEPSEEK_API_KEY")
	if err != nil {
		return err
	}
	if apiKey == "" {
		return errors.New("DEEPSEEK_API_KEY or DEEPSEEK_API_KEY_FILE is required")
	}
	modelName := valueOr(strings.TrimSpace(os.Getenv("DEEPSEEK_MODEL")), deepseek.DefaultModel)
	baseURL := strings.TrimSpace(os.Getenv("DEEPSEEK_BASE_URL"))
	judgeModelName := valueOr(strings.TrimSpace(os.Getenv("AGENT_EVAL_JUDGE_MODEL")), deepseek.ProModel)
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	generationModel, err := deepseek.NewChatModel(ctx, deepseek.Config{
		APIKey: apiKey, BaseURL: baseURL, Model: modelName,
	})
	if err != nil {
		return err
	}
	judgeModel := generationModel
	if judgeModelName != modelName {
		judgeModel, err = deepseek.NewChatModel(ctx, deepseek.Config{
			APIKey: apiKey, BaseURL: baseURL, Model: judgeModelName,
		})
		if err != nil {
			return err
		}
	}
	timezone, err := time.LoadLocation(valueOr(
		strings.TrimSpace(os.Getenv("AGENT_TENANT_TIMEZONE")),
		"Asia/Shanghai",
	))
	if err != nil {
		return fmt.Errorf("load evaluation timezone: %w", err)
	}
	var artifactVision artifact.VisionAnalyzer
	visionAPIKey, err := configurationValue("AGENT_VISION_API_KEY")
	if err != nil {
		return err
	}
	visionBaseURL := strings.TrimSpace(os.Getenv("AGENT_VISION_BASE_URL"))
	visionModel := strings.TrimSpace(os.Getenv("AGENT_VISION_MODEL"))
	visionConfigured := visionAPIKey != "" || visionBaseURL != "" || visionModel != ""
	if visionConfigured {
		if visionAPIKey == "" {
			return errors.New("AGENT_VISION_API_KEY is required when a vision base URL or model is configured")
		}
		if visionBaseURL == "" {
			visionBaseURL = artifact.DefaultVisionBaseURL
		}
		if visionModel == "" {
			visionModel = artifact.DefaultVisionModel
		}
		vision, visionErr := artifact.NewOpenAICompatibleVision(artifact.VisionConfig{
			APIKey: visionAPIKey, BaseURL: visionBaseURL, Model: visionModel,
		})
		if visionErr != nil {
			return visionErr
		}
		artifactVision = vision
	}
	reports := make([]report, 0, len(scenarios))
	for index, scenario := range scenarios {
		fmt.Fprintf(stderr, "agent-eval: running %s (%d/%d)\n", scenario.ID, index+1, len(scenarios))
		conversion, runErr := evaluation.RunScenario(ctx, scenario, evaluation.RunConfig{
			ModelName: modelName, Model: generationModel, Timezone: timezone,
			ArtifactModel: generationModel, ArtifactVision: artifactVision,
		})
		if runErr != nil {
			return fmt.Errorf("run scenario %s: %w", scenario.ID, runErr)
		}
		item := report{
			Scenario: scenario, Conversion: conversion,
			SameModel: judgeModelName == modelName,
		}
		if *withJudge {
			judgment, judgeErr := evaluation.EvaluateConversion(
				ctx, scenario, conversion,
				evaluation.JudgeConfig{ModelName: judgeModelName, Model: judgeModel},
			)
			if judgeErr != nil {
				return fmt.Errorf("judge scenario %s: %w", scenario.ID, judgeErr)
			}
			item.Judge = &judgment
		}
		reports = append(reports, item)
		fmt.Fprintf(stderr, "agent-eval: completed %s (%d/%d)\n", scenario.ID, index+1, len(scenarios))
	}
	if *format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(reports)
	}
	writeMarkdown(stdout, reports)
	return nil
}

func writeMarkdown(output io.Writer, reports []report) {
	fmt.Fprintln(output, "# Pactline Conversation Evaluation")
	for _, item := range reports {
		fmt.Fprintf(output, "\n## %s\n\n", item.Scenario.Name)
		fmt.Fprintf(output, "- Scenario: `%s`\n", item.Scenario.ID)
		fmt.Fprintf(output, "- Generator: `%s`\n", item.Conversion.Model)
		fmt.Fprintf(output, "- Production prompt: `%s`\n", item.Conversion.PromptVersion)
		fmt.Fprintf(output, "- Outcome: `%s`\n", item.Conversion.Outcome)
		fmt.Fprintf(output, "- Tokens: %d\n", item.Conversion.TotalTokens)
		if item.Conversion.GenerationError != "" {
			fmt.Fprintf(output, "- Error: %s\n", item.Conversion.GenerationError)
		}
		if task := item.Conversion.Task; task != nil {
			fmt.Fprintf(output, "\n### Captured Task\n\n")
			fmt.Fprintf(output, "**%s**\n\n", task.Title)
			fmt.Fprintf(output, "Context: %s\n\n", task.Context)
			fmt.Fprintf(output, "Expected result: %s\n\n", task.ExpectedResult)
			fmt.Fprintf(output, "Project: #%d · Priority: %s\n", task.ProjectNumber, task.Priority)
		}
		if clarification := item.Conversion.Clarification; clarification != nil {
			fmt.Fprintf(output, "\n### Clarification\n\n%s\n", clarification.Question)
			for _, candidate := range clarification.Candidates {
				fmt.Fprintf(output, "\n- %s", candidate)
			}
			fmt.Fprintln(output)
		}
		fmt.Fprintln(output, "\n### Tool trace")
		for _, trace := range item.Conversion.ToolTrace {
			fmt.Fprintf(output, "\n- `%s` · `%s` · %s", trace.ToolName, trace.CallID, trace.State)
			if trace.ErrorCategory != "" && len(trace.Arguments) > 0 {
				fmt.Fprintf(output, "\n  - Arguments: `%s`", strings.ReplaceAll(string(trace.Arguments), "`", "\\`"))
			}
			if trace.ToolName == "inspect_artifact" && len(trace.Result) > 0 {
				var description string
				if json.Unmarshal(trace.Result, &description) == nil && strings.TrimSpace(description) != "" {
					fmt.Fprintln(output, "\n  - Artifact description:")
					for _, line := range strings.Split(description, "\n") {
						fmt.Fprintf(output, "\n    > %s", line)
					}
				}
			}
		}
		fmt.Fprintln(output)
		if item.Judge != nil {
			result := item.Judge.Result
			fmt.Fprintf(output, "\n### LLM Judge\n\n")
			fmt.Fprintf(output, "- Model: `%s`\n", item.Judge.Model)
			fmt.Fprintf(output, "- Judge prompt: `%s`\n", item.Judge.PromptVersion)
			fmt.Fprintf(output, "- Verdict: **%s** · preferred action: `%s` · confidence: `%s`\n\n", result.Verdict, result.PreferredAction, result.Confidence)
			fmt.Fprintln(output, result.Summary)
			writeList(output, "Strengths", result.Strengths)
			writeList(output, "Concerns", result.Concerns)
			writeList(output, "Risks", result.Risks)
			fmt.Fprintf(output, "\n**Suggested direction:** %s\n", result.SuggestedDirection)
		}
	}
}

func writeList(output io.Writer, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(output, "\n**%s**\n", title)
	for _, value := range values {
		fmt.Fprintf(output, "\n- %s", value)
	}
	fmt.Fprintln(output)
}

func configurationValue(name string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value, nil
	}
	path := strings.TrimSpace(os.Getenv(name + "_FILE"))
	if path == "" {
		return "", nil
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s file: %w", name, err)
	}
	return strings.TrimSpace(string(encoded)), nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
