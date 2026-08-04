package evaluation

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedScenariosAreValidAndCoverExplicitDiscussionTriggers(t *testing.T) {
	scenarios, err := LoadScenarios()
	require.NoError(t, err)
	require.Len(t, scenarios, 12)
	discussionTriggers := 0
	for _, scenario := range scenarios {
		require.NoError(t, scenario.Validate())
		if strings.Contains(scenario.Trigger.Text, "讨论") {
			discussionTriggers++
		}
	}
	require.Equal(t, 11, discussionTriggers)
	buried, err := FindScenario("buried-retirement-problem")
	require.NoError(t, err)
	require.Equal(t, "m1", buried.Trigger.ReplyToMessageID)
	configured, err := FindScenario("conversation-default-project")
	require.NoError(t, err)
	configuration := configured.AgentConversationConfiguration()
	require.Equal(t, int64(14), *configuration.DefaultProjectNumber)
	require.Equal(t, "Model Delivery", configuration.DefaultProjectName)
	require.Contains(t, configuration.BusinessContext, "preview")
}

func TestRunScenarioLetsModelUpdateCurrentConversationConfiguration(t *testing.T) {
	scenario, err := FindScenario("natural-language-group-configuration")
	require.NoError(t, err)
	model := &scriptedModel{messages: []*schema.Message{
		toolCallMessage("search-projects", "search_projects", `{"query":"策略与模型"}`),
		toolCallMessage("get-configuration", "get_current_conversation_configuration", `{}`),
		toolCallMessage("update-configuration", "update_current_conversation_configuration", `{
			"expected_version":1,
			"default_project_number":14
		}`),
		toolCallMessage("respond", "respond", `{
			"response_type":"conversation_configuration",
			"source_tool_call_ids":["update-configuration"],
			"summary":"本群默认项目已更新为策略与模型。"
		}`),
		schema.AssistantMessage("done", nil),
	}}

	artifact, err := RunScenario(context.Background(), scenario, RunConfig{
		ModelName: "scripted", Model: model, Timezone: time.FixedZone("CST", 8*60*60),
	})

	require.NoError(t, err)
	require.NoError(t, artifact.Validate())
	require.Equal(t, "conversation_configuration", artifact.Outcome)
	require.Nil(t, artifact.Task)
	require.Equal(t, []string{
		"search_projects",
		"get_current_conversation_configuration",
		"update_current_conversation_configuration",
		"respond",
	}, toolNames(artifact.ToolTrace))
}

func TestJudgeSourceEvidenceExposesFixtureFactsWithoutTaskExpectations(t *testing.T) {
	scenario, err := FindScenario("image-decision-evidence")
	require.NoError(t, err)

	evidence, err := buildJudgeSourceEvidence(scenario)

	require.NoError(t, err)
	require.Len(t, evidence, 1)
	require.Equal(t, "timeout-report-image", evidence[0].ArtifactID)
	require.Contains(t, evidence[0].Content, "acct-103      49.4%")
	require.Contains(t, evidence[0].Content, "Traffic release is blocked")
	require.NotContains(t, evidence[0].Content, "create_task")
	require.False(t, evidence[0].Truncated)
}

func TestJudgeSourceEvidenceRendersWorkbookCells(t *testing.T) {
	scenario, err := FindScenario("spreadsheet-multiple-scopes")
	require.NoError(t, err)

	evidence, err := buildJudgeSourceEvidence(scenario)

	require.NoError(t, err)
	require.Len(t, evidence, 1)
	require.Contains(t, evidence[0].Content, "Sheet: Preview Experiment")
	require.Contains(t, evidence[0].Content, "Preview | 30 minutes | Threshold not defined")
	require.Contains(t, evidence[0].Content, "Automatic promotion | Not agreed")
}

func TestRunScenarioUsesOneShotCSVArtifactDescription(t *testing.T) {
	scenario, err := FindScenario("csv-conflicting-count")
	require.NoError(t, err)
	model := &scriptedModel{messages: []*schema.Message{
		toolCallMessage("inspect-csv", "inspect_artifact", `{
			"artifact_id":"affected-accounts-csv",
			"analysis_goal":"Determine whether the CSV is the complete affected account population."
		}`),
		toolCallMessage("search-projects", "search_projects", `{"query":"Media Integration"}`),
		toolCallMessage("create-task", "create_task", `{
			"title":"Verify the full affected account scope",
			"context":"Chat estimates about 300 affected accounts while the attachment is a five-account sample.",
			"expected_result":"A verified complete account scope and timeout-rate distribution are available.",
			"project_number":12,
			"milestone_id":null,
			"assignee_id":null,
			"due_date":null,
			"priority":"none"
		}`),
		toolCallMessage("respond", "respond", `{
			"response_type":"task_created",
			"source_tool_call_ids":["create-task","inspect-csv"],
			"summary":"Created a scope-verification Task without treating the CSV sample as the complete population."
		}`),
		schema.AssistantMessage("done", nil),
	}}
	artifactModel := &scriptedModel{messages: []*schema.Message{
		schema.AssistantMessage(
			"The parser observed five data rows. This is a bounded leading-row sample and does not establish the complete affected population.",
			nil,
		),
	}}

	result, err := RunScenario(context.Background(), scenario, RunConfig{
		ModelName: "scripted", Model: model, Timezone: time.FixedZone("UTC+8", 8*60*60),
		ArtifactModel: artifactModel,
	})

	require.NoError(t, err)
	require.Empty(t, result.GenerationError)
	require.Equal(t, "task_created", result.Outcome)
	require.Equal(t, []string{
		"inspect_artifact", "search_projects", "create_task", "respond",
	}, toolNames(result.ToolTrace))
	var description string
	require.NoError(t, json.Unmarshal(result.ToolTrace[0].Result, &description))
	require.Contains(t, description, "five data rows")
	require.Equal(t, 1, artifactModel.index)
}

