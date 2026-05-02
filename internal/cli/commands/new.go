package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/htxryan/c3p/internal/resolver"
	"github.com/htxryan/c3p/internal/state"
)

type newPayload struct {
	Profile     string `json:"profile"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
}

// RunNew implements `c3p new <name> [--description=<text>]`. Creates
// .claude-profiles/<name>/profile.json. Refuses if the dir already exists.
// Acquires the project lock during write.
// Mirrors src/cli/commands/new.ts.
func RunNew(opts NewOptions) (int, error) {
	if !resolver.IsValidProfileName(opts.Profile) {
		return 1, userErrorf("invalid profile name %q — names must be a bare directory name (no slashes, no leading dot, no '..')", opts.Profile)
	}
	paths := state.BuildStatePaths(opts.Cwd)
	if err := os.MkdirAll(paths.ProfilesDir, 0o755); err != nil {
		return 2, fmt.Errorf("cannot create %q: %w", paths.ProfilesDir, err)
	}

	dir := filepath.Join(paths.ProfilesDir, opts.Profile)
	if exists, err := pathExists(dir); err != nil {
		return 2, err
	} else if exists {
		return 1, userErrorf("profile %q already exists at %q", opts.Profile, dir)
	}

	err := state.WithLock(context.Background(), paths, state.AcquireOptions{}, func(_ *state.LockHandle) error {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		manifestPath := filepath.Join(dir, "profile.json")
		manifest := resolver.ProfileManifest{
			Name:        opts.Profile,
			Description: opts.Description,
		}
		data, mErr := marshalManifest(manifest)
		if mErr != nil {
			return mErr
		}
		return writeFileSync(manifestPath, data)
	})
	if err != nil {
		return 2, err
	}

	if opts.Output.JSONMode() {
		opts.Output.JSON(newPayload{Profile: opts.Profile, Description: opts.Description, Path: dir})
		return 0, nil
	}
	opts.Output.Print(fmt.Sprintf("created profile %q at %s", opts.Profile, dir))
	opts.Output.Print("Edit profile.json to set extends/includes/tags. Add files under .claude/.")
	return 0, nil
}

func pathExists(p string) (bool, error) {
	_, err := os.Stat(p)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func writeFileSync(p string, data []byte) error {
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return err
	}
	return nil
}
