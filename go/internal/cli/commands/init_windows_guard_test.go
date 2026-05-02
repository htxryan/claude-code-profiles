package commands

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestUnsafeProjectRoot_AcceptsSafePaths is the negative-control: paths
// without reserved characters or DOS-device segments must pass.
func TestUnsafeProjectRoot_AcceptsSafePaths(t *testing.T) {
	t.Parallel()
	cases := []string{
		"/home/u/repo",
		"/tmp/c3p-fixture-x9",
		"/mnt/data/project.with-dot",
		`C:\Users\dev\repo`,
		`D:\code\project`,
		"/path/CONS/repo",   // 'CONS' is not the reserved 'CON'
		"/path/console/x",   // similar
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			if reason := unsafeProjectRoot(p); reason != "" {
				t.Errorf("unsafeProjectRoot(%q) = %q, want empty", p, reason)
			}
		})
	}
}

// TestUnsafeProjectRoot_RejectsReservedChars covers the Windows-illegal
// character set. A POSIX path containing any of these would land in a
// .claude-profiles/.meta tree the kernel cannot traverse on Windows.
func TestUnsafeProjectRoot_RejectsReservedChars(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		hint string
	}{
		{"/home/u/repo<x", `"<"`},
		{"/home/u/repo>x", `">"`},
		{`/home/u/repo"x`, `"\""`},
		{"/home/u/repo|x", `"|"`},
		{"/home/u/repo?x", `"?"`},
		{"/home/u/repo*x", `"*"`},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			reason := unsafeProjectRoot(c.path)
			if reason == "" {
				t.Fatalf("unsafeProjectRoot(%q) returned empty; expected reserved-character rejection", c.path)
			}
			if !strings.Contains(reason, "reserved character") {
				t.Errorf("reason %q missing 'reserved character'", reason)
			}
		})
	}
}

// TestUnsafeProjectRoot_RejectsDOSDeviceSegments rejects any path segment
// matching the DOS-device reserved-name pattern.
func TestUnsafeProjectRoot_RejectsDOSDeviceSegments(t *testing.T) {
	t.Parallel()
	cases := []string{
		"/home/CON",
		"/home/u/PRN/repo",
		"/CON/repo",
		`C:\Users\COM1\repo`,
		"/x/lpt1.txt/repo",
		"/y/AUX",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			reason := unsafeProjectRoot(p)
			if reason == "" {
				t.Fatalf("unsafeProjectRoot(%q) returned empty; expected DOS-device rejection", p)
			}
			if !strings.Contains(reason, "Windows reserved name") {
				t.Errorf("reason %q missing 'Windows reserved name'", reason)
			}
		})
	}
}

// TestUnsafeProjectRoot_AcceptsDriveLetterColon documents that the leading
// drive-letter colon on Windows-style paths is recognised and exempt from
// the reserved-character check.
func TestUnsafeProjectRoot_AcceptsDriveLetterColon(t *testing.T) {
	t.Parallel()
	if reason := unsafeProjectRoot(`C:\Users\dev\repo`); reason != "" {
		t.Errorf("drive-letter path rejected: %q", reason)
	}
}

// TestUnsafeProjectRoot_RejectsControlBytes catches NUL and other low
// control-byte injection (defense-in-depth against unsanitized env vars
// becoming the project root).
func TestUnsafeProjectRoot_RejectsControlBytes(t *testing.T) {
	t.Parallel()
	if reason := unsafeProjectRoot("/home/u/repo\x00x"); !strings.Contains(reason, "NUL") {
		t.Errorf("expected NUL rejection, got %q", reason)
	}
	if reason := unsafeProjectRoot("/home/u/repo\x01x"); !strings.Contains(reason, "control character") {
		t.Errorf("expected control-character rejection, got %q", reason)
	}
}

// TestRunInit_RefusesUnsafeProjectRoot is the integration cell: a path
// containing a reserved DOS-device segment causes RunInit to abort with a
// UserError before any filesystem work happens.
func TestRunInit_RefusesUnsafeProjectRoot(t *testing.T) {
	t.Parallel()
	// Build a path that's reserved on every host's interpretation; we don't
	// need the directory to exist on disk because the guard runs before the
	// MkdirAll.
	cwd := filepath.Join(t.TempDir(), "CON")
	out := &captureOutput{}
	code, err := RunInit(InitOptions{Cwd: cwd, Output: out})
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("want UserError, got %v", err)
	}
	if !strings.Contains(ue.Message, "Windows") {
		t.Errorf("error message %q should mention Windows", ue.Message)
	}
	// runtime.GOOS isn't used here — the guard fires on every host so the
	// reference is just for the comment trail.
	_ = runtime.GOOS
}
