package drift

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/htxryan/claude-code-config-profiles/internal/markers"
	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

// DetectDrift runs drift detection on the live .claude/ tree against the
// active profile's recorded fingerprint.
//
// NoActive / schema-mismatch handling: ReadStateFile already degrades
// gracefully (R42) — when the file is unparseable, we get a default state
// back with ActiveProfile == nil. We surface that as FingerprintOk: false
// with empty entries so the gate auto-passes and the pre-commit hook stays
// silent.
//
// Read-only and lock-free (R43): may return slightly stale data during a
// concurrent write — acceptable.
func DetectDrift(paths state.StatePaths) (DriftReport, error) {
	res, err := state.ReadStateFile(paths)
	if err != nil {
		return DriftReport{}, err
	}
	st := res.State
	warning := res.Warning

	if st.ActiveProfile == nil {
		return DriftReport{
			SchemaVersion: DriftReportSchemaVersion,
			Active:        nil,
			FingerprintOk: false,
			Entries:       []DriftEntry{},
			ScannedFiles:  0,
			FastPathHits:  0,
			SlowPathHits:  0,
			Warning:       warning,
		}, nil
	}

	cmp, err := state.CompareFingerprint(paths.ClaudeDir, st.Fingerprint)
	if err != nil {
		return DriftReport{}, err
	}

	entries := make([]DriftEntry, 0, len(cmp.Entries))
	for _, e := range cmp.Entries {
		if e.Kind == state.DriftUnchanged {
			continue
		}
		// Per-entry copy of provenance defends against any future caller that
		// mutates the entry's slice (e.g. sorts or filters provenance) from
		// cross-contaminating siblings.
		prov := make([]state.ResolvedSourceRef, len(st.ResolvedSources))
		copy(prov, st.ResolvedSources)
		entries = append(entries, DriftEntry{
			RelPath:     e.RelPath,
			Status:      driftKindToStatus(e.Kind),
			Provenance:  prov,
			Destination: DriftDestinationClaude,
		})
	}

	// cw6/T5 (R46): section-only drift check for project-root CLAUDE.md.
	// Only runs when prior materialize recorded a section fingerprint —
	// legacy state files (cw6-pre) and clean .claude/-only profiles leave
	// the field nil and we correctly skip the check.
	if st.RootClaudeMdSection != nil {
		prov := make([]state.ResolvedSourceRef, len(st.ResolvedSources))
		copy(prov, st.ResolvedSources)
		sectionEntry, err := compareRootClaudeMdSection(paths, *st.RootClaudeMdSection, prov)
		if err != nil {
			return DriftReport{}, err
		}
		if sectionEntry != nil {
			entries = append(entries, *sectionEntry)
		}
	}

	// Re-sort: the .claude/ entries arrive pre-sorted, but the appended
	// projectRoot entry (always "CLAUDE.md") may need to slot into lex
	// order. Stable secondary sort on destination so a hypothetical
	// future collision (.claude/CLAUDE.md AND projectRoot CLAUDE.md
	// both drifted) renders deterministically.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].RelPath != entries[j].RelPath {
			return entries[i].RelPath < entries[j].RelPath
		}
		return entries[i].Destination < entries[j].Destination
	})

	active := *st.ActiveProfile
	return DriftReport{
		SchemaVersion: DriftReportSchemaVersion,
		Active:        &active,
		FingerprintOk: true,
		Entries:       entries,
		ScannedFiles:  cmp.Metrics.ScannedFiles,
		FastPathHits:  cmp.Metrics.FastPathHits,
		SlowPathHits:  cmp.Metrics.SlowPathHits,
		Warning:       warning,
	}, nil
}

func driftKindToStatus(k state.DriftKind) DriftStatus {
	switch k {
	case state.DriftModified:
		return DriftStatusModified
	case state.DriftAdded:
		return DriftStatusAdded
	case state.DriftDeleted:
		return DriftStatusDeleted
	}
	// Unknown kind: panic surfaces the gap at development time. A silent
	// cast would let an unmodeled status value into DriftReport.Entries,
	// which neither the CLI renderer nor the gate state machine knows how
	// to handle — better to fail loudly during testing than silently in
	// production.
	panic("drift: unknown state.DriftKind " + string(k))
}

