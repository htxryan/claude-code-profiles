// Package jsonout is the single deterministic JSON marshaller for every CLI
// envelope. The CI lint script (scripts/jsonout_lint.sh) forbids any
// json.Marshal call outside this package — that's how we enforce PR3
// (one place to change byte format, one place to fix HTML escaping).
//
// Output discipline:
//   - HTML escaping is OFF (Go's default escapes <,>,& as <, etc.; JS
//     JSON.stringify does not. The dual-suite IV harness compares byte-for-
//     byte across both binaries, so this must match).
//   - Output is compact (no indentation): one line per envelope, terminated
//     with `\n` so consumers can `| jq -s 'add'` or stream line-by-line.
//   - Map keys are NOT pre-sorted by this package — call sites that need a
//     stable map iteration must use the explicit ordered shape (e.g. a slice
//     of {Key, Value} pairs) rather than relying on encoding/json's
//     post-Go1.12 map-key sorting (which is what we actually want here).
package jsonout

import (
	"bytes"
	"encoding/json"
)

// Marshal returns the canonical wire bytes for a CLI envelope. Output is the
// compact form WITHOUT a trailing newline; callers (output.JSON()) append the
// newline. Errors propagate from the encoder — JSON marshal failures are
// programmer errors and the CLI surfaces them via the OutputChannel.
func Marshal(payload interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	// json.Encoder.Encode appends a trailing newline; the line-form we want
	// is "<compact JSON>\n", so we keep that newline intact.
	return out, nil
}

// MarshalLine is a convenience that returns the marshaled bytes already
// terminated with a single \n. It exists so the OutputChannel never appends
// a second newline by mistake (json.Encoder.Encode adds one already).
func MarshalLine(payload interface{}) ([]byte, error) {
	return Marshal(payload)
}
