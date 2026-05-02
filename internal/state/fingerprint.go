package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/htxryan/claude-code-config-profiles/internal/merge"
	"github.com/htxryan/claude-code-config-profiles/internal/resolver"
)

// FingerprintSchemaVersion is the schema stamp for the fingerprint format.
// Independent of the state-file schema because fingerprint storage may evolve
// (e.g. hash algorithm) faster than the surrounding StateFile shape.
const FingerprintSchemaVersion = 1

// FingerprintEntry is one file's recorded fingerprint. Two-tier (R18):
// Size and MtimeMs are the fast-path metadata check; ContentHash is the
// slow-path verification recomputed only when metadata differs.
type FingerprintEntry struct {
	// Size is the file size in bytes.
	Size int64
	// MtimeMs is the modification time in millis since epoch.
	MtimeMs int64
	// ContentHash is the hex-encoded sha256 of the file contents.
	ContentHash string
}

// Fingerprint is the per-tree fingerprint produced by materialize and
// consumed by drift detection.
type Fingerprint struct {
	SchemaVersion int
	// Files keys are posix-relative paths from the .claude/ root, matching
	// MergedFile.Path and PlanFile.RelPath conventions.
	Files map[string]FingerprintEntry
}

// HashBytes returns the hex-encoded sha256 of bytes.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// SectionFingerprint is the section-only fingerprint for the project-root
// CLAUDE.md (R46, cw6/T4). Distinct from FingerprintEntry because it tracks
// the SECTION bytes (between the :begin/:end markers), NOT the whole-file
// metadata. The whole-file mtime/size would change every time the user edits
// outside the markers, which the spec explicitly says must NOT register as
// drift.
type SectionFingerprint struct {
	Size        int64
	ContentHash string
}

// SourceFingerprint is the aggregate fingerprint of the resolved-source files
// at materialize time (azp). Used by `status` to surface "source updated since
// last materialize" without doing a full re-resolve+merge. The aggregate hash
// is sufficient (mtime+size, hashed) to flip the freshness bit; per-file
// granularity is not needed here — the next `sync` does precise work.
type SourceFingerprint struct {
	FileCount     int
	AggregateHash string
}

