package state_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/htxryan/c3p/internal/state"
)

// TestReadStateFile_MissingReturnsDefault covers R42's "missing file → default
// state + Missing warning" leg.
func TestReadStateFile_MissingReturnsDefault(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	res, err := state.ReadStateFile(paths)
	if err != nil {
		t.Fatalf("ReadStateFile: %v", err)
	}
	if res.Warning == nil || res.Warning.Code != state.StateReadWarningMissing {
		t.Fatalf("warning = %+v, want Missing", res.Warning)
	}
	if !reflect.DeepEqual(res.State, state.DefaultState()) {
		t.Fatalf("state = %+v, want DefaultState", res.State)
	}
}

// TestStateFile_RoundTrip writes a fully-populated state file and reads it
// back, asserting both the warning is nil and the value round-trips.
func TestStateFile_RoundTrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)

	profile := "myprofile"
	mat := "2026-04-25T12:00:00.000Z"
	src := state.SourceFingerprint{FileCount: 2, AggregateHash: "deadbeef"}
	sf := state.StateFile{
		SchemaVersion:  state.StateFileSchemaVersion,
		ActiveProfile:  &profile,
		MaterializedAt: &mat,
		ResolvedSources: []state.ResolvedSourceRef{
			{ID: "base", Kind: "ancestor", RootPath: "/abs/base", External: false},
			{ID: "myprofile", Kind: "profile", RootPath: "/abs/leaf", External: false},
		},
		Fingerprint: state.Fingerprint{
			SchemaVersion: state.FingerprintSchemaVersion,
			Files: map[string]state.FingerprintEntry{
				"CLAUDE.md": {Size: 100, MtimeMs: 1000, ContentHash: "abc"},
			},
		},
		ExternalTrustNotices: []state.ExternalTrustNotice{},
		SourceFingerprint:    &src,
	}

	if err := state.WriteStateFile(paths, sf); err != nil {
		t.Fatalf("WriteStateFile: %v", err)
	}
	res, err := state.ReadStateFile(paths)
	if err != nil {
		t.Fatalf("ReadStateFile: %v", err)
	}
	if res.Warning != nil {
		t.Fatalf("unexpected warning: %+v", res.Warning)
	}
	if res.State.ActiveProfile == nil || *res.State.ActiveProfile != profile {
		t.Fatalf("activeProfile = %v, want %q", res.State.ActiveProfile, profile)
	}
	if res.State.MaterializedAt == nil || *res.State.MaterializedAt != mat {
		t.Fatalf("materializedAt = %v, want %q", res.State.MaterializedAt, mat)
	}
	if got := res.State.Fingerprint.Files["CLAUDE.md"]; got.Size != 100 || got.MtimeMs != 1000 || got.ContentHash != "abc" {
		t.Fatalf("fingerprint entry = %+v", got)
	}
	if res.State.SourceFingerprint == nil || res.State.SourceFingerprint.AggregateHash != "deadbeef" {
		t.Fatalf("sourceFingerprint = %+v, want non-nil with hash 'deadbeef'", res.State.SourceFingerprint)
	}
}

// TestReadStateFile_R42_ParseError covers R42's "unparseable JSON → default +
// ParseError warning" leg.
func TestReadStateFile_R42_ParseError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.MetaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(paths.StateFile, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := state.ReadStateFile(paths)
	if err != nil {
		t.Fatalf("ReadStateFile: %v", err)
	}
	if res.Warning == nil || res.Warning.Code != state.StateReadWarningParseError {
		t.Fatalf("warning = %+v, want ParseError", res.Warning)
	}
	if !reflect.DeepEqual(res.State, state.DefaultState()) {
		t.Fatalf("state = %+v, want DefaultState", res.State)
	}
}

