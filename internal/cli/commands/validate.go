package commands

import (
	"errors"
	"fmt"
	"os"
	"strings"

	pipelineerrors "github.com/htxryan/claude-code-config-profiles/internal/errors"
	"github.com/htxryan/claude-code-config-profiles/internal/merge"
	"github.com/htxryan/claude-code-config-profiles/internal/resolver"
	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

type validateResult struct {
	Profile       string                       `json:"profile"`
	Ok            bool                         `json:"ok"`
	ErrorCode     string                       `json:"errorCode,omitempty"`
	ErrorMessage  string                       `json:"errorMessage,omitempty"`
	Warnings      []resolver.ResolutionWarning `json:"warnings"`
	ExternalPaths []resolver.ExternalTrustEntry `json:"externalPaths"`
}

type validatePayload struct {
	Results []validateResult `json:"results"`
	Pass    bool             `json:"pass"`
}

// RunValidate implements `c3p validate [<profile>]`. Read-only: resolve+merge
// without any disk write. Per-profile catch so one bad manifest doesn't hide
// others. Mirrors src/cli/commands/validate.ts.
func RunValidate(opts ValidateOptions) (int, error) {
	var names []string
	if opts.Profile != "" {
		names = []string{opts.Profile}
	} else {
		ns, err := resolver.ListProfiles(resolver.DiscoverOptions{ProjectRoot: opts.Cwd})
		if err != nil {
			return 2, err
		}
		names = ns
	}

	results := make([]validateResult, 0, len(names))
	allOk := true
	worstExitCode := 0
	for _, name := range names {
		r, code := validateOne(name, opts.Cwd)
		results = append(results, r)
		if !r.Ok {
			allOk = false
		}
		if code > worstExitCode {
			worstExitCode = code
		}
	}

	payload := validatePayload{Results: results, Pass: allOk}
	if opts.Output.JSONMode() {
		opts.Output.JSON(payload)
	} else {
		for _, r := range results {
			if r.Ok {
				opts.Output.Print(fmt.Sprintf("PASS  %s", r.Profile))
			} else {
				if opts.Brief {
					opts.Output.Print(fmt.Sprintf("FAIL  %s  [%s]", r.Profile, r.ErrorCode))
				} else {
					opts.Output.Print(fmt.Sprintf("FAIL  %s", r.Profile))
					opts.Output.Print("      " + r.ErrorMessage)
				}
			}
			for _, w := range r.Warnings {
				opts.Output.Print(fmt.Sprintf("      warning [%s]: %s", w.Code, w.Message))
			}
		}
		if allOk {
			opts.Output.Print(fmt.Sprintf("validated %d profile(s) cleanly", len(results)))
		}
	}

	// R33: exit 3 on conflict-class failures, exit 1 on invalid manifest, 0 otherwise.
	if allOk {
		return checkRootMarkers(opts.Cwd, opts.Output, opts.Profile)
	}
	if worstExitCode == 0 {
		// Defensive: if no specific code was tagged, default to 3 on any failure.
		worstExitCode = 3
	}
	return worstExitCode, nil
}

func validateOne(name, cwd string) (validateResult, int) {
	r := validateResult{Profile: name, Warnings: []resolver.ResolutionWarning{}, ExternalPaths: []resolver.ExternalTrustEntry{}}
	plan, err := resolver.Resolve(name, resolver.ResolveOptions{ProjectRoot: cwd})
	if err != nil {
		r.Ok = false
		r.ErrorMessage = err.Error()
		if pe := pipelineerrors.AsPipelineError(err); pe != nil {
			r.ErrorCode = string(pe.ErrorCode())
			if pe.ErrorCode() == pipelineerrors.CodeInvalidManifest {
				return r, 1
			}
			return r, 3
		}
		return r, 3
	}
	r.Warnings = plan.Warnings
	r.ExternalPaths = plan.ExternalPaths
	if r.Warnings == nil {
		r.Warnings = []resolver.ResolutionWarning{}
	}
	if r.ExternalPaths == nil {
		r.ExternalPaths = []resolver.ExternalTrustEntry{}
	}
	if _, err := merge.Merge(plan, merge.Options{
		Read: func(absPath string) ([]byte, error) {
			return os.ReadFile(absPath)
		},
	}); err != nil {
		r.Ok = false
		r.ErrorMessage = err.Error()
		if pe := pipelineerrors.AsPipelineError(err); pe != nil {
			r.ErrorCode = string(pe.ErrorCode())
		}
		return r, 3
	}
	r.Ok = true
	return r, 0
}

// checkRootMarkers ensures projectRoot CLAUDE.md has c3p markers when an
// active profile contributes to projectRoot.
func checkRootMarkers(cwd string, output OutputChannel, profileFilter string) (int, error) {
	paths := state.BuildStatePaths(cwd)
	st, err := state.ReadStateFile(paths)
	if err != nil {
		return 0, nil
	}
	if st.State.ActiveProfile == nil {
		return 0, nil
	}
	// Resolve the active profile and check whether it has projectRoot
	// contributors. Not strictly necessary for validate to fail when the
	// active profile DOES NOT contribute to projectRoot; we only fail when
	// it does.
	plan, rerr := resolver.Resolve(*st.State.ActiveProfile, resolver.ResolveOptions{ProjectRoot: cwd})
	if rerr != nil {
		return 0, nil
	}
	hasRoot := false
	for _, f := range plan.Files {
		if f.Destination == resolver.DestinationProjectRoot {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		return 0, nil
	}
	// Read CLAUDE.md and verify markers via a quick parse.
	content, err := os.ReadFile(paths.RootClaudeMdFile)
	if err != nil {
		// Missing CLAUDE.md when the active profile contributes is a
		// validate-failure: run `c3p init` to create it.
		_ = profileFilter
		if errors.Is(err, os.ErrNotExist) {
			output.Warn("warning: project-root CLAUDE.md is missing — run `c3p init` to create markers (R44)")
			return 1, nil
		}
		return 0, nil
	}
	parsed := parseRootMarkers(string(content))
	if !parsed {
		output.Warn("warning: project-root CLAUDE.md is missing c3p markers — run `c3p init` (R44/R45)")
		return 1, nil
	}
	return 0, nil
}

func parseRootMarkers(content string) bool {
	// Cheap regex check; full validation lives in internal/markers.
	return len(content) > 0 && (containsMarker(content, ":begin") && containsMarker(content, ":end"))
}

func containsMarker(s, suffix string) bool {
	idx := strings.Index(s, "<!-- c3p:v")
	if idx < 0 {
		return false
	}
	return strings.Index(s[idx:], suffix) >= 0
}
