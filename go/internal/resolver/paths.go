package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
)

// ResolverPaths holds the canonical paths derived from a project root.
type ResolverPaths struct {
	ProjectRoot   string
	ProfilesDir   string
	ComponentsDir string
}

// BuildPaths constructs ResolverPaths rooted at projectRoot. The input is
// canonicalized via filepath.Abs so callers may pass relative paths.
func BuildPaths(projectRoot string) ResolverPaths {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		// filepath.Abs only fails when os.Getwd fails; fall back to the input
		// rather than panicking — tests construct paths from t.TempDir() which
		// is already absolute.
		root = projectRoot
	}
	return ResolverPaths{
		ProjectRoot:   root,
		ProfilesDir:   filepath.Join(root, ".claude-profiles"),
		ComponentsDir: filepath.Join(root, ".claude-profiles", "_components"),
	}
}

// ProfileDir returns the absolute directory for a profile by name.
func ProfileDir(paths ResolverPaths, name string) string {
	return filepath.Join(paths.ProfilesDir, name)
}

// winReservedNames matches Windows DOS-device names with optional extension.
// Rejected on every host so a profile authored on POSIX cannot land on
// Windows under a name the kernel refuses to open.
var winReservedNames = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\..*)?$`)

// IsValidProfileName returns true iff name is a legal profile identifier
// (R2 + R39 + PR16 cross-platform safety).
func IsValidProfileName(name string) bool {
	if name == "" {
		return false
	}
	if name == "." || name == ".." {
		return false
	}
	if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
		return false
	}
	// Both POSIX and Windows separators rejected on every host so a manifest
	// authored on either platform behaves identically.
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	if strings.ContainsRune(name, 0) {
		return false
	}
	if winReservedNames.MatchString(name) {
		return false
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	return true
}

// IsWindowsReservedName returns true iff name matches a Windows DOS-device
// reserved name (PR16). Exported for defense-in-depth callers that re-validate
// at the persist boundary.
func IsWindowsReservedName(name string) bool {
	return winReservedNames.MatchString(name)
}

// IsExternal returns true iff absPath is outside projectRoot. Both arguments
// must already be absolute; the function does not canonicalize and does not
// follow symlinks (textual containment only — security boundary doc note).
func IsExternal(absPath, projectRoot string) bool {
	rel, err := filepath.Rel(projectRoot, absPath)
	if err != nil {
		return true
	}
	if rel == "." {
		return false
	}
	// `..` alone, or `../something`, signals escape. Guard against a bare
	// filename like `..hidden` (legitimate) being mistaken for traversal —
	// match only `..` or a `..` segment followed by the path separator.
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	if filepath.IsAbs(rel) {
		return true
	}
	return false
}

// ClassifyInclude classifies and resolves an includes entry per R37 +
// PR16a. Returns InvalidManifestError for syntactically invalid forms,
// PathTraversalError for relative-form (`./` / `../`) entries that resolve
// outside projectRoot.
//
// PR16a is enforced via TEXTUAL canonicalization (filepath.Join) only —
// the function does NOT follow symlinks. A relative include that points at
// a symlink whose target lives outside projectRoot will pass classification
// (the textual path is inside) and the link will be followed by os.Stat at
// resolve time. v1 accepts that boundary; D5 (state) and D6 (drift) trust
// the resolver's classification.
//
// TS deviation note: the TS resolver tracks `../`-outside-projectRoot
// includes as `external=true` and surfaces them as MissingInclude only
// when the directory does not exist. The Go resolver rejects up-front with
// PathTraversalError per the spec strengthening in PR16a. Downstream
// consumers (D2/D5/D7) will therefore never see a relative-kind include
// with `external=true` on Go output.
func ClassifyInclude(raw, referencingProfileDir string, paths ResolverPaths, referencedBy string) (IncludeRef, error) {
	if raw == "" {
		return IncludeRef{}, pipelineerrors.NewInvalidManifestError(
			referencingProfileDir,
			fmt.Sprintf("empty include string in profile %q", referencedBy),
		)
	}

	var (
		kind         IncludeSourceKind
		resolvedPath string
	)

	switch {
	case raw == "~" || strings.HasPrefix(raw, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return IncludeRef{}, pipelineerrors.NewInvalidManifestError(
				referencingProfileDir,
				fmt.Sprintf("cannot expand %q in profile %q: home directory unavailable: %v", raw, referencedBy, err),
			)
		}
		if raw == "~" {
			resolvedPath = filepath.Clean(home)
		} else {
			resolvedPath = filepath.Join(home, raw[2:])
		}
		// Make absolute (Clean does not).
		resolvedPath = mustAbs(resolvedPath)
		kind = IncludeKindTilde

	case strings.HasPrefix(raw, "~"):
		// "~user" form: not portable, not in R37.
		return IncludeRef{}, pipelineerrors.NewInvalidManifestError(
			referencingProfileDir,
			fmt.Sprintf(
				`include %q in profile %q — "~user" form is not supported; use "~/path" or an absolute path`,
				raw, referencedBy,
			),
		)

	case filepath.IsAbs(raw):
		resolvedPath = filepath.Clean(raw)
		kind = IncludeKindAbsolute

	case strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../"):
		resolvedPath = filepath.Join(referencingProfileDir, raw)
		// PR16a: a relative include with `../` segments that escapes the
		// project root must be rejected before any filesystem touch.
		if IsExternal(resolvedPath, paths.ProjectRoot) {
			return IncludeRef{}, pipelineerrors.NewPathTraversalError(raw, resolvedPath, referencedBy)
		}
		kind = IncludeKindRelative

	case strings.ContainsAny(raw, "/\\") || strings.ContainsRune(raw, os.PathSeparator):
		// R37 admits exactly four forms; bare-with-slashes matches none.
		// Reject explicitly so Windows-authored manifests behave identically.
		return IncludeRef{}, pipelineerrors.NewInvalidManifestError(
			referencingProfileDir,
			fmt.Sprintf(
				`include %q in profile %q is not a valid form — use a bare component name, "./..." / "../..." for relative, "/..." for absolute, or "~/..." for home-relative`,
				raw, referencedBy,
			),
		)

	default:
		resolvedPath = filepath.Join(paths.ComponentsDir, raw)
		kind = IncludeKindComponent
	}

	return IncludeRef{
		Raw:          raw,
		Kind:         kind,
		ResolvedPath: resolvedPath,
		External:     IsExternal(resolvedPath, paths.ProjectRoot),
	}, nil
}

func mustAbs(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return abs
}
