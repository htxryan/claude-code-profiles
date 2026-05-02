// Package errors holds the pipeline error sentinels shared across resolver,
// merge, markers, state, drift, and cli. Each error carries a stable Code
// for programmatic dispatch (E5 exit-code mapping) and a human-readable
// message that names the file/profile/path involved (§7 quality bar).
//
// Hierarchy mirrors the TS surface in src/errors/index.ts so the Go bin
// reproduces the same exit-code routing:
//
//	PipelineError (base interface)
//	  ├── ResolverError   — resolution-phase failures
//	  ├── MergeError      — merge-phase failures
//	  └── MaterializeError — materialize-phase failures
//
// F1 lands the four sentinels named in the epic scope (CycleError,
// ConflictError, MissingProfileError, PathTraversalError). D1–D5 add the
// remaining variants (MissingInclude, InvalidManifest, InvalidSettingsJson,
// MergeReadFailed, RootClaudeMdMarkersMissing) inside their owning epics.
package errors

import (
	"errors"
	"fmt"
	"strings"
)

// Code is the stable identifier used for programmatic dispatch (e.g. exit
// code mapping in cli/exit.go). Values are stable across versions; new
// variants are added without renaming existing ones.
type Code string

const (
	CodeMissingProfile    Code = "MissingProfile"
	CodeCycle             Code = "Cycle"
	CodeMissingInclude    Code = "MissingInclude"
	CodeConflict          Code = "Conflict"
	CodeInvalidManifest   Code = "InvalidManifest"
	CodeInvalidSettings   Code = "InvalidSettingsJson"
	CodeMergeReadFailed   Code = "MergeReadFailed"
	CodeRootMarkers       Code = "RootClaudeMdMarkersMissing"
	CodePathTraversal     Code = "PathTraversal"
)

// PipelineError is the common interface implemented by every typed pipeline
// error. Use errors.As to recover the concrete variant; use ErrorCode to
// branch on the stable code without type-switching.
type PipelineError interface {
	error
	Phase() Phase
	ErrorCode() Code
}

// Phase identifies which pipeline stage produced the error. Used by the
// CLI exit-code mapper to distinguish config errors from runtime faults.
type Phase string

const (
	PhaseResolver    Phase = "resolver"
	PhaseMerge       Phase = "merge"
	PhaseMaterialize Phase = "materialize"
)

// base is the embed for every concrete error type. Holding code+message+phase
// in one place keeps Error() and ErrorCode() consistent and trivially
// implements the PipelineError interface.
type base struct {
	code    Code
	phase   Phase
	message string
}

func (b base) Error() string     { return b.message }
func (b base) ErrorCode() Code   { return b.code }
func (b base) Phase() Phase      { return b.phase }

// ResolverError marks resolver-phase failures. Sub-types embed it so a
// single errors.As(err, *ResolverError) check classifies any resolver
// failure regardless of the concrete variant.
type ResolverError struct{ base }

// MergeError marks merge-phase failures.
type MergeError struct{ base }

// MaterializeError marks materialize-phase failures.
type MaterializeError struct{ base }

// CycleError reports a cycle in the extends chain. Members are listed in
// cycle order so the message reads as a directed walk: "a → b → c → a".
type CycleError struct {
	ResolverError
	Cycle []string
}

func NewCycleError(cycle []string) *CycleError {
	msg := fmt.Sprintf("Cycle in extends chain: %s", strings.Join(cycle, " → "))
	return &CycleError{
		ResolverError: ResolverError{base{code: CodeCycle, phase: PhaseResolver, message: msg}},
		Cycle:         append([]string(nil), cycle...),
	}
}

// ConflictError reports two or more contributors defining the same
// non-mergeable file path. Contributors is the ordered list of profile
// names involved.
type ConflictError struct {
	ResolverError
	RelPath      string
	Contributors []string
}

func NewConflictError(relPath string, contributors []string) *ConflictError {
	quoted := make([]string, len(contributors))
	for i, c := range contributors {
		quoted[i] = fmt.Sprintf("%q", c)
	}
	msg := fmt.Sprintf("How rude — Conflict at %q: defined by %s", relPath, strings.Join(quoted, " and "))
	return &ConflictError{
		ResolverError: ResolverError{base{code: CodeConflict, phase: PhaseResolver, message: msg}},
		RelPath:       relPath,
		Contributors:  append([]string(nil), contributors...),
	}
}

