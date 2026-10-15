package operations

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// SSHConfig holds the configuration for SSH connections.
type SSHConfig struct {
	Host    string // user@host
	Port    int    // SSH port (default 22)
	WorkDir string // Remote working directory
}

// SSHBashOperations executes commands on a remote machine via SSH.
type SSHBashOperations struct {
	config SSHConfig
}

// Run executes a shell command on the remote machine via ssh.
func (s SSHBashOperations) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	args := s.sshArgs()

	// Build the remote command
	// Use req.WorkDir if provided, otherwise fall back to config.WorkDir
	workDir := req.WorkDir
	if workDir == "" {
		workDir = s.config.WorkDir
	}
	remoteCmd := req.Command
	if workDir != "" {
		remoteCmd = fmt.Sprintf("cd %s && %s", shellQuote(workDir), remoteCmd)
	}
	args = append(args, remoteCmd)

	cmd := exec.CommandContext(ctx, "ssh", args...)
	isolateCommand(cmd)
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()

	result := RunResult{
		Output:   out,
		ExitCode: 0,
	}

	if err != nil && ctx.Err() != nil {
		result.ExitCode = -1
		return result, ctx.Err()
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
	}

	return result, nil
}

// SSHFileOperations performs file operations on a remote machine via SSH.
type SSHFileOperations struct {
	config SSHConfig
	bash   SSHBashOperations
}

// ReadFile reads a remote file via ssh cat.
func (s SSHFileOperations) ReadFile(ctx context.Context, path string) ([]byte, error) {
	cmd := fmt.Sprintf("cat %s", shellQuote(path))
	result, err := s.bash.Run(ctx, RunRequest{Command: cmd})
	if err != nil {
		return nil, fmt.Errorf("read remote file %s: %w", path, err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("read remote file %s failed (exit %d): %s", path, result.ExitCode, string(result.Output))
	}
	return result.Output, nil
}

// WriteFile writes data to a remote file via ssh + stdin cat.
func (s SSHFileOperations) WriteFile(ctx context.Context, path string, data []byte, perm os.FileMode) error {
	// Use ssh with heredoc-style stdin to write file
	// Build the remote mkdir + cat command
	dirArgs := s.sshArgs()
	mkdirCmd := fmt.Sprintf("mkdir -p %s && cat > %s", shellQuote(parentDirSSH(path)), shellQuote(path))
	dirArgs = append(dirArgs, mkdirCmd)

	cmd := exec.CommandContext(ctx, "ssh", dirArgs...)
	cmd.Stdin = strings.NewReader(string(data))

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("write remote file %s: %w\n%s", path, err, string(out))
	}

	// Set permissions via chmod
	if perm != 0 {
		chmodArgs := s.sshArgs()
		chmodArgs = append(chmodArgs, fmt.Sprintf("chmod %s %s", fmt.Sprintf("%o", perm), shellQuote(path)))
		chmodCmd := exec.CommandContext(ctx, "ssh", chmodArgs...)
		chmodCmd.CombinedOutput() // best-effort
	}

	return nil
}

// MkdirAll creates remote directories via ssh mkdir -p.
func (s SSHFileOperations) MkdirAll(ctx context.Context, dir string, _ os.FileMode) error {
	args := s.sshArgs()
	args = append(args, fmt.Sprintf("mkdir -p %s", shellQuote(dir)))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mkdir remote %s: %w\n%s", dir, err, string(out))
	}
	return nil
}

// Stat returns file info from the remote machine via ssh stat.
func (s SSHFileOperations) Stat(ctx context.Context, path string) (FileInfo, error) {
	// Use stat to get file info: format = "size|mtime|isDir"
	statFormat := `stat -f '%z|%m|%F' ` + shellQuote(path) + ` 2>/dev/null || stat -c '%s|%Y|%F' ` + shellQuote(path)
	result, err := s.bash.Run(ctx, RunRequest{Command: statFormat})
	if err != nil {
		return FileInfo{}, fmt.Errorf("stat remote %s: %w", path, err)
	}
	if result.ExitCode != 0 {
		return FileInfo{}, fmt.Errorf("stat remote %s failed (exit %d): %s", path, result.ExitCode, string(result.Output))
	}

	output := strings.TrimSpace(string(result.Output))
	return parseStatOutput(output, path)
}

