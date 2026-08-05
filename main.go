package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"atlas/internal/agent"
	"atlas/internal/config"
	"atlas/internal/session"
	"atlas/internal/tools"
	"atlas/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := loadEnvironment(); err != nil {
		fmt.Fprintf(os.Stderr, "could not load .env: %s\n", err)
		os.Exit(1)
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "missing OPENROUTER_API_KEY; add it to .env")
		os.Exit(1)
	}

	appConfig := config.DefaultConfig
	sessionManager := session.NewSessionManager(appConfig.SessionsDir)

	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(appConfig.BaseURL),
	)

	inputCh := make(chan string, 8)
	var program *tea.Program
	chatAgent := agent.NewAgent(
		&client,
		func() (string, bool) {
			input, ok := <-inputCh
			return input, ok
		},
		tools.DefaultTools(),
		appConfig,
		sessionManager,
		func(event agent.Event) {
			program.Send(tui.EventMsg{Kind: event.Kind, Content: event.Content, Append: event.Append})
		},
	)

	model := tui.New(inputCh)
	program = tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	go func() {
		program.Send(tui.AgentDoneMsg{Err: chatAgent.Run(ctx)})
	}()

	_, err := program.Run()
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not run terminal interface: %s\n", err)
	}
}

func loadEnvironment() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find user home directory: %w", err)
	}

	// Load preserves variables already present in the process environment. Loading
	// the project file first gives it precedence over the shared fallback file.
	for _, envFile := range []string{".env", filepath.Join(homeDir, ".atlas", ".env")} {
		if err := godotenv.Load(envFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("load %s: %w", envFile, err)
		}
	}
	return nil
}
