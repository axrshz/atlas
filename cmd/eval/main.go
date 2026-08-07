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

	modal "github.com/modal-labs/modal-client/go"
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
	suitePath := flag.String("suite", "evals/capabilities.json", "path to an eval suite")
	reportPath := flag.String("report", "", "optional path for the json report")
	trialTimeout := flag.Duration("timeout", 10*time.Minute, "timeout for each trial")
	trials := flag.Int("trials", 0, "override the suite trial count")
	modalAppName := flag.String("modal-app", "atlas-evals", "modal app used for eval sandboxes")
	modalImage := flag.String("modal-image", "golang:1.26-bookworm", "container image used for eval sandboxes")
	flag.Parse()

	if err := config.LoadEnvironment(); err != nil {
		return err
	}
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("missing OPENROUTER_API_KEY; add it to .env")
	}
	if os.Getenv("TAVILY_API_KEY") == "" {
		return fmt.Errorf("missing TAVILY_API_KEY; add it to .env")
	}

	suite, err := evals.LoadSuite(*suitePath)
	if err != nil {
		return err
	}
	if *trials > 0 {
		suite.Trials = *trials
	}

	appConfig := config.DefaultConfig
	openAIClient := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(appConfig.BaseURL),
	)
	modalClient, err := modal.NewClient()
	if err != nil {
		return fmt.Errorf("create modal client: %w", err)
	}
	defer modalClient.Close()
	modalApp, err := modalClient.Apps.FromName(context.Background(), *modalAppName, &modal.AppFromNameParams{
		CreateIfMissing: true,
	})
	if err != nil {
		return fmt.Errorf("find modal app: %w", err)
	}
	image := modalClient.Images.FromRegistry(*modalImage, nil)

	sourceWorkspace, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("find source workspace: %w", err)
	}
	judge := evals.LLMJudge{Client: &openAIClient, Model: appConfig.Model}
	target := evals.TargetFunc(func(ctx context.Context, input string) (run evals.Run, runErr error) {
		stagingWorkspace, err := evals.NewTemporaryWorkspace(sourceWorkspace)
		if err != nil {
			return evals.Run{}, err
		}
		defer func() {
			if err := stagingWorkspace.Close(); err != nil && runErr == nil {
				runErr = err
			}
		}()

		modalWorkspace, err := tools.NewModalSandbox(ctx, modalClient, modalApp, image, stagingWorkspace.Dir(), *trialTimeout)
		if err != nil {
			return evals.Run{}, err
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := modalWorkspace.Close(cleanupCtx); err != nil && runErr == nil {
				runErr = err
			}
		}()

		chatAgent := agent.NewAgent(&openAIClient, nil, modalWorkspace.Tools(), appConfig, nil, nil)
		result, err := chatAgent.RunTask(ctx, input)
		if err != nil {
			return evals.Run{}, err
		}
		run = evals.Run{Output: result.Output, ToolCalls: make([]evals.ToolCall, 0, len(result.ToolCalls))}
		for _, toolCall := range result.ToolCalls {
			run.ToolCalls = append(run.ToolCalls, evals.ToolCall{
				Name:      toolCall.Name,
				Arguments: string(toolCall.Arguments),
				Result:    toolCall.Result,
			})
		}
		return run, nil
	})

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
