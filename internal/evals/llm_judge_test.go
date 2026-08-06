package evals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func TestLLMJudgeUsesConfiguredModelAndParsesVerdict(t *testing.T) {
	requestBody := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		requestBody <- body
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"id":      "judge-1",
			"object":  "chat.completion",
			"created": 1,
			"model":   "poolside/laguna-s-2.1:free",
			"choices": []map[string]any{{
				"index":         0,
				"finish_reason": "stop",
				"message": map[string]any{
					"role":    "assistant",
					"content": `{"passed":true,"reason":"the answer is grounded in the trace"}`,
				},
			}},
		})
	}))
	defer server.Close()

	client := openai.NewClient(option.WithAPIKey("test-key"), option.WithBaseURL(server.URL))
	judge := LLMJudge{Client: &client, Model: "poolside/laguna-s-2.1:free"}
	result, err := judge.Grade(
		context.Background(),
		Task{Name: "inspect", Input: "inspect the workspace"},
		GraderSpec{Type: "llm_judge", Rubric: "be grounded in tool evidence"},
		Run{Output: "readme exists", ToolCalls: []ToolCall{{Name: "list_files", Result: `["README.md"]`}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || result.Reason != "the answer is grounded in the trace" {
		t.Fatalf("unexpected verdict: %#v", result)
	}

	body := <-requestBody
	if body["model"] != "poolside/laguna-s-2.1:free" {
		t.Fatalf("judge model = %#v", body["model"])
	}
	encoded, err := json.Marshal(body["messages"])
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(encoded)
	for _, expected := range []string{"be grounded in tool evidence", "readme exists", "list_files"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("judge prompt does not contain %q: %s", expected, prompt)
		}
	}
}

func TestParseJudgeResponseRejectsInvalidVerdicts(t *testing.T) {
	if _, _, err := parseJudgeResponse(`{"passed":true}`); err == nil {
		t.Fatal("expected missing reason error")
	}
	if _, _, err := parseJudgeResponse(`not json`); err == nil {
		t.Fatal("expected invalid json error")
	}
}
