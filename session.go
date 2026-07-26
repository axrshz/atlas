package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
)

// ErrNoSessions tells the caller that there is no saved conversation to resume.
var ErrNoSessions = errors.New("no sessions found")

// Session is one saved conversation.
//
// Messages is used while the agent is running. MessagesRaw is the JSON version
// written to disk. The OpenAI SDK message union needs this explicit conversion
// so every message type, including tool calls and tool results, survives a restart.
type Session struct {
	ID          string                                   `json:"id"`
	Name        string                                   `json:"name"`
	CreatedAt   time.Time                                `json:"created_at"`
	UpdatedAt   time.Time                                `json:"updated_at"`
	Messages    []openai.ChatCompletionMessageParamUnion `json:"-"`        // runtime messages sent to the api
	MessagesRaw []json.RawMessage                        `json:"messages"` // serialized messages written to disk
}

// SessionManager stores each session as <session id>.json in sessionsDir.
type SessionManager struct {
	sessionsDir string
}

func NewSessionManager(sessionsDir string) *SessionManager {
	return &SessionManager{sessionsDir: sessionsDir}
}

// CreateSession makes an empty session and saves it immediately.
func (sm *SessionManager) CreateSession(name string) (*Session, error) {
	if name == "" {
		name = "session-" + time.Now().Format("2006-01-02-15-04-05")
	}

	now := time.Now()
	session := &Session{
		ID:        fmt.Sprintf("%d", now.UnixNano()),
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
		Messages:  []openai.ChatCompletionMessageParamUnion{},
	}
	if err := sm.SaveSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

func (sm *SessionManager) LoadSession(sessionID string) (*Session, error) {
	// sessionPath validates the id before it becomes part of a filesystem path.
	sessionPath, err := sm.sessionPath(sessionID)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session %q not found", sessionID)
		}
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to parse session file: %w", err)
	}
	if session.ID != sessionID {
		return nil, fmt.Errorf("session id does not match its file name")
	}

	// Convert the JSON messages back into the SDK type used by Chat Completions.
	session.Messages = make([]openai.ChatCompletionMessageParamUnion, 0, len(session.MessagesRaw))
	for _, raw := range session.MessagesRaw {
		message, err := rawToMessage(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to restore a session message: %w", err)
		}
		session.Messages = append(session.Messages, message)
	}
	// Only Messages is needed while the session is active in memory.
	session.MessagesRaw = nil

	return &session, nil
}

func (sm *SessionManager) SaveSession(session *Session) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}
	sessionPath, err := sm.sessionPath(session.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(sm.sessionsDir, 0755); err != nil {
		return fmt.Errorf("failed to create sessions directory: %w", err)
	}

	// Convert each SDK message to raw JSON before saving it to the session file.
	messagesRaw := make([]json.RawMessage, 0, len(session.Messages))
	for _, message := range session.Messages {
		raw, err := messageToRaw(message)
		if err != nil {
			return fmt.Errorf("failed to serialize a session message: %w", err)
		}
		messagesRaw = append(messagesRaw, raw)
	}
	session.MessagesRaw = messagesRaw
	session.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Write to a temporary file first. Renaming it only after a complete write
	// reduces the chance of leaving a half-written session after an interruption.
	temporaryFile, err := os.CreateTemp(sm.sessionsDir, session.ID+"-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary session file: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	defer os.Remove(temporaryPath)

	if _, err := temporaryFile.Write(data); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("failed to write temporary session file: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary session file: %w", err)
	}
	if err := os.Rename(temporaryPath, sessionPath); err != nil {
		return fmt.Errorf("failed to replace session file: %w", err)
	}
	return nil
}

func (sm *SessionManager) ListSessions() ([]Session, error) {
	entries, err := os.ReadDir(sm.sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Session{}, nil
		}
		return nil, fmt.Errorf("failed to read sessions directory: %w", err)
	}

	sessions := make([]Session, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sm.sessionsDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read session %q: %w", entry.Name(), err)
		}

		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			return nil, fmt.Errorf("failed to parse session %q: %w", entry.Name(), err)
		}
		if session.ID == "" || entry.Name() != session.ID+".json" {
			return nil, fmt.Errorf("invalid session file %q", entry.Name())
		}
		// The list command only needs metadata, not the full conversation history.
		session.Messages = nil
		session.MessagesRaw = nil
		sessions = append(sessions, session)
	}

	// The newest session comes first so it can be resumed automatically.
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

func (sm *SessionManager) DeleteSession(sessionID string) error {
	sessionPath, err := sm.sessionPath(sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(sessionPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session %q not found", sessionID)
		}
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}

func (sm *SessionManager) GetLatestSession() (*Session, error) {
	sessions, err := sm.ListSessions()
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, ErrNoSessions
	}
	return sm.LoadSession(sessions[0].ID)
}

func (sm *SessionManager) sessionPath(sessionID string) (string, error) {
	// Reject separators and path traversal so a session id cannot escape sessionsDir.
	if sessionID == "" || filepath.Base(sessionID) != sessionID || strings.ContainsAny(sessionID, `/\\`) {
		return "", fmt.Errorf("invalid session id")
	}
	return filepath.Join(sm.sessionsDir, sessionID+".json"), nil
}

func messageToRaw(message openai.ChatCompletionMessageParamUnion) (json.RawMessage, error) {
	// Store the SDK union as JSON instead of maintaining our own message format.
	raw, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func rawToMessage(raw json.RawMessage) (openai.ChatCompletionMessageParamUnion, error) {
	// The SDK restores the correct union variant from the saved message JSON.
	var message openai.ChatCompletionMessageParamUnion
	if err := json.Unmarshal(raw, &message); err != nil {
		return openai.ChatCompletionMessageParamUnion{}, err
	}
	return message, nil
}
