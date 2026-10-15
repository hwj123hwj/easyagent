// Package operations provides an abstraction layer for file and command operations.
// Tools use Operations interfaces to perform their work without knowing whether
// the target is local or remote (SSH).
package operations

import (
	"context"
	"io/fs"
	"os"
	"time"
)

// ---------- Bash operations ----------

// BashOperations abstracts command execution.
type BashOperations interface {
	// Run executes a shell command and returns its output.
	// Cancellation returns the context error together with any captured output.
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}

// RunRequest is the input for BashOperations.Run.
type RunRequest struct {
	Command string        // The shell command to execute
	Timeout time.Duration // Maximum execution time (0 = no timeout)
	WorkDir string        // Working directory ("" = default)
}

// RunResult is the output of BashOperations.Run.
type RunResult struct {
	Output   []byte // Combined stdout+stderr
	ExitCode int    // Process exit code (0 = success)
}

// ---------- File operations ----------

// FileOperations abstracts file system operations.
// All paths passed to FileOperations should already be resolved and
// validated by the tools layer (ResolvePath + IsPathSafe).
type FileOperations interface {
	// ReadFile reads the entire content of the named file.
	ReadFile(ctx context.Context, path string) ([]byte, error)

	// WriteFile writes data to the named file, creating it if needed.
	WriteFile(ctx context.Context, path string, data []byte, perm os.FileMode) error

	// MkdirAll creates the named directory and any necessary parents.
	MkdirAll(ctx context.Context, dir string, perm os.FileMode) error

	// Stat returns file status information.
	Stat(ctx context.Context, path string) (FileInfo, error)

	// ReadDir reads the named directory and returns a list of entries.
	ReadDir(ctx context.Context, path string) ([]DirEntry, error)

	// Walk walks the file tree rooted at root, calling fn for each file/directory.
	Walk(ctx context.Context, root string, fn WalkFunc) error
}

// FileInfo describes a file and is returned by Stat.
type FileInfo struct {
	Name    string    // Base name of the file
	Size    int64     // Length in bytes
	Mode    fs.FileMode // File mode bits
	ModTime time.Time // Last modification time
	IsDir   bool      // Abbreviation for Mode().IsDir()
}

// DirEntry is an entry read from a directory.
type DirEntry struct {
	Name  string // Name of the entry
	IsDir bool   // Whether the entry is a directory
	// Info returns the FileInfo for this entry.
	// For remote implementations, this may involve an additional round-trip.
	Size    int64
	ModTime time.Time
}

// WalkFunc is the type of the function called for each file/directory
// visited by Walk. If an error is returned, Walk stops.
type WalkFunc func(path string, entry DirEntry, err error) error

// ---------- Combined container ----------

// Operations holds all operation interfaces in one container.
// This is the single dependency that tools receive.
type Operations struct {
	Bash  BashOperations
	Files FileOperations
}
