// Package commands hosts the per-verb handlers. Each handler takes a typed
// Options struct (with the OutputChannel and decoded flags) and returns
// (exitCode, error). Mirrors src/cli/commands/*.ts.
package commands

import "github.com/htxryan/c3p/internal/drift"

// OutputChannel is the minimal interface commands need. The cli package
// implements it; tests can inject a buffer-backed double. Defined here to
// avoid a circular import (cli → commands → cli).
type OutputChannel interface {
	Print(text string)
	JSON(payload interface{})
	Warn(text string)
	Error(text string)
	Phase(text string)
	JSONMode() bool
	IsTTY() bool
}

// HelloOptions configures `c3p hello`.
type HelloOptions struct {
	Output OutputChannel
}

// ListOptions configures `c3p list`.
type ListOptions struct {
	Cwd     string
	Output  OutputChannel
	NoColor bool
}

// StatusOptions configures `c3p status`.
type StatusOptions struct {
	Cwd     string
	Output  OutputChannel
	NoColor bool
}

// DriftOptions configures `c3p drift`.
type DriftOptions struct {
	Cwd           string
	Output        OutputChannel
	PreCommitWarn bool
	Verbose       bool
	Preview       bool
	NoColor       bool
}

// DiffOptions configures `c3p diff`.
type DiffOptions struct {
	Cwd     string
	Output  OutputChannel
	A       string
	B       string // empty = active
	Preview bool
	NoColor bool
}

// ValidateOptions configures `c3p validate`.
type ValidateOptions struct {
	Cwd     string
	Output  OutputChannel
	Profile string // empty = all
	Brief   bool
	NoColor bool
}

// NewOptions configures `c3p new`.
type NewOptions struct {
	Cwd         string
	Output      OutputChannel
	Profile     string
	Description string
	NoColor     bool
}

// UseOptions configures `c3p use`.
type UseOptions struct {
	Cwd            string
	Output         OutputChannel
	Profile        string
	Mode           drift.GateMode
	OnDriftFlag    drift.GateChoice
	SignalHandlers bool
	NoColor        bool
	WaitMs         int64
	PromptIn       interface{}
	PromptOut      interface{}
	PromptFunc     func() drift.GateChoice
}

// SyncOptions configures `c3p sync`.
type SyncOptions struct {
	Cwd            string
	Output         OutputChannel
	Mode           drift.GateMode
	OnDriftFlag    drift.GateChoice
	SignalHandlers bool
	NoColor        bool
	WaitMs         int64
	PromptIn       interface{}
	PromptOut      interface{}
	PromptFunc     func() drift.GateChoice
}

// InitOptions configures `c3p init`.
type InitOptions struct {
	Cwd                string
	Output             OutputChannel
	Starter            string
	SeedFromClaudeDir  bool
	InstallHook        bool
	SignalHandlers     bool
	NoColor            bool
	NonInteractiveMode bool
}

// HookOptions configures `c3p hook`.
type HookOptions struct {
	Cwd     string
	Output  OutputChannel
	Action  string
	Force   bool
	NoColor bool
}

// DoctorOptions configures `c3p doctor`.
type DoctorOptions struct {
	Cwd     string
	Output  OutputChannel
	NoColor bool
}

// CompletionsOptions configures `c3p completions`.
type CompletionsOptions struct {
	Output OutputChannel
	Shell  string
}
