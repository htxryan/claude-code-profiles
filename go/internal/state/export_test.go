package state

// SetTestPollHook installs a callback fired on every poll attempt inside
// acquireLockWithWait. Returns a restore func the test must defer-call.
// Test-only; this file is excluded from non-test builds.
func SetTestPollHook(fn func()) func() {
	prev := testPollHook
	testPollHook = fn
	return func() { testPollHook = prev }
}

// SetTestRenamePriorToTarget swaps the prior→target AtomicRename used by
// ReconcilePendingPrior. Returns a restore func. Used to exercise the
// scratch-restore-on-restore-failure safety property without an FS-level fault.
func SetTestRenamePriorToTarget(fn func(src, dst string) error) func() {
	prev := renamePriorToTarget
	renamePriorToTarget = fn
	return func() { renamePriorToTarget = prev }
}
