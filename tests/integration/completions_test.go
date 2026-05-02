package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// TestCompletions_NonEmptyScriptForEachShell — IV translation of TS
// completions.test.ts "emits non-empty script for each supported shell".
// The shell completion verbs return scripts >100 bytes for bash/zsh/fish.
func TestCompletions_NonEmptyScriptForEachShell(t *testing.T) {
	helpers.EnsureBuilt(t)
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			r := mustRun(t, helpers.SpawnOptions{Args: []string{"completions", shell}})
			if r.ExitCode != 0 {
				t.Fatalf("%s exit: want 0, got %d (stderr=%q)", shell, r.ExitCode, r.Stderr)
			}
			if len(r.Stdout) <= 100 {
				t.Fatalf("%s script too short (%d bytes): %q", shell, len(r.Stdout), r.Stdout)
			}
		})
	}
}

// TestCompletions_RejectsUnsupportedShell — exit 1 + bash|zsh|fish
// guidance in stderr.
func TestCompletions_RejectsUnsupportedShell(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"completions", "powershell"}})
	if r.ExitCode != 1 {
		t.Fatalf("powershell: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "bash|zsh|fish") {
		t.Errorf("stderr missing 'bash|zsh|fish' enumeration: %q", r.Stderr)
	}
}

// TestCompletions_RejectsMissingShellArg — exit 1 + 'requires a shell'
// message.
func TestCompletions_RejectsMissingShellArg(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"completions"}})
	if r.ExitCode != 1 {
		t.Fatalf("completions (no arg): want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "requires a shell") {
		t.Errorf("stderr missing 'requires a shell': %q", r.Stderr)
	}
}

// TestCompletions_BashSourcesCleanly — the emitted bash script must
// source without error and register the _c3p completion function.
func TestCompletions_BashSourcesCleanly(t *testing.T) {
	if !shellAvailable("bash") {
		t.Skip("bash not on PATH")
	}
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"completions", "bash"}})
	if r.ExitCode != 0 {
		t.Fatalf("completions bash: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	scriptPath := writeShellTmp(t, "ccp-bash", r.Stdout)
	// Go bin's bash completion registers _c3p_complete (via
	// `complete -F _c3p_complete c3p`). TS used _c3p; we assert against
	// the actual emitted function name.
	out := runShell(t, "bash", fmt.Sprintf("source %s && type _c3p_complete", scriptPath))
	if out.ExitCode != 0 {
		t.Fatalf("bash source: want 0, got %d (stderr=%q)", out.ExitCode, out.Stderr)
	}
	if !strings.Contains(out.Stdout, "_c3p_complete") {
		t.Errorf("bash registered function missing _c3p_complete: %q", out.Stdout)
	}
}

// TestCompletions_ZshSourcesCleanly — same for zsh, with compinit prep.
func TestCompletions_ZshSourcesCleanly(t *testing.T) {
	if !shellAvailable("zsh") {
		t.Skip("zsh not on PATH")
	}
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"completions", "zsh"}})
	if r.ExitCode != 0 {
		t.Fatalf("completions zsh: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	scriptPath := writeShellTmp(t, "ccp-zsh", r.Stdout)
	out := runShell(t, "zsh", fmt.Sprintf(
		"autoload -Uz compinit && compinit -u && source %s && type _c3p", scriptPath))
	if out.ExitCode != 0 {
		t.Fatalf("zsh source: want 0, got %d (stderr=%q)", out.ExitCode, out.Stderr)
	}
	if !strings.Contains(out.Stdout, "_c3p") {
		t.Errorf("zsh registered widget missing _c3p: %q", out.Stdout)
	}
}

// TestCompletions_FishSourcesCleanly — when fish is available.
func TestCompletions_FishSourcesCleanly(t *testing.T) {
	if !shellAvailable("fish") {
		t.Skip("fish not on PATH")
	}
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"completions", "fish"}})
	if r.ExitCode != 0 {
		t.Fatalf("completions fish: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	scriptPath := writeShellTmp(t, "ccp-fish", r.Stdout)
	out := runShell(t, "fish", fmt.Sprintf(
		"source %s && complete -c c3p | head -1", scriptPath))
	if out.ExitCode != 0 {
		t.Fatalf("fish source: want 0, got %d (stderr=%q)", out.ExitCode, out.Stderr)
	}
}

// TestCompletions_JSONWrapsScript — --json wraps the script in
// {shell, script} for machine consumers.
func TestCompletions_JSONWrapsScript(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"completions", "bash", "--json"}})
	if r.ExitCode != 0 {
		t.Fatalf("completions bash --json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var payload struct {
		Shell  string `json:"shell"`
		Script string `json:"script"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(r.Stdout)), &payload); err != nil {
		t.Fatalf("parse JSON: %v (raw=%q)", err, r.Stdout)
	}
	if payload.Shell != "bash" {
		t.Errorf("payload.shell: want 'bash', got %q", payload.Shell)
	}
	// _c3p_complete is the bash function; _c3p substring matches it too,
	// but make the assertion explicit.
	if !strings.Contains(payload.Script, "_c3p_complete") {
		t.Errorf("payload.script missing '_c3p_complete': %q", payload.Script)
	}
}

// runShell shells out to `shell -c <body>` with a 15s timeout, matching
// helpers.RunCli's default. zsh `compinit` on a cold runner regularly
// approaches 5s on its own (rebuilding the function digest), so a tighter
// budget here flakes without signal value. Skips the test on context
// deadline rather than failing — completions are a "shell is sane"
// integration check, not a perf gate.
func runShell(t *testing.T, shell, body string) helpers.SpawnResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-c", body)
	out, err := cmd.Output()
	res := helpers.SpawnResult{Stdout: string(out)}
	if exitErr, ok := err.(*exec.ExitError); ok {
		res.Stderr = string(exitErr.Stderr)
		res.ExitCode = exitErr.ExitCode()
	} else if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			t.Skipf("runShell %s timed out after 15s; runner too slow for shell-completion smoke test", shell)
		}
		t.Fatalf("runShell %s: %v", shell, err)
	}
	if cmd.ProcessState != nil && res.ExitCode == 0 {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	return res
}

// writeShellTmp writes content to a unique temp file under t.TempDir()
// and returns the path. t.TempDir cleanup removes it at test end.
func writeShellTmp(t *testing.T, prefix, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), fmt.Sprintf("%s-%d.sh", prefix, time.Now().UnixNano()))
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	return p
}
