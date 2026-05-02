package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"time"
)

// StateFileSchemaVersion is the schema stamp for state.json. Bumped only when
// consumers (D6/D7) must update for a breaking shape change.
const StateFileSchemaVersion = 1

// ResolvedSourceRef is one contributor source recorded at materialize time.
// D7 status/list use this to render provenance without re-running the
// resolver. Subset of resolver.Contributor — only what's needed downstream.
type ResolvedSourceRef struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	RootPath string `json:"rootPath"`
	External bool   `json:"external"`
}

// ExternalTrustNotice records that an external-trust notice has been printed
// for a resolved external path so we don't re-print on every swap (R37a).
type ExternalTrustNotice struct {
	Raw          string `json:"raw"`
	ResolvedPath string `json:"resolvedPath"`
	NoticedAt    string `json:"noticedAt"`
}

// StateFile is the on-disk shape of .claude-profiles/.meta/state.json (R14,
// R14a, R42).
//
// Always written via temp+rename so partial writes are not observable.
// ActiveProfile == "" (the JSON null) means "no active profile" — either init
// ran but no use occurred, or the file was unparseable / schema-mismatched
// and treated as NoActive (R42).
//
// Shape mirrors src/state/types.ts:StateFile so the IV harness compares
// byte-for-byte across languages. Pointer fields encode JSON null distinctly
// from "field missing"; legacy-tolerant reads accept missing optional fields.
type StateFile struct {
	SchemaVersion        int                   `json:"schemaVersion"`
	ActiveProfile        *string               `json:"activeProfile"`
	MaterializedAt       *string               `json:"materializedAt"`
	ResolvedSources      []ResolvedSourceRef   `json:"resolvedSources"`
	Fingerprint          Fingerprint           `json:"fingerprint"`
	ExternalTrustNotices []ExternalTrustNotice `json:"externalTrustNotices"`
	// Optional fields (legacy state files written before cw6/azp lack them).
	// Use pointers so we can distinguish absent from explicit null.
	RootClaudeMdSection *SectionFingerprint `json:"rootClaudeMdSection"`
	SourceFingerprint   *SourceFingerprint  `json:"sourceFingerprint"`
}

// fingerprintJSON is the JSON shape for Fingerprint. Files is rendered as a
// JSON object keyed on relPath; MarshalJSON sorts keys lexicographically so
// byte-output is stable across runs.
type fingerprintJSON struct {
	SchemaVersion int                             `json:"schemaVersion"`
	Files         map[string]fingerprintEntryJSON `json:"files"`
}

type fingerprintEntryJSON struct {
	Size        int64  `json:"size"`
	MtimeMs     int64  `json:"mtimeMs"`
	ContentHash string `json:"contentHash"`
}

type sectionFingerprintJSON struct {
	Size        int64  `json:"size"`
	ContentHash string `json:"contentHash"`
}

type sourceFingerprintJSON struct {
	FileCount     int    `json:"fileCount"`
	AggregateHash string `json:"aggregateHash"`
}

// MarshalJSON produces the canonical TS-compatible encoding of Fingerprint.
// Files keys are sorted; entry fields are emitted in the canonical order
// (size, mtimeMs, contentHash) matching JSON.stringify on the TS side.
//
// Uses encodeNoHTMLEscape for individual fields so a relPath containing `<`,
// `>`, or `&` (legitimate in some paths under .claude/) round-trips
// byte-identical to Node's JSON.stringify.
func (f Fingerprint) MarshalJSON() ([]byte, error) {
	keys := make([]string, 0, len(f.Files))
	for k := range f.Files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteByte('{')
	buf.WriteString(`"schemaVersion":`)
	v, err := encodeNoHTMLEscape(f.SchemaVersion)
	if err != nil {
		return nil, err
	}
	buf.Write(v)
	buf.WriteString(`,"files":{`)
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kj, err := encodeNoHTMLEscape(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kj)
		buf.WriteByte(':')
		ej, err := encodeNoHTMLEscape(fingerprintEntryJSON{
			Size:        f.Files[k].Size,
			MtimeMs:     f.Files[k].MtimeMs,
			ContentHash: f.Files[k].ContentHash,
		})
		if err != nil {
			return nil, err
		}
		buf.Write(ej)
	}
	buf.WriteByte('}')
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// encodeNoHTMLEscape marshals v with HTML escaping disabled (so `<`/`>`/`&`
// are preserved literal — matching Node's JSON.stringify default). The
// encoder appends a trailing newline that we strip; the result is the
// compact JSON form suitable for inline embedding.
func encodeNoHTMLEscape(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	// Encoder always emits a trailing '\n'; strip for inline use.
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out, nil
}

