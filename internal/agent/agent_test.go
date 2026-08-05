package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"atlas/internal/config"
	"atlas/internal/tools"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
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

func TestChatCompletionParamsUseOpenRouterReasoningShape(t *testing.T) {
	appConfig := config.DefaultConfig
	appConfig.ReasoningEffort = "high"
	agent := NewAgent(nil, nil, nil, appConfig, nil, nil)

	encoded, err := json.Marshal(agent.chatCompletionParams([]openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("fix it"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Reasoning struct {
			Effort  string `json:"effort"`
			Exclude bool   `json:"exclude"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal(encoded, &request); err != nil {
		t.Fatal(err)
	}
	if request.Reasoning.Effort != "high" || request.Reasoning.Exclude {
		t.Fatalf("unexpected reasoning request: %s", encoded)
	}
}

func TestStreamAccumulatorEmitsAndPreservesReasoning(t *testing.T) {
	chunks := []string{
		`{"id":"completion-1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","reasoning_details":[{"type":"reasoning.text","text":"inspect ","signature":null,"format":"unknown","index":0}]},"finish_reason":""}]}`,
		`{"id":"completion-1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.text","text":"files","signature":"signed","format":"unknown","index":0}]},"finish_reason":""}]}`,
		`{"id":"completion-1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`,
	}

	accumulator := openRouterStreamAccumulator{}
	var content, reasoning string
	for _, rawChunk := range chunks {
		var chunk openai.ChatCompletionChunk
		if err := json.Unmarshal([]byte(rawChunk), &chunk); err != nil {
			t.Fatal(err)
		}
		contentDelta, reasoningDelta, err := accumulator.addChunk(chunk)
		if err != nil {
			t.Fatal(err)
		}
		content += contentDelta
		reasoning += reasoningDelta
	}
	if content != "done" || reasoning != "inspect files" {
		t.Fatalf("content = %q, reasoning = %q", content, reasoning)
	}

	messageParam, err := accumulator.messageParam()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(messageParam)
	if err != nil {
		t.Fatal(err)
	}
	var message struct {
		Content          string `json:"content"`
		ReasoningDetails []struct {
			Text      string `json:"text"`
			Signature string `json:"signature"`
		} `json:"reasoning_details"`
	}
	if err := json.Unmarshal(encoded, &message); err != nil {
		t.Fatal(err)
	}
	if message.Content != "done" {
		t.Fatalf("content was not accumulated: %s", encoded)
	}
	if len(message.ReasoningDetails) != 1 || message.ReasoningDetails[0].Text != "inspect files" || message.ReasoningDetails[0].Signature != "signed" {
		t.Fatalf("reasoning details were not accumulated: %s", encoded)
	}
}

func TestRunInferenceStreamsDeltas(t *testing.T) {
	requestBodies := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		requestBodies <- body
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: {\"id\":\"completion-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning\":\"think\"},\"finish_reason\":\"\"}]}\n\n")
		_, _ = fmt.Fprint(response, "data: {\"id\":\"completion-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = fmt.Fprint(response, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := openai.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL),
	)
	var events []Event
	agent := NewAgent(&client, nil, nil, config.DefaultConfig, nil, func(event Event) {
		events = append(events, event)
	})
	result, err := agent.runInference(context.Background(), []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("test"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.message.Content != "done" {
		t.Fatalf("content = %q", result.message.Content)
	}
	if len(events) != 2 || events[0].Kind != "reasoning" || events[0].Content != "think" || events[1].Kind != "assistant" || events[1].Content != "done" {
		t.Fatalf("unexpected stream events: %#v", events)
	}
	for _, event := range events {
		if !event.Append {
			t.Fatalf("stream event was not marked appendable: %#v", event)
		}
	}

	requestBody := <-requestBodies
	if streaming, ok := requestBody["stream"].(bool); !ok || !streaming {
		t.Fatalf("stream request field = %#v", requestBody["stream"])
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
