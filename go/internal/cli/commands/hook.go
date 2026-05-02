package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// HookScriptContentBat is the Windows .bat companion to HookScriptContent
// (PR15). Git on Windows runs whichever pre-commit it finds at the configured
// core.hooksPath; some installations (Git for Windows + MSYS) run the POSIX
// script through sh.exe, others prefer the native .bat. We install both so
// either path triggers the same fail-open drift warning.
//
// Fail-open contract (matches POSIX):
//   - missing/broken c3p binary on PATH => exit 0 silently
//   - drift output goes to stderr but exit 0 regardless
//
// Byte-frozen: changing this string is a deliberate spec bump (W1 fitness
// function pre_commit_bat_byte_test.go pins these bytes after first ship).
// CRLF line endings are intentional — cmd.exe parses .bat files line-by-line
// and POSIX line endings on a .bat have caused subtle parser misbehavior in
// historical Git for Windows installs.
const HookScriptContentBat = "@echo off\r\n" +
	"REM c3p pre-commit drift warner. Fail-open: never blocks a commit.\r\n" +
	"REM Managed by 'c3p hook install'. To uninstall: 'c3p hook uninstall'.\r\n" +
	"where c3p >nul 2>nul\r\n" +
	"if errorlevel 1 exit /b 0\r\n" +
	"c3p drift --pre-commit-warn 1>&2\r\n" +
	"exit /b 0\r\n"

// hookBatFilename is the .bat companion filename written next to pre-commit.
// Git for Windows resolves pre-commit.bat when no extensionless pre-commit is
// available; with both present the user's PATHEXT decides which runs.
const hookBatFilename = "pre-commit.bat"

type hookPayload struct {
	Action      string `json:"action"`
	Path        string `json:"path"`
	BatPath     string `json:"batPath,omitempty"`
	BatAction   string `json:"batAction,omitempty"`
	Note        string `json:"note,omitempty"`
}

// RunHook implements `c3p hook install|uninstall`. Mirrors src/cli/commands/hook.ts.
//
// PR15: on Windows hosts, install/uninstall also manage a pre-commit.bat
// companion alongside the POSIX-shell hook. Git for Windows may invoke either
// depending on core.hooksPath / PATHEXT; both must implement equivalent
// fail-open semantics so the user-visible behavior is invariant under the
// decision git makes at commit time.
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
	return 1, userErrorf("hook: action must be install|uninstall")
}

// shouldInstallBatCompanion reports whether RunHook should also manage the
// .bat companion. Currently keyed off the host OS — installing on Windows
// writes both .sh and .bat; installing on POSIX writes only .sh.
//
// Variable rather than const so platform-conditional tests can flip it (the
// .bat byte test wants to assert install-side behaviour without requiring a
// Windows runner).
var shouldInstallBatCompanion = func() bool {
	return runtime.GOOS == "windows"
}

func runHookInstall(opts HookOptions, hookPath string) (int, error) {
	existing, err := os.ReadFile(hookPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 2, err
	}
	posixAction := "installed"
	if err == nil {
		// Existing hook — leave alone unless --force or it's already ours.
		if string(existing) == HookScriptContent {
			posixAction = "noop"
		} else if !opts.Force {
			return 1, userErrorf("hook: %q already exists; pass --force to overwrite", hookPath)
		}
	}
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		return 2, err
	}
	if posixAction == "installed" {
		if err := os.WriteFile(hookPath, []byte(HookScriptContent), 0o755); err != nil {
			return 2, err
		}
	}

	// PR15: write the .bat companion when running on a Windows host. We do
	// NOT write it on POSIX even with --force — the bat file would never be
	// consulted by git on POSIX, and silently emitting it would mislead users
	// into thinking it's a managed artifact on every platform.
	batPath, batAction := "", ""
	if shouldInstallBatCompanion() {
		batPath = filepath.Join(filepath.Dir(hookPath), hookBatFilename)
		batExisting, err := os.ReadFile(batPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return 2, err
		}
		if err == nil && string(batExisting) == HookScriptContentBat {
			batAction = "noop"
		} else if err == nil && !opts.Force {
			return 1, userErrorf("hook: %q already exists; pass --force to overwrite", batPath)
		} else {
			// 0o644: cmd.exe runs .bat by extension via PATHEXT, not by the
			// POSIX exec bit. A 0o755 here would be harmless but misleading
			// — leave the file as plain data.
			if err := os.WriteFile(batPath, []byte(HookScriptContentBat), 0o644); err != nil {
				return 2, err
			}
			batAction = "installed"
		}
	}

	note := ""
	if posixAction == "noop" && (batAction == "" || batAction == "noop") {
		note = "hook is already installed"
	}
	emitHookResultWithBat(opts, posixAction, hookPath, batAction, batPath, note)
	return 0, nil
}

func runHookUninstall(opts HookOptions, hookPath string) (int, error) {
	existing, err := os.ReadFile(hookPath)
	posixAction := ""
	posixNote := ""
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			posixAction = "noop"
			posixNote = "no hook installed"
		} else {
			return 2, err
		}
	} else if string(existing) != HookScriptContent {
		posixAction = "noop"
		posixNote = "hook content does not match — left untouched"
	} else {
		if err := os.Remove(hookPath); err != nil {
			return 2, err
		}
		posixAction = "uninstalled"
	}

	// PR15: on Windows, also drop the .bat companion if its bytes match. A
	// user-edited .bat is left alone, mirroring the POSIX rule.
	batPath, batAction := "", ""
	if shouldInstallBatCompanion() {
		batPath = filepath.Join(filepath.Dir(hookPath), hookBatFilename)
		batExisting, berr := os.ReadFile(batPath)
		if berr != nil {
			if errors.Is(berr, os.ErrNotExist) {
				batAction = "noop"
			} else {
				return 2, berr
			}
		} else if string(batExisting) != HookScriptContentBat {
			batAction = "noop"
		} else {
			if err := os.Remove(batPath); err != nil {
				return 2, err
			}
			batAction = "uninstalled"
		}
	}

	emitHookResultWithBat(opts, posixAction, hookPath, batAction, batPath, posixNote)
	return 0, nil
}

func emitHookResult(opts HookOptions, action, path, note string) {
	emitHookResultWithBat(opts, action, path, "", "", note)
}

// emitHookResultWithBat reports both the POSIX hook outcome and the optional
// .bat companion outcome. JSON mode emits a single payload with both slots so
// scripted callers see the whole picture without diffing two messages. Human
// mode prints two lines when the bat slot is populated.
func emitHookResultWithBat(opts HookOptions, action, path, batAction, batPath, note string) {
	if opts.Output.JSONMode() {
		opts.Output.JSON(hookPayload{
			Action:    action,
			Path:      path,
			BatAction: batAction,
			BatPath:   batPath,
			Note:      note,
		})
		return
	}
	if note != "" {
		opts.Output.Print(fmt.Sprintf("%s: %s (%s)", action, path, note))
	} else {
		opts.Output.Print(fmt.Sprintf("%s: %s", action, path))
	}
	if batAction != "" {
		opts.Output.Print(fmt.Sprintf("%s: %s", batAction, batPath))
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
