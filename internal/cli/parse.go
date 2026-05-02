// Hand-rolled argv parser. Mirrors src/cli/parse.ts conventions:
//   - first non-flag token is the verb
//   - global flags (--json, --cwd, --on-drift, --no-color, --quiet, --wait)
//     may appear anywhere in argv, before or after the verb
//   - verb-specific flags scoped per verb
//   - positional args after the verb are required where named (we return
//     ParseError naming the missing arg, never panic)
//
// Cobra is intentionally NOT used: cobra's parsing rules differ from the TS
// hand-rolled parser (flag positioning, error messages, --version
// short-circuit semantics) and the cross-language IV harness compares both
// surfaces. Keeping the parser surface tight to TS makes parity test work
// once instead of twice.
package cli

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/htxryan/claude-code-config-profiles/internal/drift"
)

// ParseError carries the human-readable message and a help-requested flag.
type ParseError struct {
	Message       string
	HelpRequested bool
}

func (e *ParseError) Error() string { return e.Message }

// ParseResult is either ok (with a typed invocation) or an error.
type ParseResult struct {
	Ok         bool
	Invocation ParsedInvocation
	Err        *ParseError
}

var verbs = map[string]bool{
	"init":        true,
	"list":        true,
	"use":         true,
	"status":      true,
	"drift":       true,
	"diff":        true,
	"new":         true,
	"validate":    true,
	"sync":        true,
	"hook":        true,
	"doctor":      true,
	"completions": true,
	"help":        true,
	// `hello` is intentionally hidden from help but parses + dispatches.
	"hello": true,
}

var completionShells = map[string]CompletionShell{
	"bash": ShellBash,
	"zsh":  ShellZsh,
	"fish": ShellFish,
}

