// Unit tests for the markers package — single source of truth for the
// project-root CLAUDE.md managed-block markers (cw6 / spec §12.3). Coverage
// mirrors the TS suite in tests/markers.test.ts so D2/D5/D6 consumers get
// behavioral parity for free, plus EARS R44 (validate at preflight, opening
// AND closing markers present), R45 (parse for splice), and R46 (managed
// slice byte boundary).
package markers

import (
	stderrors "errors"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────
// Marker pattern shape (spec §12.3)
// ─────────────────────────────────────────────────────────────

func TestMarkerPattern_MatchesCanonicalPair(t *testing.T) {
	text := "<!-- c3p:v1:begin -->\nbody\n<!-- c3p:v1:end -->"
	r := ParseMarkers(text)
	if r.Status != StatusValid {
		t.Fatalf("want valid, got %q", r.Status)
	}
	if r.Version != 1 {
		t.Fatalf("want version 1, got %d", r.Version)
	}
	if !strings.Contains(r.Section, "body") {
		t.Fatalf("section missing body: %q", r.Section)
	}
}

func TestMarkerPattern_BodyIsNonGreedy(t *testing.T) {
	// Two managed blocks in one document — finding the FIRST pair must not
	// collapse them. ParseMarkers will return Malformed for multi-block,
	// but findFirstPair (used internally) must isolate just the first.
	text := strings.Join([]string{
		"<!-- c3p:v1:begin -->",
		"first",
		"<!-- c3p:v1:end -->",
		"between",
		"<!-- c3p:v1:begin -->",
		"second",
		"<!-- c3p:v1:end -->",
	}, "\n")
	p, ok := findFirstPair(text)
	if !ok {
		t.Fatalf("expected to find a pair, got none")
	}
	if !strings.Contains(p.section, "first") {
		t.Fatalf("first pair section missing 'first': %q", p.section)
	}
	if strings.Contains(p.section, "second") {
		t.Fatalf("first pair section leaked into 'second' block: %q", p.section)
	}
	if strings.Contains(p.section, "between") {
		t.Fatalf("first pair section leaked into 'between' bytes: %q", p.section)
	}
}

// ─────────────────────────────────────────────────────────────
// ParseMarkers — happy / absent / malformed paths
// ─────────────────────────────────────────────────────────────

func TestParseMarkers_HappyPath(t *testing.T) {
	content := strings.Join([]string{
		"user header",
		"<!-- c3p:v1:begin -->",
		"<!-- Managed block. -->",
		"",
		"managed body line",
		"<!-- c3p:v1:end -->",
		"user footer",
		"",
	}, "\n")
	r := ParseMarkers(content)
	if r.Status != StatusValid {
		t.Fatalf("want valid, got %q", r.Status)
	}
	if r.Version != 1 {
		t.Fatalf("want version 1, got %d", r.Version)
	}
	if r.Before != "user header\n" {
		t.Fatalf("Before mismatch: %q", r.Before)
	}
	if r.After != "\nuser footer\n" {
		t.Fatalf("After mismatch: %q", r.After)
	}
	if !strings.Contains(r.Section, "managed body line") {
		t.Fatalf("Section missing body: %q", r.Section)
	}
	// Round-trip check — concatenating the splice fields with the canonical
	// markers reproduces the original content byte-for-byte (R45 splice
	// invariant).
	rebuilt := r.Before + "<!-- c3p:v1:begin -->" + r.Section + "<!-- c3p:v1:end -->" + r.After
	if rebuilt != content {
		t.Fatalf("splice not byte-identical:\nwant %q\ngot  %q", content, rebuilt)
	}
}

func TestParseMarkers_Absent(t *testing.T) {
	r := ParseMarkers("just user content\n")
	if r.Status != StatusAbsent {
		t.Fatalf("want absent, got %q", r.Status)
	}
}

func TestParseMarkers_LoneBegin(t *testing.T) {
	r := ParseMarkers("<!-- c3p:v1:begin -->\nno end\n")
	if r.Status != StatusMalformed {
		t.Fatalf("want malformed, got %q", r.Status)
	}
}

func TestParseMarkers_LoneEnd(t *testing.T) {
	r := ParseMarkers("no begin\n<!-- c3p:v1:end -->\n")
	if r.Status != StatusMalformed {
		t.Fatalf("want malformed, got %q", r.Status)
	}
}

func TestParseMarkers_VersionMismatch(t *testing.T) {
	text := "<!-- c3p:v1:begin -->\nbody\n<!-- c3p:v2:end -->\n"
	r := ParseMarkers(text)
	if r.Status != StatusMalformed {
		t.Fatalf("want malformed, got %q", r.Status)
	}
}

func TestParseMarkers_MultiBlock(t *testing.T) {
	text := strings.Join([]string{
		"<!-- c3p:v1:begin -->",
		"first",
		"<!-- c3p:v1:end -->",
		"",
		"<!-- c3p:v1:begin -->",
		"second",
		"<!-- c3p:v1:end -->",
	}, "\n")
	r := ParseMarkers(text)
	if r.Status != StatusMalformed {
		t.Fatalf("want malformed for multi-block, got %q", r.Status)
	}
}

func TestParseMarkers_HigherVersion(t *testing.T) {
	text := "<!-- c3p:v42:begin -->\nbody\n<!-- c3p:v42:end -->"
	r := ParseMarkers(text)
	if r.Status != StatusValid {
		t.Fatalf("want valid, got %q", r.Status)
	}
	if r.Version != 42 {
		t.Fatalf("want version 42, got %d", r.Version)
	}
}

func TestParseMarkers_EmptySection(t *testing.T) {
	text := strings.Join([]string{
		"before",
		"<!-- c3p:v1:begin -->",
		"<!-- c3p:v1:end -->",
		"after",
	}, "\n")
	r := ParseMarkers(text)
	if r.Status != StatusValid {
		t.Fatalf("want valid, got %q", r.Status)
	}
	if strings.TrimSpace(r.Section) != "" {
		t.Fatalf("expected empty trimmed section, got %q", r.Section)
	}
	if r.Before != "before\n" {
		t.Fatalf("Before mismatch: %q", r.Before)
	}
	if r.After != "\nafter" {
		t.Fatalf("After mismatch: %q", r.After)
	}
}

// ─────────────────────────────────────────────────────────────
// CRLF tolerance (cw6.5 followup)
// ─────────────────────────────────────────────────────────────

func TestParseMarkers_CRLF_HappyPath(t *testing.T) {
	text := strings.Join([]string{
		"user header",
		"<!-- c3p:v1:begin -->",
		"<!-- Managed block. -->",
		"",
		"managed body line",
		"<!-- c3p:v1:end -->",
		"user footer",
		"",
	}, "\r\n")
	r := ParseMarkers(text)
	if r.Status != StatusValid {
		t.Fatalf("want valid, got %q", r.Status)
	}
	if r.Version != 1 {
		t.Fatalf("want version 1, got %d", r.Version)
	}
	if r.Before != "user header\r\n" {
		t.Fatalf("Before should preserve CRLF, got %q", r.Before)
	}
	if r.After != "\r\nuser footer\r\n" {
		t.Fatalf("After should preserve CRLF, got %q", r.After)
	}
	if !strings.Contains(r.Section, "managed body line") {
		t.Fatalf("Section missing body: %q", r.Section)
	}
}

func TestParseMarkers_CRLF_LoneBeginIsMalformed(t *testing.T) {
	text := "<!-- c3p:v1:begin -->\r\nno end\r\n"
	r := ParseMarkers(text)
	if r.Status != StatusMalformed {
		t.Fatalf("want malformed, got %q", r.Status)
	}
}

func TestParseMarkers_CRLF_LoneEndIsMalformed(t *testing.T) {
	text := "no begin\r\n<!-- c3p:v1:end -->\r\n"
	r := ParseMarkers(text)
	if r.Status != StatusMalformed {
		t.Fatalf("want malformed, got %q", r.Status)
	}
}

func TestParseMarkers_CRLF_NoMarkersIsAbsent(t *testing.T) {
	r := ParseMarkers("user content\r\nmore content\r\n")
	if r.Status != StatusAbsent {
		t.Fatalf("want absent, got %q", r.Status)
	}
}

// ─────────────────────────────────────────────────────────────
// RenderManagedBlock — canonical bytes + round-trip
// ─────────────────────────────────────────────────────────────

func TestRenderManagedBlock_CanonicalV1(t *testing.T) {
	block := RenderManagedBlock("hello world\n", 1)
	for _, want := range []string{
		"<!-- c3p:v1:begin -->",
		"<!-- c3p:v1:end -->",
		"Managed block",
		"hello world",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("rendered block missing %q:\n%s", want, block)
		}
	}
}

