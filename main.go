package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"agent/internal/agent"
	"agent/internal/config"
	"agent/internal/session"
	"agent/internal/tools"
	"agent/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func main() {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "could not load .env: %s\n", err)
		os.Exit(1)
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "missing OPENROUTER_API_KEY; add it to .env")
		os.Exit(1)
	}

	appConfig := config.DefaultConfig
	if model := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL")); model != "" {
		appConfig.Model = model
	}
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
			program.Send(tui.EventMsg{Kind: event.Kind, Content: event.Content})
		},
	)

	model := tui.New(inputCh, buildReloadedAgent)
	program = tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	go func() {
		program.Send(tui.AgentDoneMsg{Err: chatAgent.Run(context.Background())})
	}()

	finalModel, err := program.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not run terminal interface: %s\n", err)
		return
	}

	if model, ok := finalModel.(tui.Model); ok && model.RestartExecutable() != "" {
		if err := startReloadedAgent(model.RestartExecutable()); err != nil {
			fmt.Fprintf(os.Stderr, "failed to start updated agent: %s\n", err)
		}
	}
}

func buildReloadedAgent() (string, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to find the working directory: %w", err)
	}

	reloadDir := filepath.Join(os.TempDir(), "atlas-reload")
	if err := os.MkdirAll(reloadDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create reload directory: %w", err)
	}

	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	reloadedExecutable := filepath.Join(reloadDir, fmt.Sprintf("atlas-%d%s", time.Now().UnixNano(), extension))

	build := exec.Command("go", "build", "-o", reloadedExecutable, ".")
	build.Dir = workingDir
	buildOutput, err := build.CombinedOutput()
	if err != nil {
		details := strings.TrimSpace(string(buildOutput))
		if details == "" {
			return "", fmt.Errorf("failed to rebuild agent: %w", err)
		}
		return "", fmt.Errorf("failed to rebuild agent: %w\n%s", err, details)
	}
	return reloadedExecutable, nil
}

func startReloadedAgent(executable string) error {
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to find the working directory: %w", err)
	}

	restarted := exec.Command(executable, os.Args[1:]...)
	restarted.Dir = workingDir
	restarted.Env = os.Environ()
	restarted.Stdin = os.Stdin
	restarted.Stdout = os.Stdout
	restarted.Stderr = os.Stderr
	return restarted.Start()
}
