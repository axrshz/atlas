package evals

import (
	"path/filepath"
	"testing"
)

func TestCapabilitySuiteExercisesEveryAgentToolAcrossFocusedTasks(t *testing.T) {
	suite, err := LoadSuite(filepath.Join("..", "..", "evals", "capabilities.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(suite.Tasks) != 5 {
		t.Fatalf("task count = %d, want 5", len(suite.Tasks))
	}

	requiredTools := []string{"list_files", "read_file", "edit_file", "delete_file", "bash", "web_search", "web_fetch"}
	coveredTools := make(map[string]bool)
	for _, task := range suite.Tasks {
		toolGraderCount := 0
		judgeCount := 0
		for _, grader := range task.Graders {
			if grader.Type == "tool_called" {
				coveredTools[grader.Value] = true
				toolGraderCount++
			}
			if grader.Type == "llm_judge" {
				judgeCount++
			}
		}
		if toolGraderCount < 3 || toolGraderCount > 5 {
			t.Errorf("task %q has %d tool graders, want 3 to 5", task.Name, toolGraderCount)
		}
		if judgeCount != 1 {
			t.Errorf("task %q has %d llm judges, want 1", task.Name, judgeCount)
		}
	}
	for _, toolName := range requiredTools {
		if !coveredTools[toolName] {
			t.Errorf("suite does not exercise %s", toolName)
		}
	}
}
