package commands

import (
	"github.com/htxryan/c3p/internal/cli/service"
	"github.com/htxryan/c3p/internal/state"
)

// RunSync implements `c3p sync`. Same orchestration as `use <activeProfile>`.
// Mirrors src/cli/commands/sync.ts.
func RunSync(opts SyncOptions) (int, error) {
	paths := state.BuildStatePaths(opts.Cwd)
	st, err := state.ReadStateFile(paths)
	if err != nil {
		return 2, err
	}
	if st.State.ActiveProfile == nil {
		return 1, userErrorf("sync: no profile is currently active — run `c3p use <name>` first")
	}
	target := *st.State.ActiveProfile

	result, err := service.RunSwap(service.SwapOptions{
		Paths:          paths,
		TargetProfile:  target,
		Mode:           opts.Mode,
		OnDriftFlag:    opts.OnDriftFlag,
		SignalHandlers: opts.SignalHandlers,
		WaitMs:         opts.WaitMs,
		IsSync:         true,
		PromptFunc:     opts.PromptFunc,
		OnPhase: func(s string) {
			opts.Output.Phase(s)
		},
	})
	if err != nil {
		return reportSwapError(opts.Output, err, target, result)
	}
	return reportSwapSuccess(opts.Output, target, result, true)
}
