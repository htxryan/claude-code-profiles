// Shared formatting helpers for command handlers. Local to commands/ so
// it doesn't fight the cli → commands import direction.
package commands

import (
	"path/filepath"
	"regexp"
	"strings"
)

// profileDir returns the absolute path to a profile's directory under
// .claude-profiles/<projectRoot>/.
func profileDir(projectRoot, name string) string {
	return filepath.Join(projectRoot, ".claude-profiles", name)
}

// ansiPattern matches ANSI escape sequences for visible-width calculations.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// visibleWidth returns the rune count of s after stripping ANSI escapes.
func visibleWidth(s string) int {
	stripped := ansiPattern.ReplaceAllString(s, "")
	return len([]rune(stripped))
}

// renderTable renders rows as a fixed-width table. The last column is not
// padded.
func renderTable(rows [][]string) string {
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
