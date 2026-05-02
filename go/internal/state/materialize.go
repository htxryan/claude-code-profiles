package state

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	c3perr "github.com/htxryan/c3p/internal/errors"
	"github.com/htxryan/c3p/internal/markers"
	"github.com/htxryan/c3p/internal/merge"
	"github.com/htxryan/c3p/internal/resolver"
)

// MaterializeOptions configure the orchestrator. Zero value is the production
// path; tests set RetainPriorForTests to inspect the rolled-aside dir.
type MaterializeOptions struct {
	// RetainPriorForTests, when true, skips the post-success drop of .prior/
	// so tests can assert the dir's contents. Production never sets this.
	RetainPriorForTests bool
}

// MaterializeResult is the orchestrator's return: the StateFile written to
// disk and the discard-backup snapshot path (passed through from the caller
// — D6's snapshotForDiscard runs BEFORE materialize on the discard-gate path
// so the path lands in the state record).
type MaterializeResult struct {
	State          StateFile
	BackupSnapshot string
}

// rootSplicePlan captures the in-memory parsed slices of the live root
// CLAUDE.md at pre-flight time (R45 atomic-across-destinations). Holding the
// slices instead of re-reading at write time prevents a TOCTOU window where a
// user edit between pre-flight and splice could inject malformed markers.
type rootSplicePlan struct {
	filePath     string
	version      int
	before       string
	after        string
	sectionBytes string
}

