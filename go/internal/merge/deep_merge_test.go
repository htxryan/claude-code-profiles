package merge

import (
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
)

func dmInputs(entries ...[2]any) []ContributorBytes {
	out := make([]ContributorBytes, len(entries))
	for i, e := range entries {
		id := e[0].(string)
		var bytes []byte
		switch v := e[1].(type) {
		case string:
			bytes = []byte(v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				panic(err)
			}
			bytes = b
		}
		out[i] = ContributorBytes{ID: id, Bytes: bytes}
	}
	return out
}

func parseAny(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("could not parse merged bytes: %v\n--bytes--\n%s", err, string(b))
	}
	return v
}

// R8: merges objects field-by-field, later scalars win.
func TestDeepMerge_LaterScalarsWin(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"x": 1, "y": 2}},
		[2]any{"b", map[string]any{"y": 99, "z": 3}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	wantNumberEqual(t, got["x"], 1)
	wantNumberEqual(t, got["y"], 99)
	wantNumberEqual(t, got["z"], 3)
	if !equalStrings(r.Contributors, []string{"a", "b"}) {
		t.Fatalf("contributors: %v", r.Contributors)
	}
}

func TestDeepMerge_NestedRecursion(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"ui": map[string]any{"theme": "dark", "font": "mono"}}},
		[2]any{"b", map[string]any{"ui": map[string]any{"theme": "light"}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	ui := got["ui"].(map[string]any)
	if ui["theme"] != "light" || ui["font"] != "mono" {
		t.Fatalf("ui: %+v", ui)
	}
}

// R8 default: arrays at the same path are REPLACED, not merged.
func TestDeepMerge_ArraysReplace(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"tools": []any{"x", "y"}}},
		[2]any{"b", map[string]any{"tools": []any{"z"}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	tools := got["tools"].([]any)
	if !equalAnySlice(tools, []any{"z"}) {
		t.Fatalf("tools: %+v", tools)
	}
}

func TestDeepMerge_NestedArraysReplace(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"permissions": map[string]any{"allow": []any{"a", "b"}}}},
		[2]any{"b", map[string]any{"permissions": map[string]any{"allow": []any{"c"}}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	allow := got["permissions"].(map[string]any)["allow"].([]any)
	if !equalAnySlice(allow, []any{"c"}) {
		t.Fatalf("allow: %+v", allow)
	}
}

func TestDeepMerge_TypeMismatchLaterWins(t *testing.T) {
	r1, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"x": map[string]any{"nested": true}}},
		[2]any{"b", map[string]any{"x": "string-now"}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := parseAny(t, r1.Bytes).(map[string]any); got["x"] != "string-now" {
		t.Fatalf("x: %+v", got["x"])
	}

	r2, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"x": []any{"array"}}},
		[2]any{"b", map[string]any{"x": map[string]any{"obj": true}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r2.Bytes).(map[string]any)
	xObj, ok := got["x"].(map[string]any)
	if !ok || xObj["obj"] != true {
		t.Fatalf("x: %+v", got["x"])
	}
}

