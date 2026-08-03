package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"agent/internal/agent"
	"agent/internal/config"
	evals "agent/internal/eval"
	"agent/internal/tools"

	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func main() {
	casesPath := flag.String("cases", "evals/cases.json", "path to the evaluation cases")
	resultsDir := flag.String("results", "evals/results", "directory for evaluation reports")
	flag.Parse()

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		fatalf("could not load .env: %s", err)
	}
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		fatalf("missing OPENROUTER_API_KEY; add it to .env")
	}

	appConfig := config.DefaultConfig
	if model := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL")); model != "" {
		appConfig.Model = model
	}
	cases, err := evals.LoadCases(*casesPath)
	if err != nil {
		fatalf("%s", err)
	}

	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(appConfig.BaseURL),
	)
	runner := evals.Runner{
		Name:  "atlas-core",
		Model: appConfig.Model,
		Cases: cases,
		AgentFactory: func(workspace string, onEvent func(agent.Event), maxSteps int) (*agent.Agent, error) {
			trialTools, err := tools.DefaultToolsAt(workspace)
			if err != nil {
				return nil, err
			}
			trialConfig := appConfig
			if maxSteps > 0 {
				trialConfig.MaxAgentSteps = maxSteps
			}
			return agent.NewAgent(&client, nil, trialTools, trialConfig, nil, onEvent), nil
		},
		Judge: evals.NewLLMJudge(&client, appConfig),
		OnProgress: func(current, total int, evalCase evals.Case) {
			fmt.Printf("[%d/%d] %s\n", current, total, evalCase.Name)
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	summary, err := runner.Run(ctx)
	if err != nil {
		fatalf("evaluation failed: %s", err)
	}

	reportPath, err := saveSummary(*resultsDir, summary)
	if err != nil {
		fatalf("failed to save evaluation report: %s", err)
	}

	for _, result := range summary.Results {
		status := "fail"
		if result.Grade.Passed && result.AgentError == "" && result.GraderError == "" {
			status = "pass"
		}
		fmt.Printf("%-5s %-28s %.2f  %s\n", status, result.CaseID, result.Grade.Score, result.Grade.Reason)
	}
	fmt.Printf("\n%d/%d passed (%.1f%%), %d agent tokens, %s\n", summary.Passed, summary.Total, summary.PassRate*100, summary.TotalTokens, summary.Duration.Round(time.Millisecond))
	fmt.Printf("report: %s\n", reportPath)

	if summary.Failed > 0 {
		os.Exit(1)
	}
}

func saveSummary(resultsDir string, summary evals.Summary) (string, error) {
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", err
	}
	filename := fmt.Sprintf("atlas-core-%s.json", summary.StartedAt.Format("20060102-150405"))
	path := filepath.Join(resultsDir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
