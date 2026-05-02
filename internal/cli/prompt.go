// Interactive prompt for the drift gate. Mirrors src/cli/prompt.ts.
// Reads a single character from stdin (cooked-mode line-buffered is fine —
// the user presses Enter to confirm). Always falls back to abort on EOF.
package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/htxryan/c3p/internal/drift"
)

// GatePromptOptions configures the interactive prompt.
type GatePromptOptions struct {
	In  io.Reader // defaults to os.Stdin
	Out io.Writer // banner/prompt destination, defaults to os.Stderr
}

// PromptGateChoice asks the user to choose discard|persist|abort. EOF or
// invalid input falls back to abort so a piped-in command never blocks.
func PromptGateChoice(opts GatePromptOptions) drift.GateChoice {
	if opts.In == nil || opts.Out == nil {
		// Defensive: production callers thread os.Stdin/os.Stderr explicitly.
		// Falling back to abort keeps the no-stdin path safe.
		return drift.GateChoiceAbort
	}
	reader := bufio.NewReader(opts.In)
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprintln(opts.Out, "Live .claude/ has uncommitted edits. Choose:")
		fmt.Fprintln(opts.Out, "  [d] discard — drop the edits (we back up to .meta/backup/)")
		fmt.Fprintln(opts.Out, "  [p] persist — write the edits back into the active profile first")
		fmt.Fprintln(opts.Out, "  [a] abort   — leave .claude/ as-is and exit")
		fmt.Fprint(opts.Out, "[d/p/a]: ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return drift.GateChoiceAbort
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "d", "discard":
			return drift.GateChoiceDiscard
		case "p", "persist":
			return drift.GateChoicePersist
		case "a", "abort", "":
			return drift.GateChoiceAbort
		}
		fmt.Fprintln(opts.Out, "please enter d, p, or a.")
	}
	return drift.GateChoiceAbort
}