// ComputeSourceFingerprint stat-walks plan.Files (sorted by relPath, absPath)
// and produces an aggregate hash over `relPath|absPath|size|mtimeMs\n` tuples.
// Missing files (ENOENT) record size=-1, mtimeMs=-1 so deletion still flips
// the aggregate. Non-ENOENT errors propagate — those are environment
// problems, not freshness signals.
//
// Mirrors src/state/fingerprint.ts:computeSourceFingerprint. Sort order must
// match the TS implementation byte-for-byte so cross-language IV harness
// agrees.
func ComputeSourceFingerprint(plan resolver.ResolvedPlan) (SourceFingerprint, error) {
	files := append([]resolver.PlanFile(nil), plan.Files...)
	sort.Slice(files, func(i, j int) bool {
		if files[i].RelPath != files[j].RelPath {
			return files[i].RelPath < files[j].RelPath
		}
		return files[i].AbsPath < files[j].AbsPath
	})
	hasher := sha256.New()
	for _, f := range files {
		size := int64(-1)
		mtimeMs := int64(-1)
		info, err := os.Stat(f.AbsPath)
		if err == nil {
			size = info.Size()
			mtimeMs = info.ModTime().UnixMilli()
		} else if !errors.Is(err, os.ErrNotExist) {
			return SourceFingerprint{}, err
		}
		fmt.Fprintf(hasher, "%s|%s|%d|%d\n", f.RelPath, f.AbsPath, size, mtimeMs)
	}
	return SourceFingerprint{
		FileCount:     len(files),
		AggregateHash: hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

// FingerprintFromMergedFiles builds a Fingerprint from in-memory merged
// bytes at the moment they're about to be written. MtimeMs is filled in
// post-write via RecordMtimes — until then we record 0 as a sentinel.
//
// Splitting hash-from-bytes (now) from mtime-from-disk (after write) avoids
// a redundant read-after-write while preserving the two-tier semantic:
// future drift detection still has a content hash to fall back on.
func FingerprintFromMergedFiles(files []merge.MergedFile) Fingerprint {
	out := make(map[string]FingerprintEntry, len(files))
	for _, f := range files {
		out[f.Path] = FingerprintEntry{
			Size:        int64(len(f.Bytes)),
			MtimeMs:     0,
			ContentHash: HashBytes(f.Bytes),
		}
	}
	return Fingerprint{SchemaVersion: FingerprintSchemaVersion, Files: out}
}

// RecordMtimes stats each file under claudeDir referenced by fingerprint and
// fills in MtimeMs values. Called immediately after a successful pending-prior
// commit (D5) to capture the post-rename mtimes that drift detection will
// compare against.
//
// Any entry whose file is missing on disk is left with mtimeMs=0; the next
// drift check will treat it as drifted (deleted).
func RecordMtimes(claudeDir string, fp Fingerprint) Fingerprint {
	out := make(map[string]FingerprintEntry, len(fp.Files))
	for relPath, entry := range fp.Files {
		entry := entry // local copy
		if info, err := os.Stat(filepath.Join(claudeDir, relPath)); err == nil {
			entry.MtimeMs = info.ModTime().UnixMilli()
		} else {
			entry.MtimeMs = 0
		}
		out[relPath] = entry
	}
	return Fingerprint{SchemaVersion: fp.SchemaVersion, Files: out}
}

// FingerprintTree walks claudeDir and produces a fingerprint by reading every
// file. Used by drift detection's slow-path / forced-recompute mode and as
// the baseline for fingerprint comparison. Returns posix-relative keys to
// match MergedFile.Path conventions.
func FingerprintTree(claudeDir string) (Fingerprint, error) {
	out := make(map[string]FingerprintEntry)
	err := filepath.WalkDir(claudeDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) && path == claudeDir {
				return filepath.SkipDir
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			// Skip symlinks/sockets/etc — same rationale as the TS walker.
			// .claude/ is a copy tree post-R39, so any non-regular entries
			// are user artifacts that drift detection ignores.
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Tolerate file-vanished-between-readdir-and-stat (Opus #4):
				// editor atomic-write swap shouldn't abort the walk.
				return nil
			}
			return err
		}
		bytes, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(claudeDir, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = FingerprintEntry{
			Size:        info.Size(),
			MtimeMs:     info.ModTime().UnixMilli(),
			ContentHash: HashBytes(bytes),
		}
		return nil
	})
	// A wholly missing claudeDir is a valid empty fingerprint (init before
	// any materialize). Other walk errors propagate.
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Fingerprint{}, err
	}
	return Fingerprint{SchemaVersion: FingerprintSchemaVersion, Files: out}, nil
}

// metadataEntry is a transient stat-only record used by the fast-path comparator.
type metadataEntry struct {
	size    int64
	mtimeMs int64
	abs     string
}

