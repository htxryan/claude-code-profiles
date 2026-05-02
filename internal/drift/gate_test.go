package drift_test

import (
	"strings"
	"testing"

	"github.com/htxryan/c3p/internal/drift"
)

func cleanReport() drift.DriftReport {
	leaf := "leaf"
	return drift.DriftReport{
		SchemaVersion: drift.DriftReportSchemaVersion,
		Active:        &leaf,
		FingerprintOk: true,
		Entries:       []drift.DriftEntry{},
		ScannedFiles:  5,
		FastPathHits:  5,
		SlowPathHits:  0,
	}
}

func driftedReport() drift.DriftReport {
	leaf := "leaf"
	return drift.DriftReport{
		SchemaVersion: drift.DriftReportSchemaVersion,
		Active:        &leaf,
		FingerprintOk: true,
		Entries: []drift.DriftEntry{
			{RelPath: "CLAUDE.md", Status: drift.DriftStatusModified},
		},
		ScannedFiles: 5,
		FastPathHits: 4,
		SlowPathHits: 1,
	}
}

func noActiveReport() drift.DriftReport {
	return drift.DriftReport{
		SchemaVersion: drift.DriftReportSchemaVersion,
		Active:        nil,
		FingerprintOk: false,
		Entries:       []drift.DriftEntry{},
	}
}

// unrecoverableReport is a drift report whose only entry is the section-
// markers-broken terminal state for project-root CLAUDE.md (R46). It
// counts as "drift exists" for the gate (FingerprintOk:true, len>0), so
// the standard hard-block invariants apply.
func unrecoverableReport() drift.DriftReport {
	leaf := "leaf"
	return drift.DriftReport{
		SchemaVersion: drift.DriftReportSchemaVersion,
		Active:        &leaf,
		FingerprintOk: true,
		Entries: []drift.DriftEntry{
			{
				RelPath:     "CLAUDE.md",
				Status:      drift.DriftStatusUnrecoverable,
				Destination: drift.DriftDestinationProjectRoot,
				Error:       "markers missing — run init",
			},
		},
		ScannedFiles: 0,
		FastPathHits: 0,
		SlowPathHits: 0,
	}
}

func TestDecideGate_NoDriftWhenClean(t *testing.T) {
	t.Parallel()
	out := drift.DecideGate(drift.GateInput{Report: cleanReport(), Mode: drift.GateModeInteractive})
	if out.Kind != drift.GateOutcomeNoDrift {
		t.Fatalf("kind = %q, want %q", out.Kind, drift.GateOutcomeNoDrift)
	}
	if out.Choice != drift.GateChoiceNoDriftProceed {
		t.Fatalf("choice = %q, want %q", out.Choice, drift.GateChoiceNoDriftProceed)
	}
}

func TestDecideGate_NoDriftWhenFingerprintNotOk(t *testing.T) {
	t.Parallel()
	out := drift.DecideGate(drift.GateInput{Report: noActiveReport(), Mode: drift.GateModeInteractive})
	if out.Kind != drift.GateOutcomeNoDrift {
		t.Fatalf("kind = %q, want %q", out.Kind, drift.GateOutcomeNoDrift)
	}
	if out.Choice != drift.GateChoiceNoDriftProceed {
		t.Fatalf("choice = %q, want %q", out.Choice, drift.GateChoiceNoDriftProceed)
	}
}

// R21 invariant: interactive + drift + no flag → prompt.
func TestDecideGate_R21_InteractiveDriftNoFlagPrompts(t *testing.T) {
	t.Parallel()
	out := drift.DecideGate(drift.GateInput{Report: driftedReport(), Mode: drift.GateModeInteractive})
	if out.Kind != drift.GateOutcomePrompt {
		t.Fatalf("kind = %q, want %q", out.Kind, drift.GateOutcomePrompt)
	}
	if out.Choice != "" {
		t.Fatalf("choice should be empty for prompt; got %q", out.Choice)
	}
}

