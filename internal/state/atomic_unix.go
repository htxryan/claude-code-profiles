//go:build !windows

package state

import (
	"errors"
	"syscall"
)

// isCrossDeviceErr maps the POSIX EXDEV errno to our typed sentinel. os.Rename
// returns *os.LinkError wrapping the underlying syscall error; errors.Is
// unwraps to the syscall.Errno.
func isCrossDeviceErr(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}
