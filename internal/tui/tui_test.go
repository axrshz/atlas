package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
