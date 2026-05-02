package commands

import (
	"bytes"
	"fmt"
	"os"

	"github.com/htxryan/c3p/internal/merge"
	"github.com/htxryan/c3p/internal/resolver"
	"github.com/htxryan/c3p/internal/state"
)

type diffFilePayload struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type diffPayload struct {
	A     string            `json:"a"`
	B     string            `json:"b"`
	Files []diffFilePayload `json:"files"`
}

// RunDiff implements `c3p diff <a> [<b>]`. When <b> is empty, compares <a>
// to the active profile. Mirrors src/cli/commands/diff.ts.
func RunDiff(opts DiffOptions) (int, error) {
	bName := opts.B
	if bName == "" {
		paths := state.BuildStatePaths(opts.Cwd)
		st, err := state.ReadStateFile(paths)
		if err != nil {
			return 2, err
		}
		if st.State.ActiveProfile == nil {
			return 1, fmt.Errorf("diff: <b> not given and no profile is currently active")
		}
		bName = *st.State.ActiveProfile
	}

	aMerged, err := resolveAndMerge(opts.A, opts.Cwd)
	if err != nil {
		return 0, err
	}
	bMerged, err := resolveAndMerge(bName, opts.Cwd)
	if err != nil {
		return 0, err
	}

	files := compareFileSets(aMerged, bMerged)
	payload := diffPayload{A: opts.A, B: bName, Files: files}

	if opts.Output.JSONMode() {
		opts.Output.JSON(payload)
		return 0, nil
	}

	if len(files) == 0 {
		opts.Output.Print(fmt.Sprintf("%s and %s have identical resolved trees", opts.A, bName))
		return 0, nil
	}
	opts.Output.Print(fmt.Sprintf("diff: %s vs %s", opts.A, bName))
	for _, f := range files {
		opts.Output.Print(fmt.Sprintf("  %s  %s", diffStatusGlyph(f.Status), f.Path))
	}
	return 0, nil
}

func resolveAndMerge(profileName, cwd string) ([]merge.MergedFile, error) {
	plan, err := resolver.Resolve(profileName, resolver.ResolveOptions{ProjectRoot: cwd})
	if err != nil {
		return nil, err
	}
	return merge.Merge(plan, merge.Options{
		Read: func(absPath string) ([]byte, error) {
			return os.ReadFile(absPath)
		},
	})
}

func compareFileSets(a, b []merge.MergedFile) []diffFilePayload {
	bByPath := map[string]merge.MergedFile{}
	for _, f := range b {
		key := string(f.Destination) + "::" + f.Path
		bByPath[key] = f
	}
	out := []diffFilePayload{}
	seen := map[string]bool{}
	for _, fa := range a {
		key := string(fa.Destination) + "::" + fa.Path
		seen[key] = true
		fb, ok := bByPath[key]
		if !ok {
			out = append(out, diffFilePayload{Path: fa.Path, Status: "only-in-a"})
			continue
		}
		if !bytes.Equal(fa.Bytes, fb.Bytes) {
			out = append(out, diffFilePayload{Path: fa.Path, Status: "modified"})
		}
	}
	for _, fb := range b {
		key := string(fb.Destination) + "::" + fb.Path
		if !seen[key] {
			out = append(out, diffFilePayload{Path: fb.Path, Status: "only-in-b"})
		}
	}
	return out
}

func diffStatusGlyph(s string) string {
	switch s {
	case "only-in-a":
		return "A"
	case "only-in-b":
		return "B"
	case "modified":
		return "~"
	}
	return "?"
}
