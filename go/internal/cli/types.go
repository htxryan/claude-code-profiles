// Types for the CLI surface — Command discriminated union, GlobalOptions,
// and verb tags. Mirrors src/cli/types.ts so the dual-suite IV harness can
// compare per-verb behavior across both binaries.
package cli

import "github.com/htxryan/c3p/internal/drift"

// CommandKind is one of the 13 public verbs plus help/version.
type CommandKind string

const (
	KindInit        CommandKind = "init"
	KindList        CommandKind = "list"
	KindUse         CommandKind = "use"
	KindStatus      CommandKind = "status"
	KindDrift       CommandKind = "drift"
	KindDiff        CommandKind = "diff"
	KindNew         CommandKind = "new"
	KindValidate    CommandKind = "validate"
	KindSync        CommandKind = "sync"
	KindHook        CommandKind = "hook"
	KindDoctor      CommandKind = "doctor"
	KindCompletions CommandKind = "completions"
	KindHelp        CommandKind = "help"
	KindVersion     CommandKind = "version"
	KindHello       CommandKind = "hello"
)

// HookAction is the install|uninstall sub-action for `c3p hook`.
type HookAction string

const (
	HookInstall   HookAction = "install"
	HookUninstall HookAction = "uninstall"
)

// CompletionShell is the shell discriminator for `c3p completions`.
type CompletionShell string

const (
	ShellBash CompletionShell = "bash"
	ShellZsh  CompletionShell = "zsh"
	ShellFish CompletionShell = "fish"
)

// Command is the discriminated union produced by ParseArgs. Each variant
// carries verb-specific args; GlobalOptions are bundled separately.
type Command struct {
	Kind CommandKind

	// init
	Starter string
	Seed    bool
	Hook    bool

	// use
	Profile string

	// drift
	PreCommitWarn bool
	Verbose       bool
	Preview       bool

	// diff
	A string
	B string // empty = compare to active

	// new
	Description string // empty = none

	// validate
	ValidateProfile string // empty = all
	Brief           bool

	// hook
	HookAction HookAction
	Force      bool

	// completions
	Shell CompletionShell

	// help
	HelpVerb string // empty = top-level
}

// GlobalOptions captures the cross-cutting flags every verb inherits.
type GlobalOptions struct {
	JSON    bool
	Cwd     string
	OnDrift drift.GateChoice // empty when unset
	NoColor bool
	Quiet   bool
	WaitMs  int64 // 0 when unset; >0 means the lock acquire should poll
	WaitSet bool  // distinguishes 0ms-wait from "not passed"

	// NonInteractive is true iff --non-interactive was passed OR CI=true is in
	// the env (mirrors the cli.go skeleton's PreRun behaviour).
	NonInteractive bool
}

// ParsedInvocation bundles the typed Command with the global flags.
type ParsedInvocation struct {
	Command Command
	Global  GlobalOptions
}
