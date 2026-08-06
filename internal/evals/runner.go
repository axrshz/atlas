package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Runner executes a suite against a target.
type Runner struct {
	Target       Target
	Judge        Judge
	TrialTimeout time.Duration
	Now          func() time.Time
}

// Run executes tasks sequentially so filesystem-oriented agents do not race.
func (runner Runner) Run(ctx context.Context, suite Suite) (Report, error) {
	if runner.Target == nil {
		return Report{}, fmt.Errorf("eval target is required")
	}
	if err := suite.Validate(); err != nil {
		return Report{}, err
	}
	if suiteNeedsJudge(suite) && runner.Judge == nil {
		return Report{}, fmt.Errorf("llm judge is required by this eval suite")
	}

	now := runner.Now
	if now == nil {
		now = time.Now
	}
	report := Report{
		Suite:     suite.Name,
		Kind:      suite.Kind,
		CreatedAt: now().UTC(),
		Tasks:     make([]TaskReport, 0, len(suite.Tasks)),
	}

	for _, task := range suite.Tasks {
		taskReport := TaskReport{Name: task.Name, Trials: make([]TrialReport, 0, suite.Trials)}
		for trial := 1; trial <= suite.Trials; trial++ {
			trialReport := runner.runTrial(ctx, task, trial)
			taskReport.Trials = append(taskReport.Trials, trialReport)
			report.TotalTrials++
			if trialReport.Passed {
				taskReport.Passed++
				report.PassedTrials++
			}
		}

		taskReport.PassRate = ratio(taskReport.Passed, suite.Trials)
		taskReport.PassAtK = taskReport.Passed > 0
		taskReport.PassAllK = taskReport.Passed == suite.Trials
		report.Tasks = append(report.Tasks, taskReport)
	}

	report.PassRate = ratio(report.PassedTrials, report.TotalTrials)
	passedAtK, passedAllK := 0, 0
	for _, task := range report.Tasks {
		if task.PassAtK {
			passedAtK++
		}
		if task.PassAllK {
			passedAllK++
		}
	}
	report.PassAtK = ratio(passedAtK, len(report.Tasks))
	report.PassAllK = ratio(passedAllK, len(report.Tasks))
	return report, nil
}

func (runner Runner) runTrial(ctx context.Context, task Task, trial int) TrialReport {
	trialCtx := ctx
	cancel := func() {}
	if runner.TrialTimeout > 0 {
		trialCtx, cancel = context.WithTimeout(ctx, runner.TrialTimeout)
	}
	defer cancel()

	startedAt := time.Now()
	run, err := runner.Target.Run(trialCtx, task.Input)
	report := TrialReport{
		Trial:      trial,
		Run:        run,
		DurationMS: time.Since(startedAt).Milliseconds(),
	}
	if err != nil {
		report.Error = err.Error()
		return report
	}

	report.Passed = true
	report.Grades = make([]GradeResult, 0, len(task.Graders))
	for _, spec := range task.Graders {
		gradeResult := GradeResult{}
		if spec.Type == "llm_judge" {
			gradeResult, err = runner.Judge.Grade(trialCtx, task, spec, run)
			if err != nil {
				gradeResult = GradeResult{
					Type:   spec.Type,
					Passed: false,
					Reason: "failed: llm judge error: " + err.Error(),
				}
			}
		} else {
			gradeResult = grade(spec, run)
		}
		report.Grades = append(report.Grades, gradeResult)
		if !gradeResult.Passed {
			report.Passed = false
		}
	}
	return report
}

func suiteNeedsJudge(suite Suite) bool {
	for _, task := range suite.Tasks {
		for _, grader := range task.Graders {
			if grader.Type == "llm_judge" {
				return true
			}
		}
	}
	return false
}

// SaveReport writes a human-readable JSON report, creating parent directories.
func SaveReport(path string, report Report) error {
	if directory := filepath.Dir(path); directory != "." {
		if err := os.MkdirAll(directory, 0755); err != nil {
			return fmt.Errorf("create report directory: %w", err)
		}
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode eval report: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write eval report: %w", err)
	}
	return nil
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}
