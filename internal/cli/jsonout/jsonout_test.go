package jsonout

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestMarshalDisablesHTMLEscaping is the PR3 byte-identity invariant: we
// must NOT escape <, >, & — JS JSON.stringify doesn't, and the dual-suite
// IV harness compares both binaries' output byte-for-byte.
func TestMarshalDisablesHTMLEscaping(t *testing.T) {
	payload := map[string]string{"path": "<root>/foo&bar"}
	got, err := Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	gotStr := strings.TrimRight(string(got), "\n")
	if !strings.Contains(gotStr, "<root>") || !strings.Contains(gotStr, "&bar") {
		t.Fatalf("HTML chars escaped (PR3 violation): %q", gotStr)
	}
	// Round-trip safety: the bytes still parse as JSON.
	var back map[string]string
	if err := json.Unmarshal(bytes.TrimRight(got, "\n"), &back); err != nil {
		t.Fatalf("round-trip parse: %v", err)
	}
	if back["path"] != payload["path"] {
		t.Fatalf("round-trip lost data: %q vs %q", back["path"], payload["path"])
	}
}

// TestMarshalAppendsTrailingNewline is the line-form invariant: the encoder
// terminates each envelope with \n so consumers can stream line-by-line
// and the OutputChannel doesn't need to add a second newline.
func TestMarshalAppendsTrailingNewline(t *testing.T) {
	got, err := Marshal(map[string]int{"k": 1})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if len(got) == 0 || got[len(got)-1] != '\n' {
		t.Fatalf("expected trailing newline; got %q", got)
	}
}

// TestMarshalIsCompact ensures we never indent — one envelope per line.
func TestMarshalIsCompact(t *testing.T) {
	got, err := Marshal(map[string]int{"a": 1, "b": 2})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got = bytes.TrimRight(got, "\n")
	if bytes.Contains(got, []byte("\n")) {
		t.Fatalf("envelope contains embedded newline (not compact): %q", got)
	}
	if bytes.Contains(got, []byte("  ")) {
		t.Fatalf("envelope contains indentation: %q", got)
	}
}

// TestMarshalLineMatchesMarshal is a sanity check — MarshalLine must be a
// pure synonym, never doubled-newlined.
func TestMarshalLineMatchesMarshal(t *testing.T) {
	a, _ := Marshal(map[string]int{"k": 1})
	b, _ := MarshalLine(map[string]int{"k": 1})
	if string(a) != string(b) {
		t.Fatalf("MarshalLine != Marshal: %q vs %q", a, b)
	}
}
