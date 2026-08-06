package evals

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var ignoredWorkspaceDirectories = map[string]struct{}{
	".git":         {},
	".gocache":     {},
	"eval-reports": {},
	"sessions":     {},
}

// TemporaryWorkspace is an isolated copy used by one eval trial.
type TemporaryWorkspace struct {
	path string
}

// WorkspaceTargetFactory creates a target whose tools are bound to one trial
// workspace.
type WorkspaceTargetFactory func(string) (Target, error)

// IsolatedTarget gives every Run call a copied temporary workspace.
type IsolatedTarget struct {
	Source    string
	NewTarget WorkspaceTargetFactory
}

func (target IsolatedTarget) Run(ctx context.Context, input string) (run Run, runErr error) {
	if target.NewTarget == nil {
		return Run{}, fmt.Errorf("workspace target factory is required")
	}
	workspace, err := NewTemporaryWorkspace(target.Source)
	if err != nil {
		return Run{}, err
	}
	defer func() {
		if err := workspace.Close(); err != nil && runErr == nil {
			runErr = err
		}
	}()

	trialTarget, err := target.NewTarget(workspace.Dir())
	if err != nil {
		return Run{}, err
	}
	return trialTarget.Run(ctx, input)
}

// NewTemporaryWorkspace copies a source workspace into a new temporary
// directory while excluding secrets, generated state, and repository metadata.
func NewTemporaryWorkspace(source string) (*TemporaryWorkspace, error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("resolve source workspace: %w", err)
	}
	info, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("inspect source workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source workspace must be a directory")
	}

	destination, err := os.MkdirTemp("", "atlas-eval-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary workspace: %w", err)
	}
	workspace := &TemporaryWorkspace{path: destination}
	if err := copyWorkspace(source, destination); err != nil {
		_ = workspace.Close()
		return nil, err
	}
	return workspace, nil
}

// Dir returns the root directory exposed to the trial's tools.
func (workspace *TemporaryWorkspace) Dir() string {
	if workspace == nil {
		return ""
	}
	return workspace.path
}

// Close permanently removes this temporary workspace.
func (workspace *TemporaryWorkspace) Close() error {
	if workspace == nil || workspace.path == "" {
		return nil
	}
	path, err := filepath.Abs(workspace.path)
	if err != nil {
		return fmt.Errorf("resolve temporary workspace: %w", err)
	}
	temporaryRoot, err := filepath.Abs(os.TempDir())
	if err != nil {
		return fmt.Errorf("resolve temporary directory: %w", err)
	}
	relativePath, err := filepath.Rel(temporaryRoot, path)
	if err != nil || relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(os.PathSeparator)) || !strings.HasPrefix(filepath.Base(path), "atlas-eval-") {
		return fmt.Errorf("refusing to remove invalid temporary workspace %q", path)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove temporary workspace: %w", err)
	}
	workspace.path = ""
	return nil
}

func copyWorkspace(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relativePath, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relativePath == "." {
			return nil
		}
		if shouldIgnoreWorkspaceEntry(relativePath, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		targetPath := filepath.Join(destination, relativePath)
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(targetPath, info.Mode().Perm())
		}
		return copyWorkspaceFile(path, targetPath, entry)
	})
}

func shouldIgnoreWorkspaceEntry(relativePath string, entry fs.DirEntry) bool {
	if entry.Type()&os.ModeSymlink != 0 {
		return true
	}
	if entry.Name() == ".env" {
		return true
	}
	if entry.IsDir() {
		if _, ignored := ignoredWorkspaceDirectories[entry.Name()]; ignored {
			return true
		}
	}
	return strings.EqualFold(filepath.Ext(relativePath), ".exe")
}

func copyWorkspaceFile(source, destination string, entry fs.DirEntry) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}

	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	return nil
}
