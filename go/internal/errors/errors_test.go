package errors

import (
	stderrors "errors"
	"strings"
	"testing"
)

func TestCycleError(t *testing.T) {
	e := NewCycleError([]string{"a", "b", "c", "a"})
	if e.ErrorCode() != CodeCycle {
		t.Fatalf("want code %q, got %q", CodeCycle, e.ErrorCode())
	}
	if e.Phase() != PhaseResolver {
		t.Fatalf("want phase %q, got %q", PhaseResolver, e.Phase())
	}
	if !strings.Contains(e.Error(), "a → b → c → a") {
		t.Fatalf("message missing cycle walk: %q", e.Error())
	}
	// Defensive copy — caller mutations must not bleed into the error.
	e.Cycle[0] = "Z"
	if strings.Contains(e.Error(), "Z") {
		t.Fatalf("Cycle field shares storage with error message: %q", e.Error())
	}
}

func TestConflictError(t *testing.T) {
	e := NewConflictError("settings.json", []string{"base", "team"})
	if e.RelPath != "settings.json" {
		t.Fatalf("want RelPath settings.json, got %q", e.RelPath)
	}
	if !strings.Contains(e.Error(), `"base"`) || !strings.Contains(e.Error(), `"team"`) {
		t.Fatalf("contributors not quoted in message: %q", e.Error())
	}
}

func TestMissingProfileError(t *testing.T) {
	e := NewMissingProfileError("staging", "prod", []string{"staging-east"})
	if !strings.Contains(e.Error(), `"staging"`) {
		t.Fatalf("missing name not quoted: %q", e.Error())
	}
	if !strings.Contains(e.Error(), `referenced by "prod"`) {
		t.Fatalf("referencedBy missing: %q", e.Error())
	}
	if !strings.Contains(e.Error(), "Did you perhaps mean: staging-east") {
		t.Fatalf("suggestion missing: %q", e.Error())
	}

	bare := NewMissingProfileError("staging", "", nil)
	if strings.Contains(bare.Error(), "referenced by") {
		t.Fatalf("bare message must omit referencedBy clause: %q", bare.Error())
	}
}

func TestPathTraversalError(t *testing.T) {
	e := NewPathTraversalError("../../etc/passwd", "/etc/passwd", "team/profile.json")
	if e.ErrorCode() != CodePathTraversal {
		t.Fatalf("want code %q, got %q", CodePathTraversal, e.ErrorCode())
	}
	for _, want := range []string{`"../../etc/passwd"`, `"/etc/passwd"`, `"team/profile.json"`} {
		if !strings.Contains(e.Error(), want) {
			t.Fatalf("message missing %q: %q", want, e.Error())
		}
	}
}

func TestPipelineErrorInterface(t *testing.T) {
	var err error = NewCycleError([]string{"a", "a"})
	pe := AsPipelineError(err)
	if pe == nil {
		t.Fatal("AsPipelineError returned nil for a CycleError")
	}
	if pe.ErrorCode() != CodeCycle {
		t.Fatalf("interface lost code: %q", pe.ErrorCode())
	}

	if AsPipelineError(stderrors.New("not pipelined")) != nil {
		t.Fatal("AsPipelineError must return nil for plain errors")
	}
}

func TestPhaseClassification(t *testing.T) {
	cases := []struct {
		name string
		err  PipelineError
		want Phase
	}{
		{"cycle", NewCycleError([]string{"a", "a"}), PhaseResolver},
		{"conflict", NewConflictError("x", []string{"a", "b"}), PhaseResolver},
		{"missingProfile", NewMissingProfileError("x", "", nil), PhaseResolver},
		{"pathTraversal", NewPathTraversalError("x", "y", "z"), PhaseResolver},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Phase(); got != tc.want {
				t.Fatalf("phase: want %q, got %q", tc.want, got)
			}
		})
	}
}

// errors.As classification works through the AsPipelineError helper
// rather than against the embedded ResolverError struct (Go's embed +
// errors.As do not compose cleanly enough for that to be the canonical
// path — Phase() is the primary discriminator).
func TestErrorsAsForPipelineInterface(t *testing.T) {
	err := error(NewConflictError("x", []string{"a", "b"}))
	pe := AsPipelineError(err)
	if pe == nil {
		t.Fatal("AsPipelineError returned nil for ConflictError")
	}
	var ce *ConflictError
	if !stderrors.As(err, &ce) {
		t.Fatal("errors.As to *ConflictError must work")
	}
	if ce.RelPath != "x" {
		t.Fatalf("ConflictError RelPath: %q", ce.RelPath)
	}
}
