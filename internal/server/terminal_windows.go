//go:build windows

package server

import "errors"

type terminalProcess struct{}

func startTerminal(string, uint16, uint16) (*terminalProcess, error) {
	return nil, errors.New("interactive terminal currently supports macOS and Linux")
}
func (*terminalProcess) read([]byte) (int, error)    { return 0, errors.New("unsupported platform") }
func (*terminalProcess) write([]byte) error          { return errors.New("unsupported platform") }
func (*terminalProcess) resize(uint16, uint16) error { return errors.New("unsupported platform") }
func (*terminalProcess) close()                      {}
