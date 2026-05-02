package resolver_test

import (
	stderrors "errors"
	"testing"

	pipelineerrors "github.com/htxryan/claude-code-config-profiles/internal/errors"
	"github.com/htxryan/claude-code-config-profiles/internal/resolver"
)

// PR16: Windows DOS-device names are rejected on every host so a profile
// authored on POSIX cannot land on Windows under a name the kernel refuses
// to open. The guard runs at every resolution boundary that admits a
// profile name (the requested profile itself + every extends ancestor).

func TestPR16_RejectsDOSDeviceProfileName(t *testing.T) {
	cases := []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM9", "LPT1", "LPT9"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if resolver.IsValidProfileName(name) {
				t.Fatalf("IsValidProfileName(%q): want false", name)
			}
			if !resolver.IsWindowsReservedName(name) {
				t.Fatalf("IsWindowsReservedName(%q): want true", name)
			}
		})
	}
}

func TestPR16_RejectsDOSDeviceWithExtension(t *testing.T) {
	cases := []string{"con.txt", "PRN.config", "COM1.json", "lpt9.bak"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if resolver.IsValidProfileName(name) {
				t.Fatalf("IsValidProfileName(%q): want false", name)
			}
			if !resolver.IsWindowsReservedName(name) {
				t.Fatalf("IsWindowsReservedName(%q): want true", name)
			}
		})
	}
}

func TestPR16_RejectsTrailingDotOrSpace(t *testing.T) {
	cases := []string{"foo.", "foo ", "x. ", "valid."}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if resolver.IsValidProfileName(name) {
				t.Fatalf("IsValidProfileName(%q): want false", name)
			}
		})
	}
}

func TestPR16_AcceptsNonReservedNames(t *testing.T) {
	cases := []string{"CONS", "PRNT", "console", "command", "com10", "lpt10", "frontend"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if !resolver.IsValidProfileName(name) {
				t.Fatalf("IsValidProfileName(%q): want true", name)
			}
		})
	}
}

func TestPR16_ResolveRejectsReservedProfileName(t *testing.T) {
	// Even if a directory named "CON" exists on disk (e.g. a POSIX dev
	// laptop), the resolver must refuse it as a profile identifier.
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"valid": {manifest: map[string]any{}},
		},
	})
	_, err := resolver.Resolve("CON", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var mpe *pipelineerrors.MissingProfileError
	if !stderrors.As(err, &mpe) {
		t.Fatalf("want MissingProfileError, got %v", err)
	}
}

func TestPR16_ResolveRejectsReservedExtends(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"extends": "CON"}},
		},
	})
	_, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var mpe *pipelineerrors.MissingProfileError
	if !stderrors.As(err, &mpe) {
		t.Fatalf("want MissingProfileError, got %v", err)
	}
}
