package state

import (
	"errors"
	"os"
	"strings"
)

// GitignoreEntries are the project-root .gitignore lines required by D5
// artifacts (R15, R23a/R28). Order is the order they're appended on first
// write; a section header precedes them so users see what added them.
//
// CLAUDE.md.*.tmp is the splice-write staging file; applyRootSplice deliberately
// places it next to the live CLAUDE.md to keep the temp+rename on the same
// filesystem (no EXDEV risk). sweepRootClaudeMdTmps removes them on the next
// c3p op, but a crash followed by `git add -A` could otherwise stage the debris.
var GitignoreEntries = []string{
	".claude/",
	".claude-profiles/.meta/",
	"CLAUDE.md.*.tmp",
}

const gitignoreSectionHeader = "# Added by c3p"

// GitignoreUpdate is the result of EnsureGitignoreEntries. Added is the
// entries we appended; an empty slice means everything was already present.
// Created is true iff the file did not exist before this call.
type GitignoreUpdate struct {
	Added   []string
	Created bool
}

// EnsureGitignoreEntries appends each missing GitignoreEntries entry to the
// project-root .gitignore (R15). Idempotent: existing entries (matched by
// exact-line equality after trim, comments and indented variants do NOT
// count as matches and would be flagged as missing) are left in place.
//
// Atomic write: tmp staging is placed inside .claude-profiles/.meta/tmp/
// (NOT next to .gitignore at the project root). A .gitignore.tmp at the
// project root would be visible in `git status` after a crash, and adding
// .gitignore.tmp to the gitignore is circular (the file we're writing IS
// the gitignore). Cross-filesystem rename is not a concern in practice —
// .claude-profiles/ is a sibling of .gitignore, both inside the project.
func EnsureGitignoreEntries(paths StatePaths) (GitignoreUpdate, error) {
	existingBytes, err := os.ReadFile(paths.GitignoreFile)
	created := false
	existing := ""
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return GitignoreUpdate{}, err
		}
		created = true
	} else {
		existing = string(existingBytes)
	}

	present := map[string]struct{}{}
	for _, line := range strings.Split(existing, "\n") {
		// Mirror the TS reference's split(/\r?\n/) by trimming a CR before
		// the trim-and-empty check.
		trimmed := strings.TrimRight(line, "\r")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		present[trimmed] = struct{}{}
	}

	toAdd := make([]string, 0, len(GitignoreEntries))
	for _, e := range GitignoreEntries {
		if _, ok := present[e]; !ok {
			toAdd = append(toAdd, e)
		}
	}
	if len(toAdd) == 0 {
		return GitignoreUpdate{Added: []string{}, Created: created}, nil
	}

	// Trim trailing whitespace; insert a single blank-line separator between
	// existing content and our section header so the file stays readable.
	trimmed := strings.TrimRight(existing, " \t\n\r")
	sep := ""
	if trimmed != "" {
		sep = "\n\n"
	}
	block := sep + gitignoreSectionHeader + "\n" + strings.Join(toAdd, "\n") + "\n"
	next := trimmed + block

	if err := os.MkdirAll(paths.TmpDir, 0o755); err != nil {
		return GitignoreUpdate{}, err
	}
	tmpPath := UniqueAtomicTmpPath(paths.TmpDir, paths.GitignoreFile)
	if err := AtomicWriteFile(paths.GitignoreFile, tmpPath, []byte(next)); err != nil {
		_ = os.Remove(tmpPath)
		return GitignoreUpdate{}, err
	}
	added := append([]string(nil), toAdd...)
	return GitignoreUpdate{Added: added, Created: created}, nil
}
