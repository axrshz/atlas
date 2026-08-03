package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGradeAcceptsJSONWrappedInText(t *testing.T) {
	grade, err := parseGrade("```json\n{\"passed\":true,\"score\":0.9,\"reason\":\"task completed\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if !grade.Passed || grade.Score != 0.9 || grade.Reason != "task completed" {
		t.Fatalf("unexpected grade: %+v", grade)
	}
}

func TestFixtureSnapshotAndDiff(t *testing.T) {
	root := t.TempDir()
	if err := writeFixture(root, map[string]string{"main.go": "before", "nested/readme.md": "keep"}); err != nil {
		t.Fatal(err)
	}
	before, err := snapshotWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("after"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := snapshotWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	diff := diffSnapshots(before, after)
	if len(diff.Modified) != 1 || diff.Modified[0] != "main.go" {
		t.Fatalf("modified = %v", diff.Modified)
	}
	if len(diff.Added) != 1 || diff.Added[0] != "new.go" {
		t.Fatalf("added = %v", diff.Added)
	}
}

func TestWriteFixtureRejectsTraversal(t *testing.T) {
	err := writeFixture(t.TempDir(), map[string]string{"../outside.txt": "no"})
	if err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestBundledSuiteContainsFiveCases(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "evals", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 5 {
		t.Fatalf("case count = %d, want 5", len(cases))
	}
}
