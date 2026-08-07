package tools

import (
	"context"
	"io/fs"
	"sort"
	"strings"
	"testing"
)

type fakeSandboxRuntime struct {
	files       map[string]string
	lastCommand string
	lastWorkdir string
}

func (runtime *fakeSandboxRuntime) ReadText(_ context.Context, remotePath string) (string, error) {
	content, exists := runtime.files[remotePath]
	if !exists {
		return "", fs.ErrNotExist
	}
	return content, nil
}

func (runtime *fakeSandboxRuntime) WriteText(_ context.Context, content, remotePath string) error {
	runtime.files[remotePath] = content
	return nil
}

func (runtime *fakeSandboxRuntime) ListFiles(_ context.Context, remotePath string) ([]sandboxEntry, error) {
	prefix := strings.TrimSuffix(remotePath, "/") + "/"
	seen := map[string]string{}
	for filePath := range runtime.files {
		if !strings.HasPrefix(filePath, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(filePath, prefix)
		parts := strings.SplitN(remainder, "/", 2)
		entryType := "file"
		if len(parts) == 2 {
			entryType = "directory"
		}
		seen[parts[0]] = entryType
	}
	entries := make([]sandboxEntry, 0, len(seen))
	for name, entryType := range seen {
		entries = append(entries, sandboxEntry{Name: name, Type: entryType})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func (runtime *fakeSandboxRuntime) Remove(_ context.Context, remotePath string) error {
	if _, exists := runtime.files[remotePath]; !exists {
		return fs.ErrNotExist
	}
	delete(runtime.files, remotePath)
	return nil
}

func (runtime *fakeSandboxRuntime) Exec(_ context.Context, command, workdir string) (string, int, error) {
	runtime.lastCommand = command
	runtime.lastWorkdir = workdir
	return "command output", 0, nil
}

func TestSandboxToolsBindOperationsToRemoteWorkspace(t *testing.T) {
	runtime := &fakeSandboxRuntime{files: map[string]string{
		"/workspace/README.md":        "atlas",
		"/workspace/internal/code.go": "package internal",
	}}
	definitions := sandboxTools(runtime, "/workspace")

	readFile := findTool(t, definitions, "read_file")
	result, err := readFile.Function(context.Background(), []byte(`{"path":"README.md"}`))
	if err != nil || result != "atlas" {
		t.Fatalf("read_file result = %q, err = %v", result, err)
	}

	editFile := findTool(t, definitions, "edit_file")
	if _, err := editFile.Function(context.Background(), []byte(`{"path":"artifacts/report.md","old_str":"","new_str":"evidence"}`)); err != nil {
		t.Fatal(err)
	}
	if runtime.files["/workspace/artifacts/report.md"] != "evidence" {
		t.Fatalf("remote artifact was not created: %#v", runtime.files)
	}

	listFiles := findTool(t, definitions, "list_files")
	result, err = listFiles.Function(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"README.md"`, `"artifacts/"`, `"artifacts/report.md"`, `"internal/code.go"`} {
		if !strings.Contains(result, expected) {
			t.Fatalf("list result %s does not contain %s", result, expected)
		}
	}

	bash := findTool(t, definitions, "bash")
	result, err = bash.Function(context.Background(), []byte(`{"command":"go test ./..."}`))
	if err != nil || result != "command output" {
		t.Fatalf("bash result = %q, err = %v", result, err)
	}
	if runtime.lastCommand != "go test ./..." || runtime.lastWorkdir != "/workspace" {
		t.Fatalf("command = %q, workdir = %q", runtime.lastCommand, runtime.lastWorkdir)
	}
}

func TestSandboxToolsRejectTraversalAndProtectEnvironmentFile(t *testing.T) {
	runtime := &fakeSandboxRuntime{files: map[string]string{"/workspace/.env": "secret"}}
	definitions := sandboxTools(runtime, "/workspace")
	readFile := findTool(t, definitions, "read_file")

	if _, err := readFile.Function(context.Background(), []byte(`{"path":"../outside"}`)); err == nil || !strings.Contains(err.Error(), "stay within") {
		t.Fatalf("traversal error = %v", err)
	}
	if _, err := readFile.Function(context.Background(), []byte(`{"path":".env"}`)); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf(".env error = %v", err)
	}
}