// Hard-block invariant: non-interactive + drift + no flag → auto abort.
func TestDecideGate_HardBlock_NonInteractiveAutoAborts(t *testing.T) {
	t.Parallel()
	out := drift.DecideGate(drift.GateInput{Report: driftedReport(), Mode: drift.GateModeNonInteractive})
	if out.Kind != drift.GateOutcomeAuto {
		t.Fatalf("kind = %q, want %q", out.Kind, drift.GateOutcomeAuto)
	}
	if out.Choice != drift.GateChoiceAbort {
		t.Fatalf("choice = %q, want %q", out.Choice, drift.GateChoiceAbort)
	}
	if !strings.Contains(out.Reason, "non-interactive") {
		t.Fatalf("reason = %q, want substring 'non-interactive'", out.Reason)
	}
}

// Flag wins over interactive prompt.
func TestDecideGate_FlagBeatsPromptInteractive(t *testing.T) {
	t.Parallel()
	for _, flag := range []drift.GateChoice{drift.GateChoiceDiscard, drift.GateChoicePersist, drift.GateChoiceAbort} {
		out := drift.DecideGate(drift.GateInput{
			Report:      driftedReport(),
			Mode:        drift.GateModeInteractive,
			OnDriftFlag: flag,
		})
		if out.Kind != drift.GateOutcomeAuto {
			t.Errorf("flag=%q: kind = %q, want auto", flag, out.Kind)
		}
		if out.Choice != flag {
			t.Errorf("flag=%q: choice = %q, want %q", flag, out.Choice, flag)
		}
	}
}

// Flag honored in non-interactive mode.
func TestDecideGate_FlagHonoredNonInteractive(t *testing.T) {
	t.Parallel()
	for _, flag := range []drift.GateChoice{drift.GateChoiceDiscard, drift.GateChoicePersist, drift.GateChoiceAbort} {
		out := drift.DecideGate(drift.GateInput{
			Report:      driftedReport(),
			Mode:        drift.GateModeNonInteractive,
			OnDriftFlag: flag,
		})
		if out.Kind != drift.GateOutcomeAuto {
			t.Errorf("flag=%q: kind = %q, want auto", flag, out.Kind)
		}
		if out.Choice != flag {
			t.Errorf("flag=%q: choice = %q, want %q", flag, out.Choice, flag)
		}
	}
}

// Epic invariant: non-interactive never returns prompt across all combos
// — including the unrecoverable-section terminal state (R46).
func TestDecideGate_NonInteractiveNeverPrompts(t *testing.T) {
	t.Parallel()
	flags := []drift.GateChoice{"", drift.GateChoiceDiscard, drift.GateChoicePersist, drift.GateChoiceAbort}
	reports := []drift.DriftReport{cleanReport(), driftedReport(), noActiveReport(), unrecoverableReport()}
	for _, f := range flags {
		for _, r := range reports {
			out := drift.DecideGate(drift.GateInput{
				Report:      r,
				Mode:        drift.GateModeNonInteractive,
				OnDriftFlag: f,
			})
			if out.Kind == drift.GateOutcomePrompt {
				t.Errorf("non-interactive returned prompt with flag=%q report=%+v", f, r)
			}
		}
	}
}

