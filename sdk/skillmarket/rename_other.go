//go:build !darwin && !linux

package skillmarket

import "fmt"

func renameNew(from, to string) error {
	return fmt.Errorf("skillmarket: atomic installation is supported on macOS and Linux")
}
