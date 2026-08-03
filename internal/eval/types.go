package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"agent/internal/agent"
)

// Case describes one isolated evaluation task and its starting files.
type Case struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Prompt         string            `json:"prompt"`
	Expected       string            `json:"expected"`
	Category       string            `json:"category"`
	Difficulty     string            `json:"difficulty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	MaxSteps       int               `json:"max_steps,omitempty"`
	Files          map[string]string `json:"files"`
}

// Grade is the structured verdict returned by a grader.
type Grade struct {
	Passed bool    `json:"passed"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
	Model  string  `json:"model,omitempty"`
}

// WorkspaceDiff summarizes the observable file outcome of a trial.
type WorkspaceDiff struct {
	Added    []string `json:"added,omitempty"`
	Modified []string `json:"modified,omitempty"`
	Deleted  []string `json:"deleted,omitempty"`
}

// TrialResult contains the agent trace, outcome, and judge verdict for one case.
type TrialResult struct {
	CaseID        string            `json:"case_id"`
	CaseName      string            `json:"case_name"`
	Category      string            `json:"category"`
	Difficulty    string            `json:"difficulty"`
	Model         string            `json:"model"`
	StartedAt     time.Time         `json:"started_at"`
	Duration      time.Duration     `json:"duration"`
	AgentResult   agent.RunResult   `json:"agent_result"`
	Trace         []agent.Event     `json:"trace"`
	InitialFiles  map[string]string `json:"initial_files"`
	FinalFiles    map[string]string `json:"final_files"`
	WorkspaceDiff WorkspaceDiff     `json:"workspace_diff"`
	Grade         Grade             `json:"grade"`
	AgentError    string            `json:"agent_error,omitempty"`
	GraderError   string            `json:"grader_error,omitempty"`
}

// Summary is the persisted output of one evaluation suite run.
type Summary struct {
	Name        string        `json:"name"`
	Model       string        `json:"model"`
	StartedAt   time.Time     `json:"started_at"`
	Duration    time.Duration `json:"duration"`
	Total       int           `json:"total"`
	Passed      int           `json:"passed"`
	Failed      int           `json:"failed"`
	PassRate    float64       `json:"pass_rate"`
	TotalTokens int64         `json:"total_tokens"`
	Results     []TrialResult `json:"results"`
}

// LoadCases reads and validates a JSON evaluation suite.
func LoadCases(path string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read eval cases: %w", err)
	}

	var cases []Case
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("failed to parse eval cases: %w", err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("eval suite contains no cases")
	}

	seen := make(map[string]struct{}, len(cases))
	for index, evalCase := range cases {
		if evalCase.ID == "" || evalCase.Prompt == "" || evalCase.Expected == "" {
			return nil, fmt.Errorf("eval case %d requires id, prompt, and expected", index+1)
		}
		if _, exists := seen[evalCase.ID]; exists {
			return nil, fmt.Errorf("duplicate eval case id %q", evalCase.ID)
		}
		seen[evalCase.ID] = struct{}{}
	}
	return cases, nil
}
