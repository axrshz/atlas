package config

import (
	"testing"
)

func TestLoadConfigAppliesDefaultsAndEnvironmentOverrides(t *testing.T) {
	t.Setenv("AGENT_MODEL", "")
	t.Setenv("AGENT_BASE_URL", "")
	t.Setenv("AGENT_REASONING_EFFORT", "low")
	t.Setenv("AGENT_SYSTEM_PROMPT", "")
	t.Setenv("AGENT_MAX_TOKENS", "1234")
	t.Setenv("AGENT_TEMPERATURE", "0")

	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Model != DefaultConfig().Model {
		t.Fatalf("model = %q, want default %q", config.Model, DefaultConfig().Model)
	}
	if config.Temperature != 0 {
		t.Fatalf("temperature = %v, want 0", config.Temperature)
	}
	if config.MaxTokens != 1234 {
		t.Fatalf("max_tokens = %d, want 1234", config.MaxTokens)
	}
	if config.ReasoningEffort != "low" {
		t.Fatalf("reasoning_effort = %q, want low", config.ReasoningEffort)
	}
}