// ParseArgs parses argv (without the leading binary name) plus an optional
// default cwd (production callers pass os.Getwd()).
func ParseArgs(argv []string, defaultCwd string) ParseResult {
	g := GlobalOptions{Cwd: defaultCwd}
	helpFlag := false
	versionFlag := false

	var verbAndArgs []string
	for i := 0; i < len(argv); i++ {
		t := argv[i]
		switch {
		case t == "--json":
			g.JSON = true
		case t == "--help" || t == "-h":
			helpFlag = true
		case t == "--version" || t == "-V":
			versionFlag = true
		case t == "--cwd":
			next, ok := nextArg(argv, i)
			if !ok || strings.HasPrefix(next, "-") {
				return errResult("--cwd requires a path argument", false)
			}
			if next == "" {
				return errResult("--cwd requires a non-empty path", false)
			}
			g.Cwd = next
			i++
		case strings.HasPrefix(t, "--cwd="):
			v := strings.TrimPrefix(t, "--cwd=")
			if v == "" {
				return errResult("--cwd requires a non-empty path", false)
			}
			g.Cwd = v
		case t == "--on-drift":
			next, ok := nextArg(argv, i)
			if !ok {
				return errResult("--on-drift requires a value (discard|persist|abort)", false)
			}
			c, ok := parseOnDrift(next)
			if !ok {
				return errResult(fmt.Sprintf(`--on-drift must be discard|persist|abort, got %q`, next), false)
			}
			g.OnDrift = c
			i++
		case strings.HasPrefix(t, "--on-drift="):
			v := strings.TrimPrefix(t, "--on-drift=")
			c, ok := parseOnDrift(v)
			if !ok {
				return errResult(fmt.Sprintf(`--on-drift must be discard|persist|abort, got %q`, v), false)
			}
			g.OnDrift = c
		case t == "--no-color":
			g.NoColor = true
		case t == "--quiet" || t == "-q":
			g.Quiet = true
		case t == "--wait":
			g.WaitMs = 30_000
			g.WaitSet = true
		case strings.HasPrefix(t, "--wait="):
			v := strings.TrimPrefix(t, "--wait=")
			seconds, err := strconv.ParseFloat(v, 64)
			if err != nil || seconds < 0 {
				return errResult(fmt.Sprintf(`--wait must be a non-negative number of seconds; got %q`, v), false)
			}
			g.WaitMs = int64(math.Round(seconds * 1000))
			g.WaitSet = true
		case t == "--non-interactive":
			g.NonInteractive = true
		default:
			verbAndArgs = append(verbAndArgs, t)
		}
	}

	// CI=true env auto-detection (only when the flag isn't already set).
	if !g.NonInteractive && os.Getenv("CI") == "true" {
		g.NonInteractive = true
	}

	if g.Quiet && g.JSON {
		return errResult("--quiet and --json are mutually exclusive", false)
	}

	if versionFlag {
		return ok(ParsedInvocation{Command: Command{Kind: KindVersion}, Global: g})
	}

	if len(verbAndArgs) == 0 {
		if helpFlag {
			return ok(ParsedInvocation{Command: Command{Kind: KindHelp}, Global: g})
		}
		return errResult(`missing command; run "c3p --help" for usage`, true)
	}

	verb := verbAndArgs[0]
	rest := verbAndArgs[1:]
	if !verbs[verb] {
		return errResult(fmt.Sprintf(`unknown command %q; run "c3p --help" for usage`, verb), false)
	}

	if verb == "help" {
		if len(rest) == 0 {
			return ok(ParsedInvocation{Command: Command{Kind: KindHelp}, Global: g})
		}
		if len(rest) > 1 {
			return errResult(fmt.Sprintf(`help takes at most one argument; got %q`, strings.Join(rest, " ")), false)
		}
		return ok(ParsedInvocation{Command: Command{Kind: KindHelp, HelpVerb: rest[0]}, Global: g})
	}

	if helpFlag {
		return ok(ParsedInvocation{Command: Command{Kind: KindHelp, HelpVerb: verb}, Global: g})
	}

	switch verb {
	case "init":
		return parseInit(rest, g)
	case "list":
		if len(rest) > 0 {
			return errResult(fmt.Sprintf(`list takes no arguments; got %q`, strings.Join(rest, " ")), false)
		}
		return ok(ParsedInvocation{Command: Command{Kind: KindList}, Global: g})
	case "status":
		if len(rest) > 0 {
			return errResult(fmt.Sprintf(`status takes no arguments; got %q`, strings.Join(rest, " ")), false)
		}
		return ok(ParsedInvocation{Command: Command{Kind: KindStatus}, Global: g})
	case "sync":
		if len(rest) > 0 {
			return errResult(fmt.Sprintf(`sync takes no arguments; got %q`, strings.Join(rest, " ")), false)
		}
		return ok(ParsedInvocation{Command: Command{Kind: KindSync}, Global: g})
	case "hello":
		if len(rest) > 0 {
			return errResult(fmt.Sprintf(`hello takes no arguments; got %q`, strings.Join(rest, " ")), false)
		}
		return ok(ParsedInvocation{Command: Command{Kind: KindHello}, Global: g})
	case "use":
		if len(rest) == 0 {
			return errResult("use requires a profile name", false)
		}
		if len(rest) > 1 {
			return errResult(fmt.Sprintf(`use takes one argument; got %q`, strings.Join(rest, " ")), false)
		}
		return ok(ParsedInvocation{Command: Command{Kind: KindUse, Profile: rest[0]}, Global: g})
	case "drift":
		return parseDrift(rest, g)
	case "diff":
		return parseDiff(rest, g)
	case "new":
		return parseNew(rest, g)
	case "validate":
		return parseValidate(rest, g)
	case "hook":
		return parseHook(rest, g)
	case "doctor":
		return parseDoctor(rest, g)
	case "completions":
		return parseCompletions(rest, g)
	}

	return errResult(fmt.Sprintf("unreachable: unknown verb %q", verb), false)
}

