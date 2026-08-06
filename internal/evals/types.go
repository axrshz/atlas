package evals

import (
	"context"
	"time"
)

// Suite is a collection of tasks evaluated with the same trial count.
type Suite struct {
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"`
	Trials int    `json:"trials"`
	Tasks  []Task `json:"tasks"`
}

// Task describes one prompt and the checks applied to its run.
type Task struct {
	Name    string       `json:"name"`
	Input   string       `json:"input"`
	Graders []GraderSpec `json:"graders"`
}

// GraderSpec configures one built-in grader.
type GraderSpec struct {
	Type          string `json:"type"`
	Value         string `json:"value,omitempty"`
	Rubric        string `json:"rubric,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
}

// ToolCall is the part of an agent trajectory exposed to graders.
type ToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
}

// Run is the observable output of one target invocation.
type Run struct {
	Output    string     `json:"output"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Target executes one task. Implementations should start each call with fresh
// conversational state.
type Target interface {
	Run(context.Context, string) (Run, error)
}

// TargetFunc adapts a function into a Target.
type TargetFunc func(context.Context, string) (Run, error)

func (function TargetFunc) Run(ctx context.Context, input string) (Run, error) {
	return function(ctx, input)
}

// Judge applies a model-backed rubric to an agent run.
type Judge interface {
	Grade(context.Context, Task, GraderSpec, Run) (GradeResult, error)
}

// JudgeFunc adapts a function into a Judge.
type JudgeFunc func(context.Context, Task, GraderSpec, Run) (GradeResult, error)

func (function JudgeFunc) Grade(ctx context.Context, task Task, spec GraderSpec, run Run) (GradeResult, error) {
	return function(ctx, task, spec, run)
}

// GradeResult explains one grader decision.
type GradeResult struct {
	Type   string `json:"type"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason"`
}

// TrialReport contains one complete agent attempt and its grades.
type TrialReport struct {
	Trial      int           `json:"trial"`
	Passed     bool          `json:"passed"`
	DurationMS int64         `json:"duration_ms"`
	Run        Run           `json:"run"`
	Grades     []GradeResult `json:"grades"`
	Error      string        `json:"error,omitempty"`
}

// TaskReport aggregates repeated trials for one task.
type TaskReport struct {
	Name     string        `json:"name"`
	Trials   []TrialReport `json:"trials"`
	Passed   int           `json:"passed"`
	PassRate float64       `json:"pass_rate"`
	PassAtK  bool          `json:"pass_at_k"`
	PassAllK bool          `json:"pass_all_k"`
}

// Report is the serializable output of a suite run.
type Report struct {
	Suite        string       `json:"suite"`
	Kind         string       `json:"kind,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	Tasks        []TaskReport `json:"tasks"`
	TotalTrials  int          `json:"total_trials"`
	PassedTrials int          `json:"passed_trials"`
	PassRate     float64      `json:"pass_rate"`
	PassAtK      float64      `json:"pass_at_k"`
	PassAllK     float64      `json:"pass_all_k"`
}