// All-unrecoverable entries are NOT special-cased at the gate. The gate
// doesn't know the target plan: a switch to a profile with no projectRoot
// contributor can legitimately recover from missing markers via Materialize's
// preflightEmptyRootSplice opt-out path. So the unrecoverable-only report
// flows through the normal decision table — interactive without a flag still
// prompts, the flag is honored, non-interactive auto-aborts. If markers are
// gone AND the new plan needs them, Materialize's R45 preflight refuses;
// gating doesn't need to duplicate that check.
func TestDecideGate_AllUnrecoverableFlowsNormalPath(t *testing.T) {
	t.Parallel()
	// Interactive + no flag → prompt (let the user choose; Materialize will
	// surface a clear error on apply if the new plan needed the markers).
	out := drift.DecideGate(drift.GateInput{
		Report: unrecoverableReport(),
		Mode:   drift.GateModeInteractive,
	})
	if out.Kind != drift.GateOutcomePrompt {
		t.Errorf("interactive no-flag: kind = %q, want prompt", out.Kind)
	}
	// Non-interactive + no flag → auto abort (the regular hard-block).
	out = drift.DecideGate(drift.GateInput{
		Report: unrecoverableReport(),
		Mode:   drift.GateModeNonInteractive,
	})
	if out.Kind != drift.GateOutcomeAuto || out.Choice != drift.GateChoiceAbort {
		t.Errorf("non-interactive no-flag: kind=%q choice=%q, want auto/abort", out.Kind, out.Choice)
	}
	// Flag is honored across both modes — including discard/persist (the
	// caller authorized the choice; Materialize gets to decide if it can land).
	for _, mode := range []drift.GateMode{drift.GateModeInteractive, drift.GateModeNonInteractive} {
		for _, flag := range []drift.GateChoice{drift.GateChoiceDiscard, drift.GateChoicePersist, drift.GateChoiceAbort} {
			out := drift.DecideGate(drift.GateInput{
				Report:      unrecoverableReport(),
				Mode:        mode,
				OnDriftFlag: flag,
			})
			if out.Kind != drift.GateOutcomeAuto {
				t.Errorf("mode=%q flag=%q: kind = %q, want auto", mode, flag, out.Kind)
			}
			if out.Choice != flag {
				t.Errorf("mode=%q flag=%q: choice = %q, want %q", mode, flag, out.Choice, flag)
			}
		}
	}
}

// Mixed entries (one unrecoverable + one regular) flow through the normal
// path. This is symmetric with the all-unrecoverable case now that the gate
// no longer special-cases the section-markers-missing terminal — the gate is
// plan-agnostic and Materialize's preflight is the source of truth for
// "can this resolution actually land".
func TestDecideGate_MixedEntriesPrompt(t *testing.T) {
	t.Parallel()
	leaf := "leaf"
	mixed := drift.DriftReport{
		SchemaVersion: drift.DriftReportSchemaVersion,
		Active:        &leaf,
		FingerprintOk: true,
		Entries: []drift.DriftEntry{
			{RelPath: "CLAUDE.md", Status: drift.DriftStatusUnrecoverable, Destination: drift.DriftDestinationProjectRoot},
			{RelPath: "agents/a.md", Status: drift.DriftStatusModified},
		},
	}
	out := drift.DecideGate(drift.GateInput{Report: mixed, Mode: drift.GateModeInteractive})
	if out.Kind != drift.GateOutcomePrompt {
		t.Errorf("kind = %q, want prompt (mixed drift should reach prompt)", out.Kind)
	}
}

// HIGH #2 defense-in-depth: passing GateChoiceNoDriftProceed as the
// --on-drift flag with drift entries present must NOT silently dispatch
// to the no-snapshot materialize path. DecideGate downgrades the bogus
// flag to abort.
func TestDecideGate_RejectsNoDriftProceedAsFlag(t *testing.T) {
	t.Parallel()
	out := drift.DecideGate(drift.GateInput{
		Report:      driftedReport(),
		Mode:        drift.GateModeInteractive,
		OnDriftFlag: drift.GateChoiceNoDriftProceed,
	})
	if out.Kind != drift.GateOutcomeAuto {
		t.Errorf("kind = %q, want auto", out.Kind)
	}
	if out.Choice != drift.GateChoiceAbort {
		t.Errorf("choice = %q, want abort (no-drift-proceed must not be honored as a flag)", out.Choice)
	}
}

func TestDecideGate_Reason_NotEmpty(t *testing.T) {
	t.Parallel()
	cases := []drift.GateInput{
		{Report: cleanReport(), Mode: drift.GateModeInteractive},
		{Report: driftedReport(), Mode: drift.GateModeInteractive},
		{Report: driftedReport(), Mode: drift.GateModeNonInteractive},
		{Report: driftedReport(), Mode: drift.GateModeInteractive, OnDriftFlag: drift.GateChoicePersist},
	}
	for _, c := range cases {
		out := drift.DecideGate(c)
		if out.Reason == "" {
			t.Errorf("Reason empty for input %+v", c)
		}
	}
}
