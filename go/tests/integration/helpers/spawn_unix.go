//go:build !windows

package helpers

import (
	"os"
	"syscall"
)

// signalName returns the name (e.g. "SIGTERM") of the signal that killed
// the process, or empty string if the process exited normally. Mirrors the
// TS NodeJS.Signals payload on `child.on("close", (code, signal) => …)`.
func signalName(ps *os.ProcessState) string {
	ws, ok := ps.Sys().(syscall.WaitStatus)
	if !ok {
		return ""
	}
	if !ws.Signaled() {
		return ""
	}
	switch ws.Signal() {
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGHUP:
		return "SIGHUP"
	case syscall.SIGPIPE:
		return "SIGPIPE"
	default:
		return ws.Signal().String()
	}
}
