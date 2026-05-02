package merge

import (
	"bytes"
	"encoding/json"
	"fmt"
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
// Output bytes are json.MarshalIndent with two-space indent followed by a
// trailing newline.
func DeepMergeStrategy(relPath string, inputs []ContributorBytes) (StrategyResult, error) {
	if len(inputs) == 0 {
		return StrategyResult{}, fmt.Errorf("deep-merge invoked with no inputs for %q", relPath)
	}

	parsed := make([]map[string]any, len(inputs))
	contributors := make([]string, len(inputs))
	for i, in := range inputs {
		text := strings.TrimSpace(string(in.Bytes))
		if text == "" {
			parsed[i] = map[string]any{}
			contributors[i] = in.ID
			continue
		}
		var value any
		dec := json.NewDecoder(bytes.NewReader(in.Bytes))
		dec.UseNumber()
		if err := dec.Decode(&value); err != nil {
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
		obj, ok := value.(map[string]any)
		if !ok {
			return StrategyResult{}, pipelineerrors.NewInvalidSettingsJsonError(
				relPath, in.ID, "top-level value is not a JSON object",
			)
		}
		parsed[i] = obj
		contributors[i] = in.ID
	}

	acc := cloneObject(parsed[0])
	for i := 1; i < len(parsed); i++ {
		merged := mergeAt(nil, acc, parsed[i])
		// Accumulator at depth 0 is always a map (R8 invariant: a non-object
		// later contributor would have been rejected upstream as
		// InvalidSettingsJson). Cast is safe; preserving the type after each
		// step keeps the recursive merge well-typed.
		acc = merged.(map[string]any)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(acc); err != nil {
		return StrategyResult{}, fmt.Errorf("encoding merged settings %q: %w", relPath, err)
	}
	// json.Encoder.Encode appends a trailing newline; that matches the TS
	// JSON.stringify(..., 2) + "\n" contract.

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

	// Both objects → deep merge field-by-field.
	earlierObj, earlierOK := earlier.(map[string]any)
	laterObj, laterOK := later.(map[string]any)
	if earlierOK && laterOK {
		out := cloneObject(earlierObj)
		for k, v := range laterObj {
			if existing, ok := out[k]; ok {
				// Always allocate a fresh path slice rather than relying on
				// append's no-aliasing behavior in the sequential case. Cheap
				// (O(depth)) and rules out a latent data race if the merge
				// loop is ever parallelized by a downstream caller.
				next := make([]string, len(pathParts), len(pathParts)+1)
				copy(next, pathParts)
				next = append(next, k)
				out[k] = mergeAt(next, existing, v)
			} else {
				out[k] = cloneValue(v)
			}
		}
		return out
	}

	// R8 default: arrays replace, scalars/type-mismatch — later wins.
	return cloneValue(later)
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return cloneObject(t)
	case []any:
		return cloneArray(t)
	default:
		return v
	}
}

func cloneObject(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneArray(in []any) []any {
	if in == nil {
		return nil
	}
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = cloneValue(v)
	}
	return out
}
