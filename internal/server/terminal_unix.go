//go:build !windows

package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

type terminalProcess struct {
	file    *os.File
	command *exec.Cmd
}

func startTerminal(cwd string, cols, rows uint16) (*terminalProcess, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	command := exec.Command(shell, "-il")
	command.Dir = cwd
	command.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	// The PTY receives an interactive login shell, including the user's normal prompt.
	if filepath.Base(shell) == "fish" {
		command.Args = []string{shell, "-i", "-l"}
	}
	file, err := pty.StartWithSize(command, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	return &terminalProcess{file: file, command: command}, nil
}

func (p *terminalProcess) read(data []byte) (int, error) { return p.file.Read(data) }
func (p *terminalProcess) write(data []byte) error       { _, err := p.file.Write(data); return err }
func (p *terminalProcess) resize(cols, rows uint16) error {
	return pty.Setsize(p.file, &pty.Winsize{Cols: cols, Rows: rows})
}
func (p *terminalProcess) close() {
	if foreground, err := unix.IoctlGetInt(int(p.file.Fd()), unix.TIOCGPGRP); err == nil && foreground > 0 && foreground != p.command.Process.Pid {
		_ = syscall.Kill(-foreground, syscall.SIGKILL)
	}
	_ = p.file.Close()
	// Terminate the shell's process group, including a currently running foreground job.
	_ = syscall.Kill(-p.command.Process.Pid, syscall.SIGKILL)
	_ = p.command.Wait()
}
