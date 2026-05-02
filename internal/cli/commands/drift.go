package commands

import (
	"fmt"

	"github.com/htxryan/claude-code-config-profiles/internal/drift"
	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

type driftEntryPayload struct {
	RelPath     string                    `json:"relPath"`
	Status      drift.DriftStatus         `json:"status"`
	Provenance  []state.ResolvedSourceRef `json:"provenance"`
	Destination drift.DriftDestination    `json:"destination,omitempty"`
	Error       string                    `json:"error,omitempty"`
}

type driftPayload struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Active        *string                   `json:"active"`
	FingerprintOk bool                      `json:"fingerprintOk"`
	Entries       []driftEntryPayload       `json:"entries"`
	ScannedFiles  int                       `json:"scannedFiles"`
	FastPathHits  int                       `json:"fastPathHits"`
	SlowPathHits  int                       `json:"slowPathHits"`
	AddedBytes    int                       `json:"addedBytes"`
	RemovedBytes  int                       `json:"removedBytes"`
	ChangedBytes  int                       `json:"changedBytes"`
	Warning       *state.StateReadWarning   `json:"warning,omitempty"`
}

// RunDrift implements `c3p drift`. Read-only (R43, no lock).
// --pre-commit-warn delegates to drift.PreCommitWarn (fail-open, exit 0).
// Mirrors src/cli/commands/drift.ts.
func RunDrift(opts DriftOptions) (int, error) {
	paths := state.BuildStatePaths(opts.Cwd)

	if opts.PreCommitWarn {
		drift.PreCommitWarn(paths)
		return 0, nil
	}

	report, err := drift.DetectDrift(paths)
	if err != nil {
		return 2, err
	}

	entries := make([]driftEntryPayload, 0, len(report.Entries))
	for _, e := range report.Entries {
		entries = append(entries, driftEntryPayload{
			RelPath:     e.RelPath,
			Status:      e.Status,
			Provenance:  e.Provenance,
			Destination: e.Destination,
			Error:       e.Error,
		})
	}

	payload := driftPayload{
		SchemaVersion: report.SchemaVersion,
		Active:        report.Active,
		FingerprintOk: report.FingerprintOk,
		Entries:       entries,
		ScannedFiles:  report.ScannedFiles,
		FastPathHits:  report.FastPathHits,
		SlowPathHits:  report.SlowPathHits,
		Warning:       report.Warning,
	}

	if opts.Output.JSONMode() {
		opts.Output.JSON(payload)
		return 0, nil
	}

	if !report.FingerprintOk {
		opts.Output.Print("(no active profile — drift check skipped)")
		return 0, nil
	}
	active := ""
	if report.Active != nil {
		active = *report.Active
	}
	if len(report.Entries) == 0 {
		opts.Output.Print(fmt.Sprintf("drift: clean (active=%s, scanned=%d)", active, report.ScannedFiles))
		return 0, nil
	}
	opts.Output.Print(fmt.Sprintf("drift: %d file(s) in .claude/ vs active profile %q", len(report.Entries), active))
	limit := len(report.Entries)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		e := report.Entries[i]
		opts.Output.Print(fmt.Sprintf("  %s  %s", statusGlyph(e.Status), e.RelPath))
		if e.Error != "" {
			opts.Output.Print("       " + e.Error)
		}
	}
	if len(report.Entries) > 10 {
		opts.Output.Print(fmt.Sprintf("  ...and %d more", len(report.Entries)-10))
	}
	if opts.Verbose {
		opts.Output.Print(fmt.Sprintf("scanned=%d fast=%d slow=%d", report.ScannedFiles, report.FastPathHits, report.SlowPathHits))
	}
	return 0, nil
}

func statusGlyph(s drift.DriftStatus) string {
	switch s {
	case drift.DriftStatusModified:
		return "M"
	case drift.DriftStatusAdded:
		return "A"
	case drift.DriftStatusDeleted:
		return "D"
	case drift.DriftStatusUnrecoverable:
		return "X"
	}
	return "?"
}