func TestRenderManagedBlock_RoundTripsThroughParseMarkers(t *testing.T) {
	body := "line a\nline b\n"
	block := RenderManagedBlock(body, 1)
	r := ParseMarkers(block)
	if r.Status != StatusValid {
		t.Fatalf("want valid, got %q", r.Status)
	}
	if r.Version != 1 {
		t.Fatalf("want version 1, got %d", r.Version)
	}
	if !strings.Contains(r.Section, "line a") || !strings.Contains(r.Section, "line b") {
		t.Fatalf("section missing body lines: %q", r.Section)
	}
}

func TestRenderManagedBlock_EmptyBody(t *testing.T) {
	block := RenderManagedBlock("", 1)
	r := ParseMarkers(block)
	if r.Status != StatusValid {
		t.Fatalf("want valid, got %q", r.Status)
	}
	if r.Version != 1 {
		t.Fatalf("want version 1, got %d", r.Version)
	}
}

func TestRenderManagedBlock_DefaultsToV1WhenZero(t *testing.T) {
	// Defensive: callers that forget to pass a version should still get a
	// well-formed v1 block rather than `<!-- c3p:v0:begin -->`.
	block := RenderManagedBlock("body", 0)
	if !strings.Contains(block, "<!-- c3p:v1:begin -->") {
		t.Fatalf("default version should be v1: %q", block)
	}
}

