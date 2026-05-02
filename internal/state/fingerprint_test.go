package state_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/htxryan/claude-code-config-profiles/internal/merge"
	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

func TestHashBytes_Sha256Hex(t *testing.T) {
	t.Parallel()
	got := state.HashBytes([]byte("abc"))
	sum := sha256.Sum256([]byte("abc"))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("HashBytes(abc) = %q, want %q", got, want)
	}
}

func TestFingerprintFromMergedFiles_RecordsHashAndZeroMtime(t *testing.T) {
	t.Parallel()
	files := []merge.MergedFile{
		{Path: "a.txt", Bytes: []byte("alpha")},
		{Path: "sub/b.txt", Bytes: []byte("beta")},
	}
	fp := state.FingerprintFromMergedFiles(files)
	if fp.SchemaVersion != state.FingerprintSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", fp.SchemaVersion, state.FingerprintSchemaVersion)
	}
	if len(fp.Files) != 2 {
		t.Fatalf("Files len = %d, want 2", len(fp.Files))
	}
	a := fp.Files["a.txt"]
	if a.Size != int64(len("alpha")) {
		t.Errorf("a.Size = %d, want %d", a.Size, len("alpha"))
	}
	if a.MtimeMs != 0 {
		t.Errorf("a.MtimeMs = %d, want 0 (sentinel)", a.MtimeMs)
	}
	if a.ContentHash != state.HashBytes([]byte("alpha")) {
		t.Errorf("a.ContentHash = %q, want %q", a.ContentHash, state.HashBytes([]byte("alpha")))
	}
}

func TestRecordMtimes_FillsFromDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fp := state.Fingerprint{
		SchemaVersion: state.FingerprintSchemaVersion,
		Files: map[string]state.FingerprintEntry{
			"a.txt": {Size: 5, MtimeMs: 0, ContentHash: state.HashBytes([]byte("alpha"))},
			"missing": {Size: 0, MtimeMs: 0, ContentHash: state.HashBytes(nil)},
		},
	}
	updated := state.RecordMtimes(dir, fp)
	if updated.Files["a.txt"].MtimeMs == 0 {
		t.Errorf("a.txt MtimeMs not populated")
	}
	if updated.Files["missing"].MtimeMs != 0 {
		t.Errorf("missing file should keep MtimeMs=0, got %d", updated.Files["missing"].MtimeMs)
	}
}

func TestFingerprintTree_RecordsAllFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "alpha", 0o644)
	mustWrite(t, filepath.Join(dir, "sub", "b.txt"), "beta", 0o644)

	fp, err := state.FingerprintTree(dir)
	if err != nil {
		t.Fatalf("FingerprintTree: %v", err)
	}
	keys := make([]string, 0, len(fp.Files))
	for k := range fp.Files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"a.txt", "sub/b.txt"}
	if !equalSlices(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	a := fp.Files["a.txt"]
	if a.ContentHash != state.HashBytes([]byte("alpha")) {
		t.Errorf("a.ContentHash mismatch")
	}
	if a.Size != 5 {
		t.Errorf("a.Size = %d, want 5", a.Size)
	}
	if a.MtimeMs <= 0 {
		t.Errorf("a.MtimeMs = %d, want > 0", a.MtimeMs)
	}
}

func TestFingerprintTree_MissingDirIsEmpty(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "nope")
	fp, err := state.FingerprintTree(missing)
	if err != nil {
		t.Fatalf("FingerprintTree on missing dir: %v", err)
	}
	if len(fp.Files) != 0 {
		t.Fatalf("expected empty fingerprint, got %d entries", len(fp.Files))
	}
}

func TestCompareFingerprint_FastPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "alpha", 0o644)

	live, err := state.FingerprintTree(dir)
	if err != nil {
		t.Fatalf("FingerprintTree: %v", err)
	}
	res, err := state.CompareFingerprint(dir, live)
	if err != nil {
		t.Fatalf("CompareFingerprint: %v", err)
	}
	if got := res.Metrics.FastPathHits; got != 1 {
		t.Errorf("FastPathHits = %d, want 1", got)
	}
	if got := res.Metrics.SlowPathHits; got != 0 {
		t.Errorf("SlowPathHits = %d, want 0", got)
	}
	if len(res.Entries) != 1 || res.Entries[0].Kind != state.DriftUnchanged {
		t.Errorf("Entries = %+v, want [{a.txt unchanged}]", res.Entries)
	}
}

