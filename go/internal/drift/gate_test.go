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
