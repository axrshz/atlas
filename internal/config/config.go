package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Model           string
	BaseURL         string
	ReasoningEffort string
	MaxTokens       int
	Temperature     float64
	SystemPrompt    string
}

func DefaultConfig() Config {
	return Config{
		Model:           "poolside/laguna-s-2.1",
		BaseURL:         "https://inference.poolside.ai/v1",
		ReasoningEffort: "high",
		MaxTokens:       4096,
		Temperature:     0.7,
		SystemPrompt:    "You are a helpful coding assistant. Use the available tools when needed. Keep your responses concise and to the point. Only answer and do what is asked; Not more, not less.",
	}
}

// LoadConfig starts with defaults and applies optional settings from .env.
func LoadConfig() (Config, error) {
	config := DefaultConfig()

	if err := applyEnvironmentOverrides(&config); err != nil {
		return config, err
	}
	if err := validateConfig(config); err != nil {
		return config, err
	}
	return config, nil
}

func applyEnvironmentOverrides(config *Config) error {
	if value := os.Getenv("AGENT_MODEL"); value != "" {
		config.Model = value
	}
	if value := os.Getenv("AGENT_BASE_URL"); value != "" {
		config.BaseURL = value
	}
	if value := os.Getenv("AGENT_REASONING_EFFORT"); value != "" {
		config.ReasoningEffort = value
	}
	if value := os.Getenv("AGENT_SYSTEM_PROMPT"); value != "" {
		config.SystemPrompt = value
	}
	if value := os.Getenv("AGENT_MAX_TOKENS"); value != "" {
		maxTokens, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid AGENT_MAX_TOKENS: %w", err)
		}
		config.MaxTokens = maxTokens
	}
	if value := os.Getenv("AGENT_TEMPERATURE"); value != "" {
		temperature, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid AGENT_TEMPERATURE: %w", err)
		}
		config.Temperature = temperature
	}
	return nil
}

func validateConfig(config Config) error {
	if strings.TrimSpace(config.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		return fmt.Errorf("base_url is required")
	}
	if config.MaxTokens <= 0 {
		return fmt.Errorf("max_tokens must be greater than zero")
	}
	if config.Temperature < 0 || config.Temperature > 2 {
		return fmt.Errorf("temperature must be between 0 and 2")
	}

	supportedEfforts := map[string]bool{
		"none": true, "minimal": true, "low": true,
		"medium": true, "high": true, "xhigh": true,
	}
	if !supportedEfforts[config.ReasoningEffort] {
		return fmt.Errorf("unsupported reasoning_effort %q", config.ReasoningEffort)
	}
	return nil
}
