package session

import (
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestSessionRoundTripPreservesMessagesAndToolCalls(t *testing.T) {
	manager := NewSessionManager(t.TempDir())
	session, err := manager.CreateSession("test session")
	if err != nil {
		t.Fatal(err)
	}

	assistant := openai.ChatCompletionAssistantMessageParam{}
	assistant.Content.OfString = openai.String("i will read the file")
	assistant.ToolCalls = []openai.ChatCompletionMessageToolCallUnionParam{
		{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: "call_1",
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      "read_file",
					Arguments: `{"path":"main.go"}`,
				},
			},
		},
	}

	session.Messages = []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("be helpful"),
		openai.UserMessage("read main.go"),
		{OfAssistant: &assistant},
		openai.ToolMessage("package main", "call_1"),
	}
	if err := manager.SaveSession(session); err != nil {
		t.Fatal(err)
	}

	loaded, err := manager.LoadSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 4 {
		t.Fatalf("message count = %d, want 4", len(loaded.Messages))
	}
	if loaded.Messages[0].OfSystem == nil {
		t.Fatal("system message was not restored")
	}
	if loaded.Messages[1].OfUser == nil {
		t.Fatal("user message was not restored")
	}

	restoredAssistant := loaded.Messages[2].OfAssistant
	if restoredAssistant == nil || len(restoredAssistant.ToolCalls) != 1 {
		t.Fatal("assistant tool call was not restored")
	}
	restoredCall := restoredAssistant.ToolCalls[0].OfFunction
	if restoredCall == nil || restoredCall.ID != "call_1" || restoredCall.Function.Name != "read_file" {
		t.Fatal("assistant function tool call changed during restore")
	}
	if loaded.Messages[3].OfTool == nil || loaded.Messages[3].OfTool.ToolCallID != "call_1" {
		t.Fatal("tool result was not restored")
	}
}