func TestRenderManagedBlock_HigherVersion(t *testing.T) {
	block := RenderManagedBlock("body", 7)
	if !strings.Contains(block, "<!-- c3p:v7:begin -->") {
		t.Fatalf("expected v7 begin marker: %q", block)
	}
	if !strings.Contains(block, "<!-- c3p:v7:end -->") {
		t.Fatalf("expected v7 end marker: %q", block)
	}
	r := ParseMarkers(block)
	if r.Status != StatusValid || r.Version != 7 {
		t.Fatalf("v7 round-trip failed: status=%q version=%d", r.Status, r.Version)
	}
}

// ─────────────────────────────────────────────────────────────
// ExtractSectionBody — inverse of RenderManagedBlock framing
// ─────────────────────────────────────────────────────────────

func TestExtractSectionBody_RoundTrip(t *testing.T) {
	original := "user body line 1\nuser body line 2"
	block := RenderManagedBlock(original, 1)
	r := ParseMarkers(block)
	if r.Status != StatusValid {
		t.Fatalf("want valid, got %q", r.Status)
	}
	got := ExtractSectionBody(r.Section)
	if got != original {
		t.Fatalf("ExtractSectionBody round-trip mismatch:\nwant %q\ngot  %q", original, got)
	}
}

func TestExtractSectionBody_EmptyBodyRoundTrip(t *testing.T) {
	block := RenderManagedBlock("", 1)
	r := ParseMarkers(block)
	if r.Status != StatusValid {
		t.Fatalf("want valid, got %q", r.Status)
	}
	got := ExtractSectionBody(r.Section)
	if got != "" {
		t.Fatalf("expected empty body, got %q", got)
	}
}

// ExtractSectionBody recognizes the LF-framed shape RenderManagedBlock
// emits. A CRLF-framed section (e.g. a hand-edited Windows file) has no
// recognizable framing under the current regex and is returned verbatim —
// pinning the behavior so a future regex relaxation surfaces in CI.
func TestExtractSectionBody_CRLFReturnsVerbatim(t *testing.T) {
	crlfSection := "\r\n<!-- Managed block. -->\r\n\r\nbody\r\n"
	got := ExtractSectionBody(crlfSection)
	if got != crlfSection {
		t.Fatalf("expected verbatim return for CRLF section (no LF framing match), got %q", got)
	}
}

func TestExtractSectionBody_VerbatimWhenNoFraming(t *testing.T) {
	// A hand-edited section that lost the self-doc framing should be
	// returned as-is (no panic, no silent mutation).
	got := ExtractSectionBody("just a body\n")
	if got != "just a body\n" {
		t.Fatalf("expected verbatim return, got %q", got)
	}
}

// ─────────────────────────────────────────────────────────────
// InjectMarkersIntoFile — init helper
// ─────────────────────────────────────────────────────────────

func TestInjectMarkersIntoFile_AppendsWhenAbsent(t *testing.T) {
	original := "# Project\n\nuser-authored content\n"
	out, err := InjectMarkersIntoFile(original)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.HasPrefix(out, original) {
		t.Fatalf("user content not preserved at the start of output:\n%q", out)
	}
	if !strings.Contains(out, "<!-- c3p:v1:begin -->") || !strings.Contains(out, "<!-- c3p:v1:end -->") {
		t.Fatalf("markers not appended: %q", out)
	}
	r := ParseMarkers(out)
	if r.Status != StatusValid {
		t.Fatalf("output does not parse cleanly: %q", r.Status)
	}
}

