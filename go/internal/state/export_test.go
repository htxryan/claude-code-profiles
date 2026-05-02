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

// SetTestPreSpliceHook installs a callback fired just before applyRootSplice
// runs (the last mutating step of Materialize). Returns a restore func. Used
// to verify the splice-ordering invariant: state.json must already carry the
// new rootSectionFp BEFORE the live root CLAUDE.md is mutated, so a splice
// failure leaves a recoverable inconsistency rather than a state-file lag.
func SetTestPreSpliceHook(fn func()) func() {
	prev := testPreSpliceHook
	testPreSpliceHook = fn
	return func() { testPreSpliceHook = prev }
}
