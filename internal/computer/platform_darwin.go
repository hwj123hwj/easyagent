package computer

import (
	"strconv"
	"strings"
	"syscall"
)

// macOS 14 (Darwin 23) introduced the screenshot API used by the bundled helper.
func platformSupported() bool {
	release, err := syscall.Sysctl("kern.osrelease")
	if err != nil {
		return false
	}
	major, err := strconv.Atoi(strings.Split(release, ".")[0])
	return err == nil && major >= 23
}