func TestInjectMarkersIntoFile_NoOpWhenPresent(t *testing.T) {
	original := strings.Join([]string{
		"# Project",
		"",
		"<!-- c3p:v1:begin -->",
		"managed body",
		"<!-- c3p:v1:end -->",
		"",
		"footer",
	}, "\n")
	out, err := InjectMarkersIntoFile(original)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out != original {
		t.Fatalf("expected no-op, got mutation:\nwant %q\ngot  %q", original, out)
	}
}

func TestInjectMarkersIntoFile_Idempotent(t *testing.T) {
	original := "# user content\n"
	once, err := InjectMarkersIntoFile(original)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	twice, err := InjectMarkersIntoFile(once)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if twice != once {
		t.Fatalf("not idempotent:\nonce  %q\ntwice %q", once, twice)
	}
}

func TestInjectMarkersIntoFile_PreservesNoTrailingNewline(t *testing.T) {
	original := "no trailing newline"
	out, err := InjectMarkersIntoFile(original)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.HasPrefix(out, original) {
		t.Fatalf("user content not preserved: %q", out)
	}
	// A separator newline must have been inserted (otherwise the begin
	// marker would be glued onto the user's last line).
	if !strings.HasPrefix(out[len(original):], "\n") {
		t.Fatalf("expected separating newline after no-trailing-newline content, got %q", out[len(original):])
	}
}

func TestInjectMarkersIntoFile_EmptyInput(t *testing.T) {
	out, err := InjectMarkersIntoFile("")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	r := ParseMarkers(out)
	if r.Status != StatusValid {
		t.Fatalf("empty-input output should parse cleanly, got %q", r.Status)
	}
}

func TestInjectMarkersIntoFile_LoneBeginFailsClosed(t *testing.T) {
	original := "<!-- c3p:v1:begin -->\nno end\n"
	_, err := InjectMarkersIntoFile(original)
	if err == nil {
		t.Fatalf("expected MalformedMarkersError, got nil")
	}
	if !IsMalformedMarkersError(err) {
		t.Fatalf("expected MalformedMarkersError, got %T: %v", err, err)
	}
	var mme *MalformedMarkersError
	if !stderrors.As(err, &mme) {
		t.Fatalf("errors.As did not unwrap MalformedMarkersError")
	}
}

func TestInjectMarkersIntoFile_LoneEndFailsClosed(t *testing.T) {
	original := "no begin\n<!-- c3p:v1:end -->\n"
	_, err := InjectMarkersIntoFile(original)
	if !IsMalformedMarkersError(err) {
		t.Fatalf("expected MalformedMarkersError, got %v", err)
	}
}

func TestInjectMarkersIntoFile_VersionMismatchFailsClosed(t *testing.T) {
	original := strings.Join([]string{
		"<!-- c3p:v1:begin -->",
		"body",
		"<!-- c3p:v2:end -->",
		"",
	}, "\n")
	_, err := InjectMarkersIntoFile(original)
	if !IsMalformedMarkersError(err) {
		t.Fatalf("expected MalformedMarkersError, got %v", err)
	}
}

func TestInjectMarkersIntoFile_MultiBlockFailsClosed(t *testing.T) {
	original := strings.Join([]string{
		"<!-- c3p:v1:begin -->",
		"first",
		"<!-- c3p:v1:end -->",
		"",
		"<!-- c3p:v1:begin -->",
		"second",
		"<!-- c3p:v1:end -->",
		"",
	}, "\n")
	_, err := InjectMarkersIntoFile(original)
	if !IsMalformedMarkersError(err) {
		t.Fatalf("expected MalformedMarkersError, got %v", err)
	}
}

func TestInjectMarkersIntoFile_PreservesEmbeddedHTML(t *testing.T) {
	original := strings.Join([]string{
		"# Project",
		"",
		"<details><summary>more</summary>",
		"Some <b>html</b> inside markdown.",
		"</details>",
		"",
	}, "\n")
	out, err := InjectMarkersIntoFile(original)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.HasPrefix(out, original) {
		t.Fatalf("user content not preserved byte-for-byte: %q", out)
	}
	r := ParseMarkers(out)
	if r.Status != StatusValid {
		t.Fatalf("output does not parse cleanly: %q", r.Status)
	}
	if !strings.Contains(r.Before, "<details>") {
		t.Fatalf("Before should retain user HTML: %q", r.Before)
	}
}

