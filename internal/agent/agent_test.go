package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"agent/internal/config"
	"agent/internal/tools"
)

func TestExecuteToolHonorsTimeout(t *testing.T) {
	appConfig := config.DefaultConfig
	appConfig.ToolTimeout = 10 * time.Millisecond
	agent := NewAgent(nil, nil, []tools.ToolDefinition{
		{
			Name: "wait",
			Function: func(ctx context.Context, _ json.RawMessage) (string, error) {
				<-ctx.Done()
				return "", ctx.Err()
			},
		},
	}, appConfig, nil, nil)

	result := agent.executeTool(context.Background(), "wait", nil)
	if !strings.Contains(result, context.DeadlineExceeded.Error()) {
		t.Fatalf("result = %q, want timeout error", result)
	}
}

func TestExecuteToolLimitsOutput(t *testing.T) {
	appConfig := config.DefaultConfig
	appConfig.MaxToolOutput = 40
	agent := NewAgent(nil, nil, []tools.ToolDefinition{
		{
			Name: "large_output",
			Function: func(context.Context, json.RawMessage) (string, error) {
				return strings.Repeat("x", 100), nil
			},
		},
	}, appConfig, nil, nil)

	result := agent.executeTool(context.Background(), "large_output", nil)
	if len(result) > appConfig.MaxToolOutput {
		t.Fatalf("result length = %d, want at most %d", len(result), appConfig.MaxToolOutput)
	}
	if !strings.Contains(result, "[tool output truncated]") {
		t.Fatal("tool output was not marked as truncated")
	}
}

func TestTruncateToolOutputPreservesUTF8(t *testing.T) {
	result := truncateToolOutput(strings.Repeat("界", 100), 40)
	if !utf8.ValidString(result) {
		t.Fatalf("result is not valid utf-8: %q", result)
	}
	if len(result) > 40 {
		t.Fatalf("result length = %d, want at most 40", len(result))
	}
}

func TestExecuteToolLimitsErrorOutput(t *testing.T) {
	appConfig := config.DefaultConfig
	appConfig.MaxToolOutput = 40
	agent := NewAgent(nil, nil, []tools.ToolDefinition{
		{
			Name: "large_error",
			Function: func(context.Context, json.RawMessage) (string, error) {
				return "", fmt.Errorf("%s", strings.Repeat("x", 100))
			},
		},
	}, appConfig, nil, nil)

	result := agent.executeTool(context.Background(), "large_error", nil)
	if len(result) > appConfig.MaxToolOutput {
		t.Fatalf("result length = %d, want at most %d", len(result), appConfig.MaxToolOutput)
	}
	if !strings.Contains(result, "[tool output truncated]") {
		t.Fatal("tool error was not marked as truncated")
	}
}
