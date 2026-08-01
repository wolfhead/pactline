package runtime

import (
	"context"
	"fmt"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	agenttools "github.com/wolfhead/pactline/internal/agent/tools"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
)

// NewFirstPartyAgent assembles the production prompt, tools, and Tool Ledger.
// The evaluation harness supplies sandbox dependencies to this same assembly
// function so prompt or tool-policy drift is visible immediately.
func NewFirstPartyAgent(
	ctx context.Context,
	run pactagent.Run,
	now time.Time,
	model einomodel.ToolCallingChatModel,
	toolSet agenttools.Set,
	repository pactagent.ToolCallRepository,
	additionalToolMiddlewares ...compose.ToolMiddleware,
) (*adk.ChatModelAgent, error) {
	ledger := pactagent.ToolLedger{
		RunID: run.ID, Repository: repository, Now: func() time.Time { return now },
	}
	toolMiddlewares := append(
		[]compose.ToolMiddleware{ledger.Middleware()},
		additionalToolMiddlewares...,
	)
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "pactline-first-party-agent",
		Description: "Creates at most one Pactline Task or answers bounded Task, Project, and Milestone questions.",
		Instruction: SystemPrompt(now, run),
		Model:       model,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools:                toolSet.Tools,
				UnknownToolsHandler:  pactagent.UnknownToolResult,
				ToolArgumentsHandler: pactagent.ValidateToolArguments,
				ToolCallMiddlewares:  toolMiddlewares,
			},
		},
		MaxIterations: MaxModelIterations,
	})
	if err != nil {
		return nil, fmt.Errorf("construct Eino Agent: %w", err)
	}
	return agent, nil
}
