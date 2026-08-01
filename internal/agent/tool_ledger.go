package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/google/uuid"
)

type ToolCallRepository interface {
	ClaimToolCall(context.Context, ToolCall) (ToolCallClaim, error)
	CompleteToolCall(context.Context, uuid.UUID, string, []byte, time.Time) error
	FailToolCall(context.Context, uuid.UUID, string, string, time.Time) error
}

type ToolLedger struct {
	RunID      uuid.UUID
	Repository ToolCallRepository
	Now        func() time.Time
}

func (l ToolLedger) Middleware() compose.ToolMiddleware {
	return compose.ToolMiddleware{
		Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
				if l.Repository == nil || l.RunID == uuid.Nil || input == nil ||
					input.CallID == "" || input.Name == "" {
					return nil, ErrToolCallProtocol
				}
				now := time.Now
				if l.Now != nil {
					now = l.Now
				}
				argumentHash := sha256.Sum256([]byte(input.Arguments))
				summary, _ := json.Marshal(map[string]any{
					"bytes": len(input.Arguments),
				})
				claim, err := l.Repository.ClaimToolCall(ctx, ToolCall{
					RunID:           l.RunID,
					ToolCallID:      input.CallID,
					ToolName:        input.Name,
					ArgumentHash:    argumentHash[:],
					ArgumentSummary: summary,
					State:           ToolCallRunning,
					StartedAt:       now().UTC(),
				})
				if err != nil {
					return nil, err
				}
				switch claim.Kind {
				case ToolCallClaimReplay:
					result, evidenceErr := addToolEvidenceID(claim.Result, input.CallID)
					if evidenceErr != nil {
						return nil, evidenceErr
					}
					return &compose.ToolOutput{Result: string(result)}, nil
				case ToolCallClaimConflict:
					return nil, ErrToolCallProtocol
				case ToolCallClaimRunning:
					// A Run lease serializes active workers. Re-execute an
					// incomplete call after a crash; mutation tools carry
					// their own stable OpenAPI idempotency key.
				case ToolCallClaimAcquired:
				default:
					return nil, ErrToolCallProtocol
				}

				output, err := next(ctx, input)
				if err != nil {
					if _, interrupt := compose.IsInterruptRerunError(err); interrupt {
						return nil, err
					}
					if failErr := l.Repository.FailToolCall(
						ctx, l.RunID, input.CallID, classifyToolError(err), now().UTC(),
					); failErr != nil {
						return nil, errors.Join(err, failErr)
					}
					return nil, err
				}
				if output == nil || !json.Valid([]byte(output.Result)) {
					return nil, fmt.Errorf("%w: tool result must be JSON", ErrToolCallProtocol)
				}
				result, err := addToolEvidenceID([]byte(output.Result), input.CallID)
				if err != nil {
					return nil, err
				}
				if err := l.Repository.CompleteToolCall(
					ctx, l.RunID, input.CallID, result, now().UTC(),
				); err != nil {
					return nil, err
				}
				output.Result = string(result)
				return output, nil
			}
		},
	}
}

func addToolEvidenceID(result []byte, toolCallID string) ([]byte, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(result, &object); err == nil && object != nil {
		encodedID, _ := json.Marshal(toolCallID)
		object["evidence_id"] = encodedID
		encoded, marshalErr := json.Marshal(object)
		if marshalErr != nil {
			return nil, fmt.Errorf("%w: encode tool evidence: %v", ErrToolCallProtocol, marshalErr)
		}
		return encoded, nil
	}
	var description string
	if err := json.Unmarshal(result, &description); err == nil {
		return result, nil
	}
	return nil, fmt.Errorf("%w: tool result must be a JSON object or string", ErrToolCallProtocol)
}

func ValidateToolArguments(_ context.Context, _ string, arguments string) (string, error) {
	if len(arguments) == 0 || len(arguments) > 32*1024 || !json.Valid([]byte(arguments)) {
		return "", fmt.Errorf("%w: malformed tool arguments", ErrToolCallProtocol)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(arguments), &object); err != nil || object == nil {
		return "", fmt.Errorf("%w: tool arguments must be a JSON object", ErrToolCallProtocol)
	}
	return arguments, nil
}

func UnknownToolResult(_ context.Context, name, _ string) (string, error) {
	result, _ := json.Marshal(map[string]string{
		"error":     "unknown_tool",
		"tool_name": name,
	})
	return string(result), nil
}

func classifyToolError(err error) string {
	switch {
	case errors.Is(err, ErrToolCallProtocol):
		return "protocol"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "timeout"
	default:
		return "execution"
	}
}
