package evals

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunnerGradesRepeatedTrials(t *testing.T) {
	calls := 0
	target := TargetFunc(func(context.Context, string) (Run, error) {
		calls++
		if calls == 1 {
			return Run{Output: "atlas", ToolCalls: []ToolCall{{Name: "read_file"}}}, nil
		}
		return Run{Output: "wrong"}, nil
	})
	suite := Suite{
		Name:   "smoke",
		Trials: 2,
		Tasks: []Task{{
			Name:  "identity",
			Input: "identify yourself",
			Graders: []GraderSpec{
				{Type: "equals", Value: "atlas"},
				{Type: "tool_called", Value: "read_file"},
			},
		}},
	}

	report, err := (Runner{Target: target}).Run(context.Background(), suite)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalTrials != 2 || report.PassedTrials != 1 || report.PassRate != 0.5 {
		t.Fatalf("unexpected totals: %#v", report)
	}
	task := report.Tasks[0]
	if !task.PassAtK || task.PassAllK || task.PassRate != 0.5 {
		t.Fatalf("unexpected task reliability: %#v", task)
	}
}

func TestRunnerRecordsTargetErrors(t *testing.T) {
	target := TargetFunc(func(context.Context, string) (Run, error) {
		return Run{}, errors.New("model unavailable")
	})
	suite := Suite{
		Name:   "errors",
		Trials: 1,
		Tasks: []Task{{
			Name:    "fails",
			Input:   "run",
			Graders: []GraderSpec{{Type: "contains", Value: "done"}},
		}},
	}

	report, err := (Runner{Target: target}).Run(context.Background(), suite)
	if err != nil {
		t.Fatal(err)
	}
	trial := report.Tasks[0].Trials[0]
	if trial.Passed || trial.Error != "model unavailable" {
		t.Fatalf("unexpected trial: %#v", trial)
	}
}

func TestRunnerAppliesTrialTimeout(t *testing.T) {
	target := TargetFunc(func(ctx context.Context, _ string) (Run, error) {
		<-ctx.Done()
		return Run{}, ctx.Err()
	})
	suite := Suite{
		Name:   "timeout",
		Trials: 1,
		Tasks: []Task{{
			Name:    "slow",
			Input:   "wait",
			Graders: []GraderSpec{{Type: "contains", Value: "done"}},
		}},
	}

	report, err := (Runner{Target: target, TrialTimeout: time.Millisecond}).Run(context.Background(), suite)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Tasks[0].Trials[0].Error, context.DeadlineExceeded.Error()) {
		t.Fatalf("timeout was not recorded: %#v", report)
	}
}

func TestRunnerUsesLLMJudge(t *testing.T) {
	judgeCalls := 0
	judge := JudgeFunc(func(_ context.Context, task Task, spec GraderSpec, run Run) (GradeResult, error) {
		judgeCalls++
		if task.Name != "quality" || spec.Rubric != "be correct" || run.Output != "good answer" {
			t.Fatalf("unexpected judge input: %#v %#v %#v", task, spec, run)
		}
		return GradeResult{Type: spec.Type, Passed: true, Reason: "meets the rubric"}, nil
	})
	suite := Suite{
		Name:   "judge",
		Trials: 1,
		Tasks: []Task{{
			Name:    "quality",
			Input:   "answer",
			Graders: []GraderSpec{{Type: "llm_judge", Rubric: "be correct"}},
		}},
	}
	target := TargetFunc(func(context.Context, string) (Run, error) {
		return Run{Output: "good answer"}, nil
	})

	report, err := (Runner{Target: target, Judge: judge}).Run(context.Background(), suite)
	if err != nil {
		t.Fatal(err)
	}
	if judgeCalls != 1 || !report.Tasks[0].Trials[0].Passed {
		t.Fatalf("judge result was not applied: %#v", report)
	}
}

func TestRunnerRequiresConfiguredLLMJudge(t *testing.T) {
	suite := Suite{
		Name:   "judge",
		Trials: 1,
		Tasks: []Task{{
			Name:    "quality",
			Input:   "answer",
			Graders: []GraderSpec{{Type: "llm_judge", Rubric: "be correct"}},
		}},
	}
	target := TargetFunc(func(context.Context, string) (Run, error) { return Run{}, nil })

	_, err := (Runner{Target: target}).Run(context.Background(), suite)
	if err == nil || !strings.Contains(err.Error(), "llm judge is required") {
		t.Fatalf("expected missing judge error, got %v", err)
	}
}

func TestDecodeSuiteRejectsUnknownFieldsAndGraders(t *testing.T) {
	_, err := DecodeSuite(strings.NewReader(`{
		"name":"bad","trials":1,"unknown":true,
		"tasks":[{"name":"task","input":"go","graders":[{"type":"contains","value":"ok"}]}]
	}`))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected unknown field error, got %v", err)
	}

	_, err = DecodeSuite(strings.NewReader(`{
		"name":"bad","trials":1,
		"tasks":[{"name":"task","input":"go","graders":[{"type":"judge","value":"ok"}]}]
	}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported grader") {
		t.Fatalf("expected grader error, got %v", err)
	}

	_, err = DecodeSuite(strings.NewReader(`{
		"name":"bad","trials":1,
		"tasks":[{"name":"task","input":"go","graders":[{"type":"llm_judge"}]}]
	}`))
	if err == nil || !strings.Contains(err.Error(), "rubric is required") {
		t.Fatalf("expected rubric error, got %v", err)
	}
}

func TestBuiltInOutputGraders(t *testing.T) {
	run := Run{Output: "Atlas finished in 42 ms"}
	tests := []GraderSpec{
		{Type: "contains", Value: "atlas"},
		{Type: "not_contains", Value: "error"},
		{Type: "regex", Value: `\d+ ms`},
	}
	for _, spec := range tests {
		if result := grade(spec, run); !result.Passed {
			t.Errorf("grader %q failed: %s", spec.Type, result.Reason)
		}
	}
}
