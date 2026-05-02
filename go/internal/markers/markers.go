// Package markers parses and renders the c3p managed-block markers
// (<!-- c3p:vN:begin --> ... <!-- c3p:vN:end -->) used to splice generated
// content into user-owned files (CLAUDE.md). This package is the single
// source of truth for marker syntax (spec §12.3); all consumers — D2 merge
// for *.md, D5 splice/materialize, D6 fingerprint scope, and validate
// (R44/R45/R46) — must call ParseMarkers / RenderManagedBlock here rather
// than re-implementing the regex elsewhere.
package markers

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// CanonicalMarkerRegex is the spec §12.3 source-of-truth regex, written
// here as a comment because Go's RE2 does not support the \1/\2
// backreferences the TS implementation uses to enforce begin/end version+tail
// equality:
//
//	<!-- c3p:v(\d+):begin([^>]*)-->([\s\S]*?)<!-- c3p:v\1:end\2-->
//
// In Go we split the patterns and enforce the equality in ParseMarkers.
// Treat MarkerBeginPattern + MarkerEndPattern + the equality check below as
// the single source of truth.
const (
	MarkerBeginPattern = `<!-- c3p:v(\d+):begin([^>]*)-->`
	MarkerEndPattern   = `<!-- c3p:v(\d+):end([^>]*)-->`
	markerAnyPattern   = `<!-- c3p:v\d+:(?:begin|end)`
)

var (
	markerBeginRegex = regexp.MustCompile(MarkerBeginPattern)
	markerEndRegex   = regexp.MustCompile(MarkerEndPattern)
	markerAnyRegex   = regexp.MustCompile(markerAnyPattern)
)

// Status describes the outcome of ParseMarkers.
type Status string

const (
	// StatusValid means a single well-formed begin/end pair was found; the
	// ParseResult Before/Section/After/Version fields are populated.
	StatusValid Status = "valid"
	// StatusMalformed means partial markers (lone :begin or :end), version
	// mismatch, or more than one well-formed pair was found.
	StatusMalformed Status = "malformed"
	// StatusAbsent means no marker bytes at all were found in the input.
	StatusAbsent Status = "absent"
)

// ParseResult is the outcome of ParseMarkers. When Status==StatusValid the
// splice fields are populated such that
//
//	Before + <begin marker> + Section + <end marker> + After == original
//
// is true byte-for-byte. For StatusMalformed and StatusAbsent the splice
// fields are zero-valued.
type ParseResult struct {
	Status  Status
	Before  string
	Section string
	After   string
	// Version is the integer captured from the :vN: field of the matched
	// begin marker (zero when Status != StatusValid).
	Version int
}

// MalformedMarkersError is returned by InjectMarkersIntoFile when the input
// already contains a malformed c3p block. Init refuses to append a second
// fresh block on top of broken bytes — that would leave the file STILL
// malformed (now with two block fragments) and trip subsequent validate / use
// calls. Failing closed shifts the discovery to init time and points the user
// at the manual repair.
type MalformedMarkersError struct {
	Message string
}

func (e *MalformedMarkersError) Error() string { return e.Message }

// ParseMarkers scans content for the c3p managed-block markers and returns
// either a valid splice (Before/Section/After/Version), Malformed (partial
// markers, version mismatch, or multiple blocks), or Absent (no marker
// bytes at all).
//
// Line-ending policy: ParseMarkers is CRLF-tolerant. The marker patterns
// match `\r` naturally (begin/end tail capture is `[^>]*` and the body is
// matched by walking byte indices), so a CLAUDE.md saved with CRLF
// terminators parses identically to the LF form. Before/Section/After are
// returned with their on-disk bytes intact.
func ParseMarkers(content string) ParseResult {
	p, ok := findFirstPair(content)
	if !ok {
		// No well-formed pair. If the input has any partial marker bytes at
		// all, treat as malformed — re-running init on a partial file would
		// silently append a second block and produce a malformed multi-block
		// file.
		if markerAnyRegex.MatchString(content) {
			return ParseResult{Status: StatusMalformed}
		}
		return ParseResult{Status: StatusAbsent}
	}

	// Multi-block check (spec §12.3 reserves "more than one match" as
	// malformed for v1). Look past the first pair for a second well-formed
	// pair.
	// Multi-block: matches TS — only a SECOND well-formed pair makes the
	// file malformed. A trailing lone marker after a valid pair is silently
	// tolerated (parity with src/markers.ts; tightening would diverge from
	// the cross-language IV harness).
	if _, ok := findFirstPair(content[p.endIdx:]); ok {
		return ParseResult{Status: StatusMalformed}
	}

	return ParseResult{
		Status:  StatusValid,
		Before:  content[:p.startIdx],
		Section: p.section,
		After:   content[p.endIdx:],
		Version: p.version,
	}
}

// pair is an internal book-keeping struct: indices into the original content
// for the start of the begin marker and the end of the end marker (exclusive
// upper bound), the captured body between them, and the parsed version.
type pair struct {
	startIdx int
	endIdx   int
	section  string
	version  int
}