func TestCompareFingerprint_SlowPathMatchesContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "alpha", 0o644)

	// Synthesize a fingerprint with mismatched mtime so the fast path falls
	// through to the slow path. Content hash matches, so the file should be
	// reported as unchanged.
	fp := state.Fingerprint{
		SchemaVersion: state.FingerprintSchemaVersion,
		Files: map[string]state.FingerprintEntry{
			"a.txt": {
				Size:        5,
				MtimeMs:     time.Now().UnixMilli() - 999_999,
				ContentHash: state.HashBytes([]byte("alpha")),
			},
		},
	}
	res, err := state.CompareFingerprint(dir, fp)
	if err != nil {
		t.Fatalf("CompareFingerprint: %v", err)
	}
	if got := res.Metrics.FastPathHits; got != 0 {
		t.Errorf("FastPathHits = %d, want 0", got)
	}
	if got := res.Metrics.SlowPathHits; got != 1 {
		t.Errorf("SlowPathHits = %d, want 1", got)
	}
	if len(res.Entries) != 1 || res.Entries[0].Kind != state.DriftUnchanged {
		t.Errorf("Entries = %+v, want [{a.txt unchanged}]", res.Entries)
	}
}

func TestCompareFingerprint_SlowPathDetectsModification(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "alpha-modified", 0o644)
	fp := state.Fingerprint{
		SchemaVersion: state.FingerprintSchemaVersion,
		Files: map[string]state.FingerprintEntry{
			"a.txt": {
				Size:        5,
				MtimeMs:     time.Now().UnixMilli() - 999_999,
				ContentHash: state.HashBytes([]byte("alpha")),
			},
		},
	}
	res, err := state.CompareFingerprint(dir, fp)
	if err != nil {
		t.Fatalf("CompareFingerprint: %v", err)
	}
	if len(res.Entries) != 1 || res.Entries[0].Kind != state.DriftModified {
		t.Errorf("Entries = %+v, want [{a.txt modified}]", res.Entries)
	}
}

func TestCompareFingerprint_AddedAndDeleted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "added.txt"), "new", 0o644)

	fp := state.Fingerprint{
		SchemaVersion: state.FingerprintSchemaVersion,
		Files: map[string]state.FingerprintEntry{
			"deleted.txt": {Size: 3, MtimeMs: 1, ContentHash: state.HashBytes([]byte("old"))},
		},
	}
	res, err := state.CompareFingerprint(dir, fp)
	if err != nil {
		t.Fatalf("CompareFingerprint: %v", err)
	}
	got := map[string]state.DriftKind{}
	for _, e := range res.Entries {
		got[e.RelPath] = e.Kind
	}
	if got["added.txt"] != state.DriftAdded {
		t.Errorf("added.txt = %q, want added", got["added.txt"])
	}
	if got["deleted.txt"] != state.DriftDeleted {
		t.Errorf("deleted.txt = %q, want deleted", got["deleted.txt"])
	}
}

func TestCompareFingerprint_RecordedMtimeZeroForcesSlowPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "alpha", 0o644)
	// Recorded entry with mtime sentinel 0 — must NOT take fast path even
	// when live mtime happens to match (which can't happen here because 0 is
	// before any real mtime). This proves FingerprintFromMergedFiles output
	// (mtime=0) is safe to compare before RecordMtimes runs.
	fp := state.Fingerprint{
		SchemaVersion: state.FingerprintSchemaVersion,
		Files: map[string]state.FingerprintEntry{
			"a.txt": {Size: 5, MtimeMs: 0, ContentHash: state.HashBytes([]byte("alpha"))},
		},
	}
	res, err := state.CompareFingerprint(dir, fp)
	if err != nil {
		t.Fatalf("CompareFingerprint: %v", err)
	}
	if res.Metrics.FastPathHits != 0 {
		t.Errorf("FastPathHits = %d, want 0 (mtime=0 sentinel)", res.Metrics.FastPathHits)
	}
	if res.Metrics.SlowPathHits != 1 {
		t.Errorf("SlowPathHits = %d, want 1", res.Metrics.SlowPathHits)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if b[i] != v {
			return false
		}
	}
	return true
}
