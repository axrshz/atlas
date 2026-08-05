package tools

import (
	"context"
	"strings"
	"testing"
)

func TestBashExecutesArbitraryCommand(t *testing.T) {
	result, err := Bash(context.Background(), []byte(`{"command":"printf unrestricted"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "unrestricted" {
		t.Fatalf("result = %q, want unrestricted", result)
	}
}

func TestBashLimitsCommandOutput(t *testing.T) {
	result, err := Bash(context.Background(), []byte(`{"command":"yes x | head -c 40000"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "[command output truncated]") {
		t.Fatal("command output was not truncated")
	}
	if len(result) > maxCommandOutput+64 {
		t.Fatalf("result length = %d, want at most %d plus marker", len(result), maxCommandOutput)
	}
}

func TestBashHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := Bash(ctx, []byte(`{"command":"printf unreachable"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, context.Canceled.Error()) {
		t.Fatalf("result = %q, want cancellation error", result)
	}
}
