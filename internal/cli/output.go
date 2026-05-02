// OutputChannel centralises stdout/stderr writes. --json silences ALL
// human-readable output; JSON payloads always go to stdout, errors always to
// stderr. Tests inject buffer-backed channels for snapshot assertions.
//
// Mirrors src/cli/output.ts.
package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

// OutputChannel is the abstraction every command writes through.
type OutputChannel interface {
	// Print writes human-readable text to stdout. No-op in --json or --quiet mode.
	Print(text string)
	// JSON writes a single JSON-serializable payload to stdout. Always writes,
	// even in quiet mode; designed for the --json invariant (one envelope per cmd).
	JSON(payload interface{})
	// Warn writes human-readable warning to stderr. No-op in --json or --quiet mode.
	Warn(text string)
	// Error writes to stderr (always — even in --json mode).
	Error(text string)
	// Phase writes a transient progress hint to stderr. Silenced under --json/--quiet.
	Phase(text string)
	// JSONMode reports whether --json was passed (some commands branch on this).
	JSONMode() bool
	// IsTTY drives colour/unicode decisions. Threaded so tests can pin.
	IsTTY() bool
}

// OutputOptions configures createOutput. Callers default Stdout/Stderr to
// the process streams; tests inject buffers.
type OutputOptions struct {
	JSON   bool
	Quiet  bool
	IsTTY  bool
	Stdout io.Writer
	Stderr io.Writer
}

type basicOutput struct {
	stdout io.Writer
	stderr io.Writer
	json   bool
	quiet  bool
	isTty  bool
}

// NewOutput builds an OutputChannel. Production callers pass {JSON, Quiet,
// IsTTY, real stdout/stderr}; tests inject buffers.
func NewOutput(opts OutputOptions) OutputChannel {
	out := opts.Stdout
	if out == nil {
		out = os.Stdout
	}
	err := opts.Stderr
	if err == nil {
		err = os.Stderr
	}
	return &basicOutput{
		stdout: out,
		stderr: err,
		json:   opts.JSON,
		quiet:  opts.Quiet,
		isTty:  opts.IsTTY,
	}
}

func (b *basicOutput) silenced() bool { return b.json || b.quiet }

func (b *basicOutput) Print(text string) {
	if b.silenced() {
		return
	}
	writeSafe(b.stdout, ensureNewline(text))
}

func (b *basicOutput) JSON(payload interface{}) {
	// Marshal via the central jsonout package so every JSON envelope routes
	// through one deterministic, HTML-escape-disabled encoder. We import it
	// indirectly to avoid a circular dependency: jsonout has no deps on cli.
	bytes, err := marshalJSONLine(payload)
	if err != nil {
		// Defensive: a programmer-error throw here must not escape as exit-2.
		errLine := []byte(`{"error":"json-serialize-failed","detail":"` + escapeJSONString(err.Error()) + `"}` + "\n")
		writeSafe(b.stdout, string(errLine))
		return
	}
	writeSafe(b.stdout, string(bytes))
}

func (b *basicOutput) Warn(text string) {
	if b.silenced() {
		return
	}
	writeSafe(b.stderr, ensureNewline(text))
}

func (b *basicOutput) Error(text string) {
	writeSafe(b.stderr, ensureNewline(text))
}

func (b *basicOutput) Phase(text string) {
	if b.silenced() {
		return
	}
	writeSafe(b.stderr, ensureNewline(text))
}

func (b *basicOutput) JSONMode() bool { return b.json }
func (b *basicOutput) IsTTY() bool    { return b.isTty }

func ensureNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

// writeSafe ignores write errors. The CLI may be piped into head/grep
// which closes the read end; an EPIPE here must not crash. We silently
// abandon further output.
func writeSafe(w io.Writer, s string) {
	_, _ = w.Write([]byte(s))
}

// escapeJSONString does just enough escaping for the json-serialize-failed
// fallback to remain valid JSON when the error message contains a backslash
// or quote. Control chars below 0x20 are encoded as \uXXXX (JSON requires
// them to be escaped — silently dropping would produce malformed JSON).
func escapeJSONString(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Style helpers — colour + unicode glyphs for human-readable output. Mirrors
// src/cli/output.ts:Style. All decisions derived from the inputs; no module-
// level state so JSON callers stay reproducible.

// Style is the colour/glyph helper.
type Style struct {
	Color   bool
	Unicode bool
}

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiCyan   = "\x1b[36m"
)

// NewStyle builds a Style helper. Production callers go through ResolveNoColor
// so the --no-color flag and NO_COLOR env are combined identically everywhere.
func NewStyle(isTty, noColor bool) Style {
	color := isTty && !noColor
	// Windows historically chokes on box-drawing/check glyphs in cmd.exe; we
	// also gate unicode on NO_COLOR because users frequently set NO_COLOR in
	// log capture pipelines that strip non-ASCII.
	unicode := color && runtime.GOOS != "windows"
	return Style{Color: color, Unicode: unicode}
}

// ResolveNoColor combines the --no-color flag and the NO_COLOR env var.
// Either being set disables colour. https://no-color.org/ semantics: any
// value (even empty string) of NO_COLOR disables colour.
func ResolveNoColor(globalNoColor bool) bool {
	if globalNoColor {
		return true
	}
	_, set := os.LookupEnv("NO_COLOR")
	return set
}

func (s Style) paint(code, text string) string {
	if !s.Color {
		return text
	}
	return code + text + ansiReset
}

// OK returns the success glyph + text.
func (s Style) OK(text string) string {
	glyph := "[ok]"
	if s.Unicode {
		glyph = "✓"
	}
	return s.paint(ansiGreen, glyph) + " " + text
}

// Skip returns the skipped glyph + dim text.
func (s Style) Skip(text string) string {
	glyph := "[skip]"
	if s.Unicode {
		glyph = "⊙"
	}
	return s.paint(ansiDim, glyph) + " " + s.paint(ansiDim, text)
}

// Warn returns the warning glyph + text.
func (s Style) Warn(text string) string {
	glyph := "[warn]"
	if s.Unicode {
		glyph = "!"
	}
	return s.paint(ansiYellow, glyph) + " " + text
}

// Fail returns the failure glyph + text (red).
func (s Style) Fail(text string) string {
	glyph := "[x]"
	if s.Unicode {
		glyph = "✗"
	}
	return s.paint(ansiRed, glyph) + " " + text
}

// Banner renders a banner header.
func (s Style) Banner(title string) string {
	if !s.Unicode {
		return "== " + title + " =="
	}
	return s.paint(ansiCyan, "╭ "+title+" ─")
}

// Dim wraps text in dim ANSI.
func (s Style) Dim(text string) string {
	return s.paint(ansiDim, text)
}

// Bold wraps text in bold ANSI.
func (s Style) Bold(text string) string {
	return s.paint(ansiBold, text)
}

// DriftStatus colours a drift status word per the spec at-a-glance contract.
func (s Style) DriftStatus(status, text string) string {
	switch status {
	case "clean":
		return s.paint(ansiGreen, text)
	case "modified", "added":
		return s.paint(ansiYellow, text)
	case "deleted", "unrecoverable":
		return s.paint(ansiRed, text)
	}
	return text
}

// ByteDelta colours a byte-count delta by magnitude.
func (s Style) ByteDelta(text string, magnitude int) string {
	if magnitude < 100 {
		return s.paint(ansiDim, text)
	}
	if magnitude > 10240 {
		return s.paint(ansiBold, text)
	}
	return text
}
