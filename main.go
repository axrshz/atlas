package main

import (
	"context"
	"fmt"
	"os"

	"agent/internal/agent"
	"agent/internal/config"
	"agent/internal/session"
	"agent/internal/tools"

	"github.com/chzyer/readline"
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

	appConfig, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %s\n", err)
		os.Exit(1)
	}

	sessionsDir := os.Getenv("AGENT_SESSIONS_DIR")
	if sessionsDir == "" {
		sessionsDir = "./sessions"
	}
	sessionManager := session.NewSessionManager(sessionsDir)

	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(appConfig.BaseURL),
	)

	lineReader, err := readline.NewEx(&readline.Config{Prompt: ""})
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not initialize terminal input: %s\n", err)
		os.Exit(1)
	}
	defer lineReader.Close()

	getUserMessage := func() (string, bool) {
		message, err := lineReader.Readline()
		if err != nil {
			return "", false
		}
		return message, true
	}

	chatAgent := agent.NewAgent(&client, getUserMessage, tools.DefaultTools(), appConfig, sessionManager)
	if err := chatAgent.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
	}
}
