package state

// SetTestPollHook installs a callback fired on every poll attempt inside
// acquireLockWithWait. Returns a restore func the test must defer-call.
// Test-only; this file is excluded from non-test builds.
func SetTestPollHook(fn func()) func() {
	prev := testPollHook
	testPollHook = fn
	return func() { testPollHook = prev }
}
