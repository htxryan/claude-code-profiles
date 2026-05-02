package merge

import (
	"bytes"
	"testing"
)

func makeInputs(entries ...[2]string) []ContributorBytes {
	out := make([]ContributorBytes, len(entries))
	for i, e := range entries {
		out[i] = ContributorBytes{ID: e[0], Bytes: []byte(e[1])}
	}
	return out
}

// R9: concat preserves contributor order and trailing newlines.
func TestConcatStrategy_OrderAndTrailingNewlines(t *testing.T) {
	r, err := ConcatStrategy("CLAUDE.md", makeInputs(
		[2]string{"base", "# base\nbase content\n"},
		[2]string{"leaf", "# leaf\nleaf content\n"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "# base\nbase content\n# leaf\nleaf content\n"
	if string(r.Bytes) != want {
		t.Fatalf("bytes: want %q, got %q", want, string(r.Bytes))
	}
	if got := r.Contributors; !equalStrings(got, []string{"base", "leaf"}) {
		t.Fatalf("contributors: want [base leaf], got %v", got)
	}
}

// R9 worked example: base ← extended ← profile + compA + compB.
func TestConcatStrategy_R9WorkedExample(t *testing.T) {
	r, err := ConcatStrategy("CLAUDE.md", makeInputs(
		[2]string{"base", "BASE\n"},
		[2]string{"extended", "EXTENDED\n"},
		[2]string{"compA", "COMPA\n"},
		[2]string{"compB", "COMPB\n"},
		[2]string{"leaf", "LEAF\n"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "BASE\nEXTENDED\nCOMPA\nCOMPB\nLEAF\n"
	if string(r.Bytes) != want {
		t.Fatalf("bytes: want %q, got %q", want, string(r.Bytes))
	}
	if got := r.Contributors; !equalStrings(got, []string{"base", "extended", "compA", "compB", "leaf"}) {
		t.Fatalf("contributors: %v", got)
	}
}

// Inserts a separator newline only when a chunk lacks one.
func TestConcatStrategy_SeparatorInsertedWhenMissing(t *testing.T) {
	r, err := ConcatStrategy("CLAUDE.md", makeInputs(
		[2]string{"a", "no-newline"},
		[2]string{"b", "after\n"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(r.Bytes) != "no-newline\nafter\n" {
		t.Fatalf("bytes: %q", string(r.Bytes))
	}
}

// Does not double newlines for chunks that already end with \n.
func TestConcatStrategy_NoDoubleNewlines(t *testing.T) {
	r, err := ConcatStrategy("CLAUDE.md", makeInputs(
		[2]string{"a", "ends-with-nl\n"},
		[2]string{"b", "x\n"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(r.Bytes) != "ends-with-nl\nx\n" {
		t.Fatalf("bytes: %q", string(r.Bytes))
	}
}

func TestConcatStrategy_SingleContributor(t *testing.T) {
	r, err := ConcatStrategy("CLAUDE.md", makeInputs([2]string{"only", "solo content\n"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(r.Bytes) != "solo content\n" {
		t.Fatalf("bytes: %q", string(r.Bytes))
	}
	if !equalStrings(r.Contributors, []string{"only"}) {
		t.Fatalf("contributors: %v", r.Contributors)
	}
}

// Binary-safe: bytes pass through verbatim, no string normalization.
func TestConcatStrategy_BinarySafe(t *testing.T) {
	utf8 := []byte("café\n")
	r, err := ConcatStrategy("notes/é.md", []ContributorBytes{
		{ID: "a", Bytes: utf8},
		{ID: "b", Bytes: []byte("X\n")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(r.Bytes[:len(utf8)], utf8) {
		t.Fatalf("first %d bytes diverged: %q", len(utf8), r.Bytes[:len(utf8)])
	}
}

// Empty contributors are skipped: no spurious blank line, omitted from
// provenance.
func TestConcatStrategy_SkipsEmpty(t *testing.T) {
	r, err := ConcatStrategy("CLAUDE.md", makeInputs(
		[2]string{"a", ""},
		[2]string{"b", "after\n"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(r.Bytes) != "after\n" {
		t.Fatalf("bytes: %q", string(r.Bytes))
	}
	if !equalStrings(r.Contributors, []string{"b"}) {
		t.Fatalf("contributors: want [b], got %v", r.Contributors)
	}
}

func TestConcatStrategy_EmptyMiddle(t *testing.T) {
	r, err := ConcatStrategy("CLAUDE.md", makeInputs(
		[2]string{"a", "X\n"},
		[2]string{"b", ""},
		[2]string{"c", "Y\n"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(r.Bytes) != "X\nY\n" {
		t.Fatalf("bytes: %q", string(r.Bytes))
	}
	if !equalStrings(r.Contributors, []string{"a", "c"}) {
		t.Fatalf("contributors: %v", r.Contributors)
	}
}

func TestConcatStrategy_NoInputsErrors(t *testing.T) {
	if _, err := ConcatStrategy("CLAUDE.md", nil); err == nil {
		t.Fatal("want error for empty inputs, got nil")
	}
}

// All-empty contributors: result is len-zero but non-nil so callers that
// check `Bytes == nil` vs `len(Bytes) == 0` see a stable contract.
func TestConcatStrategy_AllEmptyReturnsEmptyNotNil(t *testing.T) {
	r, err := ConcatStrategy("CLAUDE.md", makeInputs(
		[2]string{"a", ""},
		[2]string{"b", ""},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Bytes == nil {
		t.Fatal("Bytes is nil; want empty (len-zero, non-nil) slice")
	}
	if len(r.Bytes) != 0 {
		t.Fatalf("len(Bytes): want 0, got %d", len(r.Bytes))
	}
	if len(r.Contributors) != 0 {
		t.Fatalf("contributors: want [], got %v", r.Contributors)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
