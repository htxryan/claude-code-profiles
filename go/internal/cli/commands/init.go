package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/htxryan/c3p/internal/markers"
	"github.com/htxryan/c3p/internal/resolver"
	"github.com/htxryan/c3p/internal/state"
)

type initPayload struct {
	ProfilesDir string `json:"profilesDir"`
	Starter     string `json:"starter,omitempty"`
	Seeded      bool   `json:"seeded"`
	HookInstalled bool `json:"hookInstalled"`
	MarkersInjected bool `json:"markersInjected"`
}

// RunInit implements `c3p init`. Bootstrap .claude-profiles/, optionally seed
// from existing .claude/, write .gitignore entries, install the pre-commit
// hook, and inject markers into project-root CLAUDE.md.
// Mirrors src/cli/commands/init.ts.
func RunInit(opts InitOptions) (int, error) {
	paths := state.BuildStatePaths(opts.Cwd)

	// PR16: refuse to bootstrap if any segment of the project root is a
	// Windows reserved name or contains a Windows-reserved character. The
	// resolver guards profile names; this guard catches the project root
	// itself so a path authored on POSIX (e.g. /home/u/CON/repo) cannot
	// silently produce an unusable .claude-profiles/.meta tree on Windows.
	if reason := unsafeProjectRoot(paths.ProjectRoot); reason != "" {
		return 1, userErrorf("init: project root %q is unsafe on Windows: %s", paths.ProjectRoot, reason)
	}

	// Refuse on already-initialised .claude-profiles/.
	if existsAlready, err := pathExists(paths.ProfilesDir); err != nil {
		return 2, err
	} else if existsAlready {
		// Allow the case where the dir exists but is empty.
		// .meta is the bookkeeping subdir (not a profile); both it and any
		// other dotfile entry are excluded by HasPrefix(".") alone — no
		// need for an explicit ".meta" branch.
		entries, readErr := os.ReadDir(paths.ProfilesDir)
		if readErr != nil {
			return 2, readErr
		}
		nonHidden := 0
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".") {
				nonHidden++
			}
		}
		if nonHidden > 0 {
			return 1, userErrorf("init: %q already exists with profiles inside — refusing to overwrite", paths.ProfilesDir)
		}
	}

	starter := opts.Starter
	if starter == "" {
		starter = "default"
	}
	if !resolver.IsValidProfileName(starter) {
		return 1, userErrorf("invalid starter profile name %q", starter)
	}

	payload := initPayload{ProfilesDir: paths.ProfilesDir, Starter: starter}

	err := state.WithLock(context.Background(), paths, state.AcquireOptions{}, func(_ *state.LockHandle) error {
		if err := os.MkdirAll(paths.ProfilesDir, 0o755); err != nil {
			return err
		}

		// Write starter profile.json.
		starterDir := filepath.Join(paths.ProfilesDir, starter)
		if err := os.MkdirAll(starterDir, 0o755); err != nil {
			return err
		}
		manifestPath := filepath.Join(starterDir, "profile.json")
		if exists, err := pathExists(manifestPath); err != nil {
			return err
		} else if !exists {
			data, mErr := marshalManifest(resolver.ProfileManifest{
				Name:        starter,
				Description: "starter profile created by `c3p init`",
			})
			if mErr != nil {
				return mErr
			}
			if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
				return err
			}
		}

		// Seed from .claude/ if requested + present.
		if opts.SeedFromClaudeDir {
			liveExists, err := pathExists(paths.ClaudeDir)
			if err != nil {
				return err
			}
			if liveExists {
				dst := filepath.Join(starterDir, ".claude")
				if seedExists, err := pathExists(dst); err != nil {
					return err
				} else if !seedExists {
					if err := state.CopyTree(paths.ClaudeDir, dst); err != nil {
						return err
					}
					payload.Seeded = true
				}
			}
		}

		// Update .gitignore entries.
		if _, err := state.EnsureGitignoreEntries(paths); err != nil {
			return err
		}

		// Inject c3p markers into project-root CLAUDE.md.
		body, readErr := os.ReadFile(paths.RootClaudeMdFile)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		var bodyStr string
		if readErr == nil {
			bodyStr = string(body)
		}
		updated, mErr := markers.InjectMarkersIntoFile(bodyStr)
		if mErr != nil {
			return mErr
		}
		if updated != bodyStr {
			if err := os.WriteFile(paths.RootClaudeMdFile, []byte(updated), 0o644); err != nil {
				return err
			}
			payload.MarkersInjected = true
		}

		return nil
	})
	if err != nil {
		return 2, err
	}

	// Install pre-commit hook (outside the lock — git operations are
	// independent and the hook is idempotent).
	if opts.InstallHook {
		_, hookErr := RunHook(HookOptions{
			Cwd:    opts.Cwd,
			Output: silentOutput{},
			Action: "install",
		})
		if hookErr == nil {
			payload.HookInstalled = true
		}
	}

	if opts.Output.JSONMode() {
		opts.Output.JSON(payload)
		return 0, nil
	}
	opts.Output.Print(fmt.Sprintf("initialised %s", paths.ProfilesDir))
	if payload.Seeded {
		opts.Output.Print("seeded starter profile from existing .claude/")
	}
	if payload.MarkersInjected {
		opts.Output.Print("injected c3p markers into CLAUDE.md")
	}
	if payload.HookInstalled {
		opts.Output.Print("installed pre-commit hook")
	}
	return 0, nil
}