// TestReadStateFile_R42_SchemaMismatchLowerVersion covers schema version
// LOWER than the binary supports. R42 graceful degradation: warn + default.
func TestReadStateFile_R42_SchemaMismatchLowerVersion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.MetaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(paths.StateFile, []byte(`{"schemaVersion":0}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := state.ReadStateFile(paths)
	if err != nil {
		t.Fatalf("ReadStateFile: %v", err)
	}
	if res.Warning == nil || res.Warning.Code != state.StateReadWarningSchemaMismatch {
		t.Fatalf("warning = %+v, want SchemaMismatch", res.Warning)
	}
	if !reflect.DeepEqual(res.State, state.DefaultState()) {
		t.Fatalf("state = %+v, want DefaultState", res.State)
	}
}

// TestReadStateFile_PR28_SchemaTooNew is the PR28 fitness function: a
// schemaVersion HIGHER than the binary supports must produce *SchemaTooNewError,
// NOT a graceful degrade. Silent overwrite of a newer state file would lose
// data — the user must upgrade the bin.
func TestReadStateFile_PR28_SchemaTooNew(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.MetaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// schemaVersion 99 is far ahead of the bin.
	if err := os.WriteFile(paths.StateFile, []byte(`{"schemaVersion":99}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := state.ReadStateFile(paths)
	if err == nil {
		t.Fatalf("expected SchemaTooNewError, got nil")
	}
	if !state.IsSchemaTooNewError(err) {
		t.Fatalf("error %v is not *SchemaTooNewError", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "99") || !strings.Contains(msg, "upgrade") {
		t.Fatalf("error message %q missing version or upgrade hint", msg)
	}
}

// TestReadStateFile_R42_SchemaMismatchEntryNull is the "null entry inside
// fingerprint.files" leg of R42 degradation — without the per-entry validator
// we'd panic at compareFingerprint time on a null deref.
func TestReadStateFile_R42_SchemaMismatchEntryNull(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.MetaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	bad := `{
		"schemaVersion": 1,
		"activeProfile": null,
		"materializedAt": null,
		"resolvedSources": [],
		"fingerprint": {"schemaVersion": 1, "files": {"a.md": null}},
		"externalTrustNotices": []
	}`
	if err := os.WriteFile(paths.StateFile, []byte(bad), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := state.ReadStateFile(paths)
	if err != nil {
		t.Fatalf("ReadStateFile: %v", err)
	}
	if res.Warning == nil || res.Warning.Code != state.StateReadWarningSchemaMismatch {
		t.Fatalf("warning = %+v, want SchemaMismatch", res.Warning)
	}
}

// TestWriteStateFile_NoTmpDebris asserts R14a: write does not leave staging
// files anywhere under .claude-profiles/.
func TestWriteStateFile_NoTmpDebris(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := state.WriteStateFile(paths, state.DefaultState()); err != nil {
		t.Fatalf("WriteStateFile: %v", err)
	}
	tmps := walkTmpFiles(t, paths.ProfilesDir)
	if len(tmps) != 0 {
		t.Fatalf("found %d stray .tmp files: %v", len(tmps), tmps)
	}
}

// TestFormatTimestamp_PR2 covers PR2: timestamp format must be 3-decimal
// Z-suffixed ISO-8601 matching Date.prototype.toISOString(). Neither RFC3339
// nor RFC3339Nano matches; explicit format required.
//
// Test vector: a fixed UTC instant produces the exact expected string.
func TestFormatTimestamp_PR2(t *testing.T) {
	t.Parallel()
	// 2026-04-25T12:00:00.001Z (1ms past noon UTC).
	tm := time.Date(2026, 4, 25, 12, 0, 0, 1_000_000, time.UTC)
	got := state.FormatTimestamp(tm)
	want := "2026-04-25T12:00:00.001Z"
	if got != want {
		t.Fatalf("FormatTimestamp = %q, want %q", got, want)
	}
}

// TestFormatTimestamp_AlwaysThreeDecimals enforces PR2: even a 0-millisecond
// instant emits .000Z (RFC3339Nano would strip the trailing zeros).
func TestFormatTimestamp_AlwaysThreeDecimals(t *testing.T) {
	t.Parallel()
	tm := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
	got := state.FormatTimestamp(tm)
	want := "2026-04-25T00:00:00.000Z"
	if got != want {
		t.Fatalf("FormatTimestamp = %q, want %q (PR2 requires three decimals always)", got, want)
	}
}

// TestFormatTimestamp_RoundTripsWithToISOString asserts the format matches
// what the JS side would produce. The shape is canonical and pinnable: we
// match the regex JS Date.prototype.toISOString() outputs.
func TestFormatTimestamp_RoundTripsWithToISOString(t *testing.T) {
	t.Parallel()
	pat := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)
	for i := 0; i < 5; i++ {
		s := state.FormatTimestamp(time.Now())
		if !pat.MatchString(s) {
			t.Fatalf("FormatTimestamp produced %q, does not match toISOString shape", s)
		}
	}
}

// TestStateFile_PR2_ByteIdentity is the byte-identity fitness function: a
// state file written via WriteStateFile must contain the canonical 3-decimal
// timestamp and parse identically when read back. We construct a state with
// a known timestamp string, write it, and read the raw bytes — the on-disk
// JSON must contain the exact substring.
func TestStateFile_PR2_ByteIdentity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	mat := state.FormatTimestamp(time.Date(2026, 4, 25, 12, 0, 0, 1_000_000, time.UTC))
	sf := state.DefaultState()
	sf.MaterializedAt = &mat
	if err := state.WriteStateFile(paths, sf); err != nil {
		t.Fatalf("WriteStateFile: %v", err)
	}
	raw, err := os.ReadFile(paths.StateFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), `"materializedAt": "2026-04-25T12:00:00.001Z"`) {
		t.Fatalf("on-disk state.json missing canonical timestamp; got:\n%s", string(raw))
	}
}

