package config

type Config struct {
	Model           string
	BaseURL         string
	ReasoningEffort string
	MaxTokens       int
	Temperature     float64
	SystemPrompt    string
	SessionsDir     string
}

var DefaultConfig = Config{
	Model:           "openrouter/auto",
	BaseURL:         "https://openrouter.ai/api/v1",
	ReasoningEffort: "high",
	MaxTokens:       4096,
	Temperature:     0.7,
	SystemPrompt:    "You are Atlas, a helpful coding assistant. Use the available tools when needed. Keep your responses concise and to the point. Only answer and do what is asked; Not more, not less.",
	SessionsDir:     "./sessions",
}
