//go:build unix

package operations

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Tools are noninteractive. A new session prevents sudo/ssh (which can open
// /dev/tty despite nil Stdin) from sharing the TUI's controlling terminal.
func isolateCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