// unsafeProjectRoot reports a non-empty reason iff projectRoot is hostile to
// Windows: a path segment matching a DOS-device reserved name, or any
// reserved character (`<>:"|?*`) outside the drive-letter colon. The check
// runs on every host so a POSIX-authored path that would land somebody on a
// Windows clone in a broken state is rejected up front (PR16).
//
// Returns "" when the path is safe.
//
// Path-separator handling: we split on BOTH `/` and `\` so a path authored
// on either platform is normalised the same way. Relying on filepath.Split
// alone would miss backslash separators on POSIX hosts (where `\` is just a
// regular character), defeating the cross-host point of the guard.
func unsafeProjectRoot(projectRoot string) string {
	// Strip leading drive-letter so the colon in `C:\repo` doesn't trip the
	// reserved-character check. Anything beyond a single drive letter + colon
	// at position 0 is path content the user authored.
	body := projectRoot
	if len(body) >= 2 && body[1] == ':' {
		ch := body[0]
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
			body = body[2:]
		}
	}
	const reserved = `<>:"|?*`
	for _, r := range body {
		if r == 0 {
			return "embedded NUL byte"
		}
		// Tab/newline-class control bytes (1..31) are also illegal on Windows
		// path components; reject early so the diagnostic comes from us
		// instead of a downstream syscall errno.
		if r > 0 && r < 32 {
			return fmt.Sprintf("control character U+%04X", r)
		}
		for _, bad := range reserved {
			if r == bad {
				return fmt.Sprintf("reserved character %q", r)
			}
		}
	}

	// Walk every path segment under either separator convention so a
	// `C:\Users\COM1\repo` authored on Windows is rejected on a POSIX host
	// audit too. strings.FieldsFunc collapses adjacent separators (treats
	// `//` and `\\` as a single split), which is the right behaviour: we're
	// validating segment names, not preserving the path's textual shape.
	for _, seg := range strings.FieldsFunc(body, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if resolver.IsWindowsReservedName(seg) {
			return fmt.Sprintf("path segment %q is a Windows reserved name", seg)
		}
	}
	return ""
}

// silentOutput is a no-op OutputChannel used when init wants to delegate to
// hook install without echoing its output to the user (init prints its own
// summary).
type silentOutput struct{}

func (silentOutput) Print(string)             {}
func (silentOutput) JSON(interface{})         {}
func (silentOutput) Warn(string)              {}
func (silentOutput) Error(string)             {}
func (silentOutput) Phase(string)             {}
func (silentOutput) JSONMode() bool           { return false }
func (silentOutput) IsTTY() bool              { return false }
