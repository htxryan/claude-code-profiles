package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// HookScriptContent is the canonical pre-commit hook (R25a). Fail-open: the
// hook never blocks a commit. Byte-identical with src/cli/commands/hook.ts so
// the dual-suite IV harness compares hook content equally.
const HookScriptContent = `#!/usr/bin/env sh
# c3p pre-commit drift warner. Fail-open: never blocks a commit.
# Managed by 'c3p hook install'. To uninstall: 'c3p hook uninstall'.
if command -v c3p >/dev/null 2>&1; then
  c3p drift --pre-commit-warn 2>&1 || true
fi
exit 0
`

type hookPayload struct {
	Action string `json:"action"`
	Path   string `json:"path"`
	Note   string `json:"note,omitempty"`
}

// RunHook implements `c3p hook install|uninstall`. Mirrors src/cli/commands/hook.ts.
func RunHook(opts HookOptions) (int, error) {
	hookPath, err := resolveHookPath(opts.Cwd)
	if err != nil {
		return 2, err
	}
	switch opts.Action {
	case "install":
		return runHookInstall(opts, hookPath)
	case "uninstall":
		return runHookUninstall(opts, hookPath)
	}
	return 1, fmt.Errorf("hook: action must be install|uninstall")
}

func runHookInstall(opts HookOptions, hookPath string) (int, error) {
	existing, err := os.ReadFile(hookPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 2, err
	}
	if err == nil {
		// Existing hook — leave alone unless --force or it's already ours.
		if string(existing) == HookScriptContent {
			emitHookResult(opts, "noop", hookPath, "hook is already installed")
			return 0, nil
		}
		if !opts.Force {
			return 1, fmt.Errorf("hook: %q already exists; pass --force to overwrite", hookPath)
		}
	}
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		return 2, err
	}
	if err := os.WriteFile(hookPath, []byte(HookScriptContent), 0o755); err != nil {
		return 2, err
	}
	emitHookResult(opts, "installed", hookPath, "")
	return 0, nil
}

func runHookUninstall(opts HookOptions, hookPath string) (int, error) {
	existing, err := os.ReadFile(hookPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			emitHookResult(opts, "noop", hookPath, "no hook installed")
			return 0, nil
		}
		return 2, err
	}
	if string(existing) != HookScriptContent {
		emitHookResult(opts, "noop", hookPath, "hook content does not match — left untouched")
		return 0, nil
	}
	if err := os.Remove(hookPath); err != nil {
		return 2, err
	}
	emitHookResult(opts, "uninstalled", hookPath, "")
	return 0, nil
}

func emitHookResult(opts HookOptions, action, path, note string) {
	if opts.Output.JSONMode() {
		opts.Output.JSON(hookPayload{Action: action, Path: path, Note: note})
		return
	}
	if note != "" {
		opts.Output.Print(fmt.Sprintf("%s: %s (%s)", action, path, note))
	} else {
		opts.Output.Print(fmt.Sprintf("%s: %s", action, path))
	}
}

// resolveHookPath returns .git/hooks/pre-commit relative to cwd. When .git
// is a file (worktree gitfile), reads the gitdir and uses that.
func resolveHookPath(cwd string) (string, error) {
	gitDir := filepath.Join(cwd, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("hook: %q is not a git repository (no .git/ directory)", cwd)
		}
		return "", err
	}
	if info.IsDir() {
		return filepath.Join(gitDir, "hooks", "pre-commit"), nil
	}
	// .git is a file — read it for the gitdir path.
	body, err := os.ReadFile(gitDir)
	if err != nil {
		return "", err
	}
	const prefix = "gitdir: "
	line := string(body)
	if len(line) > len(prefix) && line[:len(prefix)] == prefix {
		gitdir := line[len(prefix):]
		// trim newline
		for len(gitdir) > 0 && (gitdir[len(gitdir)-1] == '\n' || gitdir[len(gitdir)-1] == '\r') {
			gitdir = gitdir[:len(gitdir)-1]
		}
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(cwd, gitdir)
		}
		return filepath.Join(gitdir, "hooks", "pre-commit"), nil
	}
	return "", fmt.Errorf("hook: cannot parse .git file at %q", gitDir)
}