// TestStateFile_NoHTMLEscape covers the byte-identity fix for cross-language
// IV: Go's default encoder escapes <, >, & to <, >, &; Node's
// JSON.stringify does not. A profile name or external path containing those
// characters must round-trip byte-identical to the TS bin's output.
func TestStateFile_NoHTMLEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	profile := "ng<3>&friends"
	sf := state.DefaultState()
	sf.ActiveProfile = &profile
	if err := state.WriteStateFile(paths, sf); err != nil {
		t.Fatalf("WriteStateFile: %v", err)
	}
	raw, err := os.ReadFile(paths.StateFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Go's default Marshal escapes literal `<`, `>`, `&` to the JSON \u00xx
	// form. We want those literal bytes preserved (Node's JSON.stringify
	// behavior). The presence of the escaped form indicates the encoder
	// leaked default HTML escaping — which would break byte-identity with
	// the TS bin.
	escaped := []string{
		"\\u003c", "\\u003C", // <
		"\\u003e", "\\u003E", // >
		"\\u0026", // &
	}
	for _, e := range escaped {
		if strings.Contains(string(raw), e) {
			t.Fatalf("on-disk state.json contains JSON unicode escape %q (Go default escaping leaked); got:\n%s", e, raw)
		}
	}
	if !strings.Contains(string(raw), "\"activeProfile\": \"ng<3>&friends\"") {
		t.Fatalf("expected literal '<>&' in profile name; got:\n%s", raw)
	}
}

// TestFingerprint_NoHTMLEscapeInRelPath asserts the same byte-identity rule
// for fingerprint.files keys. .claude/ subpaths can legitimately contain
// punctuation that intersects with HTML chars.
func TestFingerprint_NoHTMLEscapeInRelPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	sf := state.DefaultState()
	sf.Fingerprint.Files["agents/<weird>&path.md"] = state.FingerprintEntry{
		Size: 1, MtimeMs: 0, ContentHash: "abc",
	}
	if err := state.WriteStateFile(paths, sf); err != nil {
		t.Fatalf("WriteStateFile: %v", err)
	}
	raw, err := os.ReadFile(paths.StateFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), "\"agents/<weird>&path.md\"") {
		t.Fatalf("expected literal '<>&' in fingerprint key; got:\n%s", raw)
	}
	for _, e := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if strings.Contains(string(raw), e) {
			t.Fatalf("fingerprint key still contains JSON unicode escape %q; got:\n%s", e, raw)
		}
	}
}

// TestReadStateFile_PR28_SchemaTooNew_FloatForm covers the secondary fix:
// a future-bin's state file may legitimately render the schemaVersion as a
// JSON number with a fractional or exponent form. PR28 must still refuse,
// not silently degrade.
func TestReadStateFile_PR28_SchemaTooNew_FloatForm(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.MetaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// 2.0 should still be > 1; the previous (int-only) decode would silently
	// fall through to validateStateShape and degrade to DefaultState.
	if err := os.WriteFile(paths.StateFile, []byte(`{"schemaVersion":2.0}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := state.ReadStateFile(paths)
	if err == nil {
		t.Fatalf("expected SchemaTooNewError, got nil")
	}
	if !state.IsSchemaTooNewError(err) {
		t.Fatalf("error %v is not *SchemaTooNewError", err)
	}
}

func walkTmpFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".tmp") {
			out = append(out, p)
		}
		return nil
	})
	return out
}