// ─────────────────────────────────────────────────────────────
// Namespace tail handling (forward-compat for vN namespaced markers)
// ─────────────────────────────────────────────────────────────

// The canonical regex captures `[^>]*` between the version and the closing
// `-->`. Today this is whitespace-only (` `). Tomorrow a namespaced form
// like `<!-- c3p:v1:begin :ns -->...<!-- c3p:v1:end :ns -->` may ship; the
// parser must require begin and end tails to match, mirroring the TS
// regex's \2 backreference.

func TestParseMarkers_TailMustMatchBetweenBeginAndEnd(t *testing.T) {
	mismatched := "<!-- c3p:v1:begin :ns1 -->body<!-- c3p:v1:end :ns2 -->"
	r := ParseMarkers(mismatched)
	if r.Status != StatusMalformed {
		t.Fatalf("want malformed for tail mismatch, got %q (section=%q)", r.Status, r.Section)
	}
}

func TestParseMarkers_MatchingNonDefaultTailIsValid(t *testing.T) {
	// Namespace tails that match between :begin and :end must parse cleanly.
	matched := "<!-- c3p:v1:begin :ns -->body<!-- c3p:v1:end :ns -->"
	r := ParseMarkers(matched)
	if r.Status != StatusValid {
		t.Fatalf("want valid for matching namespace tail, got %q", r.Status)
	}
	if r.Section != "body" {
		t.Fatalf("section mismatch with namespaced markers: %q", r.Section)
	}
}

// ─────────────────────────────────────────────────────────────
// EARS coverage anchors — R44 (validate), R45 (splice), R46 (boundary)
// ─────────────────────────────────────────────────────────────

// R44 — validate at preflight: both opening and closing markers present.
// ParseMarkers is the contract: a missing opener OR closer is malformed.
func TestR44_ValidateRequiresBothOpenAndClose(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"begin only", "<!-- c3p:v1:begin -->\nbody\n"},
		{"end only", "body\n<!-- c3p:v1:end -->\n"},
		{"version mismatch", "<!-- c3p:v1:begin -->\nbody\n<!-- c3p:v2:end -->\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ParseMarkers(tc.content)
			if r.Status != StatusMalformed {
				t.Fatalf("R44: want malformed for %q, got %q", tc.name, r.Status)
			}
		})
	}
}

// R45 — parse for splice: the splice fields reconstruct the original file
// byte-for-byte when wrapped in canonical begin/end markers. Splice itself
// lives in D5; this anchors the contract D5 will rely on.
func TestR45_SpliceIsByteIdentical(t *testing.T) {
	content := "header line\n<!-- c3p:v1:begin -->\nbody\n<!-- c3p:v1:end -->\nfooter\n"
	r := ParseMarkers(content)
	if r.Status != StatusValid {
		t.Fatalf("R45 prereq: parse failed %q", r.Status)
	}
	rebuilt := r.Before + "<!-- c3p:v1:begin -->" + r.Section + "<!-- c3p:v1:end -->" + r.After
	if rebuilt != content {
		t.Fatalf("R45: splice not byte-identical:\nwant %q\ngot  %q", content, rebuilt)
	}
}

// R46 — managed-slice byte boundary. The Section field is exactly the bytes
// between the closing `>` of the begin marker and the opening `<` of the end
// marker; nothing more, nothing less. D6 fingerprinting will hash exactly
// this slice.
func TestR46_SectionBoundaryIsExact(t *testing.T) {
	content := "<!-- c3p:v1:begin -->ABC<!-- c3p:v1:end -->"
	r := ParseMarkers(content)
	if r.Status != StatusValid {
		t.Fatalf("R46 prereq: parse failed %q", r.Status)
	}
	if r.Section != "ABC" {
		t.Fatalf("R46: section should be exactly the inter-marker bytes, got %q", r.Section)
	}
}

// R46 — boundary holds across a CRLF body too: section retains the exact
// CR/LF bytes between the markers without normalization.
func TestR46_SectionBoundary_CRLF(t *testing.T) {
	content := "<!-- c3p:v1:begin -->\r\nbody\r\n<!-- c3p:v1:end -->"
	r := ParseMarkers(content)
	if r.Status != StatusValid {
		t.Fatalf("R46 CRLF prereq: parse failed %q", r.Status)
	}
	if r.Section != "\r\nbody\r\n" {
		t.Fatalf("R46 CRLF: section should preserve CRLF bytes, got %q", r.Section)
	}
}
