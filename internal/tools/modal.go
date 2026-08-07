package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	modal "github.com/modal-labs/modal-client/go"
)

const ModalWorkspaceRoot = "/workspace"

type sandboxEntry struct {
	Name string
	Type string
	Size int64
}

type sandboxRuntime interface {
	ReadText(context.Context, string) (string, error)
	WriteText(context.Context, string, string) error
	ListFiles(context.Context, string) ([]sandboxEntry, error)
	Remove(context.Context, string) error
	Exec(context.Context, string, string) (string, int, error)
}

// ModalSandbox is one remote, disposable workspace used by an eval trial.
type ModalSandbox struct {
	sandbox *modal.Sandbox
	runtime sandboxRuntime
	root    string
}

// NewModalSandbox creates a sandbox and uploads the already-filtered trial
// workspace into /workspace.
func NewModalSandbox(ctx context.Context, client *modal.Client, app *modal.App, image *modal.Image, source string, timeout time.Duration) (*ModalSandbox, error) {
	if client == nil || app == nil || image == nil {
		return nil, fmt.Errorf("modal client, app, and image are required")
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	sandbox, err := client.Sandboxes.Create(ctx, app, image, &modal.SandboxCreateParams{
		CPU:          1,
		MemoryMiB:    1024,
		Timeout:      timeout,
		BlockNetwork: false,
	})
	if err != nil {
		return nil, fmt.Errorf("create modal sandbox: %w", err)
	}
	workspace := &ModalSandbox{
		sandbox: sandbox,
		runtime: modalRuntime{sandbox: sandbox},
		root:    ModalWorkspaceRoot,
	}
	if err := workspace.upload(ctx, source); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = workspace.Close(cleanupCtx)
		return nil, err
	}
	return workspace, nil
}

// Tools returns Atlas's normal tools with filesystem and shell operations
// bound to this sandbox. Web operations continue to use the explicit web tools.
func (workspace *ModalSandbox) Tools() []ToolDefinition {
	return sandboxTools(workspace.runtime, workspace.root)
}

// Close terminates the remote sandbox and discards its filesystem.
func (workspace *ModalSandbox) Close(ctx context.Context) error {
	if workspace == nil || workspace.sandbox == nil {
		return nil
	}
	if _, err := workspace.sandbox.Terminate(ctx, nil); err != nil {
		return fmt.Errorf("terminate modal sandbox: %w", err)
	}
	workspace.sandbox = nil
	return nil
}

func (workspace *ModalSandbox) upload(ctx context.Context, source string) error {
	source, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve staged workspace: %w", err)
	}
	if err := workspace.sandbox.Filesystem.MakeDirectory(ctx, workspace.root, nil); err != nil {
		return fmt.Errorf("create modal workspace: %w", err)
	}

	err = filepath.WalkDir(source, func(localPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relativePath, err := filepath.Rel(source, localPath)
		if err != nil {
			return err
		}
		if relativePath == "." {
			return nil
		}
		remotePath := path.Join(workspace.root, filepath.ToSlash(relativePath))
		if entry.IsDir() {
			return workspace.sandbox.Filesystem.MakeDirectory(ctx, remotePath, nil)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return workspace.sandbox.Filesystem.CopyFromLocal(ctx, localPath, remotePath, nil)
	})
	if err != nil {
		return fmt.Errorf("upload workspace to modal: %w", err)
	}
	return nil
}

func sandboxTools(runtime sandboxRuntime, root string) []ToolDefinition {
	readFile := ReadFileDefinition
	readFile.Function = func(ctx context.Context, input json.RawMessage) (string, error) {
		return sandboxReadFile(runtime, root, ctx, input)
	}
	listFiles := ListFilesDefinition
	listFiles.Function = func(ctx context.Context, input json.RawMessage) (string, error) {
		return sandboxListFiles(runtime, root, ctx, input)
	}
	editFile := EditFileDefinition
	editFile.Function = func(ctx context.Context, input json.RawMessage) (string, error) {
		return sandboxEditFile(runtime, root, ctx, input)
	}
	deleteFile := DeleteFileDefinition
	deleteFile.Function = func(ctx context.Context, input json.RawMessage) (string, error) {
		return sandboxDeleteFile(runtime, root, ctx, input)
	}
	bash := BashDefinition
	bash.Description = "Execute a bash command inside the disposable Modal sandbox. The command starts in /workspace."
	bash.Function = func(ctx context.Context, input json.RawMessage) (string, error) {
		return sandboxBash(runtime, root, ctx, input)
	}

	return []ToolDefinition{
		readFile,
		listFiles,
		editFile,
		deleteFile,
		bash,
		WebSearchDefinition,
		WebFetchDefinition,
	}
}

func sandboxReadFile(runtime sandboxRuntime, root string, ctx context.Context, input json.RawMessage) (string, error) {
	var args ReadFileInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid read_file input: %w", err)
	}
	filePath, err := sandboxPath(root, args.Path)
	if err != nil {
		return "", err
	}
	if path.Base(filePath) == ".env" {
		return "", fmt.Errorf("reading .env is not allowed")
	}
	return runtime.ReadText(ctx, filePath)
}