func TestDeepMerge_PreservesUntouchedKeys(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"keep": "me", "ui": map[string]any{"font": "mono"}}},
		[2]any{"b", map[string]any{"ui": map[string]any{"theme": "dark"}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	if got["keep"] != "me" {
		t.Fatalf("keep: %+v", got["keep"])
	}
	ui := got["ui"].(map[string]any)
	if ui["font"] != "mono" || ui["theme"] != "dark" {
		t.Fatalf("ui: %+v", ui)
	}
}

func TestDeepMerge_EmptyAndWhitespaceTreatedAsObject(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", []ContributorBytes{
		{ID: "a", Bytes: []byte("")},
		{ID: "b", Bytes: []byte("   \n")},
		{ID: "c", Bytes: []byte(`{"ok":true}`)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	if got["ok"] != true {
		t.Fatalf("ok: %+v", got["ok"])
	}
}

func TestDeepMerge_EmitsTrailingNewline(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"x": 1}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(string(r.Bytes), "\n") {
		t.Fatalf("output lacks trailing newline: %q", string(r.Bytes))
	}
}

// Unparseable JSON surfaces InvalidSettingsJsonError naming the contributor.
func TestDeepMerge_UnparseableSurfacesError(t *testing.T) {
	_, err := DeepMergeStrategy("settings.json", []ContributorBytes{
		{ID: "good", Bytes: []byte(`{"ok":true}`)},
		{ID: "broken", Bytes: []byte(`{not json}`)},
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var se *pipelineerrors.InvalidSettingsJsonError
	if !stderrors.As(err, &se) {
		t.Fatalf("want InvalidSettingsJsonError, got %T (%v)", err, err)
	}
	if se.Contributor != "broken" {
		t.Fatalf("contributor: %q", se.Contributor)
	}
	if se.RelPath != "settings.json" {
		t.Fatalf("relPath: %q", se.RelPath)
	}
	if !strings.Contains(se.Error(), "broken") || !strings.Contains(se.Error(), "settings.json") {
		t.Fatalf("message lacks contributor/path: %q", se.Error())
	}
}

// JSON must be a top-level object: arrays/null/scalars are rejected.
func TestDeepMerge_TopLevelArrayRejected(t *testing.T) {
	_, err := DeepMergeStrategy("settings.json", []ContributorBytes{
		{ID: "arr", Bytes: []byte(`[{"a":1}]`)},
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var se *pipelineerrors.InvalidSettingsJsonError
	if !stderrors.As(err, &se) {
		t.Fatalf("want InvalidSettingsJsonError, got %T", err)
	}
}

func TestDeepMerge_TopLevelNullOrScalarRejected(t *testing.T) {
	for _, raw := range []string{"null", "42", `"str"`, "true"} {
		_, err := DeepMergeStrategy("settings.json", []ContributorBytes{
			{ID: "n", Bytes: []byte(raw)},
		})
		if err == nil {
			t.Fatalf("want error for %q, got nil", raw)
		}
		var se *pipelineerrors.InvalidSettingsJsonError
		if !stderrors.As(err, &se) {
			t.Fatalf("want InvalidSettingsJsonError for %q, got %T", raw, err)
		}
	}
}

// Trailing JSON values after the first object are rejected as malformed
// settings (a contributor with `{"a":1}{"b":2}` would otherwise silently
// drop the second object).
func TestDeepMerge_TrailingDataRejected(t *testing.T) {
	_, err := DeepMergeStrategy("settings.json", []ContributorBytes{
		{ID: "trail", Bytes: []byte(`{"a":1}{"b":2}`)},
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var se *pipelineerrors.InvalidSettingsJsonError
	if !stderrors.As(err, &se) {
		t.Fatalf("want InvalidSettingsJsonError, got %T (%v)", err, err)
	}
}

// Inputs are not mutated by deep-merge.
func TestDeepMerge_DoesNotMutateInputs(t *testing.T) {
	a := []byte(`{"x":{"nested":1}}`)
	b := []byte(`{"x":{"other":2}}`)
	aCopy := append([]byte(nil), a...)
	bCopy := append([]byte(nil), b...)
	if _, err := DeepMergeStrategy("settings.json", []ContributorBytes{
		{ID: "a", Bytes: a}, {ID: "b", Bytes: b},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(aCopy) != string(a) || string(bCopy) != string(b) {
		t.Fatal("input bytes mutated by deep-merge")
	}
}

// ─── R12: hooks-by-event carve-out ───────────────────────────────────────

// R12: action arrays at hooks.<EventName> are CONCATENATED, not replaced.
func TestDeepMerge_R12_HookEventArraysConcat(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{"run": "from-a"}}}}},
		[2]any{"b", map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{"run": "from-b"}}}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	pre := got["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Fatalf("PreToolUse: %+v", pre)
	}
	if pre[0].(map[string]any)["run"] != "from-a" || pre[1].(map[string]any)["run"] != "from-b" {
		t.Fatalf("PreToolUse order: %+v", pre)
	}
}

// R12 wins over R8: even when array-replace would otherwise apply, the
// hooks.<EventName> path concatenates.
func TestDeepMerge_R12_OverridesR8(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"hooks": map[string]any{"PreToolUse": []any{"a1", "a2"}}}},
		[2]any{"b", map[string]any{"hooks": map[string]any{"PreToolUse": []any{"b1"}}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	pre := got["hooks"].(map[string]any)["PreToolUse"].([]any)
	if !equalAnySlice(pre, []any{"a1", "a2", "b1"}) {
		t.Fatalf("PreToolUse: %+v", pre)
	}
}

// Different events from different contributors merge into the hooks object.
func TestDeepMerge_R12_DifferentEventsCoexist(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"hooks": map[string]any{"PreToolUse": []any{"pre-a"}}}},
		[2]any{"b", map[string]any{"hooks": map[string]any{"PostToolUse": []any{"post-b"}}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	hooks := got["hooks"].(map[string]any)
	if !equalAnySlice(hooks["PreToolUse"].([]any), []any{"pre-a"}) {
		t.Fatalf("PreToolUse: %+v", hooks["PreToolUse"])
	}
	if !equalAnySlice(hooks["PostToolUse"].([]any), []any{"post-b"}) {
		t.Fatalf("PostToolUse: %+v", hooks["PostToolUse"])
	}
}

func TestDeepMerge_R12_AccumulatesSameEventInOrder(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"base", map[string]any{"hooks": map[string]any{"Stop": []any{"s1"}}}},
		[2]any{"extended", map[string]any{"hooks": map[string]any{"Stop": []any{"s2"}}}},
		[2]any{"compA", map[string]any{"hooks": map[string]any{"Stop": []any{"s3"}}}},
		[2]any{"leaf", map[string]any{"hooks": map[string]any{"Stop": []any{"s4"}}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	stop := got["hooks"].(map[string]any)["Stop"].([]any)
	if !equalAnySlice(stop, []any{"s1", "s2", "s3", "s4"}) {
		t.Fatalf("Stop: %+v", stop)
	}
}

// R12 fires at depth 2 only — top-level hooks key (depth 1) and deeper paths
// fall back to R8 array-replace.
func TestDeepMerge_R12_OnlyAtDepth2(t *testing.T) {
	// Depth 1: top-level array under "hooks" is NOT R12.
	r1, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"hooks": []any{"a"}}},
		[2]any{"b", map[string]any{"hooks": []any{"b"}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got1 := parseAny(t, r1.Bytes).(map[string]any)
	if !equalAnySlice(got1["hooks"].([]any), []any{"b"}) {
		t.Fatalf("hooks (depth-1): %+v", got1["hooks"])
	}

	// Depth 3: hooks.<E>.actions is NOT R12.
	r2, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"hooks": map[string]any{"PreToolUse": map[string]any{"actions": []any{"x"}}}}},
		[2]any{"b", map[string]any{"hooks": map[string]any{"PreToolUse": map[string]any{"actions": []any{"y"}}}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got2 := parseAny(t, r2.Bytes).(map[string]any)
	actions := got2["hooks"].(map[string]any)["PreToolUse"].(map[string]any)["actions"].([]any)
	if !equalAnySlice(actions, []any{"y"}) {
		t.Fatalf("hooks.PreToolUse.actions: %+v", actions)
	}
}

// Type mismatch at hooks.<E> falls back to R8 last-wins.
func TestDeepMerge_R12_FallbackOnTypeMismatch(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"hooks": map[string]any{"PreToolUse": []any{"a1"}}}},
		[2]any{"b", map[string]any{"hooks": map[string]any{"PreToolUse": map[string]any{"not": "an array"}}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	pre := got["hooks"].(map[string]any)["PreToolUse"].(map[string]any)
	if pre["not"] != "an array" {
		t.Fatalf("PreToolUse: %+v", pre)
	}
}

// array → non-array → array resets the accumulator (R8 wins on mismatch,
// then R12 resumes with only whatever survived).
func TestDeepMerge_R12_ResetOnIntermediateMismatch(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"hooks": map[string]any{"PreToolUse": []any{"a1", "a2"}}}},
		[2]any{"b", map[string]any{"hooks": map[string]any{"PreToolUse": "string-instead"}}},
		[2]any{"c", map[string]any{"hooks": map[string]any{"PreToolUse": []any{"c1"}}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	pre := got["hooks"].(map[string]any)["PreToolUse"].([]any)
	if !equalAnySlice(pre, []any{"c1"}) {
		t.Fatalf("PreToolUse: %+v", pre)
	}
}

// Two sibling event keys under hooks within the SAME contributor merge step:
// exercises the path-tracking code with map-iteration order non-determinism
// to catch any pathParts-aliasing regression.
func TestDeepMerge_R12_TwoSiblingEventsSingleContributor(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"hooks": map[string]any{
			"PreToolUse":  []any{"a-pre"},
			"PostToolUse": []any{"a-post"},
		}}},
		[2]any{"b", map[string]any{"hooks": map[string]any{
			"PreToolUse":  []any{"b-pre"},
			"PostToolUse": []any{"b-post"},
		}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	hooks := got["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	post := hooks["PostToolUse"].([]any)
	if !equalAnySlice(pre, []any{"a-pre", "b-pre"}) {
		t.Fatalf("PreToolUse: %+v", pre)
	}
	if !equalAnySlice(post, []any{"a-post", "b-post"}) {
		t.Fatalf("PostToolUse: %+v", post)
	}
}

// R12 must not touch arrays under unrelated top-level keys.
func TestDeepMerge_R12_OnlyHooksKey(t *testing.T) {
	r, err := DeepMergeStrategy("settings.json", dmInputs(
		[2]any{"a", map[string]any{"other": map[string]any{"PreToolUse": []any{"a"}}}},
		[2]any{"b", map[string]any{"other": map[string]any{"PreToolUse": []any{"b"}}}},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := parseAny(t, r.Bytes).(map[string]any)
	pre := got["other"].(map[string]any)["PreToolUse"].([]any)
	if !equalAnySlice(pre, []any{"b"}) {
		t.Fatalf("other.PreToolUse: %+v", pre)
	}
}

func wantNumberEqual(t *testing.T, got any, want int) {
	t.Helper()
	switch v := got.(type) {
	case json.Number:
		n, _ := v.Int64()
		if int(n) != want {
			t.Fatalf("number: want %d, got %s", want, v.String())
		}
	case float64:
		if int(v) != want {
			t.Fatalf("number: want %d, got %v", want, v)
		}
	default:
		t.Fatalf("non-number value: %+v (%T)", got, got)
	}
}

func equalAnySlice(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !equalAny(a[i], b[i]) {
			return false
		}
	}
	return true
}

func equalAny(a, b any) bool {
	switch av := a.(type) {
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return false
		}
		return equalAnySlice(av, bv)
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !equalAny(v, bv[k]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
