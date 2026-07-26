package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"agent/internal/config"
	"agent/internal/session"
	"agent/internal/tools"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

var errAgentReloading = errors.New("agent is reloading")

type Agent struct {
	client         *openai.Client
	getUserMessage func() (string, bool)
	tools          []tools.ToolDefinition
	config         config.Config
	sessionManager *session.SessionManager
	session        *session.Session
}

func NewAgent(client *openai.Client, getUserMessage func() (string, bool), toolDefinitions []tools.ToolDefinition, appConfig config.Config, sessionManager *session.SessionManager) *Agent {
	return &Agent{
		client:         client,
		getUserMessage: getUserMessage,
		tools:          toolDefinitions,
		config:         appConfig,
		sessionManager: sessionManager,
	}
}

func (a *Agent) Run(ctx context.Context) error {
	if err := a.loadOrCreateSession(); err != nil {
		return err
	}

	fmt.Printf("chat with %s (use ctrl-c to quit)\n", a.config.Model)
	fmt.Println("commands: /new, /sessions, /delete-session <id>, /reload, /help")

	for {
		fmt.Print("\033[94myou\033[0m: ")
		userInput, ok := a.getUserMessage()
		if !ok {
			return a.saveSession()
		}

		if handled, err := a.handleCommand(userInput); handled || err != nil {
			if errors.Is(err, errAgentReloading) {
				return nil
			}
			if err != nil {
				fmt.Printf("error: %s\n", err)
			}
			continue
		}

		a.session.Messages = append(a.session.Messages, openai.UserMessage(userInput))
		if err := a.saveSession(); err != nil {
			return err
		}

		for {
			response, err := a.runInference(ctx, a.session.Messages)
			if err != nil {
				return err
			}
			if len(response.Choices) == 0 {
				return fmt.Errorf("poolside returned no completion choices")
			}

			message := response.Choices[0].Message
			a.session.Messages = append(a.session.Messages, message.ToParam())
			if err := a.saveSession(); err != nil {
				return err
			}

			if message.Content != "" {
				fmt.Printf("\033[93mlaguna\033[0m: %s\n", message.Content)
			}
			if len(message.ToolCalls) == 0 {
				break
			}

			for _, toolCall := range message.ToolCalls {
				functionCall, ok := toolCall.AsAny().(openai.ChatCompletionMessageFunctionToolCall)
				if !ok {
					return fmt.Errorf("unsupported tool call type %q", toolCall.Type)
				}

				result := a.executeTool(
					functionCall.Function.Name,
					json.RawMessage(functionCall.Function.Arguments),
				)
				a.session.Messages = append(a.session.Messages, openai.ToolMessage(result, functionCall.ID))
				if err := a.saveSession(); err != nil {
					return err
				}
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
		fmt.Printf("resuming session: %s\n", a.session.Name)
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

	session, err := a.sessionManager.CreateSession(name)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	a.session = session
	fmt.Printf("started new session: %s\n", a.session.Name)
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
		fmt.Println("commands: /new, /sessions, /delete-session <id>, /reload, /help")
		return true, nil
	case command == "/reload":
		return true, a.reload()
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
		for _, session := range sessions {
			fmt.Printf("%s  %s  %s\n", session.ID, session.UpdatedAt.Format(time.RFC3339), session.Name)
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

// reload builds the current source into a temporary executable, starts it, and
// returns a sentinel error so Run can exit the old process cleanly.
func (a *Agent) reload() error {
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to find the working directory: %w", err)
	}

	reloadDir := filepath.Join(os.TempDir(), "atlas-reload")
	if err := os.MkdirAll(reloadDir, 0755); err != nil {
		return fmt.Errorf("failed to create reload directory: %w", err)
	}

	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	reloadedExecutable := filepath.Join(reloadDir, fmt.Sprintf("atlas-%d%s", time.Now().UnixNano(), extension))

	fmt.Println("rebuilding agent...")
	build := exec.Command("go", "build", "-o", reloadedExecutable, ".")
	build.Dir = workingDir
	buildOutput, err := build.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to rebuild agent: %w\n%s", err, strings.TrimSpace(string(buildOutput)))
	}

	fmt.Println("starting updated agent...")
	restarted := exec.Command(reloadedExecutable, os.Args[1:]...)
	restarted.Dir = workingDir
	restarted.Env = os.Environ()
	restarted.Stdin = os.Stdin
	restarted.Stdout = os.Stdout
	restarted.Stderr = os.Stderr
	if err := restarted.Start(); err != nil {
		return fmt.Errorf("failed to start updated agent: %w", err)
	}

	return errAgentReloading
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

func (a *Agent) executeTool(name string, input json.RawMessage) string {
	for _, tool := range a.tools {
		if tool.Name != name {
			continue
		}

		fmt.Printf("\033[92mtool\033[0m: %s(%s)\n", name, input)
		response, err := tool.Function(input)
		if err != nil {
			return fmt.Sprintf("error: %s", err)
		}
		return response
	}

	return "error: tool not found"
}
