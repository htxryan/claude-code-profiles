// Preview helpers — unified-diff renderer (capped lines) and head-preview
// for added/deleted entries. Mirrors src/cli/preview.ts but minimal: a
// per-line LCS-free diff that's good enough for "show me what changed".
package cli

import (
	"bytes"
	"fmt"
	"strings"
)

// PreviewMaxLines bounds the rendered preview per file. Matches TS.
const PreviewMaxLines = 20

// IsBinary returns true iff the first 8KB of bytes contain a NUL — the same
// heuristic git uses. Avoids printing garbled binary content.
func IsBinary(data []byte) bool {
	limit := 8192
	if len(data) < limit {
		limit = len(data)
	}
	return bytes.IndexByte(data[:limit], 0) >= 0
}

// RenderHeadPreview returns the first PreviewMaxLines of data prefixed with
// the given marker (e.g. "+ " for added, "- " for deleted). Long lines are
// truncated to 200 chars to keep terminal output bounded.
func RenderHeadPreview(data []byte, marker string) string {
	if IsBinary(data) {
		return "  (binary file omitted)"
	}
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > PreviewMaxLines {
		lines = lines[:PreviewMaxLines]
	}
	var b strings.Builder
	for _, ln := range lines {
		hadNewline := strings.HasSuffix(ln, "\n")
		if hadNewline {
			ln = strings.TrimSuffix(ln, "\n")
		}
		if len(ln) > 200 {
			ln = ln[:200] + "…"
		}
		b.WriteString(marker)
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	if len(lines) == PreviewMaxLines {
		b.WriteString("  …\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderUnifiedDiff returns a minimal +/-/space line-diff of a vs b, capped
// at PreviewMaxLines lines. The algorithm is a simple per-line walk: equal
// runs become " " context, removals become "-", additions "+". No LCS, so
// blocks of mixed insert/delete may misalign — acceptable for previews.
func RenderUnifiedDiff(a, b []byte) string {
	if IsBinary(a) || IsBinary(b) {
		return "  (binary file omitted)"
	}
	la := strings.Split(string(a), "\n")
	lb := strings.Split(string(b), "\n")
	// Trim a single trailing empty line (the final newline-induced empty)
	if len(la) > 0 && la[len(la)-1] == "" {
		la = la[:len(la)-1]
	}
	if len(lb) > 0 && lb[len(lb)-1] == "" {
		lb = lb[:len(lb)-1]
	}
	// Walk: when lines match, emit " " context; otherwise emit -/+ pairs.
	i, j := 0, 0
	var lines []string
	for i < len(la) && j < len(lb) {
		if la[i] == lb[j] {
			lines = append(lines, " "+truncate(la[i], 200))
			i++
			j++
			continue
		}
		// Look ahead a small window for a sync point.
		const window = 8
		matched := false
		for k := 1; k <= window && (i+k < len(la) || j+k < len(lb)); k++ {
			// la[i+k] == lb[j]?
			if i+k < len(la) && la[i+k] == lb[j] {
				for x := 0; x < k; x++ {
					lines = append(lines, "-"+truncate(la[i+x], 200))
				}
				i += k
				matched = true
				break
			}
			// la[i] == lb[j+k]?
			if j+k < len(lb) && la[i] == lb[j+k] {
				for x := 0; x < k; x++ {
					lines = append(lines, "+"+truncate(lb[j+x], 200))
				}
				j += k
				matched = true
				break
			}
		}
		if !matched {
			// Substitution.
			lines = append(lines, "-"+truncate(la[i], 200))
			lines = append(lines, "+"+truncate(lb[j], 200))
			i++
			j++
		}
		if len(lines) >= PreviewMaxLines {
			break
		}
	}
	for ; i < len(la) && len(lines) < PreviewMaxLines; i++ {
		lines = append(lines, "-"+truncate(la[i], 200))
	}
	for ; j < len(lb) && len(lines) < PreviewMaxLines; j++ {
		lines = append(lines, "+"+truncate(lb[j], 200))
	}
	if len(lines) >= PreviewMaxLines {
		lines = append(lines, "…")
	}
	return strings.Join(lines, "\n")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// FormatByteDelta returns a "(+45 -3 ~10 bytes)" delta string. Each component
// is omitted when zero. Returns "(no change)" when all three are zero.
func FormatByteDelta(added, removed, changed int) string {
	if added == 0 && removed == 0 && changed == 0 {
		return "(no change)"
	}
	parts := []string{}
	if added > 0 {
		parts = append(parts, fmt.Sprintf("+%d", added))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("-%d", removed))
	}
	if changed > 0 {
		parts = append(parts, fmt.Sprintf("~%d", changed))
	}
	return "(" + strings.Join(parts, " ") + " bytes)"
}
