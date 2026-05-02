// Pure formatters: relativeTime, table rendering, error formatting.
// Mirrors src/cli/format.ts but only the surface needed by Go commands.
package cli

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
	"github.com/htxryan/c3p/internal/resolver"
)

// RelativeTime returns "3h ago" / "5d ago" / "just now" for the given timestamp.
// Empty input returns "never".
func RelativeTime(iso string) string {
	if iso == "" {
		return "never"
	}
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		// Try the millisecond-precision UTC format c3p writes.
		t, err = time.Parse("2006-01-02T15:04:05.000Z", iso)
		if err != nil {
			return iso
		}
	}
	d := time.Since(t)
	if d < 0 {
		return "just now"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	case d < 30*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	default:
		months := int(d.Hours() / 24 / 30)
		if months == 1 {
			return "1mo ago"
		}
		return fmt.Sprintf("%dmo ago", months)
	}
}

// TimestampWithRelative renders "<iso> (Xh ago)" for human output.
func TimestampWithRelative(iso string) string {
	if iso == "" {
		return "never"
	}
	return iso + " (" + RelativeTime(iso) + ")"
}

// ansiPattern matches ANSI escape sequences for visible-width calculations.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// visibleWidth returns the rune count of s after stripping ANSI escapes.
func visibleWidth(s string) int {
	stripped := ansiPattern.ReplaceAllString(s, "")
	return len([]rune(stripped))
}

// RenderTable renders a 2D string slice as a fixed-width table. The last
// column is not padded so trailing whitespace doesn't pollute output. ANSI
// escapes in cells don't count toward width. Rows shorter than width are
// left-justified to match TS behaviour.
func RenderTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	widths := make([]int, cols)
	for _, r := range rows {
		for i, cell := range r {
			w := visibleWidth(cell)
			if w > widths[i] {
				widths[i] = w
			}
		}
	}
	var b strings.Builder
	for i, r := range rows {
		for j := 0; j < cols; j++ {
			cell := ""
			if j < len(r) {
				cell = r[j]
			}
			if j == cols-1 {
				b.WriteString(cell)
			} else {
				b.WriteString(cell)
				pad := widths[j] - visibleWidth(cell) + 2
				if pad < 1 {
					pad = 1
				}
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
		if i < len(rows)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// FormatError renders any error suitable for stderr emission. Pipeline
// errors get their .Error() (which already names file/profile per §7).
func FormatError(err error) string {
	if err == nil {
		return ""
	}
	return "c3p: " + err.Error()
}

// FormatResolutionWarnings renders warnings as one line each, prefixed by
// the warning code. Returns empty string when no warnings.
func FormatResolutionWarnings(warnings []resolver.ResolutionWarning) string {
	if len(warnings) == 0 {
		return ""
	}
	var b strings.Builder
	for i, w := range warnings {
		if i > 0 {
			b.WriteByte('\n')
		}
		if w.Source != "" {
			b.WriteString(fmt.Sprintf("warning: %s [%s] (%s)", w.Message, w.Code, w.Source))
		} else {
			b.WriteString(fmt.Sprintf("warning: %s [%s]", w.Message, w.Code))
		}
	}
	return b.String()
}

// PluralIze is a tiny helper for "1 file" vs "2 files".
func PluralIze(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// IsPipelineErrorCode reports whether err is a pipeline error of the given
// code. Convenience wrapper for error-code dispatch.
func IsPipelineErrorCode(err error, code pipelineerrors.Code) bool {
	pe := pipelineerrors.AsPipelineError(err)
	return pe != nil && pe.ErrorCode() == code
}
