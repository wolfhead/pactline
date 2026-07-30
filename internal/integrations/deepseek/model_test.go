package deepseek

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

func TestNewChatModelValidatesAndDefaultsConfiguration(t *testing.T) {
	_, err := NewChatModel(context.Background(), Config{})
	require.ErrorIs(t, err, ErrAPIKeyRequired)

	_, err = NewChatModel(context.Background(), Config{
		APIKey:       "test",
		ThinkingMode: "sometimes",
	})
	require.ErrorIs(t, err, ErrInvalidThinkingMode)

	_, err = NewChatModel(context.Background(), Config{
		APIKey:          "test",
		ThinkingMode:    ThinkingEnabled,
		ReasoningEffort: "medium",
	})
	require.ErrorIs(t, err, ErrInvalidReasoningEffort)

	_, err = NewChatModel(context.Background(), Config{
		APIKey:          "test",
		ThinkingMode:    ThinkingDisabled,
		ReasoningEffort: "high",
	})
	require.ErrorIs(t, err, ErrInvalidReasoningEffort)
}

func TestChatModelPreservesReasoningAcrossToolCalls(t *testing.T) {
	var requests []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		var request map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")

		if len(requests) == 1 {
			_, _ = io.WriteString(w, `{
				"id":"response-1",
				"object":"chat.completion",
				"created":1,
				"model":"deepseek-v4-pro",
				"choices":[{
					"index":0,
					"message":{
						"role":"assistant",
						"content":"",
						"reasoning_content":"I need the project number.",
						"tool_calls":[{
							"index":0,
							"id":"call-project",
							"type":"function",
							"function":{"name":"search_projects","arguments":"{\"query\":\"Pactline\"}"}
						}]
					},
					"finish_reason":"tool_calls"
				}],
				"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
			}`)
			return
		}

		_, _ = io.WriteString(w, `{
			"id":"response-2",
			"object":"chat.completion",
			"created":2,
			"model":"deepseek-v4-pro",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"ready",
					"reasoning_content":"The project is resolved."
				},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":20,"completion_tokens":4,"total_tokens":24}
		}`)
	}))
	defer server.Close()

	model, err := NewChatModel(context.Background(), Config{
		APIKey:          "test-key",
		BaseURL:         server.URL,
		Model:           "deepseek-v4-pro",
		ThinkingMode:    ThinkingEnabled,
		ReasoningEffort: "high",
		Timeout:         time.Second,
	})
	require.NoError(t, err)
	withTools, err := model.WithTools([]*schema.ToolInfo{{
		Name: "search_projects",
		Desc: "Find an exact Pactline project.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {Type: schema.String, Required: true},
		}),
	}})
	require.NoError(t, err)

	first, err := withTools.Generate(context.Background(), []*schema.Message{
		schema.UserMessage("Create a task in Pactline."),
	})
	require.NoError(t, err)
	require.Equal(t, "I need the project number.", first.ReasoningContent)
	require.Len(t, first.ToolCalls, 1)

	second, err := withTools.Generate(context.Background(), []*schema.Message{
		schema.UserMessage("Create a task in Pactline."),
		first,
		schema.ToolMessage(`{"project_number":12}`, "call-project"),
	})
	require.NoError(t, err)
	require.Equal(t, "ready", second.Content)
	require.Len(t, requests, 2)

	require.Equal(t, "deepseek-v4-pro", requests[0]["model"])
	require.Equal(t, "high", requests[0]["reasoning_effort"])
	require.Equal(t, map[string]interface{}{"type": "enabled"}, requests[0]["thinking"])

	messages, ok := requests[1]["messages"].([]interface{})
	require.True(t, ok)
	require.Len(t, messages, 3)
	assistant, ok := messages[1].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "I need the project number.", assistant["reasoning_content"])
	require.NotEmpty(t, assistant["tool_calls"])
}

func TestChatModelWithToolsRetainsRequiredRequestOptions(t *testing.T) {
	var request map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"response",
			"object":"chat.completion",
			"created":1,
			"model":"deepseek-v4-pro",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`)
	}))
	defer server.Close()

	model, err := NewChatModel(context.Background(), Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	require.NoError(t, err)
	withTools, err := model.WithTools([]*schema.ToolInfo{{
		Name:        "noop",
		Desc:        "No operation.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}})
	require.NoError(t, err)
	_, err = withTools.Generate(context.Background(), []*schema.Message{
		schema.UserMessage("hello"),
	})
	require.NoError(t, err)
	require.Equal(t, "deepseek-v4-flash", request["model"])
	require.NotContains(t, request, "reasoning_effort")
	require.Equal(t, map[string]interface{}{"type": "disabled"}, request["thinking"])
}

func TestChatModelStreamingPreservesReasoningAndToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+
			`{"id":"stream-1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Resolve "},"finish_reason":null}]}`+
			"\n\n")
		_, _ = io.WriteString(w, "data: "+
			`{"id":"stream-1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"reasoning_content":"project.","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"search_projects","arguments":"{\"query\":\"Pactline\"}"}}]},"finish_reason":"tool_calls"}]}`+
			"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	model, err := NewChatModel(context.Background(), Config{
		APIKey: "test-key", BaseURL: server.URL, Timeout: time.Second,
	})
	require.NoError(t, err)
	stream, err := model.Stream(context.Background(), []*schema.Message{
		schema.UserMessage("Create a task."),
	})
	require.NoError(t, err)
	defer stream.Close()
	var chunks []*schema.Message
	for {
		chunk, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		require.NoError(t, recvErr)
		chunks = append(chunks, chunk)
	}
	message, err := schema.ConcatMessages(chunks)
	require.NoError(t, err)
	require.Equal(t, "Resolve project.", message.ReasoningContent)
	require.Len(t, message.ToolCalls, 1)
	require.Equal(t, "search_projects", message.ToolCalls[0].Function.Name)
	require.JSONEq(t, `{"query":"Pactline"}`, message.ToolCalls[0].Function.Arguments)
}
