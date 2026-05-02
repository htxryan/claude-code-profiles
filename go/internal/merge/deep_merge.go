package merge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
)

// DeepMergeStrategy implements R8 (deep-merge for settings.json) plus the
// R12 carve-out (hooks-by-event arrays concatenated at depth 2).
//
// Default semantics (R8):
//   - Objects merge recursively.
//   - Arrays at the same path are REPLACED by the later contributor.
//   - Scalars (string/number/boolean/null) — later wins.
//   - Type mismatches (e.g. object vs array) — later wins.
//
// Carve-out (R12, takes precedence over R8):
//   - At the path hooks.<EventName>, action arrays are CONCATENATED in
//     contributor order rather than replaced. The surrounding hooks object
//     still deep-merges so different events from different contributors
//     accumulate without clobbering. R12 fires only when both sides at
//     hooks.<EventName> are arrays.
//
// Output bytes mirror TS's `JSON.stringify(acc, null, 2) + "\n"`:
//   - Object keys are emitted in insertion order (NOT sorted) so byte-equal
//     output across the TS and Go runtimes is preserved for identical
//     contributor inputs (load-bearing for R18 drift content-hash gates).
//   - Numbers are parsed as float64 (mirroring JSON.parse), so `1.0` →
//     `1`, integers above 2^53 lose precision the same way they would in
//     TS. Numbers serialize via `encoding/json`, which already implements
//     ES6's fixed-vs-exponent rule (fixed in [1e-6, 1e21), exponential
//     outside) and de-pads exponents — matching JS Number.prototype
//     .toString byte-for-byte.
//
// Empty / whitespace-only contributors parse to an empty object — they
// add nothing semantically but are retained in `Contributors` for E5
// provenance (asymmetry with ConcatStrategy is intentional; see comment
// below).
func DeepMergeStrategy(relPath string, inputs []ContributorBytes) (StrategyResult, error) {
	if len(inputs) == 0 {
		return StrategyResult{}, fmt.Errorf("deep-merge invoked with no inputs for %q", relPath)
	}

	parsed := make([]*orderedObject, len(inputs))
	contributors := make([]string, len(inputs))
	for i, in := range inputs {
		text := strings.TrimSpace(string(in.Bytes))
		if text == "" {
			// Empty / whitespace-only file → empty {}. Asymmetry with
			// ConcatStrategy (which drops empty contributors entirely) is
			// deliberate: a settings.json `{}` is a meaningful "I declare
			// this file but have no overrides" signal that downstream
			// provenance (E5) should still attribute, whereas an empty
			// markdown contributor would just inject a blank line.
			parsed[i] = newOrderedObject()
			contributors[i] = in.ID
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(in.Bytes))
		value, err := decodeJSONValue(dec)
		if err != nil {
			return StrategyResult{}, pipelineerrors.NewInvalidSettingsJsonError(relPath, in.ID, err.Error())
		}
		// Reject trailing JSON values so a contributor's settings.json
		// containing `{"a":1}{"b":2}` fails loudly instead of silently
		// dropping the second object.
		if dec.More() {
			return StrategyResult{}, pipelineerrors.NewInvalidSettingsJsonError(
				relPath, in.ID, "trailing data after JSON value",
			)
		}
		obj, ok := value.(*orderedObject)
		if !ok {
			return StrategyResult{}, pipelineerrors.NewInvalidSettingsJsonError(
				relPath, in.ID, "top-level value is not a JSON object",
			)
		}
		parsed[i] = obj
		contributors[i] = in.ID
	}

	acc := cloneOrderedValue(parsed[0]).(*orderedObject)
	for i := 1; i < len(parsed); i++ {
		merged := mergeAt(nil, acc, parsed[i])
		// Accumulator at depth 0 is always an object (R8 invariant: a
		// non-object later contributor would have been rejected upstream
		// as InvalidSettingsJson). Cast is safe.
		acc = merged.(*orderedObject)
	}

	var buf bytes.Buffer
	if err := encodeJSONValue(&buf, acc, 0); err != nil {
		return StrategyResult{}, fmt.Errorf("encoding merged settings %q: %w", relPath, err)
	}
	buf.WriteByte('\n') // mirror TS `JSON.stringify(_, null, 2) + "\n"`

	return StrategyResult{
		Bytes:        buf.Bytes(),
		Contributors: contributors,
	}, nil
}

// mergeAt recursively merges later into earlier. Returns a new value (no
// in-place mutation of inputs). pathParts is the dotted path used to detect
// the R12 carve-out at hooks.<EventName>.
func mergeAt(pathParts []string, earlier, later any) any {
	// R12 carve-out: at hooks.<EventName>, concat arrays instead of replacing.
	if len(pathParts) == 2 && pathParts[0] == "hooks" {
		ea, eaOK := earlier.([]any)
		la, laOK := later.([]any)
		if eaOK && laOK {
			out := make([]any, 0, len(ea)+len(la))
			out = append(out, ea...)
			out = append(out, la...)
			return cloneArray(out)
		}
	}

	// Both objects → deep merge field-by-field, preserving insertion order.
	earlierObj, earlierOK := earlier.(*orderedObject)
	laterObj, laterOK := later.(*orderedObject)
	if earlierOK && laterOK {
		out := cloneObject(earlierObj)
		for _, k := range laterObj.keys {
			v := laterObj.values[k]
			if existing, ok := out.get(k); ok {
				// Always allocate a fresh path slice rather than relying on
				// append's no-aliasing behavior in the sequential case.
				next := make([]string, len(pathParts), len(pathParts)+1)
				copy(next, pathParts)
				next = append(next, k)
				out.set(k, mergeAt(next, existing, v))
			} else {
				out.set(k, cloneOrderedValue(v))
			}
		}
		return out
	}

	// R8 default: arrays replace, scalars/type-mismatch — later wins.
	return cloneOrderedValue(later)
}