func sandboxListFiles(runtime sandboxRuntime, root string, ctx context.Context, input json.RawMessage) (string, error) {
	var args ListFilesInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid list_files input: %w", err)
	}
	directory := args.Path
	if directory == "" {
		directory = "."
	}
	directoryPath, err := sandboxPath(root, directory)
	if err != nil {
		return "", err
	}

	queue := []string{directoryPath}
	files := make([]string, 0)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		entries, err := runtime.ListFiles(ctx, current)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			if entry.Name == ".env" {
				continue
			}
			entryPath := path.Join(current, entry.Name)
			relativePath := strings.TrimPrefix(entryPath, directoryPath+"/")
			if entry.Type == string(modal.FileTypeDirectory) {
				files = append(files, relativePath+"/")
				queue = append(queue, entryPath)
				continue
			}
			files = append(files, relativePath)
		}
	}
	sort.Strings(files)
	result, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func sandboxEditFile(runtime sandboxRuntime, root string, ctx context.Context, input json.RawMessage) (string, error) {
	var args EditFileInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid edit_file input: %w", err)
	}
	if args.Path == "" || args.OldStr == args.NewStr {
		return "", fmt.Errorf("invalid input parameters")
	}
	filePath, err := sandboxPath(root, args.Path)
	if err != nil {
		return "", err
	}
	if path.Base(filePath) == ".env" {
		return "", fmt.Errorf("editing .env is not allowed")
	}

	content, err := runtime.ReadText(ctx, filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && args.OldStr == "" {
			if err := runtime.WriteText(ctx, args.NewStr, filePath); err != nil {
				return "", err
			}
			return fmt.Sprintf("successfully created file %s", args.Path), nil
		}
		return "", err
	}
	if args.OldStr == "" {
		return "", fmt.Errorf("old_str may be empty only when creating a new file")
	}
	if strings.Count(content, args.OldStr) != 1 {
		return "", fmt.Errorf("old_str must match exactly once")
	}
	if err := runtime.WriteText(ctx, strings.Replace(content, args.OldStr, args.NewStr, 1), filePath); err != nil {
		return "", err
	}
	return "ok", nil
}

func sandboxDeleteFile(runtime sandboxRuntime, root string, ctx context.Context, input json.RawMessage) (string, error) {
	var args DeleteFileInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid delete_file input: %w", err)
	}
	filePath, err := sandboxPath(root, args.Path)
	if err != nil {
		return "", err
	}
	if path.Base(filePath) == ".env" {
		return "", fmt.Errorf("deleting .env is not allowed")
	}
	if err := runtime.Remove(ctx, filePath); err != nil {
		return "", err
	}
	return "ok", nil
}

func sandboxBash(runtime sandboxRuntime, root string, ctx context.Context, input json.RawMessage) (string, error) {
	var args BashInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid bash input: %w", err)
	}
	command := strings.TrimSpace(args.Command)
	if command == "" {
		return "", fmt.Errorf("command is required")
	}
	result, exitCode, err := runtime.Exec(ctx, command, root)
	if err != nil {
		if result == "" {
			return fmt.Sprintf("error: %s", err), nil
		}
		return fmt.Sprintf("%s\nerror: %s", result, err), nil
	}
	if exitCode != 0 {
		return fmt.Sprintf("%s\nerror: exit status %d", result, exitCode), nil
	}
	return result, nil
}

func sandboxPath(root, relativePath string) (string, error) {
	if strings.TrimSpace(relativePath) == "" {
		return "", fmt.Errorf("path is required")
	}
	relativePath = strings.ReplaceAll(relativePath, "\\", "/")
	if path.IsAbs(relativePath) {
		return "", fmt.Errorf("path must stay within the working directory")
	}
	cleaned := path.Clean(relativePath)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path must stay within the working directory")
	}
	return path.Join(root, cleaned), nil
}

type modalRuntime struct {
	sandbox *modal.Sandbox
}

func (runtime modalRuntime) ReadText(ctx context.Context, remotePath string) (string, error) {
	content, err := runtime.sandbox.Filesystem.ReadText(ctx, remotePath, nil)
	if err != nil {
		var notFound modal.SandboxFilesystemNotFoundError
		if errors.As(err, &notFound) {
			return "", fs.ErrNotExist
		}
	}
	return content, err
}

func (runtime modalRuntime) WriteText(ctx context.Context, content, remotePath string) error {
	return runtime.sandbox.Filesystem.WriteText(ctx, content, remotePath, nil)
}

func (runtime modalRuntime) ListFiles(ctx context.Context, remotePath string) ([]sandboxEntry, error) {
	entries, err := runtime.sandbox.Filesystem.ListFiles(ctx, remotePath, nil)
	if err != nil {
		return nil, err
	}
	result := make([]sandboxEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, sandboxEntry{Name: entry.Name, Type: string(entry.Type), Size: entry.Size})
	}
	return result, nil
}

func (runtime modalRuntime) Remove(ctx context.Context, remotePath string) error {
	return runtime.sandbox.Filesystem.Remove(ctx, remotePath, nil)
}

func (runtime modalRuntime) Exec(ctx context.Context, command, workdir string) (string, int, error) {
	process, err := runtime.sandbox.Exec(ctx, []string{"bash", "-lc", command}, &modal.SandboxExecParams{
		Workdir: workdir,
		Stdout:  modal.Pipe,
		Stderr:  modal.Pipe,
	})
	if err != nil {
		return "", 0, err
	}

	output := newLimitedBuffer(maxCommandOutput)
	var copies sync.WaitGroup
	copies.Add(2)
	go func() {
		defer copies.Done()
		_, _ = io.Copy(output, process.Stdout)
	}()
	go func() {
		defer copies.Done()
		_, _ = io.Copy(output, process.Stderr)
	}()
	exitCode, waitErr := process.Wait(ctx, nil)
	copies.Wait()
	result := output.String()
	if output.Truncated() {
		result += "\n...[command output truncated]"
	}
	return result, exitCode, waitErr
}
