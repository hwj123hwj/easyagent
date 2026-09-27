package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathPolicy controls file-tool access without changing path resolution or cwd.
// The zero value keeps the existing workspace restriction. It does not change
// OS permissions, shell access, or tool confirmation requirements.
type PathPolicy struct {
	AllowOutsideWorkspace bool
}

func (p PathPolicy) Allows(workspace, path string) bool {
	return p.AllowOutsideWorkspace || IsPathSafe(workspace, path)
}

// parentDir returns the directory containing the given file path.
func parentDir(path string) string {
	return filepath.Dir(path)
}

// ResolvePath resolves a possibly-relative path against a workspace root.
// If workspace is non-empty, relative paths are joined with workspace.
// Absolute paths are cleaned but returned as-is.
func ResolvePath(workspace, path string) string {
	path = filepath.Clean(path)
	if filepath.IsAbs(path) {
		return path
	}
	if workspace != "" {
		return filepath.Clean(filepath.Join(workspace, path))
	}
	// No workspace: resolve relative to cwd
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// IsPathSafe checks whether the resolved path stays within the workspace.
// If workspace is empty, it always returns true (no restriction).
// Symlinks are resolved via filepath.EvalSymlinks to prevent sandbox escape.
func IsPathSafe(workspace, path string) bool {
	if workspace == "" {
		return true
	}
	resolved := ResolvePath(workspace, path)

	absWorkspace, err := filepath.EvalSymlinks(filepath.Clean(workspace))
	if err != nil {
		// workspace doesn't exist — no symlink can exist inside it.
		// Fall back to string-based check.
		return stringCheck(resolved, workspace)
	}

	resolvedReal, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		if info, statErr := os.Lstat(resolved); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return false // A dangling symlink must not become a write escape.
		}
		// resolved doesn't exist. Walk up to the nearest existing ancestor.
		parent := resolved
		for parent != "/" && parent != "." {
			parent = filepath.Dir(parent)
			if _, err := os.Lstat(parent); err == nil {
				resolvedReal, err = filepath.EvalSymlinks(parent)
				if err != nil {
					// parent exists but is itself a broken symlink — unsafe
					return false
				}
				break
			}
		}
		if resolvedReal == "" {
			// No ancestor exists at all (e.g. /workspace doesn't exist on this
			// machine). Fall back to string check — nothing can be a symlink.
			return stringCheck(resolved, workspace)
		}
	}

	return resolvedReal == absWorkspace || strings.HasPrefix(resolvedReal, absWorkspace+string(filepath.Separator))
}

// stringCheck is the fallback: pure string prefix comparison, no symlink resolution.
func stringCheck(resolved, workspace string) bool {
	absWorkspace := filepath.Clean(workspace)
	absWorkspace += string(filepath.Separator)
	return strings.HasPrefix(resolved, absWorkspace) || resolved == filepath.Clean(workspace)
}

// EnsureDir creates the parent directory of the given path if it doesn't exist.
func EnsureDir(path string) error {
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0o755)
	}
	return nil
}

// FormatByteCount returns a human-readable byte count string.
func FormatByteCount(n int) string {
	const (
		KB = 1024
		MB = 1024 * KB
	)
	switch {
	case n >= MB:
		return fmt.Sprintf("%.1fM", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.1fK", float64(n)/float64(KB))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