// Materialize applies merged to disk as the new .claude/ tree atomically
// (R13 / R14 / R14a / R16 / R16a / R22b / R45 / R46).
//
// The protocol:
//   (a) write merged bytes (.claude/-destination only) to .meta/pending/
//   (b) atomically rename existing .claude/ to .meta/prior/ (if exists)
//   (c) atomically rename .meta/pending/ to .claude/
//
// On success: write state.json (atomic), apply project-root section splice
// if applicable (R45), then drop .prior/ in the foreground (we await for
// test determinism; production crash injection between this and the next op
// is recoverable — reconcile sees no .prior/ since rmrf is idempotent).
//
// Lock precondition: caller MUST hold the project lock (D7 swap orchestration
// wraps drift gate + materialize + persist + state-write in a single
// WithLock). The lock brackets BOTH writes (.claude/ rename pair AND the
// projectRoot section splice) AND the state-write so partial-success windows
// are not observable.
//
// R45 PRE-FLIGHT: if any projectRoot files are in this plan, verify the live
// root CLAUDE.md has well-formed markers BEFORE we touch anything. Missing
// or malformed markers abort the WHOLE materialize — neither destination
// must see any side-effect.
//
// discardBackup is the path returned by SnapshotForDiscard (D6 calls that
// before materialize on the discard-gate path so it lands in the state
// record). Pass "" when no snapshot was taken (clean swap, sync, etc.).
func Materialize(
	paths StatePaths,
	plan resolver.ResolvedPlan,
	merged []merge.MergedFile,
	opts MaterializeOptions,
	discardBackup string,
) (MaterializeResult, error) {
	// Reconcile any leftover .pending/.prior from a prior crashed run BEFORE
	// we start writing. After reconcile, live .claude/ is consistent with
	// whatever the previous successful state was.
	if _, err := ReconcileMaterialize(paths); err != nil {
		return MaterializeResult{}, err
	}

	// Split merged by destination. The .claude/ group goes through the
	// historical pending/prior whole-tree protocol; projectRoot goes through
	// a section-splice on the live root CLAUDE.md. Both writes happen under
	// the caller's lock so the pair is atomic-across-destinations from a
	// concurrent reader's POV.
	claudeMerged := make([]merge.MergedFile, 0, len(merged))
	rootMerged := make([]merge.MergedFile, 0)
	for _, m := range merged {
		if m.Destination == resolver.DestinationProjectRoot {
			rootMerged = append(rootMerged, m)
		} else {
			claudeMerged = append(claudeMerged, m)
		}
	}

	// Read prior state up-front so we can decide BEFORE side-effects whether
	// the empty-splice path applies (new plan has no projectRoot file but the
	// PRIOR materialize did — clear the stale section from the live file).
	prior, err := ReadStateFile(paths)
	if err != nil {
		return MaterializeResult{}, err
	}

	// R45 PRE-FLIGHT (CRITICAL ORDERING): plan + verify markers BEFORE any
	// side-effects. If projectRoot files are in this plan the live CLAUDE.md
	// MUST have valid markers; if not, abort the whole materialize. If the
	// new plan has no projectRoot but the prior state recorded one, stage
	// an empty splice to clear the stale bytes.
	var splicePlan *rootSplicePlan
	if len(rootMerged) > 0 {
		p, err := preflightRootSplice(paths, rootMerged)
		if err != nil {
			return MaterializeResult{}, err
		}
		splicePlan = p
	} else if prior.State.RootClaudeMdSection != nil {
		p, err := preflightEmptyRootSplice(paths)
		if err != nil {
			return MaterializeResult{}, err
		}
		splicePlan = p
	}

	// Step a: write .claude/-destination merged bytes to pending. RmRf any
	// leftover pending (paranoia on top of reconcile's clean) so a stale
	// pending from a non-c3p source doesn't pollute the new tree.
	//
	// Empty claudeMerged (a profile that contributes ONLY a projectRoot
	// CLAUDE.md, with no .claude/ files) is handled — WriteFiles creates the
	// empty pending dir and the rename swap below produces an empty .claude/.
	// That matches the spec's "we own .claude/ entirely" contract.
	if err := RmRf(paths.PendingDir); err != nil {
		return MaterializeResult{}, err
	}
	if err := WriteFiles(paths.PendingDir, claudeMerged); err != nil {
		_ = RmRf(paths.PendingDir)
		return MaterializeResult{}, err
	}

	// Step b: rename existing live .claude/ to .prior/ if it exists.
	liveExists, err := PathExists(paths.ClaudeDir)
	if err != nil {
		return MaterializeResult{}, err
	}
	if liveExists {
		// Defensive: drop any leftover .prior/ first. atomic-rename into an
		// existing target is an error on Windows.
		if err := RmRf(paths.PriorDir); err != nil {
			return MaterializeResult{}, err
		}
		if err := AtomicRename(paths.ClaudeDir, paths.PriorDir); err != nil {
			_ = RmRf(paths.PendingDir)
			return MaterializeResult{}, err
		}
	}

	// Step c: rename pending → claudeDir. If this fails, restore from prior.
	if err := AtomicRename(paths.PendingDir, paths.ClaudeDir); err != nil {
		// Step c failure: try to roll prior back. attemptStepCRollback
		// surfaces restore failures to stderr (the user needs to know if
		// both swap AND rollback failed). We propagate the original step-c
		// error.
		attemptStepCRollback(paths)
		return MaterializeResult{}, err
	}

	// Step c' (the splice): write the projectRoot section bytes if planned.
	// Happens AFTER the .claude/ swap so a failure here leaves the .claude/
	// swap committed (.prior/ cleanup hasn't run yet — reconcile + re-run is
	// safe). We surface the splice error so the user knows projectRoot did
	// NOT land.
	var rootSectionFp *SectionFingerprint
	if splicePlan != nil {
		fp, err := applyRootSplice(paths, splicePlan)
		if err != nil {
			return MaterializeResult{}, err
		}
		// Empty-splice path (new plan has no projectRoot contributor):
		// record nil rather than the empty-section fingerprint. The state
		// field tracks "is there a managed section we own"; the new plan's
		// answer is no.
		if len(rootMerged) > 0 {
			rootSectionFp = &fp
		}
	}

	// Step c succeeded. Build the state file BEFORE removing prior so a
	// crash during state-write still leaves a recoverable artifact. Next
	// run sees .claude/ + .prior/; reconcile detects "fresh materialize
	// landed but prior cleanup didn't run" — but the state file points at
	// the old plan, so a follow-up reconcile will restore from .prior/. The
	// re-materialize is idempotent.

	// Compute fingerprint from merged bytes (we have them in memory) and
	// overlay mtimes from the freshly-renamed live tree. Whole-file
	// fingerprints (R18/R19) apply ONLY to .claude/-destination files; the
	// projectRoot section uses a separate section-only fingerprint (R46).
	fingerprint := FingerprintFromMergedFiles(claudeMerged)
	fingerprint = RecordMtimes(paths.ClaudeDir, fingerprint)

	// Preserve external-trust notices from prior state and add any new ones
	// for external paths in this plan that haven't been noticed before.
	trustNotices := mergeExternalTrustNotices(prior.State.ExternalTrustNotices, plan)

	// azp: capture the source fingerprint AFTER the rename pair lands so any
	// same-second mtime jitter from copyTree doesn't bake into the source
	// fingerprint. We want to record the mtime the user's editor will write
	// next, not our own write timestamps.
	srcFp, err := ComputeSourceFingerprint(plan)
	if err != nil {
		return MaterializeResult{}, err
	}

	now := FormatTimestamp(time.Now())
	profile := plan.ProfileName
	newState := StateFile{
		SchemaVersion:        StateFileSchemaVersion,
		ActiveProfile:        &profile,
		MaterializedAt:       &now,
		ResolvedSources:      contributorsToSourceRefs(plan.Contributors),
		Fingerprint:          fingerprint,
		ExternalTrustNotices: trustNotices,
		RootClaudeMdSection:  rootSectionFp,
		SourceFingerprint:    &srcFp,
	}
	if err := WriteStateFile(paths, newState); err != nil {
		return MaterializeResult{}, err
	}

	// Foreground-drop prior dir. Production crash injection between this
	// and the next op is recoverable (reconcile sees no .prior/ because
	// rmrf is idempotent).
	if !opts.RetainPriorForTests {
		exists, err := PathExists(paths.PriorDir)
		if err != nil {
			return MaterializeResult{}, err
		}
		if exists {
			if err := RmRf(paths.PriorDir); err != nil {
				return MaterializeResult{}, err
			}
		}
	}

	return MaterializeResult{State: newState, BackupSnapshot: discardBackup}, nil
}

