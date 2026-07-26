package main

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func main() {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "could not load .env: %s\n", err)
		os.Exit(1)
	}

	apiKey := os.Getenv("POOLSIDE_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "missing POOLSIDE_API_KEY; add it to .env")
		os.Exit(1)
	}

	configPath := os.Getenv("AGENT_CONFIG")
	config, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %s\n", err)
		os.Exit(1)
	}

	sessionsDir := os.Getenv("AGENT_SESSIONS_DIR")
	if sessionsDir == "" {
		sessionsDir = "./sessions"
	}
	sessionManager := NewSessionManager(sessionsDir)

	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(config.BaseURL),
	)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	getUserMessage := func() (string, bool) {
		if !scanner.Scan() {
			return "", false
		}
		return scanner.Text(), true
	}

	agent := NewAgent(&client, getUserMessage, defaultTools(), config, sessionManager)
	if err := agent.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
	}
}
