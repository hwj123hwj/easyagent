package skillmarket

import "golang.org/x/sys/unix"

// renameNew atomically publishes a package without replacing an existing path.
func renameNew(from, to string) error {
	return unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
}
