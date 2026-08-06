package evals

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// LoadSuite reads and validates a suite from JSON.
func LoadSuite(path string) (Suite, error) {
	file, err := os.Open(path)
	if err != nil {
		return Suite{}, fmt.Errorf("open eval suite: %w", err)
	}
	defer file.Close()

	return DecodeSuite(file)
}

// DecodeSuite reads and validates a suite from a stream.
func DecodeSuite(reader io.Reader) (Suite, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	var suite Suite
	if err := decoder.Decode(&suite); err != nil {
		return Suite{}, fmt.Errorf("decode eval suite: %w", err)
	}
	if err := suite.Validate(); err != nil {
		return Suite{}, err
	}
	return suite, nil
}

// Validate rejects suites that cannot produce meaningful results.
func (suite Suite) Validate() error {
	if strings.TrimSpace(suite.Name) == "" {
		return fmt.Errorf("eval suite name is required")
	}
	if suite.Trials < 1 {
		return fmt.Errorf("eval suite trials must be at least 1")
	}
	if len(suite.Tasks) == 0 {
		return fmt.Errorf("eval suite must contain at least one task")
	}

	seen := make(map[string]struct{}, len(suite.Tasks))
	for index, task := range suite.Tasks {
		if strings.TrimSpace(task.Name) == "" {
			return fmt.Errorf("task %d name is required", index+1)
		}
		if _, exists := seen[task.Name]; exists {
			return fmt.Errorf("task name %q is duplicated", task.Name)
		}
		seen[task.Name] = struct{}{}
		if strings.TrimSpace(task.Input) == "" {
			return fmt.Errorf("task %q input is required", task.Name)
		}
		if len(task.Graders) == 0 {
			return fmt.Errorf("task %q must have at least one grader", task.Name)
		}
		for _, grader := range task.Graders {
			if err := validateGrader(grader); err != nil {
				return fmt.Errorf("task %q: %w", task.Name, err)
			}
		}
	}
	return nil
}
