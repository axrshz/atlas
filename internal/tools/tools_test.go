package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultToolsAtBindsFilesystemToolsToWorkspace(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "bound.txt"), []byte("workspace content"), 0644); err != nil {
		t.Fatal(err)
	}

	definitions, err := DefaultToolsAt(workspace)
	if err != nil {
		t.Fatal(err)
	}
	var readFile ToolDefinition
	for _, definition := range definitions {
		if definition.Name == "read_file" {
			readFile = definition
			break
		}
	}
	if readFile.Function == nil {
		t.Fatal("read_file was not included")
	}
	result, err := readFile.Function(context.Background(), []byte(`{"path":"bound.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "workspace content" {
		t.Fatalf("result = %q, want workspace content", result)
	}
}

func TestBashExecutesArbitraryCommand(t *testing.T) {
	result, err := Bash(context.Background(), []byte(`{"command":"go version"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "go version") {
		t.Fatalf("result = %q, want go version output", result)
	}
}

func TestBashLimitsCommandOutput(t *testing.T) {
	command := `{"command":"yes x | head -c 40000"}`
	if runtime.GOOS == "windows" {
		command = `{"command":"[Console]::Out.Write(('x' * 40000))"}`
	}
	result, err := Bash(context.Background(), []byte(command))
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

	result, err := Bash(ctx, []byte(`{"command":"go version"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, context.Canceled.Error()) {
		t.Fatalf("result = %q, want cancellation error", result)
	}
}
