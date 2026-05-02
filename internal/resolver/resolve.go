package resolver

import (
	"fmt"
	"path/filepath"
	"sort"

	pipelineerrors "github.com/htxryan/claude-code-config-profiles/internal/errors"
)

// ResolveOptions configures a Resolve call.
type ResolveOptions struct {
	ProjectRoot string
}

// Resolve resolves profileName into a ResolvedPlan. See the package doc and
// src/resolver/resolve.ts for the algorithm. Errors returned are pipeline
// error sub-types: *MissingProfileError, *CycleError, *MissingIncludeError,
// *ConflictError, *InvalidManifestError, *PathTraversalError.
func Resolve(profileName string, opts ResolveOptions) (*ResolvedPlan, error) {
	paths := BuildPaths(opts.ProjectRoot)
	warnings := []ResolutionWarning{}

	// 1. Build extends chain newest → oldest, detecting cycle/missing.
	newestFirst, err := buildExtendsChain(profileName, paths)
	if err != nil {
		return nil, err
	}

	// 2. Reverse to oldest-first.
	oldestFirst := make([]chainEntry, len(newestFirst))
	for i, e := range newestFirst {
		oldestFirst[len(newestFirst)-1-i] = e
	}

	// 3. Build contributors in canonical order.
	contributors := []Contributor{}
	includes := []IncludeRef{}
	externalPaths := []ExternalTrustEntry{}
	seenExternal := map[string]struct{}{}
	seenContributorPaths := map[string]struct{}{}
	leafIndex := len(oldestFirst) - 1
	for i, entry := range oldestFirst {
		isLeaf := i == leafIndex
		if !isLeaf {
			contributors = append(contributors, makeAncestorContributor(entry.name, paths, entry.manifest))
			if err := emitIncludes(entry.includes, entry.dir, entry.name, paths,
				&contributors, &includes, &externalPaths, seenExternal, seenContributorPaths, &warnings); err != nil {
				return nil, err
			}
		} else {
			if err := emitIncludes(entry.includes, entry.dir, entry.name, paths,
				&contributors, &includes, &externalPaths, seenExternal, seenContributorPaths, &warnings); err != nil {
				return nil, err
			}
			contributors = append(contributors, makeProfileContributor(entry.name, paths, entry.manifest))
		}
	}

	// 4. Walk every contributor's `.claude/` and root, collect files.
	files, err := collectFiles(contributors)
	if err != nil {
		return nil, err
	}
	if files == nil {
		files = []PlanFile{}
	}

	// 5. Conflict detection (R11). Group by (destination, relPath).
	byKey := map[string][]int{}
	for i, f := range files {
		key := string(f.Destination) + "::" + f.RelPath
		byKey[key] = append(byKey[key], i)
	}
	for _, idxs := range byKey {
		if len(idxs) < 2 {
			continue
		}
		relPath := files[idxs[0]].RelPath
		if IsMergeable(relPath) {
			continue
		}
		group := make([]PlanFile, 0, len(idxs))
		for _, idx := range idxs {
			group = append(group, files[idx])
		}
		if err := detectConflict(relPath, group, contributors); err != nil {
			return nil, err
		}
	}

	// 6. Sort files: lex by relPath, then contributorIndex, then destination.
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].RelPath != files[j].RelPath {
			return files[i].RelPath < files[j].RelPath
		}
		if files[i].ContributorIndex != files[j].ContributorIndex {
			return files[i].ContributorIndex < files[j].ContributorIndex
		}
		return files[i].Destination < files[j].Destination
	})

	// Aggregate manifest warnings.
	for _, e := range oldestFirst {
		warnings = append(warnings, e.warnings...)
	}

	chain := make([]string, len(oldestFirst))
	for i, e := range oldestFirst {
		chain[i] = e.name
	}

	return &ResolvedPlan{
		SchemaVersion: ResolvedPlanSchemaVersion,
		ProfileName:   profileName,
		Chain:         chain,
		Includes:      includes,
		Contributors:  contributors,
		Files:         files,
		Warnings:      warnings,
		ExternalPaths: externalPaths,
	}, nil
}

type chainEntry struct {
	name     string
	dir      string
	manifest ProfileManifest
	includes []string
	warnings []ResolutionWarning
}

func buildExtendsChain(profileName string, paths ResolverPaths) ([]chainEntry, error) {
	var (
		chain        []chainEntry
		visitedOrder []string
		visitedSet   = map[string]struct{}{}
		current      = profileName
		referencedBy string
	)

	for {
		// R2: profile identifiers are bare directory names. Reject anything
		// that could escape `.claude-profiles/` via traversal, slashes, or
		// the hidden/`_components` conventions before touching the filesystem.
		if !IsValidProfileName(current) {
			return nil, pipelineerrors.NewMissingProfileError(current, referencedBy, nil)
		}

		if _, seen := visitedSet[current]; seen {
			start := indexOf(visitedOrder, current)
			cycle := append([]string{}, visitedOrder[start:]...)
			cycle = append(cycle, current)
			return nil, pipelineerrors.NewCycleError(cycle)
		}
		visitedSet[current] = struct{}{}
		visitedOrder = append(visitedOrder, current)

		dir := ProfileDir(paths, current)
		if !IsDirectory(dir) {
			return nil, pipelineerrors.NewMissingProfileError(current, referencedBy, nil)
		}

		res, err := LoadManifest(dir, current)
		if err != nil {
			return nil, err
		}

		chain = append(chain, chainEntry{
			name:     current,
			dir:      dir,
			manifest: res.Manifest,
			includes: append([]string{}, res.Manifest.Includes...),
			warnings: res.Warnings,
		})

		if res.Manifest.Extends == "" {
			return chain, nil
		}
		referencedBy = current
		current = res.Manifest.Extends
	}
}