func parseInit(rest []string, g GlobalOptions) ParseResult {
	starter := "default"
	seed := true
	hook := true
	var positional []string
	for i := 0; i < len(rest); i++ {
		t := rest[i]
		switch {
		case t == "--no-seed":
			seed = false
		case t == "--no-hook":
			hook = false
		case t == "--starter":
			next, ok := nextArg(rest, i)
			if !ok || strings.HasPrefix(next, "-") {
				return errResult("--starter requires a value", false)
			}
			starter = next
			i++
		case strings.HasPrefix(t, "--starter="):
			v := strings.TrimPrefix(t, "--starter=")
			if v == "" {
				return errResult("--starter requires a non-empty value", false)
			}
			starter = v
		case strings.HasPrefix(t, "--"):
			return errResult(fmt.Sprintf(`init: unknown flag %q`, t), false)
		default:
			positional = append(positional, t)
		}
	}
	if len(positional) > 0 {
		return errResult(fmt.Sprintf(`init takes no positional arguments; got %q`, strings.Join(positional, " ")), false)
	}
	return ok(ParsedInvocation{
		Command: Command{Kind: KindInit, Starter: starter, Seed: seed, Hook: hook},
		Global:  g,
	})
}

func parseDrift(rest []string, g GlobalOptions) ParseResult {
	var preCommitWarn, verbose, preview bool
	var positional []string
	for _, t := range rest {
		switch {
		case t == "--pre-commit-warn":
			preCommitWarn = true
		case t == "--verbose":
			verbose = true
		case t == "--preview":
			preview = true
		case strings.HasPrefix(t, "--"):
			return errResult(fmt.Sprintf(`drift: unknown flag %q`, t), false)
		default:
			positional = append(positional, t)
		}
	}
	if len(positional) > 0 {
		return errResult(fmt.Sprintf(`drift takes no positional arguments; got %q`, strings.Join(positional, " ")), false)
	}
	return ok(ParsedInvocation{
		Command: Command{Kind: KindDrift, PreCommitWarn: preCommitWarn, Verbose: verbose, Preview: preview},
		Global:  g,
	})
}

func parseDiff(rest []string, g GlobalOptions) ParseResult {
	var preview bool
	var positional []string
	for _, t := range rest {
		switch {
		case t == "--preview":
			preview = true
		case strings.HasPrefix(t, "--"):
			return errResult(fmt.Sprintf(`diff: unknown flag %q`, t), false)
		default:
			positional = append(positional, t)
		}
	}
	if len(positional) == 0 {
		return errResult("diff requires at least one profile name", false)
	}
	if len(positional) > 2 {
		return errResult(fmt.Sprintf(`diff takes one or two profile names; got %q`, strings.Join(positional, " ")), false)
	}
	cmd := Command{Kind: KindDiff, A: positional[0], Preview: preview}
	if len(positional) == 2 {
		cmd.B = positional[1]
	}
	return ok(ParsedInvocation{Command: cmd, Global: g})
}

func parseNew(rest []string, g GlobalOptions) ParseResult {
	description := ""
	hasDescription := false
	var positional []string
	for i := 0; i < len(rest); i++ {
		t := rest[i]
		switch {
		case t == "--description":
			next, ok := nextArg(rest, i)
			if !ok || strings.HasPrefix(next, "--") {
				return errResult("--description requires a value", false)
			}
			description = next
			hasDescription = true
			i++
		case strings.HasPrefix(t, "--description="):
			description = strings.TrimPrefix(t, "--description=")
			hasDescription = true
		case strings.HasPrefix(t, "--"):
			return errResult(fmt.Sprintf(`new: unknown flag %q`, t), false)
		default:
			positional = append(positional, t)
		}
	}
	if len(positional) == 0 {
		return errResult("new requires a profile name", false)
	}
	if len(positional) > 1 {
		return errResult(fmt.Sprintf(`new takes one positional argument; got %q`, strings.Join(positional, " ")), false)
	}
	cmd := Command{Kind: KindNew, Profile: positional[0]}
	if hasDescription {
		cmd.Description = description
	}
	return ok(ParsedInvocation{Command: cmd, Global: g})
}

