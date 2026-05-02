package commands

import (
	"fmt"

	"github.com/htxryan/c3p/internal/drift"
	"github.com/htxryan/c3p/internal/state"
)

type statusDriftSummary struct {
	FingerprintOk bool `json:"fingerprintOk"`
	Modified      int  `json:"modified"`
	Added         int  `json:"added"`
	Deleted       int  `json:"deleted"`
	Unrecoverable int  `json:"unrecoverable"`
	Total         int  `json:"total"`
}

type statusPayload struct {
	ActiveProfile     *string                  `json:"activeProfile"`
	MaterializedAt    *string                  `json:"materializedAt"`
	Drift             statusDriftSummary       `json:"drift"`
	SourceFresh       *bool                    `json:"sourceFresh"`
	SourceFingerprint *state.SourceFingerprint `json:"sourceFingerprint"`
	Warnings          []string                 `json:"warnings"`
}

// RunStatus implements the `status` verb. Read-only.
// Mirrors src/cli/commands/status.ts.
func RunStatus(opts StatusOptions) (int, error) {
	paths := state.BuildStatePaths(opts.Cwd)
	st, err := state.ReadStateFile(paths)
	if err != nil {
		return 2, err
	}

	report, err := drift.DetectDrift(paths)
	if err != nil {
		return 2, err
	}

	summary := summarizeDrift(report)
	payload := statusPayload{
		ActiveProfile:     st.State.ActiveProfile,
		MaterializedAt:    st.State.MaterializedAt,
		Drift:             summary,
		SourceFingerprint: st.State.SourceFingerprint,
		Warnings:          []string{},
	}
	// Skip the Missing warning for fresh projects: a state.json that
	// doesn't exist is the normal "init hasn't run" path, not a degraded
	// file. ParseError / SchemaMismatch still surfaces.
	if st.Warning != nil && st.Warning.Code != state.StateReadWarningMissing {
		payload.Warnings = append(payload.Warnings, fmt.Sprintf("%s: %s", st.Warning.Code, st.Warning.Detail))
	}

	if opts.Output.JSONMode() {
		opts.Output.JSON(payload)
		return 0, nil
	}

	if st.State.ActiveProfile == nil {
		opts.Output.Print("active profile: (none)")
		opts.Output.Print("Run `c3p use <name>` to select one.")
		for _, w := range payload.Warnings {
			opts.Output.Warn("warn: " + w)
		}
		return 0, nil
	}

	active := *st.State.ActiveProfile
	opts.Output.Print(fmt.Sprintf("active profile: %s", active))
	if st.State.MaterializedAt != nil {
		opts.Output.Print("materialized:   " + *st.State.MaterializedAt)
	}
	if summary.Total == 0 {
		opts.Output.Print("drift:          none")
	} else {
		opts.Output.Print(fmt.Sprintf("drift:          %d file(s) (M:%d A:%d D:%d X:%d)",
			summary.Total, summary.Modified, summary.Added, summary.Deleted, summary.Unrecoverable))
		opts.Output.Print("                run `c3p drift` for details")
	}
	for _, w := range payload.Warnings {
		opts.Output.Warn("warn: " + w)
	}
	return 0, nil
}

func summarizeDrift(r drift.DriftReport) statusDriftSummary {
	s := statusDriftSummary{FingerprintOk: r.FingerprintOk, Total: len(r.Entries)}
	for _, e := range r.Entries {
		switch e.Status {
		case drift.DriftStatusModified:
			s.Modified++
		case drift.DriftStatusAdded:
			s.Added++
		case drift.DriftStatusDeleted:
			s.Deleted++
		case drift.DriftStatusUnrecoverable:
			s.Unrecoverable++
		}
	}
	return s
}
