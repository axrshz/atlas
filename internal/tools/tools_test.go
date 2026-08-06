package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultToolsForWorkspaceBindFilesystemOperations(t *testing.T) {
	workspace := t.TempDir()
	definitions, err := DefaultToolsForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}

	editFile := findTool(t, definitions, "edit_file")
	if _, err := editFile.Function(context.Background(), []byte(`{"path":"isolated.txt","old_str":"","new_str":"inside"}`)); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "isolated.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "inside" {
		t.Fatalf("workspace content = %q", content)
	}

	readFile := findTool(t, definitions, "read_file")
	result, err := readFile.Function(context.Background(), []byte(`{"path":"isolated.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "inside" {
		t.Fatalf("read result = %q", result)
	}
}

func findTool(t *testing.T, definitions []ToolDefinition, name string) ToolDefinition {
	t.Helper()
	for _, definition := range definitions {
		if definition.Name == name {
			return definition
		}
	}
	t.Fatalf("tool %q was not found", name)
	return ToolDefinition{}
}

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
