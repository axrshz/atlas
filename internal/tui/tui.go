package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// EventMsg adds one agent event to the conversation view.
type EventMsg struct {
	Kind    string
	Content string
}

// AgentDoneMsg reports an unexpected stop from the background agent loop.
type AgentDoneMsg struct {
	Err error
}

// ReloadFunc rebuilds the application and returns the path of the new executable.
type ReloadFunc func() (string, error)

type reloadResultMsg struct {
	executable string
	err        error
}

// Model is the minimal terminal interface around the agent.
type Model struct {
	input             textinput.Model
	conversation      viewport.Model
	events            []EventMsg
	inputCh           chan<- string
	reload            ReloadFunc
	restartExecutable string
	width             int
	height            int
}

func New(inputCh chan<- string, reload ReloadFunc) Model {
	input := textinput.New()
	input.Prompt = "you: "
	input.Placeholder = "write a message"
	input.Focus()

	return Model{
		input:        input,
		conversation: viewport.New(80, 20),
		inputCh:      inputCh,
		reload:       reload,
	}
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		wasAtBottom := m.conversation.AtBottom()
		m.width = message.Width
		m.height = message.Height
		m.input.Width = max(1, message.Width-6)
		m.conversation.Width = max(1, message.Width)
		m.conversation.Height = max(1, message.Height-5)
		m.refreshConversation(wasAtBottom)
		return m, nil

	case EventMsg:
		m.addEvent(message)
		return m, nil

	case AgentDoneMsg:
		if message.Err != nil {
			m.addEvent(EventMsg{Kind: "error", Content: message.Err.Error()})
		}
		return m, nil

	case reloadResultMsg:
		if message.err != nil {
			m.addEvent(EventMsg{Kind: "error", Content: message.err.Error()})
			return m, nil
		}
		m.restartExecutable = message.executable
		return m, tea.Quit

	case tea.MouseMsg:
		var command tea.Cmd
		m.conversation, command = m.conversation.Update(message)
		return m, command

	case tea.KeyMsg:
		switch message.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "pgup", "pgdown":
			var command tea.Cmd
			m.conversation, command = m.conversation.Update(message)
			return m, command
		case "enter":
			input := strings.TrimSpace(m.input.Value())
			if input == "" {
				return m, nil
			}
			m.input.Reset()

			if input == "/reload" {
				m.addEvent(EventMsg{Kind: "system", Content: "rebuilding agent..."})
				return m, m.reloadCommand()
			}

			m.addEvent(EventMsg{Kind: "you", Content: input})
			return m, sendInput(m.inputCh, input)
		}
	}

	var command tea.Cmd
	m.input, command = m.input.Update(message)
	return m, command
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading..."
	}

	return fmt.Sprintf(
		"atlas\n\n%s\n\n%s\npgup/pgdown or mouse wheel scroll | ctrl+c quit | /reload rebuild and restart",
		m.conversation.View(),
		m.input.View(),
	)
}

// RestartExecutable is non-empty after a successful /reload command.
func (m Model) RestartExecutable() string {
	return m.restartExecutable
}

func (m *Model) addEvent(event EventMsg) {
	wasAtBottom := m.conversation.AtBottom()
	m.events = append(m.events, event)
	m.refreshConversation(wasAtBottom)
}

func (m *Model) refreshConversation(followBottom bool) {
	entries := make([]string, 0, len(m.events))
	for _, event := range m.events {
		entry := fmt.Sprintf("%s: %s", event.Kind, event.Content)
		entries = append(entries, ansi.Wrap(entry, max(1, m.conversation.Width), " "))
	}
	m.conversation.SetContent(strings.Join(entries, "\n\n"))
	if followBottom {
		m.conversation.GotoBottom()
	} else {
		m.conversation.SetYOffset(m.conversation.YOffset)
	}
}

func (m Model) reloadCommand() tea.Cmd {
	return func() tea.Msg {
		if m.reload == nil {
			return reloadResultMsg{err: fmt.Errorf("reload is not configured")}
		}
		executable, err := m.reload()
		return reloadResultMsg{executable: executable, err: err}
	}
}

func sendInput(inputCh chan<- string, input string) tea.Cmd {
	return func() tea.Msg {
		inputCh <- input
		return nil
	}
}
