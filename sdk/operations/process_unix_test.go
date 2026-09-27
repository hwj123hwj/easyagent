//go:build unix

package operations

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLocalCommandHasOwnSession(t *testing.T) {
	result, err := (LocalBashOperations{}).Run(context.Background(), RunRequest{Command: `ps -o pid=,pgid= -p $$`, Timeout: time.Second})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("run: %+v %v", result, err)
	}
	fields := strings.Fields(string(result.Output))
	if len(fields) != 2 || fields[0] != fields[1] {
		t.Fatalf("shell not process group leader: %q", result.Output)
	}
	pid, _ := strconv.Atoi(fields[0])
	if pid == os.Getpid() {
		t.Fatal("test ran in parent process")
	}
}

func TestIsolatedCommandCannotOpenControllingTTY(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "exec 3<>/dev/tty")
	isolateCommand(cmd)
	if err := cmd.Run(); err == nil {
		t.Fatal("tool inherited controlling terminal")
	}
}

func TestCancelTerminatesPipelineWithoutWaitingForChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	defer timer.Stop()
	defer cancel()
	start := time.Now()
	result, err := (LocalBashOperations{}).Run(ctx, RunRequest{Command: "sleep 10 | cat; wait"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode == 0 || time.Since(start) > 2*time.Second {
		t.Fatalf("pipeline survived cancellation: %+v elapsed=%s", result, time.Since(start))
	}
}
