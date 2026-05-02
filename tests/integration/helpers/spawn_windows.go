//go:build windows

package helpers

import "os"

// signalName is a Windows no-op: ProcessState.Sys() returns syscall.WaitStatus
// which has no Signal() concept. Tests that exercise SIGINT-style cleanup
// (sigint.test.ts equivalent) will need a Windows-specific path; F1 ships
// the empty surface so the helper compiles on all platforms.
func signalName(ps *os.ProcessState) string {
	return ""
}