// MissingProfileError reports an extends/CLI reference to a profile that
// doesn't exist on disk. ReferencedBy is empty when the missing name was a
// top-level CLI argument; Suggestions carries optional Levenshtein-2
// "did you mean" candidates populated by command handlers.
type MissingProfileError struct {
	ResolverError
	Missing      string
	ReferencedBy string
	Suggestions  []string
}

func NewMissingProfileError(missing, referencedBy string, suggestions []string) *MissingProfileError {
	ref := ""
	if referencedBy != "" {
		ref = fmt.Sprintf(" (referenced by %q)", referencedBy)
	}
	sug := ""
	if len(suggestions) > 0 {
		sug = fmt.Sprintf(" (I do beg your pardon. Did you perhaps mean: %s?)", strings.Join(suggestions, ", "))
	}
	msg := fmt.Sprintf("Profile %q does not exist%s%s", missing, ref, sug)
	return &MissingProfileError{
		ResolverError: ResolverError{base{code: CodeMissingProfile, phase: PhaseResolver, message: msg}},
		Missing:       missing,
		ReferencedBy:  referencedBy,
		Suggestions:   append([]string(nil), suggestions...),
	}
}

// PathTraversalError reports an includes/extends path that, after
// canonicalization, escapes its allowed root (PR16a). The error names the
// raw input and the resolved-but-rejected absolute path so users can locate
// the offending manifest entry.
type PathTraversalError struct {
	ResolverError
	Raw          string
	ResolvedPath string
	ReferencedBy string
}

func NewPathTraversalError(raw, resolvedPath, referencedBy string) *PathTraversalError {
	msg := fmt.Sprintf(
		"Path %q (resolved to %q) referenced by %q escapes the project root — refusing to traverse",
		raw, resolvedPath, referencedBy,
	)
	return &PathTraversalError{
		ResolverError: ResolverError{base{code: CodePathTraversal, phase: PhaseResolver, message: msg}},
		Raw:           raw,
		ResolvedPath:  resolvedPath,
		ReferencedBy:  referencedBy,
	}
}

// MissingIncludeError reports an includes entry that does not resolve to an
// existing directory. Names the raw entry, the canonicalized path that was
// looked up, and the referencing profile so the user can locate the bad
// manifest entry.
type MissingIncludeError struct {
	ResolverError
	Raw          string
	ResolvedPath string
	ReferencedBy string
}

func NewMissingIncludeError(raw, resolvedPath, referencedBy string) *MissingIncludeError {
	msg := fmt.Sprintf(
		"Include %q (resolved to %q) referenced by %q does not exist",
		raw, resolvedPath, referencedBy,
	)
	return &MissingIncludeError{
		ResolverError: ResolverError{base{code: CodeMissingInclude, phase: PhaseResolver, message: msg}},
		Raw:           raw,
		ResolvedPath:  resolvedPath,
		ReferencedBy:  referencedBy,
	}
}

// InvalidManifestError reports an unparseable or schema-invalid profile.json
// (distinct from R36 unknown-field warnings, which are recoverable).
type InvalidManifestError struct {
	ResolverError
	Path   string
	Detail string
}

func NewInvalidManifestError(path, detail string) *InvalidManifestError {
	msg := fmt.Sprintf("Manifest at %q is invalid: %s", path, detail)
	return &InvalidManifestError{
		ResolverError: ResolverError{base{code: CodeInvalidManifest, phase: PhaseResolver, message: msg}},
		Path:          path,
		Detail:        detail,
	}
}

// AsPipelineError unwraps err and returns the first PipelineError in the
// chain, or nil if none is present. Equivalent to errors.As with the
// PipelineError interface but spelled as a single call site for the CLI
// exit-code mapper.
func AsPipelineError(err error) PipelineError {
	var pe PipelineError
	if errors.As(err, &pe) {
		return pe
	}
	return nil
}
