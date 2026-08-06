package evals

import (
	"fmt"
	"regexp"
	"strings"
)

var graderTypes = map[string]struct{}{
	"contains":     {},
	"not_contains": {},
	"equals":       {},
	"llm_judge":    {},
	"regex":        {},
	"tool_called":  {},
}

func validateGrader(spec GraderSpec) error {
	if _, exists := graderTypes[spec.Type]; !exists {
		return fmt.Errorf("unsupported grader type %q", spec.Type)
	}
	if spec.Type == "llm_judge" {
		if strings.TrimSpace(spec.Rubric) == "" {
			return fmt.Errorf("grader %q rubric is required", spec.Type)
		}
		return nil
	}
	if spec.Value == "" {
		return fmt.Errorf("grader %q value is required", spec.Type)
	}
	if spec.Type == "regex" {
		if _, err := regexp.Compile(spec.Value); err != nil {
			return fmt.Errorf("invalid regex grader: %w", err)
		}
	}
	return nil
}

func grade(spec GraderSpec, run Run) GradeResult {
	result := GradeResult{Type: spec.Type}
	actual, expected := run.Output, spec.Value
	if !spec.CaseSensitive && spec.Type != "regex" {
		actual = strings.ToLower(actual)
		expected = strings.ToLower(expected)
	}

	switch spec.Type {
	case "contains":
		result.Passed = strings.Contains(actual, expected)
		result.Reason = fmt.Sprintf("output contains %q", spec.Value)
	case "not_contains":
		result.Passed = !strings.Contains(actual, expected)
		result.Reason = fmt.Sprintf("output excludes %q", spec.Value)
	case "equals":
		result.Passed = strings.TrimSpace(actual) == strings.TrimSpace(expected)
		result.Reason = fmt.Sprintf("output equals %q", spec.Value)
	case "regex":
		result.Passed = regexp.MustCompile(spec.Value).MatchString(run.Output)
		result.Reason = fmt.Sprintf("output matches %q", spec.Value)
	case "tool_called":
		for _, toolCall := range run.ToolCalls {
			name := toolCall.Name
			if !spec.CaseSensitive {
				name = strings.ToLower(name)
			}
			if name == expected {
				result.Passed = true
				break
			}
		}
		result.Reason = fmt.Sprintf("tool %q was called", spec.Value)
	}
	if !result.Passed {
		result.Reason = "failed: " + result.Reason
	}
	return result
}