// preflightRootSplice reads the live project-root CLAUDE.md, verifies markers,
// and stages a splice plan. Returns *RootClaudeMdMarkersMissingError when the
// file is absent OR markers are missing/malformed (R44/R45). Both conditions
// produce the same actionable message per spec §12.4 — the user's remediation
// is identical (run init).
//
// We hold the parsed slices rather than re-reading at write time to prevent a
// TOCTOU window where a user edit could inject malformed markers between
// pre-flight and splice.
func preflightRootSplice(paths StatePaths, rootMerged []merge.MergedFile) (*rootSplicePlan, error) {
	filePath := paths.RootClaudeMdFile
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, c3perr.NewRootClaudeMdMarkersMissingError(filePath)
		}
		return nil, err
	}
	parsed := markers.ParseMarkers(string(contentBytes))
	if parsed.Status != markers.StatusValid {
		return nil, c3perr.NewRootClaudeMdMarkersMissingError(filePath)
	}

	// Concat all rootMerged entries' bytes in path order. In practice this
	// is always a single CLAUDE.md entry (the merge engine groups by path),
	// but the loop is robust if the spec ever expands what lives at
	// projectRoot.
	sortedRoot := append([]merge.MergedFile(nil), rootMerged...)
	sort.Slice(sortedRoot, func(i, j int) bool {
		return sortedRoot[i].Path < sortedRoot[j].Path
	})
	var sb strings.Builder
	for _, m := range sortedRoot {
		sb.Write(m.Bytes)
	}

	return &rootSplicePlan{
		filePath:     filePath,
		version:      parsed.Version,
		before:       parsed.Before,
		after:        parsed.After,
		sectionBytes: sb.String(),
	}, nil
}

