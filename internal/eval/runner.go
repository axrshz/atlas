package eval

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"agent/internal/agent"
)

const (
	defaultTrialTimeout = 3 * time.Minute
	defaultJudgeTimeout = 2 * time.Minute
	maxSnapshotFile     = 128 << 10
	maxSnapshotTotal    = 512 << 10
)

type AgentFactory func(workspace string, onEvent func(agent.Event), maxSteps int) (*agent.Agent, error)

type Runner struct {
	Name         string
	Model        string
	Cases        []Case
	AgentFactory AgentFactory
	Judge        Judge
	OnProgress   func(current, total int, evalCase Case)
	JudgeTimeout time.Duration
}

func (runner *Runner) Run(ctx context.Context) (Summary, error) {
	if runner.AgentFactory == nil || runner.Judge == nil {
		return Summary{}, fmt.Errorf("agent factory and judge are required")
	}
	if len(runner.Cases) == 0 {
		return Summary{}, fmt.Errorf("at least one eval case is required")
	}

	startedAt := time.Now()
	summary := Summary{Name: runner.Name, Model: runner.Model, StartedAt: startedAt}
	for index, evalCase := range runner.Cases {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if runner.OnProgress != nil {
			runner.OnProgress(index+1, len(runner.Cases), evalCase)
		}

		result := runner.runCase(ctx, evalCase)
		summary.Results = append(summary.Results, result)
		summary.TotalTokens += result.AgentResult.TotalTokens
		if result.Grade.Passed && result.AgentError == "" && result.GraderError == "" {
			summary.Passed++
		} else {
			summary.Failed++
		}
	}

	summary.Total = len(summary.Results)
	if summary.Total > 0 {
		summary.PassRate = float64(summary.Passed) / float64(summary.Total)
	}
	summary.Duration = time.Since(startedAt)
	return summary, nil
}

func (runner *Runner) runCase(ctx context.Context, evalCase Case) TrialResult {
	startedAt := time.Now()
	result := TrialResult{
		CaseID:     evalCase.ID,
		CaseName:   evalCase.Name,
		Category:   evalCase.Category,
		Difficulty: evalCase.Difficulty,
		Model:      runner.Model,
		StartedAt:  startedAt,
		Trace:      []agent.Event{},
	}

	workspace, err := os.MkdirTemp("", "atlas-eval-"+safeName(evalCase.ID)+"-")
	if err != nil {
		result.AgentError = fmt.Sprintf("failed to create trial workspace: %s", err)
		result.Grade = Grade{Passed: false, Score: 0, Reason: result.AgentError}
		result.Duration = time.Since(startedAt)
		return result
	}
	defer os.RemoveAll(workspace)

	if err := writeFixture(workspace, evalCase.Files); err != nil {
		result.AgentError = err.Error()
		result.Grade = Grade{Passed: false, Score: 0, Reason: result.AgentError}
		result.Duration = time.Since(startedAt)
		return result
	}
	result.InitialFiles, err = snapshotWorkspace(workspace)
	if err != nil {
		result.AgentError = err.Error()
		result.Grade = Grade{Passed: false, Score: 0, Reason: result.AgentError}
		result.Duration = time.Since(startedAt)
		return result
	}

	agentInstance, err := runner.AgentFactory(workspace, func(event agent.Event) {
		result.Trace = append(result.Trace, event)
	}, evalCase.MaxSteps)
	if err != nil {
		result.AgentError = fmt.Sprintf("failed to create agent: %s", err)
		result.Grade = Grade{Passed: false, Score: 0, Reason: result.AgentError}
		result.Duration = time.Since(startedAt)
		return result
	}

	timeout := defaultTrialTimeout
	if evalCase.TimeoutSeconds > 0 {
		timeout = time.Duration(evalCase.TimeoutSeconds) * time.Second
	}
	trialCtx, cancelTrial := context.WithTimeout(ctx, timeout)
	result.AgentResult, err = agentInstance.RunTask(trialCtx, evalCase.Prompt)
	cancelTrial()
	if err != nil {
		result.AgentError = err.Error()
	}

	result.FinalFiles, err = snapshotWorkspace(workspace)
	if err != nil {
		if result.AgentError == "" {
			result.AgentError = err.Error()
		}
	} else {
		result.WorkspaceDiff = diffSnapshots(result.InitialFiles, result.FinalFiles)
	}

	if result.AgentError != "" {
		result.Grade = Grade{Passed: false, Score: 0, Reason: result.AgentError}
		result.Duration = time.Since(startedAt)
		return result
	}

	judgeTimeout := runner.JudgeTimeout
	if judgeTimeout <= 0 {
		judgeTimeout = defaultJudgeTimeout
	}
	judgeCtx, cancelJudge := context.WithTimeout(ctx, judgeTimeout)
	result.Grade, err = runner.Judge.Grade(judgeCtx, evalCase, result.AgentResult, result.Trace, result.FinalFiles, result.WorkspaceDiff)
	cancelJudge()
	if err != nil {
		result.GraderError = err.Error()
		result.Grade = Grade{Passed: false, Score: 0, Reason: result.GraderError}
	}
	result.Duration = time.Since(startedAt)
	return result
}

func writeFixture(root string, files map[string]string) error {
	for relativePath, content := range files {
		target, err := safeJoin(root, relativePath)
		if err != nil {
			return fmt.Errorf("invalid fixture path %q: %w", relativePath, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("failed to create fixture directory: %w", err)
		}
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write fixture file: %w", err)
		}
	}
	return nil
}

func snapshotWorkspace(root string) (map[string]string, error) {
	files := make(map[string]string)
	totalBytes := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(data) > maxSnapshotFile || totalBytes+len(data) > maxSnapshotTotal || !utf8.Valid(data) {
			return nil
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relativePath)] = string(data)
		totalBytes += len(data)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to capture workspace: %w", err)
	}
	return files, nil
}

func diffSnapshots(before, after map[string]string) WorkspaceDiff {
	diff := WorkspaceDiff{}
	for path, beforeContent := range before {
		afterContent, exists := after[path]
		if !exists {
			diff.Deleted = append(diff.Deleted, path)
		} else if afterContent != beforeContent {
			diff.Modified = append(diff.Modified, path)
		}
	}
	for path := range after {
		if _, exists := before[path]; !exists {
			diff.Added = append(diff.Added, path)
		}
	}
	sort.Strings(diff.Added)
	sort.Strings(diff.Modified)
	sort.Strings(diff.Deleted)
	return diff
}

func safeJoin(root, relativePath string) (string, error) {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return "", fmt.Errorf("path must be relative")
	}
	target := filepath.Join(root, filepath.FromSlash(relativePath))
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path must stay within the workspace")
	}
	return target, nil
}

func safeName(value string) string {
	value = strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' {
			return character
		}
		return '-'
	}, value)
	return strings.Trim(value, "-")
}
