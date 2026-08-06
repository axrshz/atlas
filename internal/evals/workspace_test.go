package evals

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTemporaryWorkspaceCopiesAndIsolatesSource(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, source, "README.md", "source")
	writeTestFile(t, source, filepath.Join("internal", "code.go"), "package internal")
	writeTestFile(t, source, ".env", "SECRET=value")
	writeTestFile(t, source, filepath.Join(".git", "config"), "git")
	writeTestFile(t, source, filepath.Join("sessions", "session.json"), "session")
	writeTestFile(t, source, filepath.Join("eval-reports", "report.json"), "report")
	writeTestFile(t, source, "atlas.exe", "binary")

	workspace, err := NewTemporaryWorkspace(source)
	if err != nil {
		t.Fatal(err)
	}
	workspacePath := workspace.Dir()

	for _, expected := range []string{"README.md", filepath.Join("internal", "code.go")} {
		if _, err := os.Stat(filepath.Join(workspace.Dir(), expected)); err != nil {
			t.Fatalf("expected %s to be copied: %v", expected, err)
		}
	}
	for _, excluded := range []string{".env", ".git", "sessions", "eval-reports", "atlas.exe"} {
		if _, err := os.Stat(filepath.Join(workspace.Dir(), excluded)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be excluded, got %v", excluded, err)
		}
	}

	if err := os.WriteFile(filepath.Join(workspace.Dir(), "README.md"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	sourceContent, err := os.ReadFile(filepath.Join(source, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sourceContent) != "source" {
		t.Fatalf("source workspace was modified: %q", sourceContent)
	}

	if err := workspace.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("temporary workspace was not removed: %v", err)
	}
}

func TestIsolatedTargetStartsEveryRunFromFreshCopy(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, source, "state.txt", "original")
	var workspaces []string
	target := IsolatedTarget{
		Source: source,
		NewTarget: func(workspace string) (Target, error) {
			workspaces = append(workspaces, workspace)
			return TargetFunc(func(context.Context, string) (Run, error) {
				path := filepath.Join(workspace, "state.txt")
				content, err := os.ReadFile(path)
				if err != nil {
					return Run{}, err
				}
				if err := os.WriteFile(path, []byte("changed"), 0644); err != nil {
					return Run{}, err
				}
				return Run{Output: string(content)}, nil
			}), nil
		},
	}

	for trial := 0; trial < 2; trial++ {
		run, err := target.Run(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if run.Output != "original" {
			t.Fatalf("trial %d observed %q", trial+1, run.Output)
		}
	}
	if len(workspaces) != 2 || workspaces[0] == workspaces[1] {
		t.Fatalf("trials did not receive distinct workspaces: %#v", workspaces)
	}
	for _, workspace := range workspaces {
		if _, err := os.Stat(workspace); !os.IsNotExist(err) {
			t.Fatalf("workspace was not removed: %s", workspace)
		}
	}
	content, err := os.ReadFile(filepath.Join(source, "state.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Fatalf("source changed to %q", content)
	}
}

func writeTestFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
