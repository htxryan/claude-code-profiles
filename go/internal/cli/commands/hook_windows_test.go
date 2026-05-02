//go:build windows

package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestS18_BatFailOpenWhenC3PMissing is the Windows-conditional fitness cell
// for S18 (W1 PR6 #4): when `c3p` is absent from PATH, the .bat companion
// must exit 0 silently. Mirrors the POSIX S18 covered by the cross-platform
// drift test suite.
//
// We materialize the bat directly from the frozen constant rather than
// shelling through `c3p hook install` so the test's failure surface is the
// bat content's own fail-open arithmetic, not the install pipeline.
func TestS18_BatFailOpenWhenC3PMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	batPath := filepath.Join(dir, "pre-commit.bat")
	if err := os.WriteFile(batPath, []byte(HookScriptContentBat), 0o755); err != nil {
		t.Fatalf("write bat: %v", err)
	}

	cmd := exec.Command("cmd.exe", "/c", batPath)
	// Empty PATH so the `where c3p` probe inside the bat fails — exactly
	// the S18 trigger condition. We keep SystemRoot so cmd.exe can still
	// locate its built-ins.
	cmd.Env = []string{
		"PATH=",
		"SystemRoot=" + os.Getenv("SystemRoot"),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bat exited non-zero (S18 broken): err=%v, output=%q", err, out)
	}
}