// preflightEmptyRootSplice stages a splice that clears the managed-block bytes
// when the new plan contributes nothing to projectRoot but the prior state had
// a section. Returns nil when the splice should be skipped — specifically:
//   - root CLAUDE.md is missing (user removed it, or never ran init); or
//   - markers are absent/malformed (user opted out per migration doc by
//     deleting the marker block).
//
// Unlike preflightRootSplice we do NOT throw on missing markers — the prior
// plan needed them, but the new plan does not, and the pre-flight error
// message ("run init") would mislead a `use` that no longer touches
// projectRoot.
func preflightEmptyRootSplice(paths StatePaths) (*rootSplicePlan, error) {
	filePath := paths.RootClaudeMdFile
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	parsed := markers.ParseMarkers(string(contentBytes))
	if parsed.Status != markers.StatusValid {
		return nil, nil
	}
	return &rootSplicePlan{
		filePath:     filePath,
		version:      parsed.Version,
		before:       parsed.Before,
		after:        parsed.After,
		sectionBytes: "",
	}, nil
}

// applyRootSplice renders the managed block from the staged section bytes,
// concatenates with the preserved before/after slices, and writes atomically
// via temp+rename in the same directory as the final file (no EXDEV risk).
//
// Idempotence detail: RenderManagedBlock always appends a trailing \n after
// <end> so the marker sits on its own line in a freshly-init'd file. When
// splicing INTO a file that already has user content below the markers, the
// parsed `after` carries a leading \n (the original separator between <end>
// and the next user line). Naively concatenating block + after would double
// that \n and grow the file by one byte per materialize — failing
// byte-equality on a no-op re-apply. Strip the rendered block's trailing \n
// when `after` already provides one.
//
// Returns the section-only fingerprint (R46): we re-parse newContent (the
// bytes we just wrote) rather than re-reading from disk because (a) we just
// wrote them and (b) re-reading would introduce a TOCTOU window. Hashing
// in-memory plan.sectionBytes (the raw merge input) would silently disagree
// with what drift extracts from disk, because RenderManagedBlock wraps the
// body with newlines and the self-doc comment line — those bytes ARE between
// :begin and :end on disk.
func applyRootSplice(paths StatePaths, plan *rootSplicePlan) (SectionFingerprint, error) {
	block := markers.RenderManagedBlock(plan.sectionBytes, plan.version)
	if strings.HasPrefix(plan.after, "\n") && strings.HasSuffix(block, "\n") {
		block = block[:len(block)-1]
	}
	newContent := plan.before + block + plan.after

	tmpPath := RootClaudeMdTmpPath(paths)
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return SectionFingerprint{}, err
	}
	if _, err := f.Write([]byte(newContent)); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return SectionFingerprint{}, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return SectionFingerprint{}, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return SectionFingerprint{}, err
	}
	if err := AtomicRename(tmpPath, plan.filePath); err != nil {
		_ = os.Remove(tmpPath)
		return SectionFingerprint{}, err
	}
	FsyncDir(plan.filePath)

	reparsed := markers.ParseMarkers(newContent)
	if reparsed.Status != markers.StatusValid {
		// Defensive: RenderManagedBlock just produced these markers; if
		// parse doesn't find them, our renderer is broken (test-only
		// signal). Emit a typed error so test failures point at the right
		// module.
		return SectionFingerprint{}, fmt.Errorf("applyRootSplice: rendered managed block did not round-trip through ParseMarkers — investigate RenderManagedBlock / MarkerRegex")
	}
	sectionBytes := []byte(reparsed.Section)
	return SectionFingerprint{
		Size:        int64(len(sectionBytes)),
		ContentHash: HashBytes(sectionBytes),
	}, nil
}

