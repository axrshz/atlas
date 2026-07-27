package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestEnterAddsMessageAndSendsInputToAgent(t *testing.T) {
	inputCh := make(chan string, 1)
	model := New(inputCh, nil)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(Model)
	model.input.SetValue("hello")

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command != nil {
		command()
	}

	select {
	case input := <-inputCh:
		if input != "hello" {
			t.Fatalf("input = %q, want hello", input)
		}
	default:
		t.Fatal("message was not sent to the agent")
	}

	if !strings.Contains(model.View(), "you: hello") {
		t.Fatal("conversation view does not show the submitted message")
	}
}

func TestAgentOutputWrapsToViewportWidth(t *testing.T) {
	model := New(make(chan string, 1), nil)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 20, Height: 12})
	model = updated.(Model)
	updated, _ = model.Update(EventMsg{
		Kind:    "laguna",
		Content: "abcdefghijklmnopqrstuvwxyz0123456789",
	})
	model = updated.(Model)

	for _, line := range strings.Split(model.conversation.View(), "\n") {
		if width := ansi.StringWidth(line); width > model.conversation.Width {
			t.Fatalf("rendered line width = %d, viewport width = %d, line = %q", width, model.conversation.Width, line)
		}
	}
	if !strings.Contains(model.conversation.View(), "456789") {
		t.Fatal("the end of the wrapped agent output is not visible")
	}
}

func TestPageUpScrollsConversation(t *testing.T) {
	model := populatedScrollableModel(t)
	bottomOffset := model.conversation.YOffset

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)

	if model.conversation.YOffset >= bottomOffset {
		t.Fatalf("page up did not move viewport: offset stayed at %d", model.conversation.YOffset)
	}
}

func TestMouseWheelScrollsConversation(t *testing.T) {
	model := populatedScrollableModel(t)
	bottomOffset := model.conversation.YOffset

	updated, _ := model.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	})
	model = updated.(Model)

	if model.conversation.YOffset >= bottomOffset {
		t.Fatalf("mouse wheel did not move viewport: offset stayed at %d", model.conversation.YOffset)
	}
}

func TestNewOutputPreservesManualScrollPosition(t *testing.T) {
	model := populatedScrollableModel(t)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	scrolledOffset := model.conversation.YOffset

	updated, _ = model.Update(EventMsg{Kind: "laguna", Content: "new output"})
	model = updated.(Model)

	if model.conversation.YOffset != scrolledOffset {
		t.Fatalf("offset = %d after new output, want %d", model.conversation.YOffset, scrolledOffset)
	}
}

func populatedScrollableModel(t *testing.T) Model {
	t.Helper()
	model := New(make(chan string, 1), nil)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	model = updated.(Model)

	for index := range 12 {
		updated, _ = model.Update(EventMsg{
			Kind:    "laguna",
			Content: strings.Repeat(string(rune('a'+index)), 30),
		})
		model = updated.(Model)
	}
	if model.conversation.AtTop() {
		t.Fatal("test conversation is not scrollable")
	}
	return model
}
