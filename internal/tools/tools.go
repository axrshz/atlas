package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const maxCommandOutput = 32 << 10

type ReadFileInput struct {
	Path string `json:"path"`
}

func ReadFile(ctx context.Context, input json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
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

func ListFiles(ctx context.Context, input json.RawMessage) (string, error) {
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
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
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

func EditFile(ctx context.Context, input json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
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

func DeleteFile(ctx context.Context, input json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
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

func Bash(ctx context.Context, input json.RawMessage) (string, error) {
	var args BashInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid bash input: %w", err)
	}
	command := strings.TrimSpace(args.Command)
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	root, err := filepath.Abs(".")
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = root
	cmd.Env = os.Environ()
	output := newLimitedBuffer(maxCommandOutput)
	cmd.Stdout = output
	cmd.Stderr = output

	err = cmd.Run()
	result := output.String()
	if output.Truncated() {
		result += "\n...[command output truncated]"
	}
	if err != nil {
		if result == "" {
			return fmt.Sprintf("error: %s", err), nil
		}
		return fmt.Sprintf("%s\nerror: %s", result, err), nil
	}
	return result, nil
}

type limitedBuffer struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{data: make([]byte, 0, limit), limit: limit}
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	written := len(data)
	remaining := buffer.limit - len(buffer.data)
	if remaining > 0 {
		if remaining > len(data) {
			remaining = len(data)
		}
		buffer.data = append(buffer.data, data[:remaining]...)
	}
	if remaining < len(data) {
		buffer.truncated = true
	}
	return written, nil
}

func (buffer *limitedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return string(buffer.data)
}

func (buffer *limitedBuffer) Truncated() bool {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.truncated
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
