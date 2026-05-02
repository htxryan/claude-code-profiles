// Command dispatcher. Pattern-matches Command.Kind, calls the handler,
// and returns an exit code + error. Mirrors src/cli/dispatch.ts.
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/htxryan/c3p/internal/cli/commands"
	"github.com/htxryan/c3p/internal/drift"
)

// DispatchContext carries the shared runtime state every handler needs.
type DispatchContext struct {
	Output         OutputChannel
	Version        string
	SignalHandlers bool
}

// Dispatch routes a parsed invocation to the right command handler and
// returns its exit code (or 0 on err == nil).
func Dispatch(inv ParsedInvocation, ctx DispatchContext) (int, error) {
	output := ctx.Output
	g := inv.Global
	mode := drift.GateModeInteractive
	if g.NonInteractive || !output.IsTTY() {
		mode = drift.GateModeNonInteractive
	}

	cwd, err := filepath.Abs(g.Cwd)
	if err != nil {
		return ExitSystem, fmt.Errorf("cannot resolve --cwd %q: %w", g.Cwd, err)
	}

	switch inv.Command.Kind {
	case KindVersion:
		if output.JSONMode() {
			output.JSON(struct {
				Version string `json:"version"`
			}{Version: ctx.Version})
		} else if output.IsTTY() {
			output.Print(VersionString(ctx.Version) + " at your service.")
			output.Print("Human-machine relations, configuration division.")
		} else {
			output.Print(VersionString(ctx.Version))
		}
		return ExitOK, nil

	case KindHelp:
		text := TopLevelHelp()
		if inv.Command.HelpVerb != "" {
			text = VerbHelp(inv.Command.HelpVerb)
		}
		if output.JSONMode() {
			if inv.Command.HelpVerb != "" {
				output.JSON(struct {
					Help string `json:"help"`
					Verb string `json:"verb"`
				}{Help: text, Verb: inv.Command.HelpVerb})
			} else {
				output.JSON(struct {
					Help string `json:"help"`
				}{Help: text})
			}
		} else {
			output.Print(text)
		}
		return ExitOK, nil

	case KindHello:
		return commands.RunHello(commands.HelloOptions{Output: outputAdapter{output}})

	case KindList:
		return commands.RunList(commands.ListOptions{
			Cwd:     cwd,
			Output:  outputAdapter{output},
			NoColor: g.NoColor,
		})

	case KindStatus:
		return commands.RunStatus(commands.StatusOptions{
			Cwd:     cwd,
			Output:  outputAdapter{output},
			NoColor: g.NoColor,
		})

	case KindDrift:
		return commands.RunDrift(commands.DriftOptions{
			Cwd:           cwd,
			Output:        outputAdapter{output},
			PreCommitWarn: inv.Command.PreCommitWarn,
			Verbose:       inv.Command.Verbose,
			Preview:       inv.Command.Preview,
			NoColor:       g.NoColor,
		})

	case KindDiff:
		return commands.RunDiff(commands.DiffOptions{
			Cwd:     cwd,
			Output:  outputAdapter{output},
			A:       inv.Command.A,
			B:       inv.Command.B,
			Preview: inv.Command.Preview,
			NoColor: g.NoColor,
		})

	case KindValidate:
		return commands.RunValidate(commands.ValidateOptions{
			Cwd:     cwd,
			Output:  outputAdapter{output},
			Profile: inv.Command.ValidateProfile,
			Brief:   inv.Command.Brief,
			NoColor: g.NoColor,
		})

	case KindNew:
		return commands.RunNew(commands.NewOptions{
			Cwd:         cwd,
			Output:      outputAdapter{output},
			Profile:     inv.Command.Profile,
			Description: inv.Command.Description,
			NoColor:     g.NoColor,
		})

	case KindUse:
		return commands.RunUse(commands.UseOptions{
			Cwd:            cwd,
			Output:         outputAdapter{output},
			Profile:        inv.Command.Profile,
			Mode:           mode,
			OnDriftFlag:    g.OnDrift,
			SignalHandlers: ctx.SignalHandlers,
			NoColor:        g.NoColor,
			WaitMs:         g.WaitMs,
			PromptIn:       os.Stdin,
			PromptOut:      os.Stderr,
			PromptFunc:     defaultPromptFunc(g.NoColor),
		})

	case KindSync:
		return commands.RunSync(commands.SyncOptions{
			Cwd:            cwd,
			Output:         outputAdapter{output},
			Mode:           mode,
			OnDriftFlag:    g.OnDrift,
			SignalHandlers: ctx.SignalHandlers,
			NoColor:        g.NoColor,
			WaitMs:         g.WaitMs,
			PromptIn:       os.Stdin,
			PromptOut:      os.Stderr,
			PromptFunc:     defaultPromptFunc(g.NoColor),
		})

	case KindInit:
		return commands.RunInit(commands.InitOptions{
			Cwd:                cwd,
			Output:             outputAdapter{output},
			Starter:            inv.Command.Starter,
			SeedFromClaudeDir:  inv.Command.Seed,
			InstallHook:        inv.Command.Hook,
			SignalHandlers:     ctx.SignalHandlers,
			NoColor:            g.NoColor,
			NonInteractiveMode: mode == drift.GateModeNonInteractive,
		})

	case KindHook:
		return commands.RunHook(commands.HookOptions{
			Cwd:     cwd,
			Output:  outputAdapter{output},
			Action:  string(inv.Command.HookAction),
			Force:   inv.Command.Force,
			NoColor: g.NoColor,
		})

	case KindDoctor:
		return commands.RunDoctor(commands.DoctorOptions{
			Cwd:     cwd,
			Output:  outputAdapter{output},
			NoColor: g.NoColor,
		})

	case KindCompletions:
		return commands.RunCompletions(commands.CompletionsOptions{
			Output: outputAdapter{output},
			Shell:  string(inv.Command.Shell),
		})
	}

	return ExitSystem, fmt.Errorf("unreachable: unknown command kind %q", inv.Command.Kind)
}

// defaultPromptFunc returns a closure that drives the interactive prompt
// against os.Stdin/os.Stderr. Returns nil for non-TTY contexts so the
// orchestrator's defensive abort-fallback kicks in.
func defaultPromptFunc(_ bool) func() drift.GateChoice {
	return func() drift.GateChoice {
		return PromptGateChoice(GatePromptOptions{
			In:  os.Stdin,
			Out: os.Stderr,
		})
	}
}

// outputAdapter exposes our OutputChannel to the commands package via a
// minimal interface. It exists so commands/* doesn't import cli/*
// (avoids a circular import: cli imports commands, commands would import cli).
type outputAdapter struct{ inner OutputChannel }

func (a outputAdapter) Print(s string)              { a.inner.Print(s) }
func (a outputAdapter) JSON(p interface{})          { a.inner.JSON(p) }
func (a outputAdapter) Warn(s string)               { a.inner.Warn(s) }
func (a outputAdapter) Error(s string)              { a.inner.Error(s) }
func (a outputAdapter) Phase(s string)              { a.inner.Phase(s) }
func (a outputAdapter) JSONMode() bool              { return a.inner.JSONMode() }
func (a outputAdapter) IsTTY() bool                 { return a.inner.IsTTY() }