// UnmarshalJSON parses the TS-compatible encoding of Fingerprint and
// validates per-entry shape (R42 graceful degradation: malformed entries
// surface as a typed validation failure to the reader, which downgrades to
// defaultState).
func (f *Fingerprint) UnmarshalJSON(b []byte) error {
	var raw fingerprintJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	f.SchemaVersion = raw.SchemaVersion
	if raw.Files == nil {
		f.Files = map[string]FingerprintEntry{}
		return nil
	}
	out := make(map[string]FingerprintEntry, len(raw.Files))
	for k, v := range raw.Files {
		out[k] = FingerprintEntry{
			Size:        v.Size,
			MtimeMs:     v.MtimeMs,
			ContentHash: v.ContentHash,
		}
	}
	f.Files = out
	return nil
}

// MarshalJSON for SectionFingerprint emits {size,contentHash}. HTML escape
// disabled for byte-identity with JSON.stringify (the contentHash is hex so
// no special chars, but we keep the discipline uniform).
func (s SectionFingerprint) MarshalJSON() ([]byte, error) {
	return encodeNoHTMLEscape(sectionFingerprintJSON{
		Size:        s.Size,
		ContentHash: s.ContentHash,
	})
}

func (s *SectionFingerprint) UnmarshalJSON(b []byte) error {
	var raw sectionFingerprintJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	s.Size = raw.Size
	s.ContentHash = raw.ContentHash
	return nil
}

// MarshalJSON for SourceFingerprint emits {fileCount,aggregateHash}. HTML
// escape disabled for byte-identity discipline (the aggregateHash is hex but
// we apply the rule uniformly to all custom marshallers).
func (s SourceFingerprint) MarshalJSON() ([]byte, error) {
	return encodeNoHTMLEscape(sourceFingerprintJSON{
		FileCount:     s.FileCount,
		AggregateHash: s.AggregateHash,
	})
}

func (s *SourceFingerprint) UnmarshalJSON(b []byte) error {
	var raw sourceFingerprintJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	s.FileCount = raw.FileCount
	s.AggregateHash = raw.AggregateHash
	return nil
}

// DefaultState returns the "NoActive" StateFile — used when the file doesn't
// exist, fails to parse, has a schema mismatch, or is otherwise invalid (R42
// graceful degradation).
func DefaultState() StateFile {
	return StateFile{
		SchemaVersion:        StateFileSchemaVersion,
		ActiveProfile:        nil,
		MaterializedAt:       nil,
		ResolvedSources:      []ResolvedSourceRef{},
		Fingerprint:          Fingerprint{SchemaVersion: FingerprintSchemaVersion, Files: map[string]FingerprintEntry{}},
		ExternalTrustNotices: []ExternalTrustNotice{},
		RootClaudeMdSection:  nil,
		SourceFingerprint:    nil,
	}
}

// StateReadWarningCode enumerates the recoverable read failures (R42).
type StateReadWarningCode string

const (
	StateReadWarningMissing        StateReadWarningCode = "Missing"
	StateReadWarningParseError     StateReadWarningCode = "ParseError"
	StateReadWarningSchemaMismatch StateReadWarningCode = "SchemaMismatch"
)

// StateReadWarning is the recoverable-read result. Path names the file we
// tried to read; Detail explains the failure (empty for Missing).
type StateReadWarning struct {
	Code   StateReadWarningCode
	Path   string
	Detail string
}

