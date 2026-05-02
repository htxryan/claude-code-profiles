package drift_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/htxryan/c3p/internal/drift"
)

// PR24: drift snapshot immutability across the gate.
//
// The contract: the DriftReport built at "display time" (just before the user
// is prompted) must equal the DriftReport that the gate's choice operates on
// at "apply time". The orchestrator MUST NOT re-detect drift between display
// and apply — silent re-detection would let a parallel editor race the prompt
// and make the user's answer apply to a different report than what they saw.
//
// We don't implement drift caching ourselves: in this codebase, the
// orchestrator is D7 (CLI). What we DO own here is the unit-level guarantee
// that a single DriftReport instance, as produced by DetectDrift, is a
// stable, copy-friendly snapshot — modifying disk after capture must NOT
// retroactively change the report value.
//
// This test pins the property: capture report → mutate disk → assert report
// has not silently shifted. If a future refactor makes DriftReport hold
// pointers/file-handles that lazily resolve, this test breaks.
func TestPR24_DriftReport_IsImmutableAfterCapture(t *testing.T) {
	t.Parallel()
	paths, _, _ := materializeBaseTree(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("CHANGED\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r1, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(r1.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(r1.Entries))
	}

	// Snapshot the value (deep-copy via re-marshalling the entries slice).
	snapshot := drift.DriftReport{
		SchemaVersion: r1.SchemaVersion,
		Active:        r1.Active,
		FingerprintOk: r1.FingerprintOk,
		Entries:       append([]drift.DriftEntry(nil), r1.Entries...),
		ScannedFiles:  r1.ScannedFiles,
		FastPathHits:  r1.FastPathHits,
		SlowPathHits:  r1.SlowPathHits,
		Warning:       r1.Warning,
	}

	// Mutate the live tree AFTER capture. If r1 secretly holds a lazy
	// reference, the next read of r1.Entries would change.
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("DOUBLY-CHANGED\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// The captured value MUST be unchanged.
	if !reflect.DeepEqual(snapshot, r1) {
		t.Errorf("DriftReport mutated after disk change\n got: %+v\nwant: %+v", r1, snapshot)
	}
}

// PR24: re-detection between display and apply MUST NOT happen silently
// inside ApplyGate. We verify that ApplyGate accepts the choice given to it
// and operates on whatever the live state is at lock-acquire time (via the
// state package primitives) — but does not internally re-call DetectDrift.
//
// This is a property test: if ApplyGate did call DetectDrift internally, then
// passing a "stale" GateChoice (e.g. a discard choice that was decided when
// drift existed, even though no drift exists at apply time) would behave
// differently. We assert that ApplyGate honors the caller's choice
// regardless of the current drift state.
func TestPR24_ApplyGate_DoesNotReDetectInternally(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	// At this point: leaf is materialized, no drift. The orchestrator's
	// "display time" decision was discard (user picked it). ApplyGate must
	// proceed with discard rather than silently downgrade to no-drift-proceed.
	res, err := drift.ApplyGate(drift.GateChoiceDiscard, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	// The discard branch took a snapshot — even though there's nothing
	// "drifted" right now. That's the immutability contract.
	// Specifically: SnapshotForDiscard always runs on the discard branch.
	// Our snapshot path returns "" iff .claude/ doesn't exist; here it does,
	// so the snapshot is non-empty.
	if res.BackupSnapshot == "" {
		t.Errorf("backupSnapshot empty; ApplyGate(discard) must always run SnapshotForDiscard")
	}
	// And the materialize succeeded (live tree is the new profile).
	live, err := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(live) != "OTHER\n" {
		t.Errorf("live = %q, want OTHER\\n", live)
	}
}

// PR24 supporting evidence: DetectDrift on identical inputs is deterministic.
// If DetectDrift is non-deterministic (e.g. depends on goroutine scheduling),
// the orchestrator can't safely "use the same report" — every read might
// produce a slightly different one even without disk changes.
func TestPR24_DetectDriftIsDeterministicOnFixedInputs(t *testing.T) {
	t.Parallel()
	paths, _, _ := materializeBaseTree(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("DRIFT\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r1, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift 1: %v", err)
	}
	r2, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift 2: %v", err)
	}
	if !reflect.DeepEqual(r1, r2) {
		t.Errorf("DetectDrift not deterministic\nfirst:  %+v\nsecond: %+v", r1, r2)
	}
}

