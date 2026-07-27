package config

import "testing"

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Model == "" || config.BaseURL == "" || config.SystemPrompt == "" {
		t.Fatal("default model configuration must not be empty")
	}
	if config.MaxTokens <= 0 {
		t.Fatal("default max tokens must be greater than zero")
	}
	if config.SessionsDir != "./sessions" {
		t.Fatalf("sessions directory = %q, want ./sessions", config.SessionsDir)
	}
}