// SchemaTooNewError is raised when state.json carries a schemaVersion higher
// than this binary supports (PR28). Refuse to operate so the user doesn't
// silently lose data — the older bin would otherwise overwrite a newer-format
// state file with its own narrower shape.
//
// Distinct from StateReadWarningSchemaMismatch (which is the legacy/lower-
// version path that R42 graceful-degrades): a higher-than-supported version
// is an upgrade-required signal, not a corrupt-file signal.
//
// OnDiskRaw preserves the exact bytes of the schemaVersion field (e.g. "2.0",
// "1e3", "1.5") so the rendered error message remains diagnostic when the
// value is fractional, infinite, or NaN — int truncation would otherwise
// produce a self-contradictory "has schemaVersion 1 but binary supports up to 1".
type SchemaTooNewError struct {
	Path        string
	OnDisk      int
	OnDiskRaw   string
	BinMaxKnown int
}

func (e *SchemaTooNewError) Error() string {
	return fmt.Sprintf(
		"State file at %q has schemaVersion %s but this binary supports up to %d — refusing to operate to prevent data loss; please upgrade c3p to a version that knows about schema %s.",
		e.Path, e.OnDiskRaw, e.BinMaxKnown, e.OnDiskRaw,
	)
}

// ReadStateResult is the outcome of ReadStateFile. Warning is non-nil when
// the on-disk file was missing/unparseable/schema-mismatched and we degraded
// to DefaultState (R42).
type ReadStateResult struct {
	State   StateFile
	Warning *StateReadWarning
}

