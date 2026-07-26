package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	bashTimeout           = 30 * time.Second
	maxCommandOutputBytes = 64 * 1024
)

var allowedBashCommands = map[string]struct{}{
	"go test ./...":  {},
	"go vet ./...":   {},
	"go build ./...": {},
	"gofmt -w *.go":  {},
	"pwd":            {},
}

type ReadFileInput struct {
	Path string `json:"path"`
}

func ReadFile(input json.RawMessage) (string, error) {
	var args ReadFileInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid read_file input: %w", err)
	}
	if args.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	filePath, err := workspacePath(args.Path)
	if err != nil {
		return "", err
	}
	if filepath.Base(filePath) == ".env" {
		return "", fmt.Errorf("reading .env is not allowed")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

type ListFilesInput struct {
	Path string `json:"path"`
}

func ListFiles(input json.RawMessage) (string, error) {
	var args ListFilesInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid list_files input: %w", err)
	}

	dir := "."
	if args.Path != "" {
		dir = args.Path
	}
	dirPath, err := workspacePath(dir)
	if err != nil {
		return "", err
	}

	var files []string
	err = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == dirPath || filepath.Base(path) == ".env" {
			return nil
		}

		relPath, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			files = append(files, relPath+string(os.PathSeparator))
			return nil
		}
		files = append(files, relPath)
		return nil
	})
	if err != nil {
		return "", err
	}

	result, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

type EditFileInput struct {
	Path   string `json:"path"`
	OldStr string `json:"old_str"`
	NewStr string `json:"new_str"`
}

func EditFile(input json.RawMessage) (string, error) {
	var args EditFileInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid edit_file input: %w", err)
	}
	if args.Path == "" || args.OldStr == args.NewStr {
		return "", fmt.Errorf("invalid input parameters")
	}

	filePath, err := workspacePath(args.Path)
	if err != nil {
		return "", err
	}
	if filepath.Base(filePath) == ".env" {
		return "", fmt.Errorf("editing .env is not allowed")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) && args.OldStr == "" {
			return createNewFile(filePath, args.NewStr)
		}
		return "", err
	}
	if args.OldStr == "" {
		return "", fmt.Errorf("old_str may be empty only when creating a new file")
	}
	if strings.Count(string(content), args.OldStr) != 1 {
		return "", fmt.Errorf("old_str must match exactly once")
	}

	newContent := strings.Replace(string(content), args.OldStr, args.NewStr, 1)
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return "", err
	}
	return "ok", nil
}

func createNewFile(filePath, content string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	return fmt.Sprintf("successfully created file %s", filePath), nil
}

type DeleteFileInput struct {
	Path string `json:"path"`
}

func DeleteFile(input json.RawMessage) (string, error) {
	var args DeleteFileInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid delete_file input: %w", err)
	}
	if args.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	filePath, err := workspacePath(args.Path)
	if err != nil {
		return "", err
	}
	if filepath.Base(filePath) == ".env" {
		return "", fmt.Errorf("deleting .env is not allowed")
	}

	if err := os.Remove(filePath); err != nil {
		return "", err
	}
	return "ok", nil
}

type BashInput struct {
	Command string `json:"command"`
}

func Bash(input json.RawMessage) (string, error) {
	var args BashInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid bash input: %w", err)
	}
	command := strings.TrimSpace(args.Command)
	if command == "" {
		return "", fmt.Errorf("command is required")
	}
	if _, ok := allowedBashCommands[command]; !ok {
		return "", fmt.Errorf("command %q is not allowed; allowed commands: go test ./..., go vet ./..., go build ./..., gofmt -w *.go, pwd", command)
	}

	root, err := filepath.Abs(".")
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), bashTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = root
	cmd.Env = safeCommandEnvironment()

	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	var result strings.Builder
	if stdoutStr != "" {
		result.WriteString(stdoutStr)
	}
	if stderrStr != "" {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		result.WriteString("stderr: ")
		result.WriteString(stderrStr)
	}
	if stdout.truncated || stderr.truncated {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		result.WriteString("output truncated after 64 kb")
	}

	if err != nil {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		if ctx.Err() == context.DeadlineExceeded {
			result.WriteString("error: command timed out after 30 seconds")
		} else {
			result.WriteString(fmt.Sprintf("error: %s", err.Error()))
		}
	}

	return result.String(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	truncated bool
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	remaining := maxCommandOutputBytes - buffer.Len()
	if remaining <= 0 {
		buffer.truncated = true
		return len(data), nil
	}
	if len(data) > remaining {
		_, _ = buffer.Buffer.Write(data[:remaining])
		buffer.truncated = true
		return len(data), nil
	}
	return buffer.Buffer.Write(data)
}

func safeCommandEnvironment() []string {
	environment := os.Environ()
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		upperKey := strings.ToUpper(key)
		if strings.Contains(upperKey, "API_KEY") ||
			strings.Contains(upperKey, "ACCESS_KEY") ||
			strings.Contains(upperKey, "TOKEN") ||
			strings.Contains(upperKey, "SECRET") ||
			strings.Contains(upperKey, "PASSWORD") ||
			strings.Contains(upperKey, "CREDENTIAL") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func workspacePath(relativePath string) (string, error) {
	root, err := filepath.Abs(".")
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, relativePath))
	if err != nil {
		return "", err
	}

	relPath, err := filepath.Rel(root, target)
	if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path must stay within the working directory")
	}
	return target, nil
}
