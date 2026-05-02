// Package drift provides drift detection (R18, R19, R20, R46), the three-way
// gate state machine (R21–R24), and the pre-commit fail-open hook entry point
// (R25, R25a). The package is the visible iteration surface: a user opens
// `.claude/`, edits a file, then runs `c3p use other` and lands at the gate.
//
// Lock discipline: detectDrift is read-only and lock-free (R43). applyGate is
// inside the locked region; the caller (D7 swap orchestration) wraps the
// detect → decide → apply pipeline in a single WithLock so the gate's
// snapshot is byte-identical to what gets applied (PR24).
package drift

import (
	"github.com/htxryan/c3p/internal/state"
)

// DriftReportSchemaVersion is the schema stamp on every DriftReport. Bumped
// only on a breaking shape change; additive optional fields don't bump.
const DriftReportSchemaVersion = 1

// DriftStatus is one drifted file's terminal status. `unrecoverable` is the
// cw6/T5 (R46) terminal for the project-root CLAUDE.md when its markers are
// missing or malformed — neither modified nor deleted captures that state
// (the file is present but structurally broken; the user must run init).
type DriftStatus string

const (
	DriftStatusModified      DriftStatus = "modified"
	DriftStatusAdded         DriftStatus = "added"
	DriftStatusDeleted       DriftStatus = "deleted"
	DriftStatusUnrecoverable DriftStatus = "unrecoverable"
)

// DriftDestination disambiguates `.claude/<path>` from the project-root
// CLAUDE.md (cw6/T5). Optional in JSON shape — legacy consumers keying on
// relPath alone keep working — but we carry it explicitly here so consumers
// (D7 status / drift commands) have a single discriminant for the entry
// origin.
type DriftDestination string

const (
	DriftDestinationClaude      DriftDestination = ".claude"
	DriftDestinationProjectRoot DriftDestination = "projectRoot"
)

// DriftEntry is one drifted file in the report. Provenance is the source-set
// granularity — the union of contributors recorded at last successful
// materialize (R20). Per-file granularity would require either re-resolving
// at drift time (expensive) or per-file contributors in state.json (storage
// explosion).
type DriftEntry struct {
	// RelPath is the path relative to .claude/ (posix), matching MergedFile.Path.
	// For projectRoot entries the relPath is the bare basename "CLAUDE.md".
	RelPath string `json:"relPath"`
	// Status is modified/added/deleted (R19) or unrecoverable (R46/markers gone).
	Status DriftStatus `json:"status"`
	// Provenance lists materialization sources from .state.json (R20). Empty
	// only when state.json is absent or schema-mismatched.
	Provenance []state.ResolvedSourceRef `json:"provenance"`
	// Destination disambiguates .claude/ entries from the project-root
	// CLAUDE.md. Defaults to ".claude"; only set explicitly for projectRoot.
	Destination DriftDestination `json:"destination,omitempty"`
	// Error is a human-readable remediation, set ONLY when Status ==
	// DriftStatusUnrecoverable. Printed verbatim by the CLI.
	Error string `json:"error,omitempty"`
}

// DriftReport is the cross-epic, load-bearing contract produced by
// DetectDrift and consumed by DecideGate / preCommitWarn / D7.
//
// Invariants (enforced by tests):
//   - Entries lex-sorted by RelPath (with Destination as stable secondary)
//   - Entries never contains a "unchanged" status (filtered)
//   - FastPathHits + SlowPathHits == ScannedFiles + (deleted count)
//   - When FingerprintOk == false → len(Entries) == 0 and counts are 0
type DriftReport struct {
	SchemaVersion int `json:"schemaVersion"`
	// Active is the active profile name from .state.json, or empty if NoActive.
	Active string `json:"active"`
	// FingerprintOk is true iff state.json was readable AND activeProfile is
	// non-null. False signals "no meaningful drift check possible".
	FingerprintOk bool `json:"fingerprintOk"`
	// Entries is the per-file drift list. Status != unchanged. R19, R20.
	Entries []DriftEntry `json:"entries"`
	// ScannedFiles is the total live files seen by the metadata walk.
	ScannedFiles int `json:"scannedFiles"`
	// FastPathHits is files matched by the metadata fast path (R18).
	FastPathHits int `json:"fastPathHits"`
	// SlowPathHits is files that required content-hash comparison or were
	// add/delete.
	SlowPathHits int `json:"slowPathHits"`
	// Warning is a non-fatal warning surfaced when state.json was unparseable
	// or schema-mismatched (R42 / S17). Nil when the state file was healthy.
	Warning *state.StateReadWarning `json:"warning,omitempty"`
}

// GateChoice is the user's three resolution options at the gate (R21, R23,
// R24) plus the implicit no-drift outcome — exposed as a distinct value so
// the orchestrator's switch covers all cases.
type GateChoice string

const (
	GateChoiceDiscard         GateChoice = "discard"
	GateChoicePersist         GateChoice = "persist"
	GateChoiceAbort           GateChoice = "abort"
	GateChoiceNoDriftProceed  GateChoice = "no-drift-proceed"
)

// GateMode is whether the current session can interactively prompt the user.
// CLI sets this from stdin TTY detection (D7).
type GateMode string

const (
	GateModeInteractive    GateMode = "interactive"
	GateModeNonInteractive GateMode = "non-interactive"
)

// GateInput is the input bundle for DecideGate. OnDriftFlag is the parsed
// value of --on-drift=<choice> from argv, propagated by D7 — when present, it
// overrides the prompt in BOTH modes.
type GateInput struct {
	Report      DriftReport
	Mode        GateMode
	OnDriftFlag GateChoice // empty string when unset
}

// GateOutcomeKind is the orchestrator's required action.
type GateOutcomeKind string

const (
	// GateOutcomeNoDrift means there is nothing to gate on; proceed.
	GateOutcomeNoDrift GateOutcomeKind = "no-drift"
	// GateOutcomeAuto means the gate resolved without prompting (flag or
	// hard-block auto-abort in non-interactive mode).
	GateOutcomeAuto GateOutcomeKind = "auto"
	// GateOutcomePrompt means the caller must prompt the user and feed the
	// answer back into ApplyGate.
	GateOutcomePrompt GateOutcomeKind = "prompt"
)

// GateOutcome is the result of DecideGate. Kind == GateOutcomePrompt means
// the caller must drive the prompt. Other kinds carry a fully-resolved
// Choice. Reason is human-readable for audit/debug logs.
type GateOutcome struct {
	Kind   GateOutcomeKind
	Choice GateChoice // empty when Kind == GateOutcomePrompt
	Reason string
}