// ─── orderedObject — JSON objects that preserve key insertion order ──────

// orderedObject is the deep-merge engine's stand-in for `map[string]any` —
// it preserves key insertion order so the encoded bytes mirror TS
// `JSON.stringify` (which iterates own-string-key properties in insertion
// order). Without this, Go's default `map` iteration would alphabetize
// keys, defeating byte-parity gates.
type orderedObject struct {
	keys   []string
	values map[string]any
}

func newOrderedObject() *orderedObject {
	return &orderedObject{values: map[string]any{}}
}

func (o *orderedObject) get(k string) (any, bool) {
	v, ok := o.values[k]
	return v, ok
}

func (o *orderedObject) set(k string, v any) {
	if _, exists := o.values[k]; !exists {
		o.keys = append(o.keys, k)
	}
	o.values[k] = v
}

// ─── parsing — JSON → orderedObject / []any / scalars ───────────────────

func decodeJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeFromToken(dec, tok)
}

func decodeFromToken(dec *json.Decoder, tok json.Token) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := newOrderedObject()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("expected string object key, got %T", keyTok)
				}
				v, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				obj.set(key, v)
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return arr, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", t)
		}
	case bool, string, float64, nil:
		return t, nil
	default:
		return nil, fmt.Errorf("unexpected token %T", tok)
	}
}

// ─── encoding — orderedObject → JSON bytes (TS JSON.stringify parity) ───

func encodeJSONValue(buf *bytes.Buffer, v any, indent int) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case float64:
		buf.WriteString(formatJSNumber(t))
	case string:
		writeJSONString(buf, t)
	case []any:
		return encodeJSONArray(buf, t, indent)
	case *orderedObject:
		return encodeJSONObject(buf, t, indent)
	default:
		return fmt.Errorf("unsupported value type %T", v)
	}
	return nil
}

func encodeJSONArray(buf *bytes.Buffer, arr []any, indent int) error {
	if len(arr) == 0 {
		buf.WriteString("[]")
		return nil
	}
	pad := strings.Repeat("  ", indent)
	childPad := strings.Repeat("  ", indent+1)
	buf.WriteString("[\n")
	for i, el := range arr {
		buf.WriteString(childPad)
		if err := encodeJSONValue(buf, el, indent+1); err != nil {
			return err
		}
		if i < len(arr)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(pad)
	buf.WriteByte(']')
	return nil
}

func encodeJSONObject(buf *bytes.Buffer, obj *orderedObject, indent int) error {
	if len(obj.keys) == 0 {
		buf.WriteString("{}")
		return nil
	}
	pad := strings.Repeat("  ", indent)
	childPad := strings.Repeat("  ", indent+1)
	buf.WriteString("{\n")
	for i, k := range obj.keys {
		buf.WriteString(childPad)
		writeJSONString(buf, k)
		buf.WriteString(": ")
		if err := encodeJSONValue(buf, obj.values[k], indent+1); err != nil {
			return err
		}
		if i < len(obj.keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(pad)
	buf.WriteByte('}')
	return nil
}

// writeJSONString writes a JSON-escaped string. Uses encoding/json with
// HTML escaping disabled so `<`, `>`, `&` round-trip verbatim like
// TS's JSON.stringify.
//
// Residual divergence: Go's encoder still escapes U+2028 / U+2029 (JS line
// separators) even with SetEscapeHTML(false); JS JSON.stringify does not
// escape them. Settings files are not expected to contain these code
// points, so we accept the divergence.
func writeJSONString(buf *bytes.Buffer, s string) {
	var inner bytes.Buffer
	enc := json.NewEncoder(&inner)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // appends '\n'
	out := inner.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	buf.Write(out)
}

// formatJSNumber formats a float64 the way JS Number.prototype.toString
// (and therefore JSON.stringify) does. NaN / +Inf / -Inf serialize as
// "null" per the JSON.stringify spec.
//
// Go's `encoding/json` already implements the ES6 fixed-vs-exponent rule
// (fixed in [1e-6, 1e21), exponential outside) and de-pads exponents
// (e.g. `1e-07` → `1e-7`), so json.Marshal gives full byte parity with
// JS for all finite floats. We short-circuit NaN/Inf (json.Marshal would
// error) and -0 (json.Marshal preserves the sign; JS collapses to "0").
func formatJSNumber(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "null"
	}
	if f == 0 {
		return "0" // collapse -0 → "0" like JS
	}
	b, err := json.Marshal(f)
	if err != nil {
		// Unreachable: NaN/Inf are the only errors json.Marshal returns
		// for float64, and we handled them above.
		return "null"
	}
	return string(b)
}

// ─── clone helpers ──────────────────────────────────────────────────────

func cloneOrderedValue(v any) any {
	switch t := v.(type) {
	case *orderedObject:
		return cloneObject(t)
	case []any:
		return cloneArray(t)
	default:
		return v
	}
}

func cloneObject(in *orderedObject) *orderedObject {
	if in == nil {
		return nil
	}
	out := newOrderedObject()
	for _, k := range in.keys {
		out.set(k, cloneOrderedValue(in.values[k]))
	}
	return out
}

func cloneArray(in []any) []any {
	if in == nil {
		return nil
	}
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = cloneOrderedValue(v)
	}
	return out
}
