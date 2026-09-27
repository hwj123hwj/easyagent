//go:build !unix

package operations

import "os/exec"

func isolateCommand(cmd *exec.Cmd) {}
