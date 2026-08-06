package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"atlas/internal/agent"
	"atlas/internal/config"
	"atlas/internal/evals"
	"atlas/internal/tools"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	suitePath := flag.String("suite", "evals/smoke.json", "path to an eval suite")
	reportPath := flag.String("report", "", "optional path for the json report")
	trialTimeout := flag.Duration("timeout", 5*time.Minute, "timeout for each trial")
	trials := flag.Int("trials", 0, "override the suite trial count")
	flag.Parse()

	if err := config.LoadEnvironment(); err != nil {
		return err
	}
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("missing OPENROUTER_API_KEY; add it to .env")
	}

	suite, err := evals.LoadSuite(*suitePath)
	if err != nil {
		return err
	}
	if *trials > 0 {
		suite.Trials = *trials
	}

	appConfig := config.DefaultConfig
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(appConfig.BaseURL),
	)
	sourceWorkspace, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("find source workspace: %w", err)
	}
	judge := evals.LLMJudge{Client: &client, Model: appConfig.Model}
	target := evals.IsolatedTarget{
		Source: sourceWorkspace,
		NewTarget: func(workspace string) (evals.Target, error) {
			trialTools, err := tools.DefaultToolsForWorkspace(workspace)
			if err != nil {
				return nil, err
			}
			chatAgent := agent.NewAgent(&client, nil, trialTools, appConfig, nil, nil)
			return evals.TargetFunc(func(ctx context.Context, input string) (evals.Run, error) {
				result, err := chatAgent.RunTask(ctx, input)
				if err != nil {
					return evals.Run{}, err
				}
				run := evals.Run{Output: result.Output, ToolCalls: make([]evals.ToolCall, 0, len(result.ToolCalls))}
				for _, toolCall := range result.ToolCalls {
					run.ToolCalls = append(run.ToolCalls, evals.ToolCall{
						Name:      toolCall.Name,
						Arguments: string(toolCall.Arguments),
						Result:    toolCall.Result,
					})
				}
				return run, nil
			}), nil
		},
	}

	report, err := (evals.Runner{Target: target, Judge: judge, TrialTimeout: *trialTimeout}).Run(context.Background(), suite)
	if err != nil {
		return err
	}
	printSummary(report, suite.Trials)
	if *reportPath != "" {
		if err := evals.SaveReport(*reportPath, report); err != nil {
			return err
		}
		fmt.Printf("report: %s\n", *reportPath)
	}
	return nil
}

func printSummary(report evals.Report, trials int) {
	fmt.Printf("suite: %s (%s)\n", report.Suite, report.Kind)
	for _, task := range report.Tasks {
		fmt.Printf("  %s: %d/%d passed\n", task.Name, task.Passed, len(task.Trials))
		for _, trial := range task.Trials {
			if trial.Passed {
				continue
			}
			if trial.Error != "" {
				fmt.Printf("    trial %d: %s\n", trial.Trial, trial.Error)
				continue
			}
			for _, grade := range trial.Grades {
				if !grade.Passed {
					fmt.Printf("    trial %d: %s\n", trial.Trial, grade.Reason)
				}
			}
		}
	}
	fmt.Printf("trials: %d/%d passed (%.1f%%)\n", report.PassedTrials, report.TotalTrials, report.PassRate*100)
	fmt.Printf("pass@%d: %.1f%%\n", trials, report.PassAtK*100)
	fmt.Printf("pass^%d: %.1f%%\n", trials, report.PassAllK*100)
}
