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
// compact form WITH a single trailing newline (json.Encoder.Encode appends
// one and we keep it intact, so the result is "<compact JSON>\n"). Callers
// (output.JSON()) write these bytes verbatim and MUST NOT append a second
// newline. Errors propagate from the encoder — JSON marshal failures are
// programmer errors and the CLI surfaces them via the OutputChannel.
func Marshal(payload interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MarshalLine is currently an alias for Marshal — both already include the
// terminating "\n" because json.Encoder.Encode appends one. Kept as a
// distinct entry point for callers that want to make the line-terminated
// contract explicit at the call site.
func MarshalLine(payload interface{}) ([]byte, error) {
	return Marshal(payload)
}
