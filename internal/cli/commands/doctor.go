package commands

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/htxryan/claude-code-config-profiles/internal/markers"
	"github.com/htxryan/claude-code-config-profiles/internal/resolver"
	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

type doctorCheck struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	Detail      string `json:"detail,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

type doctorPayload struct {
	Pass   bool          `json:"pass"`
	Checks []doctorCheck `json:"checks"`
}

const (
	doctorOk   = "ok"
	doctorWarn = "warn"
	doctorFail = "fail"
	doctorSkip = "skip"
)

// RunDoctor implements `c3p doctor`. Read-only environment health check.
// Mirrors src/cli/commands/doctor.ts.
func RunDoctor(opts DoctorOptions) (int, error) {
	paths := state.BuildStatePaths(opts.Cwd)
	checks := []doctorCheck{}

	checks = append(checks, checkProfilesDir(paths))
	stReader, st := readStateForDoctor(paths)
	checks = append(checks, stReader)
	checks = append(checks, checkLock(paths))
	checks = append(checks, checkGitignore(paths))
	checks = append(checks, checkPreCommitHook(opts.Cwd))
	checks = append(checks, checkBackupRetention(paths))
	checks = append(checks, checkRootClaudeMdMarkers(paths, st))
	checks = append(checks, checkActiveProfileResolves(paths, st))

	pass := true
	for _, c := range checks {
		if c.Status == doctorWarn || c.Status == doctorFail {
			pass = false
		}
	}

	payload := doctorPayload{Pass: pass, Checks: checks}
	if opts.Output.JSONMode() {
		opts.Output.JSON(payload)
		if !pass {
			return 1, nil
		}
		return 0, nil
	}

	rows := [][]string{{"CHECK", "STATUS", "DETAIL"}}
	for _, c := range checks {
		row := []string{c.Label, strings.ToUpper(c.Status), c.Detail}
		rows = append(rows, row)
	}
	opts.Output.Print(renderTable(rows))
	if !pass {
		opts.Output.Print("")
		for _, c := range checks {
			if c.Remediation != "" && (c.Status == doctorWarn || c.Status == doctorFail) {
				opts.Output.Print(fmt.Sprintf("→ %s: %s", c.Label, c.Remediation))
			}
		}
		return 1, nil
	}
	return 0, nil
}

func checkProfilesDir(paths state.StatePaths) doctorCheck {
	c := doctorCheck{ID: "profiles-dir", Label: "profiles directory exists"}
	if exists, err := pathExists(paths.ProfilesDir); err != nil || !exists {
		c.Status = doctorWarn
		c.Detail = paths.ProfilesDir + " not found"
		c.Remediation = "run `c3p init` to bootstrap"
		return c
	}
	c.Status = doctorOk
	c.Detail = paths.ProfilesDir
	return c
}

func readStateForDoctor(paths state.StatePaths) (doctorCheck, state.StateFile) {
	c := doctorCheck{ID: "state-file", Label: "state.json schema"}
	st, err := state.ReadStateFile(paths)
	if err != nil {
		c.Status = doctorFail
		c.Detail = err.Error()
		return c, state.DefaultState()
	}
	if st.Warning != nil {
		// "Missing" is the normal pre-`use` state for a fresh project — not
		// degradation. Only ParseError / SchemaMismatch warrant a warn here.
		if st.Warning.Code == state.StateReadWarningMissing {
			c.Status = doctorOk
			c.Detail = "no state.json yet (no `c3p use` has run)"
			return c, st.State
		}
		c.Status = doctorWarn
		c.Detail = string(st.Warning.Code) + ": " + st.Warning.Detail
		c.Remediation = "run `c3p use <name>` to rewrite a clean state file"
		return c, st.State
	}
	c.Status = doctorOk
	if st.State.ActiveProfile == nil {
		c.Detail = "no active profile (fresh project)"
	} else {
		c.Detail = "active=" + *st.State.ActiveProfile
	}
	return c, st.State
}

func checkLock(paths state.StatePaths) doctorCheck {
	c := doctorCheck{ID: "lock", Label: "project lock"}
	if exists, err := pathExists(paths.LockFile); err != nil || !exists {
		c.Status = doctorOk
		c.Detail = "no lock held"
		return c
	}
	// Try a non-blocking acquire — if it succeeds, no peer holds the
	// advisory lock (the file just persists from a prior release).
	handle, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err == nil {
		_ = handle.Release()
		c.Status = doctorOk
		c.Detail = "lock file present but free"
		return c
	}
	body, _ := os.ReadFile(paths.LockFile)
	c.Status = doctorWarn
	c.Detail = "held: " + strings.TrimSpace(string(body))
	c.Remediation = "if PID is dead, remove " + paths.LockFile
	return c
}

func checkGitignore(paths state.StatePaths) doctorCheck {
	c := doctorCheck{ID: "gitignore", Label: ".gitignore entries"}
	body, err := os.ReadFile(paths.GitignoreFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.Status = doctorWarn
			c.Detail = "no .gitignore file"
			c.Remediation = "run `c3p init` to add entries"
			return c
		}
		c.Status = doctorFail
		c.Detail = err.Error()
		return c
	}
	required := []string{".claude/", ".claude-profiles/.meta/"}
	missing := []string{}
	content := string(body)
	for _, r := range required {
		if !strings.Contains(content, r) {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		c.Status = doctorWarn
		c.Detail = "missing: " + strings.Join(missing, ", ")
		c.Remediation = "run `c3p init` to add"
		return c
	}
	c.Status = doctorOk
	c.Detail = "all required entries present"
	return c
}

func checkPreCommitHook(cwd string) doctorCheck {
	c := doctorCheck{ID: "pre-commit-hook", Label: "git pre-commit hook"}
	hookPath, err := resolveHookPath(cwd)
	if err != nil {
		c.Status = doctorSkip
		c.Detail = err.Error()
		return c
	}
	body, err := os.ReadFile(hookPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.Status = doctorWarn
			c.Detail = "no hook installed"
			c.Remediation = "run `c3p hook install`"
			return c
		}
		c.Status = doctorFail
		c.Detail = err.Error()
		return c
	}
	if string(body) != HookScriptContent {
		c.Status = doctorWarn
		c.Detail = "hook content does not match canonical c3p hook"
		c.Remediation = "if intentional, leave alone; otherwise `c3p hook install --force`"
		return c
	}
	c.Status = doctorOk
	c.Detail = "installed and canonical"
	return c
}

func checkBackupRetention(paths state.StatePaths) doctorCheck {
	c := doctorCheck{ID: "backup-retention", Label: "backup count <= 5"}
	snaps, err := state.ListSnapshots(paths)
	if err != nil {
		// state.ListSnapshots returns (nil, nil) for a missing backup
		// directory; an actual error means a real IO/permission fault and
		// must be surfaced rather than masked as "no backups yet."
		if errors.Is(err, os.ErrNotExist) {
			c.Status = doctorOk
			c.Detail = "no backup directory yet"
			return c
		}
		c.Status = doctorFail
		c.Detail = err.Error()
		c.Remediation = "check filesystem permissions on the backup directory"
		return c
	}
	if len(snaps) == 0 {
		c.Status = doctorOk
		c.Detail = "no backups yet"
		return c
	}
	if len(snaps) > 5 {
		c.Status = doctorWarn
		c.Detail = fmt.Sprintf("%d snapshots (cap is 5)", len(snaps))
		c.Remediation = "snapshots will be pruned on next discard"
		return c
	}
	c.Status = doctorOk
	c.Detail = fmt.Sprintf("%d snapshots", len(snaps))
	return c
}

func checkRootClaudeMdMarkers(paths state.StatePaths, st state.StateFile) doctorCheck {
	c := doctorCheck{ID: "root-claude-md", Label: "project-root CLAUDE.md markers"}
	if st.ActiveProfile == nil {
		c.Status = doctorSkip
		c.Detail = "no active profile"
		return c
	}
	body, err := os.ReadFile(paths.RootClaudeMdFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.Status = doctorWarn
			c.Detail = "no CLAUDE.md at project root"
			c.Remediation = "run `c3p init` to create"
			return c
		}
		c.Status = doctorFail
		c.Detail = err.Error()
		return c
	}
	parsed := markers.ParseMarkers(string(body))
	switch parsed.Status {
	case markers.StatusValid:
		c.Status = doctorOk
		c.Detail = "markers valid"
	case markers.StatusMalformed:
		c.Status = doctorFail
		c.Detail = "markers malformed"
		c.Remediation = "edit CLAUDE.md to remove broken markers, then run `c3p init`"
	case markers.StatusAbsent:
		c.Status = doctorWarn
		c.Detail = "no markers"
		c.Remediation = "run `c3p init` to inject"
	}
	return c
}

func checkActiveProfileResolves(paths state.StatePaths, st state.StateFile) doctorCheck {
	c := doctorCheck{ID: "active-resolves", Label: "active profile resolves"}
	if st.ActiveProfile == nil {
		c.Status = doctorSkip
		c.Detail = "no active profile"
		return c
	}
	_, err := resolver.Resolve(*st.ActiveProfile, resolver.ResolveOptions{ProjectRoot: paths.ProjectRoot})
	if err != nil {
		c.Status = doctorFail
		c.Detail = err.Error()
		c.Remediation = "fix the profile manifest or `c3p use <other>`"
		return c
	}
	c.Status = doctorOk
	c.Detail = *st.ActiveProfile
	return c
}

