package merge

import (
	"bytes"
	"testing"
)

// R10: last-wins picks the highest contributor index and lists only that
// contributor in provenance.
func TestLastWinsStrategy_LastBytesWin(t *testing.T) {
	r, err := LastWinsStrategy("commands/x.sh", []ContributorBytes{
		{ID: "a", Bytes: []byte("from-a\n")},
		{ID: "b", Bytes: []byte("from-b\n")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(r.Bytes) != "from-b\n" {
		t.Fatalf("bytes: want %q, got %q", "from-b\n", string(r.Bytes))
	}
	if len(r.Contributors) != 1 || r.Contributors[0] != "b" {
		t.Fatalf("contributors: want [b], got %v", r.Contributors)
	}
}

func TestLastWinsStrategy_SinglePassthrough(t *testing.T) {
	in := []byte("solo")
	r, err := LastWinsStrategy("agents/foo.json", []ContributorBytes{{ID: "only", Bytes: in}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(r.Bytes, in) {
		t.Fatalf("bytes diverged: want %q, got %q", in, r.Bytes)
	}
	if len(r.Contributors) != 1 || r.Contributors[0] != "only" {
		t.Fatalf("contributors: want [only], got %v", r.Contributors)
	}
}

// Future callers (D5, validate) must be able to mutate the output without
// corrupting input bytes still held by the orchestrator.
func TestLastWinsStrategy_FreshBufferNotAliased(t *testing.T) {
	in := []byte("payload")
	r, err := LastWinsStrategy("agents/foo.json", []ContributorBytes{{ID: "only", Bytes: in}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := range r.Bytes {
		r.Bytes[i] = 0
	}
	if string(in) != "payload" {
		t.Fatalf("input was mutated by output zeroing: %q", string(in))
	}
}

func TestLastWinsStrategy_OnlyLastInProvenance(t *testing.T) {
	r, err := LastWinsStrategy("plugin.json", []ContributorBytes{
		{ID: "x1", Bytes: []byte("1")},
		{ID: "x2", Bytes: []byte("2")},
		{ID: "x3", Bytes: []byte("3")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(r.Bytes) != "3" {
		t.Fatalf("bytes: want %q, got %q", "3", string(r.Bytes))
	}
	if len(r.Contributors) != 1 || r.Contributors[0] != "x3" {
		t.Fatalf("contributors: want [x3], got %v", r.Contributors)
	}
}

func TestLastWinsStrategy_NoInputsErrors(t *testing.T) {
	if _, err := LastWinsStrategy("foo.txt", nil); err == nil {
		t.Fatal("want error for empty inputs, got nil")
	}
}

// A zero-byte winner must still produce a valid result with empty bytes
// and the winner in provenance — distinct from "no inputs" (which errors).
func TestLastWinsStrategy_ZeroByteWinner(t *testing.T) {
	r, err := LastWinsStrategy("plugin.json", []ContributorBytes{
		{ID: "a", Bytes: []byte("from-a\n")},
		{ID: "empty", Bytes: []byte{}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Bytes) != 0 {
		t.Fatalf("bytes: want empty, got %q", string(r.Bytes))
	}
	if len(r.Contributors) != 1 || r.Contributors[0] != "empty" {
		t.Fatalf("contributors: want [empty], got %v", r.Contributors)
	}
}
