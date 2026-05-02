package state

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/htxryan/c3p/internal/resolver"
)

// StatePaths is the canonical bundle of on-disk paths owned by the state
// layer. All artifacts (state file, lock, atomic-write staging, materialize
// pending/prior, discard backup) are addressed through this single struct so
// reconciliation, materialization, and persist agree on what lives where.
//
// Mirrors src/state/paths.ts:StatePaths. D4 lands the primitive paths; the
// pending/prior protocol that consumes them is in D5.
type StatePaths struct {
	// ProjectRoot is the absolute project root.
	ProjectRoot string
	// ProfilesDir is the absolute path to .claude-profiles/.
	ProfilesDir string
	// MetaDir is .claude-profiles/.meta/, the parent of every bookkeeping
	// artifact owned by this CLI.
	MetaDir string
	// ClaudeDir is the absolute path to the live .claude/ tree.
	ClaudeDir string
	// StateFile is .claude-profiles/.meta/state.json.
	StateFile string
	// LockFile is .claude-profiles/.meta/lock.
	LockFile string
	// TmpDir is the atomic-write staging directory (.meta/tmp/).
	TmpDir string
	// PendingDir is the materialize-side staging directory (R16 step a).
	PendingDir string
	// PriorDir is the materialize-side prior backup (R16 step b).
	PriorDir string
	// BackupDir is the discard-backup root (.meta/backup/).
	BackupDir string
	// GitignoreFile is the project-root .gitignore.
	GitignoreFile string
	// RootClaudeMdFile is the project-root CLAUDE.md (cw6/T4) — peer of .claude/.
	RootClaudeMdFile string
}

// BuildStatePaths constructs StatePaths from a project root. The input is
// canonicalized via filepath.Abs so callers may pass relative roots in tests.
func BuildStatePaths(projectRoot string) StatePaths {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		// filepath.Abs only fails when os.Getwd fails; fall back to the input
		// so callers passing absolute t.TempDir() paths are unaffected.
		root = projectRoot
	}
	profilesDir := filepath.Join(root, ".claude-profiles")
	metaDir := filepath.Join(profilesDir, ".meta")
	return StatePaths{
		ProjectRoot:      root,
		ProfilesDir:      profilesDir,
		MetaDir:          metaDir,
		ClaudeDir:        filepath.Join(root, ".claude"),
		StateFile:        filepath.Join(metaDir, "state.json"),
		LockFile:         filepath.Join(metaDir, "lock"),
		TmpDir:           filepath.Join(metaDir, "tmp"),
		PendingDir:       filepath.Join(metaDir, "pending"),
		PriorDir:         filepath.Join(metaDir, "prior"),
		BackupDir:        filepath.Join(metaDir, "backup"),
		GitignoreFile:    filepath.Join(root, ".gitignore"),
		RootClaudeMdFile: filepath.Join(root, "CLAUDE.md"),
	}
}

// PersistPaths is the per-profile bundle used by D5 persist (R22b transactional
// pair). The persist flow uses the same pending/prior protocol as materialize
// but targets the *profile's* .claude/ rather than the live one.
type PersistPaths struct {
	// ProfileDir is .claude-profiles/<profile>/.
	ProfileDir string
	// TargetClaudeDir is .claude-profiles/<profile>/.claude/ (the persist target).
	TargetClaudeDir string
	// PendingDir is the persist staging dir.
	PendingDir string
	// PriorDir is the persist prior backup.
	PriorDir string
}

// BuildPersistPaths constructs PersistPaths for a profile. Re-validates the
// profile name for defense-in-depth against any caller that bypasses the
// resolver's own validation (multi-reviewer P2 in TS land).
func BuildPersistPaths(paths StatePaths, profileName string) (PersistPaths, error) {
	if !validPersistProfileName(profileName) {
		return PersistPaths{}, fmt.Errorf("invalid profile name for persist target: %q", profileName)
	}
	profileDir := filepath.Join(paths.ProfilesDir, profileName)
	return PersistPaths{
		ProfileDir:      profileDir,
		TargetClaudeDir: filepath.Join(profileDir, ".claude"),
		PendingDir:      filepath.Join(profileDir, ".pending"),
		PriorDir:        filepath.Join(profileDir, ".prior"),
	}, nil
}

func validPersistProfileName(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.ContainsRune(name, 0) {
		return false
	}
	// Reject any path separator on either platform so a name authored on one
	// host can never escape the profilesDir on the other.
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	if resolver.IsWindowsReservedName(name) {
		return false
	}
	return true
}

// rootMdTmpCounter is a process-wide monotonic counter for unique
// CLAUDE.md.*.tmp filenames. Combined with PID + crypto-random suffix it
// resists collisions across forks and concurrent goroutines.
var rootMdTmpCounter atomic.Uint64

// RootClaudeMdTmpPath builds a unique tmp staging path for a section-splice
// write of project-root CLAUDE.md. The tmp lives in the SAME directory as the
// final file so the rename is guaranteed same-filesystem (no EXDEV) and crash
// debris is locatable by a stable glob: <projectRoot>/CLAUDE.md.*.tmp.
//
// Pattern: <basename>.<pid>.<counter>-<random>.tmp — mirrors uniqueAtomicTmpPath
// (atomic.go) so operators can correlate the pid across diagnostics. The .tmp
// suffix is the recovery sentinel that reconcileMaterialize (D5) greps for.
func RootClaudeMdTmpPath(paths StatePaths) string {
	counter := rootMdTmpCounter.Add(1) - 1
	nonce := fmt.Sprintf("%d-%s", counter, randomNonce())
	return fmt.Sprintf("%s.%d.%s.tmp", paths.RootClaudeMdFile, os.Getpid(), nonce)
}

// rootClaudeMdTmpPattern matches the shape of RootClaudeMdTmpPath's output.
// Tied to the CLAUDE.md basename so we never sweep an unrelated .tmp file the
// user created (mirrors src/state/paths.ts:isRootClaudeMdTmpName).
var rootClaudeMdTmpPattern = regexp.MustCompile(`^CLAUDE\.md\.\d+\.\d+-[a-z0-9]+\.tmp$`)

// IsRootClaudeMdTmpName returns true iff name is a leftover section-splice tmp
// that reconcile should sweep. Matches the shape produced by
// RootClaudeMdTmpPath; non-matching .tmp files are user artifacts.
func IsRootClaudeMdTmpName(name string) bool {
	return rootClaudeMdTmpPattern.MatchString(name)
}

// randomNonce returns 8 hex characters of cryptographic randomness. Used by
// the unique-tmp path builders so concurrent writers never share a staging
// path even when PID + counter coincide (e.g. across fork bursts).
func randomNonce() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand is documented to never fail on supported platforms; if
		// the kernel RNG is genuinely broken, a fixed nonce is preferable to
		// crashing in a path-construction helper.
		return "00000000"
	}
	return hex.EncodeToString(b[:])
}