// ReadDir reads directory entries from the remote machine via ssh ls.
func (s SSHFileOperations) ReadDir(ctx context.Context, path string) ([]DirEntry, error) {
	// Use ls -la to get directory listing
	cmd := fmt.Sprintf("ls -la --time-style=full-iso %s 2>/dev/null || ls -la -T %s", shellQuote(path), shellQuote(path))
	result, err := s.bash.Run(ctx, RunRequest{Command: cmd})
	if err != nil {
		return nil, fmt.Errorf("read remote dir %s: %w", path, err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("read remote dir %s failed (exit %d): %s", path, result.ExitCode, string(result.Output))
	}

	return parseLsOutput(string(result.Output))
}

// Walk walks the file tree on the remote machine via ssh find.
func (s SSHFileOperations) Walk(ctx context.Context, root string, fn WalkFunc) error {
	// Use find to list all files/dirs with type info
	cmd := fmt.Sprintf("find %s -exec stat -f '%%z|%%m|%%F|%%N' {} \\; 2>/dev/null || find %s -exec stat -c '%%s|%%Y|%%F|%%n' {} \\;", shellQuote(root), shellQuote(root))
	result, err := s.bash.Run(ctx, RunRequest{Command: cmd, Timeout: 30 * time.Second})
	if err != nil {
		return fmt.Errorf("walk remote %s: %w", root, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("walk remote %s failed (exit %d): %s", root, result.ExitCode, string(result.Output))
	}

	lines := strings.Split(strings.TrimSpace(string(result.Output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		info, path, err := parseWalkLine(line)
		if err != nil {
			if walkErr := fn(path, DirEntry{}, err); walkErr != nil {
				return walkErr
			}
			continue
		}
		if walkErr := fn(path, info, nil); walkErr != nil {
			return walkErr
		}
	}
	return nil
}

// sshArgs builds the common SSH arguments.
func (s SSHFileOperations) sshArgs() []string {
	return buildSSHArgs(s.config)
}

// sshArgs builds the common SSH arguments.
func (s SSHBashOperations) sshArgs() []string {
	return buildSSHArgs(s.config)
}

func buildSSHArgs(config SSHConfig) []string {
	args := []string{}
	if config.Port != 0 && config.Port != 22 {
		args = append(args, "-p", strconv.Itoa(config.Port))
	}
	if config.Host != "" {
		args = append(args, config.Host)
	}
	args = append(args, "--")
	return args
}

// NewSSHOperations creates an Operations container backed by SSH execution.
func NewSSHOperations(config SSHConfig) *Operations {
	bash := SSHBashOperations{config: config}
	return &Operations{
		Bash:  bash,
		Files: SSHFileOperations{config: config, bash: bash},
	}
}

// ---------- SSH Helper functions ----------

// shellQuote wraps a string in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	// Replace ' with '\'' (end quote, escaped quote, start quote)
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// parentDirSSH extracts the parent directory from a path.
func parentDirSSH(path string) string {
	if idx := strings.LastIndex(path, "/"); idx > 0 {
		return path[:idx]
	}
	return "/"
}

// parseStatOutput parses "size|mtime|type" format from stat command.
func parseStatOutput(output string, name string) (FileInfo, error) {
	parts := strings.SplitN(output, "|", 3)
	if len(parts) < 3 {
		return FileInfo{}, fmt.Errorf("unexpected stat output: %s", output)
	}

	size := int64(0)
	fmt.Sscanf(parts[0], "%d", &size)

	mtime := time.Time{}
	// Try common time formats
	for _, format := range []string{
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05",
		time.RFC3339,
	} {
		if t, err := time.Parse(format, strings.TrimSpace(parts[1])); err == nil {
			mtime = t
			break
		}
	}

	isDir := strings.Contains(parts[2], "directory")

	return FileInfo{
		Name:    name,
		Size:    size,
		ModTime: mtime,
		IsDir:   isDir,
	}, nil
}

// parseLsOutput parses ls -la output into DirEntry slice.
func parseLsOutput(output string) ([]DirEntry, error) {
	var entries []DirEntry
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "total") {
			continue
		}

		// Parse ls -la line: perms links owner group size date time name
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}

		name := strings.Join(fields[8:], " ")
		if name == "." || name == ".." {
			continue
		}

		perms := fields[0]
		isDir := strings.HasPrefix(perms, "d")

		size := int64(0)
		fmt.Sscanf(fields[4], "%d", &size)

		entries = append(entries, DirEntry{
			Name:  name,
			IsDir: isDir,
			Size:  size,
			// ModTime: best-effort, not critical for most uses
		})
	}

	return entries, nil
}

// parseWalkLine parses a single "size|mtime|type|path" line from find+stat.
func parseWalkLine(line string) (DirEntry, string, error) {
	parts := strings.SplitN(line, "|", 4)
	if len(parts) < 4 {
		return DirEntry{}, "", fmt.Errorf("unexpected walk line: %s", line)
	}

	size := int64(0)
	fmt.Sscanf(parts[0], "%d", &size)

	isDir := strings.Contains(parts[2], "directory")
	path := parts[3]

	return DirEntry{
		Name:  path,
		IsDir: isDir,
		Size:  size,
	}, path, nil
}