// compareRootClaudeMdSection compares the live project-root CLAUDE.md section
// against the recorded section fingerprint (R46).
//
// Returns:
//   - (nil, nil) if no drift (section bytes unchanged) OR benign-skipped
//   - (entry, nil) drift detected; status = "modified" for content drift, or
//     "unrecoverable" when the file is missing OR markers are gone
//   - error only on non-ENOENT IO failures (EACCES, EIO) — those are
//     environment problems, not user-content drift signals.
//
// Why no fast-path: there's no per-section mtime to short-circuit on. The
// file's whole-file mtime is meaningless for section drift (the user could
// have touched the file without changing the section). We always read +
// parse + hash the section bytes. This is fine: it's exactly one extra file
// read per drift call, and CLAUDE.md is small (kilobytes).
func compareRootClaudeMdSection(
	paths state.StatePaths,
	recorded state.SectionFingerprint,
	provenance []state.ResolvedSourceRef,
) (*DriftEntry, error) {
	contentBytes, err := os.ReadFile(paths.RootClaudeMdFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// We have a recorded section but the file is gone. The file
			// itself is structurally lost — `unrecoverable` rather than
			// `deleted` because the user's remediation is `init` (recreate
			// the file with markers), not the standard discard/persist gate.
			return &DriftEntry{
				RelPath:     "CLAUDE.md",
				Status:      DriftStatusUnrecoverable,
				Provenance:  provenance,
				Destination: DriftDestinationProjectRoot,
				Error: fmt.Sprintf(
					"project-root CLAUDE.md is missing — run `c3p init` to recreate, then `c3p validate` to verify (file: %s; see docs/migration/cw6-section-ownership.md)",
					paths.RootClaudeMdFile,
				),
			}, nil
		}
		// Other IO errors (EACCES, EIO) propagate.
		return nil, err
	}

	parsed := markers.ParseMarkers(string(contentBytes))
	if parsed.Status != markers.StatusValid {
		// Both "absent" (file present, no markers) and "malformed" (lone /
		// partial / multi-block / version-mismatch) collapse to the same
		// `unrecoverable` status: the user has broken the structural
		// contract. The error message names the file and points at the two
		// commands that can fix it. We deliberately do NOT include the raw
		// parse reason — users care about the next command to run.
		return &DriftEntry{
			RelPath:     "CLAUDE.md",
			Status:      DriftStatusUnrecoverable,
			Provenance:  provenance,
			Destination: DriftDestinationProjectRoot,
			Error: fmt.Sprintf(
				"project-root CLAUDE.md markers are missing or malformed — run `c3p init` to repair, then `c3p validate` to verify (file: %s; see docs/migration/cw6-section-ownership.md)",
				paths.RootClaudeMdFile,
			),
		}, nil
	}

	// Section is locatable: compare bytes between markers against the
	// recorded fingerprint. Hash the section as utf8 bytes, matching the
	// materialize-side hash.
	//
	// cw6.1 followup: a size mismatch is a sufficient drift signal on its
	// own (sha256 collisions across different byte lengths are not the
	// threat model — content with a different size is by definition
	// different content). Returning early on size mismatch saves the hash
	// on a guaranteed drift.
	sectionBytes := []byte(parsed.Section)
	if int64(len(sectionBytes)) != recorded.Size {
		return &DriftEntry{
			RelPath:     "CLAUDE.md",
			Status:      DriftStatusModified,
			Provenance:  provenance,
			Destination: DriftDestinationProjectRoot,
		}, nil
	}
	liveHash := state.HashBytes(sectionBytes)
	if liveHash == recorded.ContentHash {
		return nil, nil
	}
	return &DriftEntry{
		RelPath:     "CLAUDE.md",
		Status:      DriftStatusModified,
		Provenance:  provenance,
		Destination: DriftDestinationProjectRoot,
	}, nil
}
