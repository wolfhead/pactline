// Package deepseek configures the DeepSeek model boundary used by Pactline's
// first-party Agent. Business code depends on Eino model interfaces rather
// than the provider SDK directly.
package deepseek

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	einodeepseek "github.com/cloudwego/eino-ext/components/model/deepseek"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	DefaultModel           = "deepseek-v4-pro"
	DefaultReasoningEffort = "high"
	DefaultTimeout         = 5 * time.Minute

	ThinkingEnabled  = "enabled"
	ThinkingDisabled = "disabled"
)

var (
	ErrAPIKeyRequired         = errors.New("DeepSeek API key is required")
	ErrInvalidThinkingMode    = errors.New("DeepSeek thinking mode is invalid")
	ErrInvalidReasoningEffort = errors.New("DeepSeek reasoning effort is invalid")
)

type Config struct {
	APIKey          string
	BaseURL         string
	Model           string
	ThinkingMode    string
	ReasoningEffort string
	MaxTokens       int
	Timeout         time.Duration
}

// NewChatModel creates a concurrency-safe Eino ToolCallingChatModel with
// Pactline's required DeepSeek V4 thinking defaults.
func NewChatModel(ctx context.Context, config Config) (einomodel.ToolCallingChatModel, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}

	inner, err := einodeepseek.NewChatModel(ctx, &einodeepseek.ChatModelConfig{
		APIKey:    normalized.APIKey,
		BaseURL:   normalized.BaseURL,
		Model:     normalized.Model,
		MaxTokens: normalized.MaxTokens,
		Timeout:   normalized.Timeout,
		ThinkingConfig: &einodeepseek.ThinkingConfig{
			Type: normalized.ThinkingMode,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("configure DeepSeek chat model: %w", err)
	}

	return &chatModel{
		inner: inner,
		requestOptions: []einomodel.Option{
			einodeepseek.WithExtraFields(map[string]interface{}{
				"reasoning_effort": normalized.ReasoningEffort,
			}),
		},
	}, nil
}

type chatModel struct {
	inner          einomodel.ToolCallingChatModel
	requestOptions []einomodel.Option
}

func (m *chatModel) Generate(
	ctx context.Context,
	input []*schema.Message,
	options ...einomodel.Option,
) (*schema.Message, error) {
	return m.inner.Generate(ctx, input, appendRequestOptions(options, m.requestOptions)...)
}

func (m *chatModel) Stream(
	ctx context.Context,
	input []*schema.Message,
	options ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	return m.inner.Stream(ctx, input, appendRequestOptions(options, m.requestOptions)...)
}

func (m *chatModel) WithTools(
	tools []*schema.ToolInfo,
) (einomodel.ToolCallingChatModel, error) {
	withTools, err := m.inner.WithTools(tools)
	if err != nil {
		return nil, fmt.Errorf("configure DeepSeek tools: %w", err)
	}
	return &chatModel{
		inner:          withTools,
		requestOptions: append([]einomodel.Option(nil), m.requestOptions...),
	}, nil
}

func appendRequestOptions(
	callOptions []einomodel.Option,
	requiredOptions []einomodel.Option,
) []einomodel.Option {
	combined := make([]einomodel.Option, 0, len(callOptions)+len(requiredOptions))
	combined = append(combined, callOptions...)
	combined = append(combined, requiredOptions...)
	return combined
}

func normalizeConfig(config Config) (Config, error) {
	config.APIKey = strings.TrimSpace(config.APIKey)
	if config.APIKey == "" {
		return Config{}, ErrAPIKeyRequired
	}
	config.Model = strings.TrimSpace(config.Model)
	if config.Model == "" {
		config.Model = DefaultModel
	}
	config.ThinkingMode = strings.TrimSpace(config.ThinkingMode)
	if config.ThinkingMode == "" {
		config.ThinkingMode = ThinkingEnabled
	}
	if config.ThinkingMode != ThinkingEnabled && config.ThinkingMode != ThinkingDisabled {
		return Config{}, ErrInvalidThinkingMode
	}
	config.ReasoningEffort = strings.TrimSpace(config.ReasoningEffort)
	if config.ReasoningEffort == "" {
		config.ReasoningEffort = DefaultReasoningEffort
	}
	if config.ReasoningEffort != "high" && config.ReasoningEffort != "max" {
		return Config{}, ErrInvalidReasoningEffort
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultTimeout
	}
	return config, nil
}
