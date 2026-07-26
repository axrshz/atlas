package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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

	root, err := filepath.Abs(".")
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = root
	cmd.Env = os.Environ()

	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) == 0 {
			return fmt.Sprintf("error: %s", err), nil
		}
		return fmt.Sprintf("%s\nerror: %s", output, err), nil
	}
	return string(output), nil
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
