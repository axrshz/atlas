package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"atlas/internal/config"
	"atlas/internal/session"
	"atlas/internal/tools"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

const commandsMessage = "commands: /new, /sessions, /delete-session <id>, /help"

// Event is a message for the user interface to display.
type Event struct {
	Kind    string
	Content string
	Append  bool
}

type inferenceResult struct {
	message      openai.ChatCompletionMessage
	messageParam openai.ChatCompletionMessageParamUnion
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

		a.session.Messages = append(a.session.Messages, openai.UserMessage(userInput))
		if err := a.saveSession(); err != nil {
			return err
		}

		for {
			response, err := a.runInference(ctx, a.session.Messages)
			if err != nil {
				return err
			}

			message := response.message
			a.session.Messages = append(a.session.Messages, response.messageParam)
			if err := a.saveSession(); err != nil {
				return err
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
					ctx,
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

func (a *Agent) runInference(ctx context.Context, conversation []openai.ChatCompletionMessageParamUnion) (inferenceResult, error) {
	params := a.chatCompletionParams(conversation)
	stream := a.client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()

	accumulator := openRouterStreamAccumulator{}
	for stream.Next() {
		contentDelta, reasoningDelta, err := accumulator.addChunk(stream.Current())
		if err != nil {
			return inferenceResult{}, err
		}
		if reasoningDelta != "" {
			a.emitDelta("reasoning", reasoningDelta)
		}
		if contentDelta != "" {
			a.emitDelta("assistant", contentDelta)
		}
	}
	if err := stream.Err(); err != nil {
		return inferenceResult{}, fmt.Errorf("stream openrouter response: %w", err)
	}
	if len(accumulator.Choices) == 0 {
		return inferenceResult{}, fmt.Errorf("openrouter returned no completion choices")
	}

	messageParam, err := accumulator.messageParam()
	if err != nil {
		return inferenceResult{}, err
	}
	return inferenceResult{
		message:      accumulator.Choices[0].Message,
		messageParam: messageParam,
	}, nil
}

func (a *Agent) chatCompletionParams(conversation []openai.ChatCompletionMessageParamUnion) openai.ChatCompletionNewParams {
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
	if a.config.ReasoningEffort != "" {
		params.SetExtraFields(map[string]any{
			"reasoning": map[string]any{
				"effort":  a.config.ReasoningEffort,
				"exclude": false,
			},
		})
	}
	return params
}

type openRouterStreamAccumulator struct {
	openai.ChatCompletionAccumulator
	reasoning               string
	reasoningDetails        []map[string]json.RawMessage
	reasoningDetailsPresent bool
	textStarted             bool
}

func (accumulator *openRouterStreamAccumulator) addChunk(chunk openai.ChatCompletionChunk) (string, string, error) {
	if !accumulator.AddChunk(chunk) {
		return "", "", fmt.Errorf("could not accumulate openrouter stream chunk")
	}
	if len(chunk.Choices) == 0 {
		return "", "", nil
	}

	delta := chunk.Choices[0].Delta
	var fields struct {
		Reasoning        string          `json:"reasoning"`
		ReasoningDetails json.RawMessage `json:"reasoning_details"`
	}
	if raw := delta.RawJSON(); raw != "" {
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			return "", "", fmt.Errorf("decode openrouter stream reasoning: %w", err)
		}
	}

	reasoningDelta := ""
	if hasJSONValue(fields.ReasoningDetails) {
		accumulator.reasoningDetailsPresent = true
		var details []map[string]json.RawMessage
		if err := json.Unmarshal(fields.ReasoningDetails, &details); err != nil {
			return "", "", fmt.Errorf("decode openrouter reasoning details: %w", err)
		}
		for _, detail := range details {
			visibleText, err := accumulator.addReasoningDetail(detail)
			if err != nil {
				return "", "", err
			}
			if !accumulator.textStarted {
				reasoningDelta += visibleText
			}
		}
		if len(details) == 0 && fields.Reasoning != "" {
			accumulator.reasoning += fields.Reasoning
			if !accumulator.textStarted {
				reasoningDelta = fields.Reasoning
			}
		}
	} else if fields.Reasoning != "" {
		accumulator.reasoning += fields.Reasoning
		if !accumulator.textStarted {
			reasoningDelta = fields.Reasoning
		}
	}

	if delta.Content != "" {
		accumulator.textStarted = true
	}
	return delta.Content, reasoningDelta, nil
}

func (accumulator *openRouterStreamAccumulator) addReasoningDetail(detail map[string]json.RawMessage) (string, error) {
	detailType, err := rawJSONString(detail["type"])
	if err != nil {
		return "", fmt.Errorf("decode openrouter reasoning detail type: %w", err)
	}

	visibleText := ""
	textField := ""
	switch detailType {
	case "reasoning.text":
		textField = "text"
	case "reasoning.summary":
		textField = "summary"
	}
	if textField != "" {
		visibleText, err = rawJSONString(detail[textField])
		if err != nil {
			return "", fmt.Errorf("decode openrouter %s: %w", textField, err)
		}
	}

	lastIndex := len(accumulator.reasoningDetails) - 1
	if textField == "" || lastIndex < 0 {
		accumulator.reasoningDetails = append(accumulator.reasoningDetails, detail)
		return visibleText, nil
	}
	lastType, err := rawJSONString(accumulator.reasoningDetails[lastIndex]["type"])
	if err != nil {
		return "", fmt.Errorf("decode accumulated openrouter reasoning detail type: %w", err)
	}
	if lastType != detailType {
		accumulator.reasoningDetails = append(accumulator.reasoningDetails, detail)
		return visibleText, nil
	}

	lastDetail := accumulator.reasoningDetails[lastIndex]
	previousText, err := rawJSONString(lastDetail[textField])
	if err != nil {
		return "", fmt.Errorf("decode accumulated openrouter %s: %w", textField, err)
	}
	combinedText, err := json.Marshal(previousText + visibleText)
	if err != nil {
		return "", err
	}
	lastDetail[textField] = combinedText
	for key, value := range detail {
		if key == textField {
			continue
		}
		if !hasJSONValue(lastDetail[key]) && hasJSONValue(value) {
			lastDetail[key] = value
		}
	}
	return visibleText, nil
}

func (accumulator *openRouterStreamAccumulator) messageParam() (openai.ChatCompletionMessageParamUnion, error) {
	message := accumulator.Choices[0].Message
	assistant := message.ToAssistantMessageParam()
	if accumulator.reasoningDetailsPresent {
		reasoningDetails, err := json.Marshal(accumulator.reasoningDetails)
		if err != nil {
			return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("encode openrouter reasoning details: %w", err)
		}
		assistant.SetExtraFields(map[string]any{"reasoning_details": json.RawMessage(reasoningDetails)})
	} else if accumulator.reasoning != "" {
		assistant.SetExtraFields(map[string]any{"reasoning": accumulator.reasoning})
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant}, nil
}

func rawJSONString(value json.RawMessage) (string, error) {
	if !hasJSONValue(value) {
		return "", nil
	}
	var decoded string
	if err := json.Unmarshal(value, &decoded); err != nil {
		return "", err
	}
	return decoded, nil
}

func hasJSONValue(value json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(value))
	return trimmed != "" && trimmed != "null"
}

func (a *Agent) executeTool(ctx context.Context, name string, input json.RawMessage) string {
	for _, tool := range a.tools {
		if tool.Name != name {
			continue
		}

		a.emit("tool", "%s(%s)", name, input)
		toolCtx := ctx
		cancel := func() {}
		if a.config.ToolTimeout > 0 {
			toolCtx, cancel = context.WithTimeout(ctx, a.config.ToolTimeout)
		}
		response, err := tool.Function(toolCtx, input)
		cancel()
		if err != nil {
			return truncateToolOutput(fmt.Sprintf("error: %s", err), a.config.MaxToolOutput)
		}
		return truncateToolOutput(response, a.config.MaxToolOutput)
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
	a.onEvent(Event{Kind: kind, Content: fmt.Sprintf(format, args...)})
}

func (a *Agent) emitDelta(kind, content string) {
	a.onEvent(Event{Kind: kind, Content: content, Append: true})
}
