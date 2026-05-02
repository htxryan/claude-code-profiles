//go:build windows

package state

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isCrossDeviceErr maps the Windows ERROR_NOT_SAME_DEVICE error to our typed
// sentinel. MoveFileExW returns this when source and dest are on different
// volumes; os.Rename surfaces it as a syscall.Errno wrapping the Windows code.
func isCrossDeviceErr(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}