// findFirstPair walks the content looking for a begin marker followed by an
// end marker with the same version + tail bytes. The non-greedy semantics of
// the canonical TS regex are reproduced here by taking the FIRST end with a
// matching version+tail after each begin. Returned indices are relative to
// the input string (no offset bookkeeping); callers that pre-slice translate
// them back themselves.
//
// Overflow handling: a pathologically long version string (e.g. 25 digits)
// makes strconv.Atoi return ErrRange. We skip such pairs rather than emitting
// a StatusValid result with Version=0 — a downstream consumer that re-renders
// at v=0 (clamped to v1 by RenderManagedBlock) would corrupt a high-version
// file. Because the begin/end byte shape still matches markerAnyRegex, the
// caller's lone-marker check surfaces the overflowed input as StatusMalformed.
func findFirstPair(content string) (pair, bool) {
	beginLocs := markerBeginRegex.FindAllStringSubmatchIndex(content, -1)
	for _, beginLoc := range beginLocs {
		// FindAllStringSubmatchIndex returns flat slices of the form
		// [matchStart, matchEnd, group1Start, group1End, ...]. Group 1 is
		// the version digits; group 2 is the tail.
		beginStart := beginLoc[0]
		beginEnd := beginLoc[1]
		ver := content[beginLoc[2]:beginLoc[3]]
		tail := content[beginLoc[4]:beginLoc[5]]

		v, err := strconv.Atoi(ver)
		if err != nil {
			continue
		}

		afterBegin := content[beginEnd:]
		endLocs := markerEndRegex.FindAllStringSubmatchIndex(afterBegin, -1)
		for _, endLoc := range endLocs {
			endVer := afterBegin[endLoc[2]:endLoc[3]]
			endTail := afterBegin[endLoc[4]:endLoc[5]]
			if endVer != ver || endTail != tail {
				continue
			}
			absEndStart := beginEnd + endLoc[0]
			absEndEnd := beginEnd + endLoc[1]
			return pair{
				startIdx: beginStart,
				endIdx:   absEndEnd,
				section:  content[beginEnd:absEndStart],
				version:  v,
			}, true
		}
		// No matching end for THIS begin; try the next begin (a later
		// begin/end pair may still be valid).
	}
	return pair{}, false
}

// RenderManagedBlock builds the canonical managed block (begin marker +
// self-doc comment + body + end marker) for the given section bytes. version
// is accepted explicitly so future migrations (v2+) can produce the right
// shape; v1 is the only currently-rendered form.
//
// The self-doc comment line is part of the spec §12.2 "recommended form" —
// it tells the next human reading the file what the block is and how to
// regenerate it, which dramatically reduces the chance of someone editing
// between the markers and watching their changes vanish on the next `c3p use`.
//
// The body is always wrapped with leading + trailing newlines so the markers
// sit on their own lines regardless of whether sectionBytes is empty.
func RenderManagedBlock(sectionBytes string, version int) string {
	if version <= 0 {
		version = 1
	}
	begin := fmt.Sprintf("<!-- c3p:v%d:begin -->", version)
	end := fmt.Sprintf("<!-- c3p:v%d:end -->", version)
	const selfDoc = "<!-- Managed block. Do not edit between markers — changes are overwritten on next `c3p use`. -->"
	body := "\n"
	if len(sectionBytes) > 0 {
		body = "\n" + sectionBytes + "\n"
	}
	return begin + "\n" + selfDoc + "\n" + body + end + "\n"
}

// extractSectionBodyRegex undoes the framing RenderManagedBlock added so a
// freshly-parsed Section recovers the original `body` argument. Used by D5
// persist to write only the user-meaningful body to disk.
var extractSectionBodyRegex = regexp.MustCompile(`(?s)^\n<!--[^>]*-->\n\n(.*?)\n?$`)

// ExtractSectionBody is the inverse of RenderManagedBlock for the framing
// (leading newline + self-doc HTML comment + blank line + body + trailing
// newline). When the section has no recognizable framing — e.g. a
// hand-edited section that lost the self-doc line — the input is returned
// verbatim so the round trip still completes (the next full materialize will
// re-render the self-doc).
func ExtractSectionBody(section string) string {
	m := extractSectionBodyRegex.FindStringSubmatch(section)
	if m == nil {
		return section
	}
	return m[1]
}

// InjectMarkersIntoFile is the init helper: ensure the file contents contain
// a managed block. If markers already exist (well-formed), the input is
// returned unchanged. If absent, a fresh empty managed block is appended at
// end-of-file with byte-for-byte preservation of everything above (a single
// separating newline is inserted only when the existing content does not
// already end in one). If the input is malformed (lone marker, version
// mismatch, or multi-block), MalformedMarkersError is returned — appending a
// second block on top of broken bytes would leave the file still malformed.
func InjectMarkersIntoFile(content string) (string, error) {
	parsed := ParseMarkers(content)
	switch parsed.Status {
	case StatusValid:
		return content, nil
	case StatusMalformed:
		return "", &MalformedMarkersError{
			Message: "CLAUDE.md contains a malformed c3p marker block (lone `:begin`, lone `:end`, version mismatch, or multiple blocks). Refusing to append a second block on top of broken markers — please delete the partial marker text manually and re-run `c3p init`.",
		}
	}
	separator := ""
	if len(content) > 0 && content[len(content)-1] != '\n' {
		separator = "\n"
	}
	return content + separator + RenderManagedBlock("", 1), nil
}

// IsMalformedMarkersError reports whether err is (or wraps) a
// MalformedMarkersError. Provided for callers that want a one-call test
// without spelling out errors.As.
func IsMalformedMarkersError(err error) bool {
	var target *MalformedMarkersError
	return errors.As(err, &target)
}
