package operations

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// LocalBashOperations executes commands on the local machine.
type LocalBashOperations struct{}

// Run executes a shell command locally using sh -c.
func (LocalBashOperations) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", req.Command)
	isolateCommand(cmd)
	cmd.WaitDelay = time.Second
	if req.WorkDir != "" {
		cmd.Dir = req.WorkDir
	}

	out, err := cmd.CombinedOutput()
	result := RunResult{
		Output:   out,
		ExitCode: 0,
	}

	if err != nil {
		// Try to extract exit code
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = -1
		} else {
			result.ExitCode = 1
		}
	}

	return result, nil
}

// LocalFileOperations performs file operations on the local filesystem.
type LocalFileOperations struct{}

// ReadFile reads a file from the local filesystem.
func (LocalFileOperations) ReadFile(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile writes data to a file on the local filesystem.
func (LocalFileOperations) WriteFile(_ context.Context, path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

// MkdirAll creates a directory and any necessary parents on the local filesystem.
func (LocalFileOperations) MkdirAll(_ context.Context, dir string, perm os.FileMode) error {
	return os.MkdirAll(dir, perm)
}

// Stat returns file info from the local filesystem.
func (LocalFileOperations) Stat(_ context.Context, path string) (FileInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, err
	}
	return osFileInfoToFileInfo(fi), nil
}

// ReadDir reads directory entries from the local filesystem.
func (LocalFileOperations) ReadDir(_ context.Context, path string) ([]DirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	result := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			// Skip entries we can't stat
			continue
		}
		result = append(result, DirEntry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return result, nil
}

// Walk walks the file tree rooted at root on the local filesystem.
func (LocalFileOperations) Walk(ctx context.Context, root string, fn WalkFunc) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fn(path, DirEntry{}, err)
		}

		entry := DirEntry{
			Name:  d.Name(),
			IsDir: d.IsDir(),
		}
		// Best-effort stat for size/modtime
		if info, statErr := d.Info(); statErr == nil {
			entry.Size = info.Size()
			entry.ModTime = info.ModTime()
		}

		return fn(path, entry, nil)
	})
}

// osFileInfoToFileInfo converts an os.FileInfo to our FileInfo type.
func osFileInfoToFileInfo(fi fs.FileInfo) FileInfo {
	return FileInfo{
		Name:    fi.Name(),
		Size:    fi.Size(),
		Mode:    fi.Mode(),
		ModTime: fi.ModTime(),
		IsDir:   fi.IsDir(),
	}
}

// NewLocalOperations creates an Operations container backed by local execution.
func NewLocalOperations() *Operations {
	return &Operations{
		Bash:  LocalBashOperations{},
		Files: LocalFileOperations{},
	}
}

// formatError wraps an error with context about the operation.
func formatError(op string, path string, err error) error {
	return fmt.Errorf("%s %s: %w", op, path, err)
}
