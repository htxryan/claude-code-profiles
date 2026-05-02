package resolver

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiscoverOptions is the input to ListProfiles.
type DiscoverOptions struct {
	ProjectRoot string
}

// ListProfiles enumerates profiles by scanning top-level directories of
// `.claude-profiles/` (R1), excluding entries beginning with `_` or `.`.
// Returns directory names (= canonical profile identifiers per R2), sorted
// lexicographically for deterministic output.
func ListProfiles(opts DiscoverOptions) ([]string, error) {
	paths := BuildPaths(opts.ProjectRoot)
	entries, err := os.ReadDir(paths.ProfilesDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// ProfileExists returns true iff `.claude-profiles/<name>/` exists and is a
// directory.
func ProfileExists(name, projectRoot string) bool {
	paths := BuildPaths(projectRoot)
	dir := filepath.Join(paths.ProfilesDir, name)
	s, err := os.Stat(dir)
	if err != nil {
		return false
	}
	return s.IsDir()
}