func emitIncludes(
	rawIncludes []string,
	referencingProfileDir, referencedBy string,
	paths ResolverPaths,
	contributors *[]Contributor,
	includes *[]IncludeRef,
	externalPaths *[]ExternalTrustEntry,
	seenExternal, seenContributorPaths map[string]struct{},
	warnings *[]ResolutionWarning,
) error {
	for _, raw := range rawIncludes {
		ref, err := ClassifyInclude(raw, referencingProfileDir, paths, referencedBy)
		if err != nil {
			return err
		}
		// Append before the existence check so partial-progress shapes match
		// the TS resolver: on error, *includes is discarded by the caller
		// (Resolve returns nil, err). Do not switch this to a partial-plan
		// return without auditing every consumer for the new shape.
		*includes = append(*includes, ref)

		if !IsDirectory(ref.ResolvedPath) {
			return pipelineerrors.NewMissingIncludeError(raw, ref.ResolvedPath, referencedBy)
		}

		if _, dup := seenContributorPaths[ref.ResolvedPath]; dup {
			*warnings = append(*warnings, ResolutionWarning{
				Code:    WarningDuplicateInclude,
				Message: fmt.Sprintf("Include %q in profile %q resolves to %q, which was already included; skipping duplicate", raw, referencedBy, ref.ResolvedPath),
				Source:  referencedBy,
			})
			continue
		}
		seenContributorPaths[ref.ResolvedPath] = struct{}{}

		*contributors = append(*contributors, Contributor{
			Kind:      ContributorInclude,
			ID:        raw,
			RootPath:  ref.ResolvedPath,
			ClaudeDir: filepath.Join(ref.ResolvedPath, ".claude"),
			External:  ref.External,
		})

		if ref.External {
			if _, seen := seenExternal[ref.ResolvedPath]; !seen {
				seenExternal[ref.ResolvedPath] = struct{}{}
				*externalPaths = append(*externalPaths, ExternalTrustEntry{
					Raw:          raw,
					ResolvedPath: ref.ResolvedPath,
				})
			}
		}
	}
	return nil
}

func makeAncestorContributor(name string, paths ResolverPaths, manifest ProfileManifest) Contributor {
	dir := ProfileDir(paths, name)
	mc := manifest
	return Contributor{
		Kind:      ContributorAncestor,
		ID:        name,
		RootPath:  dir,
		ClaudeDir: filepath.Join(dir, ".claude"),
		External:  false,
		Manifest:  &mc,
	}
}

func makeProfileContributor(name string, paths ResolverPaths, manifest ProfileManifest) Contributor {
	dir := ProfileDir(paths, name)
	mc := manifest
	return Contributor{
		Kind:      ContributorProfile,
		ID:        name,
		RootPath:  dir,
		ClaudeDir: filepath.Join(dir, ".claude"),
		External:  false,
		Manifest:  &mc,
	}
}

func collectFiles(contributors []Contributor) ([]PlanFile, error) {
	var out []PlanFile
	for i, c := range contributors {
		// .claude/ subtree → destination='.claude'.
		entries, err := WalkClaudeDir(c.ClaudeDir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			out = append(out, PlanFile{
				RelPath:          e.RelPath,
				AbsPath:          e.AbsPath,
				ContributorIndex: i,
				MergePolicy:      PolicyFor(e.RelPath),
				Destination:      DestinationClaude,
			})
		}

		// profile-root files (CLAUDE.md peer of profile.json) → destination='projectRoot'.
		rootEntries, err := WalkProfileRoot(c.RootPath)
		if err != nil {
			return nil, err
		}
		for _, e := range rootEntries {
			out = append(out, PlanFile{
				RelPath:          e.RelPath,
				AbsPath:          e.AbsPath,
				ContributorIndex: i,
				MergePolicy:      PolicyFor(e.RelPath),
				Destination:      DestinationProjectRoot,
			})
		}
	}
	return out, nil
}

// detectConflict implements R11: given multiple non-mergeable contributions
// for the same (destination, relPath), decide if it's a conflict.
//   - profile-itself contributing → never a conflict (profile always overrides)
//   - 2+ ancestors only           → no conflict (R10 last-wins)
//   - any include involved        → conflict
func detectConflict(relPath string, group []PlanFile, contributors []Contributor) error {
	hasProfile, hasInclude := false, false
	for _, f := range group {
		k := contributors[f.ContributorIndex].Kind
		if k == ContributorProfile {
			hasProfile = true
		}
		if k == ContributorInclude {
			hasInclude = true
		}
	}
	if hasProfile {
		return nil
	}
	if !hasInclude {
		return nil
	}

	seen := map[string]struct{}{}
	dedup := []string{}
	for _, f := range group {
		id := contributors[f.ContributorIndex].ID
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		dedup = append(dedup, id)
	}
	return pipelineerrors.NewConflictError(relPath, dedup)
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