// ReadStateFile reads .state.json, validating shape. Never returns an error
// for file-content problems (those produce DefaultState + warning per R42).
// Filesystem errors other than ENOENT (permission denied, IO error) are
// surfaced.
//
// PR28: a schemaVersion strictly greater than StateFileSchemaVersion produces
// a *SchemaTooNewError instead of degrading to DefaultState — that path
// would silently downgrade an upgraded user's state file on the next write.
func ReadStateFile(paths StatePaths) (ReadStateResult, error) {
	raw, err := os.ReadFile(paths.StateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ReadStateResult{
				State:   DefaultState(),
				Warning: &StateReadWarning{Code: StateReadWarningMissing, Path: paths.StateFile},
			}, nil
		}
		return ReadStateResult{}, err
	}

	// First pass: peek at schemaVersion alone so PR28 can fire before a full
	// shape validation runs. A schema-too-new file may legitimately have
	// fields the current code doesn't model; refusing up front avoids a
	// confusing "missing field" error message that obscures the real cause.
	//
	// Decode as json.Number first (preserves arbitrary precision) and parse
	// as float64 — so 2.0, 1e2, and integer-overflow forms all compare
	// correctly against StateFileSchemaVersion. PR28 says "refuse on schema
	// strictly greater"; a future bin's value of 2 must always lose to a
	// strictly-greater check regardless of how the JSON was rendered.
	var peek struct {
		SchemaVersion json.Number `json:"schemaVersion"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&peek); err != nil {
		return ReadStateResult{
			State: DefaultState(),
			Warning: &StateReadWarning{
				Code:   StateReadWarningParseError,
				Path:   paths.StateFile,
				Detail: err.Error(),
			},
		}, nil
	}
	if peek.SchemaVersion != "" {
		// json.Number.Float64 accepts 2, 2.0, 1e2 — anything JSON.parse on
		// the JS side would interpret as a number. The strictly-greater check
		// covers fractional and exponent forms (1e400 → +Inf, +Inf > 1) so the
		// refuse-to-operate gate fires for any future-bin shape.
		//
		// strconv.ErrRange (overflow forms like 1e400) returns +Inf alongside
		// the error; we treat that as a schema-too-new signal (any binary that
		// emitted such a value is unmistakably newer than us). Other parse
		// errors fall through to validateStateShape, which surfaces a
		// SchemaMismatch warning with the raw bytes.
		//
		// Only the comparison runs in float space; OnDisk uses math.Round so
		// state files with values like 1.5 don't render as "schemaVersion 1
		// but bin supports 1" in the error message. OnDiskRaw preserves the
		// exact JSON bytes for the user-facing message so Inf/NaN/exponent
		// forms remain diagnostic.
		v, err := peek.SchemaVersion.Float64()
		overflowed := errors.Is(err, strconv.ErrRange)
		if err == nil || overflowed {
			if v > float64(StateFileSchemaVersion) || math.IsNaN(v) {
				onDisk := 0
				if !math.IsInf(v, 0) && !math.IsNaN(v) {
					onDisk = int(math.Round(v))
				}
				return ReadStateResult{}, &SchemaTooNewError{
					Path:        paths.StateFile,
					OnDisk:      onDisk,
					OnDiskRaw:   peek.SchemaVersion.String(),
					BinMaxKnown: StateFileSchemaVersion,
				}
			}
		}
	}

	parsed, warn := validateStateShape(raw, paths.StateFile)
	if warn != nil {
		return ReadStateResult{State: DefaultState(), Warning: warn}, nil
	}
	return ReadStateResult{State: parsed, Warning: nil}, nil
}

// validateStateShape enforces the StateFile schema. We accept only the
// current SchemaVersion; mismatches produce a SchemaMismatch warning so we
// degrade gracefully (R42). The check is intentionally narrow — only enough
// structure to safely consume the file.
func validateStateShape(raw []byte, path string) (StateFile, *StateReadWarning) {
	bad := func(detail string) *StateReadWarning {
		return &StateReadWarning{
			Code:   StateReadWarningSchemaMismatch,
			Path:   path,
			Detail: detail,
		}
	}
	// We use a hand-rolled validator over the raw map so a per-entry shape
	// problem (e.g. fingerprint.files[k].size missing) lands in the same
	// SchemaMismatch warning rather than a Go-specific decoding error.
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return StateFile{}, &StateReadWarning{
			Code:   StateReadWarningParseError,
			Path:   path,
			Detail: err.Error(),
		}
	}
	if generic == nil {
		return StateFile{}, bad("top-level value is not a JSON object")
	}

	var sv int
	if err := json.Unmarshal(generic["schemaVersion"], &sv); err != nil {
		return StateFile{}, bad(fmt.Sprintf("schemaVersion %s (expected %d)", string(generic["schemaVersion"]), StateFileSchemaVersion))
	}
	if sv != StateFileSchemaVersion {
		return StateFile{}, bad(fmt.Sprintf("schemaVersion %d (expected %d)", sv, StateFileSchemaVersion))
	}

	var sf StateFile
	sf.SchemaVersion = sv

	// activeProfile: string | null
	if rawAP, ok := generic["activeProfile"]; ok {
		if string(rawAP) == "null" {
			sf.ActiveProfile = nil
		} else {
			var s string
			if err := json.Unmarshal(rawAP, &s); err != nil {
				return StateFile{}, bad("activeProfile must be string or null")
			}
			sf.ActiveProfile = &s
		}
	} else {
		return StateFile{}, bad("activeProfile must be string or null")
	}

	if rawMA, ok := generic["materializedAt"]; ok {
		if string(rawMA) == "null" {
			sf.MaterializedAt = nil
		} else {
			var s string
			if err := json.Unmarshal(rawMA, &s); err != nil {
				return StateFile{}, bad("materializedAt must be string or null")
			}
			sf.MaterializedAt = &s
		}
	} else {
		return StateFile{}, bad("materializedAt must be string or null")
	}

	rawRS, hasRS := generic["resolvedSources"]
	if !hasRS || len(rawRS) == 0 || rawRS[0] != '[' {
		return StateFile{}, bad("resolvedSources must be an array")
	}
	if err := json.Unmarshal(rawRS, &sf.ResolvedSources); err != nil {
		return StateFile{}, bad(fmt.Sprintf("resolvedSources: %s", err.Error()))
	}

	rawFP, hasFP := generic["fingerprint"]
	if !hasFP || len(rawFP) == 0 || rawFP[0] != '{' {
		return StateFile{}, bad("fingerprint must be a JSON object")
	}
	var fpMap map[string]json.RawMessage
	if err := json.Unmarshal(rawFP, &fpMap); err != nil {
		return StateFile{}, bad(fmt.Sprintf("fingerprint: %s", err.Error()))
	}
	var fpv int
	if err := json.Unmarshal(fpMap["schemaVersion"], &fpv); err != nil {
		return StateFile{}, bad(fmt.Sprintf("fingerprint.schemaVersion %s (expected %d)", string(fpMap["schemaVersion"]), FingerprintSchemaVersion))
	}
	if fpv != FingerprintSchemaVersion {
		return StateFile{}, bad(fmt.Sprintf("fingerprint.schemaVersion %d (expected %d)", fpv, FingerprintSchemaVersion))
	}
	rawFiles, hasFiles := fpMap["files"]
	if !hasFiles || len(rawFiles) == 0 || rawFiles[0] != '{' {
		return StateFile{}, bad("fingerprint.files must be a JSON object")
	}
	var filesMap map[string]json.RawMessage
	if err := json.Unmarshal(rawFiles, &filesMap); err != nil {
		return StateFile{}, bad(fmt.Sprintf("fingerprint.files: %s", err.Error()))
	}
	files := make(map[string]FingerprintEntry, len(filesMap))
	for k, v := range filesMap {
		if len(v) == 0 || v[0] != '{' {
			return StateFile{}, bad(fmt.Sprintf("fingerprint.files[%q] must be an object", k))
		}
		var entryMap map[string]json.RawMessage
		if err := json.Unmarshal(v, &entryMap); err != nil {
			return StateFile{}, bad(fmt.Sprintf("fingerprint.files[%q]: %s", k, err.Error()))
		}
		var size float64
		if err := json.Unmarshal(entryMap["size"], &size); err != nil {
			return StateFile{}, bad(fmt.Sprintf("fingerprint.files[%q].size must be a number", k))
		}
		var mtime float64
		if err := json.Unmarshal(entryMap["mtimeMs"], &mtime); err != nil {
			return StateFile{}, bad(fmt.Sprintf("fingerprint.files[%q].mtimeMs must be a number", k))
		}
		var hash string
		if err := json.Unmarshal(entryMap["contentHash"], &hash); err != nil {
			return StateFile{}, bad(fmt.Sprintf("fingerprint.files[%q].contentHash must be a string", k))
		}
		files[k] = FingerprintEntry{
			Size:        int64(size),
			MtimeMs:     int64(mtime),
			ContentHash: hash,
		}
	}
	sf.Fingerprint = Fingerprint{SchemaVersion: fpv, Files: files}

	rawETN, hasETN := generic["externalTrustNotices"]
	if !hasETN || len(rawETN) == 0 || rawETN[0] != '[' {
		return StateFile{}, bad("externalTrustNotices must be an array")
	}
	if err := json.Unmarshal(rawETN, &sf.ExternalTrustNotices); err != nil {
		return StateFile{}, bad(fmt.Sprintf("externalTrustNotices: %s", err.Error()))
	}

	// Optional: rootClaudeMdSection
	if rawSec, ok := generic["rootClaudeMdSection"]; ok && string(rawSec) != "null" {
		if len(rawSec) == 0 || rawSec[0] != '{' {
			return StateFile{}, bad("rootClaudeMdSection must be an object or null")
		}
		var secMap map[string]json.RawMessage
		if err := json.Unmarshal(rawSec, &secMap); err != nil {
			return StateFile{}, bad(fmt.Sprintf("rootClaudeMdSection: %s", err.Error()))
		}
		var size float64
		if err := json.Unmarshal(secMap["size"], &size); err != nil {
			return StateFile{}, bad("rootClaudeMdSection.size must be a number")
		}
		var hash string
		if err := json.Unmarshal(secMap["contentHash"], &hash); err != nil {
			return StateFile{}, bad("rootClaudeMdSection.contentHash must be a string")
		}
		sf.RootClaudeMdSection = &SectionFingerprint{Size: int64(size), ContentHash: hash}
	}

	// Optional: sourceFingerprint
	if rawSrc, ok := generic["sourceFingerprint"]; ok && string(rawSrc) != "null" {
		if len(rawSrc) == 0 || rawSrc[0] != '{' {
			return StateFile{}, bad("sourceFingerprint must be an object or null")
		}
		var srcMap map[string]json.RawMessage
		if err := json.Unmarshal(rawSrc, &srcMap); err != nil {
			return StateFile{}, bad(fmt.Sprintf("sourceFingerprint: %s", err.Error()))
		}
		var fc float64
		if err := json.Unmarshal(srcMap["fileCount"], &fc); err != nil {
			return StateFile{}, bad("sourceFingerprint.fileCount must be a number")
		}
		var hash string
		if err := json.Unmarshal(srcMap["aggregateHash"], &hash); err != nil {
			return StateFile{}, bad("sourceFingerprint.aggregateHash must be a string")
		}
		sf.SourceFingerprint = &SourceFingerprint{FileCount: int(fc), AggregateHash: hash}
	}

	return sf, nil
}

// MarshalJSON for StateFile produces the canonical key order matching the TS
// implementation's JSON.stringify output, so a state file written by the Go
// bin can be byte-compared against the TS bin (cross-language IV gate).
//
// Order: schemaVersion, activeProfile, materializedAt, resolvedSources,
// fingerprint, externalTrustNotices, rootClaudeMdSection, sourceFingerprint.
//
// Uses encodeNoHTMLEscape for inner field rendering so a profile name or
// external path containing `<`, `>`, `&` round-trips byte-identical to JS
// (Go's default Marshal escapes those to \u00XX). The encoder-with-
// SetEscapeHTML-false path on the OUTER encoder doesn't help here because
// our MarshalJSON's return value is inserted verbatim by the encoder.
func (s StateFile) MarshalJSON() ([]byte, error) {
	rs := s.ResolvedSources
	if rs == nil {
		rs = []ResolvedSourceRef{}
	}
	etn := s.ExternalTrustNotices
	if etn == nil {
		etn = []ExternalTrustNotice{}
	}
	if s.Fingerprint.Files == nil {
		s.Fingerprint.Files = map[string]FingerprintEntry{}
	}

	type field struct {
		name string
		v    interface{}
	}
	fields := []field{
		{"schemaVersion", s.SchemaVersion},
		{"activeProfile", s.ActiveProfile},
		{"materializedAt", s.MaterializedAt},
		{"resolvedSources", rs},
		{"fingerprint", s.Fingerprint},
		{"externalTrustNotices", etn},
		{"rootClaudeMdSection", s.RootClaudeMdSection},
		{"sourceFingerprint", s.SourceFingerprint},
	}

	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		nameJSON, err := encodeNoHTMLEscape(f.name)
		if err != nil {
			return nil, err
		}
		buf.Write(nameJSON)
		buf.WriteByte(':')
		valueJSON, err := encodeNoHTMLEscape(f.v)
		if err != nil {
			return nil, err
		}
		buf.Write(valueJSON)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// WriteStateFile atomically writes state.json (R14a). Ensures .meta/tmp/
// exists, writes to a unique tmp, fsyncs, and renames into place. Output is
// pretty-printed (2-space indent) so an accidentally checked-in file is
// readable in `git diff`. Trailing newline for tool friendliness.
func WriteStateFile(paths StatePaths, state StateFile) error {
	if err := os.MkdirAll(paths.TmpDir, 0o755); err != nil {
		return err
	}
	data, err := MarshalStateFileJSON(state)
	if err != nil {
		return err
	}
	tmpPath := UniqueAtomicTmpPath(paths.TmpDir, paths.StateFile)
	if err := AtomicWriteFile(paths.StateFile, tmpPath, data); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// MarshalStateFileJSON returns the on-disk byte form of state.json. Exposed
// for byte-identity tests and for any caller that wants to hash the canonical
// form without going through disk.
//
// Uses json.Encoder with SetEscapeHTML(false) so output matches Node's
// JSON.stringify byte-for-byte: Go's default encoder escapes <, >, & to
// </>/&, while JS does not. The cross-language IV gate
// requires byte-identity for any path/profile name containing those bytes.
//
// json.Encoder.Encode appends a trailing newline already; we don't add one.
func MarshalStateFileJSON(state StateFile) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(state); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FormatTimestamp produces a 3-decimal Z-suffixed ISO-8601 timestamp matching
// JS Date.prototype.toISOString() (PR2). Neither RFC3339 nor RFC3339Nano
// matches: RFC3339 omits sub-second; RFC3339Nano emits up to 9 decimals and
// strips trailing zeros. The TS bin writes exactly 3 fractional digits (ms),
// always Z-suffixed, with NO timezone offset. We pin that format here so a
// state file written by Go is byte-identical to one written by Node.
//
// Format: 2006-01-02T15:04:05.000Z
func FormatTimestamp(t time.Time) string {
	utc := t.UTC()
	// time.Format ".000" emits exactly three fractional digits, padded with
	// zeros. The literal Z suffix replaces the offset because UTC time.Format
	// would otherwise emit "+0000" (and "-07:00" off-UTC).
	return utc.Format("2006-01-02T15:04:05.000Z")
}