func parseValidate(rest []string, g GlobalOptions) ParseResult {
	var brief bool
	var positional []string
	for _, t := range rest {
		switch {
		case t == "--brief":
			brief = true
		case strings.HasPrefix(t, "--"):
			return errResult(fmt.Sprintf(`validate: unknown flag %q`, t), false)
		default:
			positional = append(positional, t)
		}
	}
	if len(positional) > 1 {
		return errResult(fmt.Sprintf(`validate takes at most one profile name; got %q`, strings.Join(positional, " ")), false)
	}
	cmd := Command{Kind: KindValidate, Brief: brief}
	if len(positional) == 1 {
		cmd.ValidateProfile = positional[0]
	}
	return ok(ParsedInvocation{Command: cmd, Global: g})
}

func parseHook(rest []string, g GlobalOptions) ParseResult {
	var force bool
	var positional []string
	for _, t := range rest {
		switch {
		case t == "--force":
			force = true
		case strings.HasPrefix(t, "--"):
			return errResult(fmt.Sprintf(`hook: unknown flag %q`, t), false)
		default:
			positional = append(positional, t)
		}
	}
	if len(positional) == 0 {
		return errResult("hook requires an action (install|uninstall)", false)
	}
	if len(positional) > 1 {
		return errResult(fmt.Sprintf(`hook takes one positional argument; got %q`, strings.Join(positional, " ")), false)
	}
	action := positional[0]
	if action != "install" && action != "uninstall" {
		return errResult(fmt.Sprintf(`hook action must be install|uninstall, got %q`, action), false)
	}
	if action == "uninstall" && force {
		return errResult("hook uninstall does not accept --force (a non-matching hook is never removed)", false)
	}
	return ok(ParsedInvocation{
		Command: Command{Kind: KindHook, HookAction: HookAction(action), Force: force},
		Global:  g,
	})
}

func parseDoctor(rest []string, g GlobalOptions) ParseResult {
	var positional []string
	for _, t := range rest {
		if strings.HasPrefix(t, "--") {
			return errResult(fmt.Sprintf(`doctor: unknown flag %q`, t), false)
		}
		positional = append(positional, t)
	}
	if len(positional) > 0 {
		return errResult(fmt.Sprintf(`doctor takes no positional arguments; got %q`, strings.Join(positional, " ")), false)
	}
	return ok(ParsedInvocation{Command: Command{Kind: KindDoctor}, Global: g})
}

func parseCompletions(rest []string, g GlobalOptions) ParseResult {
	var positional []string
	for _, t := range rest {
		if strings.HasPrefix(t, "--") {
			return errResult(fmt.Sprintf(`completions: unknown flag %q`, t), false)
		}
		positional = append(positional, t)
	}
	if len(positional) == 0 {
		return errResult("completions requires a shell (bash|zsh|fish)", false)
	}
	if len(positional) > 1 {
		return errResult(fmt.Sprintf(`completions takes one positional argument; got %q`, strings.Join(positional, " ")), false)
	}
	shell, ok := completionShells[positional[0]]
	if !ok {
		return errResult(fmt.Sprintf(`completions shell must be bash|zsh|fish, got %q`, positional[0]), false)
	}
	return okResult(ParsedInvocation{Command: Command{Kind: KindCompletions, Shell: shell}, Global: g})
}

func parseOnDrift(s string) (drift.GateChoice, bool) {
	switch s {
	case "discard":
		return drift.GateChoiceDiscard, true
	case "persist":
		return drift.GateChoicePersist, true
	case "abort":
		return drift.GateChoiceAbort, true
	}
	return "", false
}

func nextArg(argv []string, i int) (string, bool) {
	if i+1 >= len(argv) {
		return "", false
	}
	return argv[i+1], true
}

func ok(inv ParsedInvocation) ParseResult     { return ParseResult{Ok: true, Invocation: inv} }
func okResult(inv ParsedInvocation) ParseResult { return ok(inv) }

func errResult(msg string, helpRequested bool) ParseResult {
	return ParseResult{Ok: false, Err: &ParseError{Message: msg, HelpRequested: helpRequested}}
}
