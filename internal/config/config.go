package config

import "time"

type Config struct {
	Model           string
	BaseURL         string
	ReasoningEffort string
	MaxTokens       int
	MaxAgentSteps   int
	Temperature     float64
	SystemPrompt    string
	SessionsDir     string
	ToolTimeout     time.Duration
	MaxToolOutput   int
}

var DefaultConfig = Config{
	Model:           "openrouter/auto",
	BaseURL:         "https://openrouter.ai/api/v1",
	ReasoningEffort: "high",
	MaxTokens:       4096,
	MaxAgentSteps:   30,
	Temperature:     0.7,
	SystemPrompt:    "You are Atlas, a helpful coding assistant. Use the available tools when needed. Keep your responses concise and to the point. Only answer and do what is asked; Not more, not less.",
	SessionsDir:     "./sessions",
	ToolTimeout:     2 * time.Minute,
	MaxToolOutput:   32 << 10,
}