func TestRunScenarioCapturesProductionToolSelectionWithoutMutation(t *testing.T) {
	scenario, err := FindScenario("buried-retirement-problem")
	require.NoError(t, err)
	model := &scriptedModel{messages: []*schema.Message{
		toolCallMessage("search-projects", "search_projects", `{"query":"Delivery Engine"}`),
		toolCallMessage("create-task", "create_task", `{
			"title":"Design stale device label retirement",
			"context":"Long-lived installation labels can leave stale device state.",
			"expected_result":"A reviewed retirement approach defines expiry and cleanup behavior.",
			"project_number":11,
			"milestone_id":null,
			"assignee_id":null,
			"due_date":null,
			"priority":"none"
		}`),
		toolCallMessage("respond", "respond", `{
			"response_type":"task_created",
			"source_tool_call_ids":["create-task"],
			"summary":"Captured the unresolved retirement design work."
		}`),
		schema.AssistantMessage("done", nil),
	}}

	artifact, err := RunScenario(context.Background(), scenario, RunConfig{
		ModelName: "scripted", Model: model, Timezone: time.FixedZone("UTC+8", 8*60*60),
	})

	require.NoError(t, err)
	require.NoError(t, artifact.Validate())
	require.Equal(t, "task_created", artifact.Outcome)
	require.NotNil(t, artifact.Task)
	require.Equal(t, "Design stale device label retirement", artifact.Task.Title)
	require.Equal(t, int64(11), artifact.Task.ProjectNumber)
	require.NotNil(t, artifact.Response)
	require.Equal(t, "task_created", artifact.Response.Type)
	require.Equal(t, []string{"search_projects", "create_task", "respond"}, toolNames(artifact.ToolTrace))
	require.JSONEq(t, `{"query":"Delivery Engine"}`, string(artifact.ToolTrace[0].Arguments))
	require.Contains(t, string(artifact.ToolTrace[1].Arguments), "Design stale device label retirement")
}

func TestJudgeRecordsStructuredCriticismWithoutGoldenTask(t *testing.T) {
	scenario, err := FindScenario("open-ended-no-commitment")
	require.NoError(t, err)
	conversion := ConversionArtifact{
		Version: ArtifactVersion, ScenarioID: scenario.ID,
		RunID: stableUUID("judge-test"), Model: "generator",
		PromptVersion: "production-v1", Outcome: "general_response",
	}
	judgeModel := &scriptedModel{messages: []*schema.Message{
		toolCallMessage("record", "record_evaluation", `{
			"verdict":"weak",
			"summary":"The response does not preserve the unresolved decision boundary.",
			"strengths":[],
			"concerns":["No concrete commitment exists"],
			"risks":["A premature Task could imply agreement"],
			"suggested_direction":"Ask what exploratory outcome should be preserved.",
			"preferred_action":"clarify",
			"confidence":"high"
		}`),
		schema.AssistantMessage("done", nil),
	}}

	artifact, err := EvaluateConversion(
		context.Background(), scenario, conversion,
		JudgeConfig{ModelName: "judge", Model: judgeModel},
	)

	require.NoError(t, err)
	require.Equal(t, JudgePromptVersion, artifact.PromptVersion)
	require.Equal(t, "weak", artifact.Result.Verdict)
	require.Equal(t, "clarify", artifact.Result.PreferredAction)
}

func toolCallMessage(id, name, arguments string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID: id, Type: "function",
		Function: schema.FunctionCall{Name: name, Arguments: arguments},
	}})
}

func toolNames(traces []ToolTrace) []string {
	result := make([]string, 0, len(traces))
	for _, trace := range traces {
		result = append(result, trace.ToolName)
	}
	return result
}

type scriptedModel struct {
	mu       sync.Mutex
	messages []*schema.Message
	index    int
}

func (m *scriptedModel) Generate(
	context.Context,
	[]*schema.Message,
	...model.Option,
) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.index >= len(m.messages) {
		return schema.AssistantMessage("done", nil), nil
	}
	message := m.messages[m.index]
	m.index++
	return message, nil
}

func (m *scriptedModel) Stream(
	ctx context.Context,
	input []*schema.Message,
	options ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (m *scriptedModel) WithTools(
	[]*schema.ToolInfo,
) (model.ToolCallingChatModel, error) {
	return m, nil
}
