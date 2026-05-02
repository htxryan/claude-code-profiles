package commands

import (
	"errors"
	"fmt"

	"github.com/htxryan/c3p/internal/cli/service"
	"github.com/htxryan/c3p/internal/drift"
	pipelineerrors "github.com/htxryan/c3p/internal/errors"
	"github.com/htxryan/c3p/internal/resolver"
	"github.com/htxryan/c3p/internal/state"
)

type usePayload struct {
	Action         drift.ApplyGateAction `json:"action"`
	Drift          int                   `json:"drift"`
	BackupSnapshot *string               `json:"backupSnapshot"`
	Profile        string                `json:"profile"`
}

// RunUse implements `c3p use <name>`. Thin wrapper over service.RunSwap.
// Mirrors src/cli/commands/use.ts.
func RunUse(opts UseOptions) (int, error) {
	paths := state.BuildStatePaths(opts.Cwd)

	// Pre-validate profile name. A path-like name never reaches the
	// resolver — surface a clear user-error message.
	if !resolver.IsValidProfileName(opts.Profile) {
		return 1, userErrorf("invalid profile name %q — names must be a bare directory name (no slashes, no leading dot, no '..')", opts.Profile)
	}

	result, err := service.RunSwap(service.SwapOptions{
		Paths:          paths,
		TargetProfile:  opts.Profile,
		Mode:           opts.Mode,
		OnDriftFlag:    opts.OnDriftFlag,
		SignalHandlers: opts.SignalHandlers,
		WaitMs:         opts.WaitMs,
		PromptFunc:     opts.PromptFunc,
		OnPhase: func(s string) {
			opts.Output.Phase(s)
		},
	})

	if err != nil {
		// Enrich missing-profile typo with Levenshtein suggestions.
		var mpe *pipelineerrors.MissingProfileError
		if errors.As(err, &mpe) && mpe.ReferencedBy == "" {
			err = enrichMissingProfile(mpe, opts.Cwd)
		}
		return reportSwapError(opts.Output, err, opts.Profile, result)
	}

	return reportSwapSuccess(opts.Output, opts.Profile, result, false)
}

// reportSwapSuccess prints the human/JSON summary for a completed swap.
func reportSwapSuccess(output OutputChannel, target string, result service.SwapResult, isSync bool) (int, error) {
	driftCount := len(result.Drift.Entries)
	payload := usePayload{
		Action:         result.Action,
		Drift:          driftCount,
		BackupSnapshot: result.BackupSnapshot,
		Profile:        target,
	}
	if output.JSONMode() {
		output.JSON(payload)
		return 0, nil
	}
	verb := "switched to"
	if isSync {
		verb = "synced"
	}
	switch result.Action {
	case drift.ApplyActionMaterialized:
		output.Print(fmt.Sprintf("%s %s", verb, target))
	case drift.ApplyActionPersistedAndMaterialized:
		output.Print(fmt.Sprintf("%s %s (drift persisted into prior active profile)", verb, target))
	case drift.ApplyActionAborted:
		output.Print("aborted (no changes)")
		return 1, nil
	}
	if result.BackupSnapshot != nil {
		output.Print(fmt.Sprintf("backup: %s", *result.BackupSnapshot))
	}
	for _, w := range result.ResolverWarnings {
		output.Warn(fmt.Sprintf("warning [%s]: %s", w.Code, w.Message))
	}
	return 0, nil
}

// reportSwapError formats and surfaces a swap failure. The CLI's outer
// dispatch routes to the right exit code via ExitCodeFor.
func reportSwapError(output OutputChannel, err error, target string, result service.SwapResult) (int, error) {
	// Abort error: surface a structured payload in --json mode and a
	// short banner in text mode. Exit code 1 (user-error) — the wrapping
	// CliUserError carries that.
	var sa *service.SwapAbortError
	if errors.As(err, &sa) {
		if output.JSONMode() {
			output.JSON(struct {
				Action         drift.ApplyGateAction `json:"action"`
				Drift          int                   `json:"drift"`
				BackupSnapshot *string               `json:"backupSnapshot"`
				Profile        string                `json:"profile"`
				Aborted        bool                  `json:"aborted"`
				Message        string                `json:"message"`
			}{
				Action:         drift.ApplyActionAborted,
				Drift:          len(result.Drift.Entries),
				BackupSnapshot: result.BackupSnapshot,
				Profile:        target,
				Aborted:        true,
				Message:        sa.Message,
			})
		} else {
			output.Print(sa.Message)
		}
		return 1, sa
	}
	// All other errors propagate verbatim — CLI Run() formats and exits.
	return 0, err
}

func enrichMissingProfile(err *pipelineerrors.MissingProfileError, projectRoot string) error {
	candidates, listErr := resolver.ListProfiles(resolver.DiscoverOptions{ProjectRoot: projectRoot})
	if listErr != nil || len(candidates) == 0 {
		return err
	}
	suggestions := levenshteinNeighbors(err.Missing, candidates, 2)
	if len(suggestions) == 0 {
		return err
	}
	return pipelineerrors.NewMissingProfileError(err.Missing, err.ReferencedBy, suggestions)
}

// levenshteinNeighbors / levenshtein are local copies — commands/ can't
// import cli/, where the canonical implementation lives. The behaviour is
// asserted by the suggest tests.
func levenshteinNeighbors(target string, candidates []string, maxDist int) []string {
	type scored struct {
		name string
		d    int
	}
	var hits []scored
	for _, c := range candidates {
		if c == target {
			continue
		}
		d := levenshtein(target, c)
		if d <= maxDist {
			hits = append(hits, scored{name: c, d: d})
		}
	}
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0; j-- {
			a, b := hits[j-1], hits[j]
			less := a.d > b.d || (a.d == b.d && a.name > b.name)
			if !less {
				break
			}
			hits[j-1], hits[j] = b, a
		}
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.name
	}
	return out
}

func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	la, lb := len(ar), len(br)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur := make([]int, lb+1)
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