// fingerprintTreeMetadataOnly walks claudeDir and returns stat-only entries
// (no file reads). Used by CompareFingerprint to avoid hashing every file
// when most haven't changed (two-tier R18).
func fingerprintTreeMetadataOnly(claudeDir string) (map[string]metadataEntry, error) {
	out := make(map[string]metadataEntry)
	err := filepath.WalkDir(claudeDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) && path == claudeDir {
				return filepath.SkipDir
			}
			return walkErr
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(claudeDir, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = metadataEntry{
			size:    info.Size(),
			mtimeMs: info.ModTime().UnixMilli(),
			abs:     path,
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return out, nil
}

// DriftKind enumerates the per-file drift outcomes used by E4. "unchanged" is
// included in CompareResult so callers can compute summaries by kind without
// re-walking the tree.
type DriftKind string

const (
	DriftUnchanged DriftKind = "unchanged"
	DriftModified  DriftKind = "modified"
	DriftAdded     DriftKind = "added"
	DriftDeleted   DriftKind = "deleted"
)

// FileDrift is one file's drift status, produced by CompareFingerprint.
type FileDrift struct {
	RelPath string
	Kind    DriftKind
}

// CompareMetrics captures fast/slow-path counts so E4's DriftReport can
// surface "0 fast-path hits across 200 files" as a smell signal for clock-
// skew or atomic-write tools.
type CompareMetrics struct {
	// ScannedFiles is the total live files seen by the metadata walk.
	ScannedFiles int
	// FastPathHits is the count where size+mtime matched and we skipped
	// hashing. Counts only files in the live∩recorded intersection.
	FastPathHits int
	// SlowPathHits is the count where metadata diverged (or one side was
	// missing) and we either hashed or attributed an add/delete.
	SlowPathHits int
}

// CompareResult bundles the per-file drift list with fast/slow-path metrics.
type CompareResult struct {
	Entries []FileDrift
	Metrics CompareMetrics
}

// CompareFingerprint compares a recorded fingerprint against the live tree
// at claudeDir. Two-tier (R18): the fast path is a metadata-only walk; only
// files whose metadata signals a possible change are opened and sha256-hashed.
//
// Returns one entry per relPath in the union of recorded + live trees.
// Unchanged files are included so callers can compute summaries by kind.
func CompareFingerprint(claudeDir string, recorded Fingerprint) (CompareResult, error) {
	liveMeta, err := fingerprintTreeMetadataOnly(claudeDir)
	if err != nil {
		return CompareResult{}, err
	}

	// Union of recorded + live keys, deterministic-sorted for stable output.
	unionSet := make(map[string]struct{}, len(recorded.Files)+len(liveMeta))
	for k := range recorded.Files {
		unionSet[k] = struct{}{}
	}
	for k := range liveMeta {
		unionSet[k] = struct{}{}
	}
	union := make([]string, 0, len(unionSet))
	for k := range unionSet {
		union = append(union, k)
	}
	sort.Strings(union)

	fast := 0
	slow := 0
	out := make([]FileDrift, 0, len(union))
	for _, relPath := range union {
		r, hasR := recorded.Files[relPath]
		l, hasL := liveMeta[relPath]
		switch {
		case !hasR && hasL:
			slow++
			out = append(out, FileDrift{RelPath: relPath, Kind: DriftAdded})
		case hasR && !hasL:
			slow++
			out = append(out, FileDrift{RelPath: relPath, Kind: DriftDeleted})
		case hasR && hasL:
			// Fast path: stat metadata matches → definitely unchanged. The
			// recorded mtime==0 sentinel disables fast path so we still hash
			// when we never recorded a real mtime (test-injected fingerprints).
			if r.Size == l.size && r.MtimeMs != 0 && l.mtimeMs == r.MtimeMs {
				fast++
				out = append(out, FileDrift{RelPath: relPath, Kind: DriftUnchanged})
				continue
			}
			// Slow path: hash this single file and compare.
			slow++
			bytes, err := os.ReadFile(l.abs)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					// File deleted between metadata walk and ReadFile — treat as
					// deleted (Opus #4).
					out = append(out, FileDrift{RelPath: relPath, Kind: DriftDeleted})
					continue
				}
				return CompareResult{}, err
			}
			liveHash := HashBytes(bytes)
			if liveHash == r.ContentHash {
				out = append(out, FileDrift{RelPath: relPath, Kind: DriftUnchanged})
			} else {
				out = append(out, FileDrift{RelPath: relPath, Kind: DriftModified})
			}
		}
	}

	return CompareResult{
		Entries: out,
		Metrics: CompareMetrics{
			ScannedFiles: len(liveMeta),
			FastPathHits: fast,
			SlowPathHits: slow,
		},
	}, nil
}

