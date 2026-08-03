package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"agent/internal/config"
	"agent/internal/session"
	"agent/internal/tools"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

const commandsMessage = "commands: /new, /sessions, /delete-session <id>, /reload, /help"

// Event is a message for the user interface to display.
type Event struct {
	Kind      string        `json:"kind"`
	Content   string        `json:"content"`
	Timestamp time.Time     `json:"timestamp"`
	Duration  time.Duration `json:"duration,omitempty"`
}

// RunResult is the outcome of one user turn through the agentic loop.
type RunResult struct {
	FinalResponse    string `json:"final_response"`
	Steps            int    `json:"steps"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
}

type Agent struct {
	client         *openai.Client
	getUserMessage func() (string, bool)
	tools          []tools.ToolDefinition
	config         config.Config
	sessionManager *session.SessionManager
	session        *session.Session
	onEvent        func(Event)
}

func NewAgent(client *openai.Client, getUserMessage func() (string, bool), toolDefinitions []tools.ToolDefinition, appConfig config.Config, sessionManager *session.SessionManager, onEvent func(Event)) *Agent {
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	return &Agent{
		client:         client,
		getUserMessage: getUserMessage,
		tools:          toolDefinitions,
		config:         appConfig,
		sessionManager: sessionManager,
		onEvent:        onEvent,
	}
}

func (a *Agent) Run(ctx context.Context) error {
	if err := a.loadOrCreateSession(); err != nil {
		return err
	}

	a.emit("system", "chat with %s (ctrl+c to quit)", a.config.Model)
	a.emit("system", commandsMessage)

	for {
		userInput, ok := a.getUserMessage()
		if !ok {
			return a.saveSession()
		}

		if handled, err := a.handleCommand(userInput); handled || err != nil {
			if err != nil {
				a.emit("error", "%s", err)
			}
			continue
		}

		if _, err := a.runTurn(ctx, userInput); err != nil {
			return err
		}
	}
}

// RunTask runs one task without the interactive input loop. A fresh Agent
// should be created for every independent task or evaluation trial.
func (a *Agent) RunTask(ctx context.Context, task string) (RunResult, error) {
	if strings.TrimSpace(task) == "" {
		return RunResult{}, fmt.Errorf("task is required")
	}
	if a.session == nil {
		if err := a.loadOrCreateSession(); err != nil {
			return RunResult{}, err
		}
	}
	return a.runTurn(ctx, task)
}

func (a *Agent) runTurn(ctx context.Context, userInput string) (RunResult, error) {
	a.session.Messages = append(a.session.Messages, openai.UserMessage(userInput))
	if err := a.saveSession(); err != nil {
		return RunResult{}, err
	}

	result := RunResult{}
	for {
		if a.config.MaxAgentSteps > 0 && result.Steps >= a.config.MaxAgentSteps {
			return result, fmt.Errorf("agent exceeded the maximum of %d model steps", a.config.MaxAgentSteps)
		}
		result.Steps++

		response, err := a.runInference(ctx, a.session.Messages)
		if err != nil {
			return result, err
		}
		if len(response.Choices) == 0 {
			return result, fmt.Errorf("openrouter returned no completion choices")
		}
		result.PromptTokens += response.Usage.PromptTokens
		result.CompletionTokens += response.Usage.CompletionTokens
		result.TotalTokens += response.Usage.TotalTokens

		message := response.Choices[0].Message
		a.session.Messages = append(a.session.Messages, message.ToParam())
		if err := a.saveSession(); err != nil {
			return result, err
		}

		if message.Content != "" {
			a.emit("assistant", "%s", message.Content)
		}
		if len(message.ToolCalls) == 0 {
			result.FinalResponse = message.Content
			return result, nil
		}

		for _, toolCall := range message.ToolCalls {
			functionCall, ok := toolCall.AsAny().(openai.ChatCompletionMessageFunctionToolCall)
			if !ok {
				return result, fmt.Errorf("unsupported tool call type %q", toolCall.Type)
			}

			toolResult := a.executeTool(
				ctx,
				functionCall.Function.Name,
				json.RawMessage(functionCall.Function.Arguments),
			)
			a.session.Messages = append(a.session.Messages, openai.ToolMessage(toolResult, functionCall.ID))
			if err := a.saveSession(); err != nil {
				return result, err
			}
		}
	}
}

func (a *Agent) loadOrCreateSession() error {
	if a.sessionManager == nil {
		now := time.Now()
		a.session = &session.Session{
			ID:        "temporary",
			Name:      "temporary session",
			CreatedAt: now,
			UpdatedAt: now,
			Messages:  []openai.ChatCompletionMessageParamUnion{},
		}
		return a.ensureSystemPrompt()
	}

	loadedSession, err := a.sessionManager.GetLatestSession()
	if err == nil {
		a.session = loadedSession
		a.emit("system", "resuming session: %s", a.session.Name)
		return a.ensureSystemPrompt()
	}
	if !errors.Is(err, session.ErrNoSessions) {
		return fmt.Errorf("failed to load latest session: %w", err)
	}

	return a.startNewSession("")
}

func (a *Agent) startNewSession(name string) error {
	if a.sessionManager == nil {
		return fmt.Errorf("session persistence is not configured")
	}

	newSession, err := a.sessionManager.CreateSession(name)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	a.session = newSession
	a.emit("system", "started new session: %s", a.session.Name)
	return a.ensureSystemPrompt()
}

func (a *Agent) ensureSystemPrompt() error {
	if a.config.SystemPrompt == "" || hasSystemMessage(a.session.Messages) {
		return a.saveSession()
	}

	a.session.Messages = append(a.session.Messages, openai.SystemMessage(a.config.SystemPrompt))
	return a.saveSession()
}

func hasSystemMessage(messages []openai.ChatCompletionMessageParamUnion) bool {
	for _, message := range messages {
		if message.OfSystem != nil {
			return true
		}
	}
	return false
}

func (a *Agent) handleCommand(input string) (bool, error) {
	command := strings.TrimSpace(input)
	switch {
	case command == "/help":
		a.emit("system", commandsMessage)
		return true, nil
	case command == "/new":
		return true, a.startNewSession("")
	case command == "/sessions":
		if a.sessionManager == nil {
			return true, fmt.Errorf("session persistence is not configured")
		}
		sessions, err := a.sessionManager.ListSessions()
		if err != nil {
			return true, err
		}
		for _, savedSession := range sessions {
			a.emit("system", "%s  %s  %s", savedSession.ID, savedSession.UpdatedAt.Format(time.RFC3339), savedSession.Name)
		}
		return true, nil
	case strings.HasPrefix(command, "/delete-session "):
		if a.sessionManager == nil {
			return true, fmt.Errorf("session persistence is not configured")
		}
		sessionID := strings.TrimSpace(strings.TrimPrefix(command, "/delete-session "))
		if sessionID == "" {
			return true, fmt.Errorf("a session id is required")
		}
		if a.session != nil && sessionID == a.session.ID {
			return true, fmt.Errorf("cannot delete the active session; use /new first")
		}
		return true, a.sessionManager.DeleteSession(sessionID)
	case strings.HasPrefix(command, "/"):
		return true, fmt.Errorf("unknown command %q", command)
	default:
		return false, nil
	}
}

func (a *Agent) saveSession() error {
	if a.sessionManager == nil || a.session == nil {
		return nil
	}
	if err := a.sessionManager.SaveSession(a.session); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

func (a *Agent) runInference(ctx context.Context, conversation []openai.ChatCompletionMessageParamUnion) (*openai.ChatCompletion, error) {
	tools := make([]openai.ChatCompletionToolUnionParam, 0, len(a.tools))
	for _, tool := range a.tools {
		tools = append(tools, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        tool.Name,
			Description: openai.String(tool.Description),
			Parameters:  tool.InputSchema,
		}))
	}

	params := openai.ChatCompletionNewParams{
		Model:               a.config.Model,
		Messages:            conversation,
		Tools:               tools,
		MaxCompletionTokens: openai.Int(int64(a.config.MaxTokens)),
		Temperature:         openai.Float(a.config.Temperature),
	}
	return a.client.Chat.Completions.New(
		ctx,
		params,
		option.WithJSONSet("reasoning.effort", a.config.ReasoningEffort),
	)
}

func (a *Agent) executeTool(ctx context.Context, name string, input json.RawMessage) string {
	for _, tool := range a.tools {
		if tool.Name != name {
			continue
		}

		a.emit("tool", "%s(%s)", name, input)
		startedAt := time.Now()
		toolCtx := ctx
		cancel := func() {}
		if a.config.ToolTimeout > 0 {
			toolCtx, cancel = context.WithTimeout(ctx, a.config.ToolTimeout)
		}
		response, err := tool.Function(toolCtx, input)
		cancel()
		if err != nil {
			response = truncateToolOutput(fmt.Sprintf("error: %s", err), a.config.MaxToolOutput)
		} else {
			response = truncateToolOutput(response, a.config.MaxToolOutput)
		}
		a.emitDuration("tool_result", time.Since(startedAt), "%s: %s", name, response)
		return response
	}

	return "error: tool not found"
}

func truncateToolOutput(output string, maxBytes int) string {
	if maxBytes <= 0 || len(output) <= maxBytes {
		return output
	}

	const suffix = "\n...[tool output truncated]"
	if maxBytes <= len(suffix) {
		return suffix[:maxBytes]
	}

	end := maxBytes - len(suffix)
	for end > 0 && end < len(output) && !utf8.RuneStart(output[end]) {
		end--
	}
	return output[:end] + suffix
}

func (a *Agent) emit(kind string, format string, args ...any) {
	a.onEvent(Event{Kind: kind, Content: fmt.Sprintf(format, args...), Timestamp: time.Now()})
}

func (a *Agent) emitDuration(kind string, duration time.Duration, format string, args ...any) {
	a.onEvent(Event{Kind: kind, Content: fmt.Sprintf(format, args...), Timestamp: time.Now(), Duration: duration})
}