// attemptStepCRollback tries to restore the rolled-aside .prior/ back to
// .claude/ after step c failed. If .claude/ is non-empty (a partial step c
// landed bytes there), rmrf it first so the rename doesn't fail with
// ENOTEMPTY. Errors are surfaced via stderr because the user needs to know
// if both the swap AND the rollback failed.
func attemptStepCRollback(paths StatePaths) {
	priorExists, _ := PathExists(paths.PriorDir)
	if !priorExists {
		_ = RmRf(paths.PendingDir)
		return
	}
	if exists, _ := PathExists(paths.ClaudeDir); exists {
		_ = RmRf(paths.ClaudeDir)
	}
	if err := AtomicRename(paths.PriorDir, paths.ClaudeDir); err != nil {
		writeRollbackStderr(paths.ClaudeDir, err)
	}
	_ = RmRf(paths.PendingDir)
}

// writeRollbackStderr is split out so the test layer can replace it (we use a
// package-level var so tests can capture the message instead of polluting
// stderr in CI).
var writeRollbackStderr = func(claudeDir string, err error) {
	fmt.Fprintf(stderrSink(), "c3p: rollback failed restoring %s: %v\n", claudeDir, err)
}

func stderrSink() io.Writer { return os.Stderr }

// mergeExternalTrustNotices is R37a's append-only merge. Keep all existing
// notices, append a notice for each external path in the plan that's not
// already recorded.
func mergeExternalTrustNotices(existing []ExternalTrustNotice, plan resolver.ResolvedPlan) []ExternalTrustNotice {
	seen := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		seen[e.ResolvedPath] = struct{}{}
	}
	out := make([]ExternalTrustNotice, len(existing))
	copy(out, existing)
	now := FormatTimestamp(time.Now())
	for _, ext := range plan.ExternalPaths {
		if _, ok := seen[ext.ResolvedPath]; ok {
			continue
		}
		seen[ext.ResolvedPath] = struct{}{}
		out = append(out, ExternalTrustNotice{
			Raw:          ext.Raw,
			ResolvedPath: ext.ResolvedPath,
			NoticedAt:    now,
		})
	}
	return out
}

// contributorsToSourceRefs projects the plan's Contributors to the subset
// recorded in state.json (R14). The state file does not need the manifest
// or claudeDir; D7's status renders from these refs alone.
func contributorsToSourceRefs(contribs []resolver.Contributor) []ResolvedSourceRef {
	out := make([]ResolvedSourceRef, len(contribs))
	for i, c := range contribs {
		out[i] = ResolvedSourceRef{
			ID:       c.ID,
			Kind:     string(c.Kind),
			RootPath: c.RootPath,
			External: c.External,
		}
	}
	return out
}

// ReadRecordedFingerprint returns the recorded fingerprint files map from
// the active state file. Helper for D6 drift detection — returns an empty
// map when no state exists.
func ReadRecordedFingerprint(paths StatePaths) (map[string]FingerprintEntry, error) {
	res, err := ReadStateFile(paths)
	if err != nil {
		return nil, err
	}
	return res.State.Fingerprint.Files, nil
}

// IsLiveConsistentWithRecord verifies that the live tree's stat snapshot is
// internally consistent — every recorded file exists with the recorded size.
// Returns true iff so. Lighter than a full fingerprint comparison; used to
// gate "are we in a clean materialized state" UX.
func IsLiveConsistentWithRecord(paths StatePaths) (bool, error) {
	res, err := ReadStateFile(paths)
	if err != nil {
		return false, err
	}
	if res.State.ActiveProfile == nil {
		return false, nil
	}
	for relPath, entry := range res.State.Fingerprint.Files {
		info, err := os.Stat(filepath.Join(paths.ClaudeDir, relPath))
		if err != nil {
			return false, nil
		}
		if info.Size() != entry.Size {
			return false, nil
		}
	}
	return true, nil
}
