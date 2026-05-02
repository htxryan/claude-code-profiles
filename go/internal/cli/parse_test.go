package cli

import (
	"testing"

	"github.com/htxryan/c3p/internal/drift"
)

func TestParseTopLevelHelp(t *testing.T) {
	r := ParseArgs([]string{"--help"}, "/cwd")
	if !r.Ok {
		t.Fatalf("--help failed to parse: %v", r.Err)
	}
	if r.Invocation.Command.Kind != KindHelp {
		t.Fatalf("want KindHelp, got %q", r.Invocation.Command.Kind)
	}
}

func TestParseVersion(t *testing.T) {
	r := ParseArgs([]string{"--version"}, "/cwd")
	if !r.Ok || r.Invocation.Command.Kind != KindVersion {
		t.Fatalf("--version: %+v", r)
	}
}

func TestParseGlobalFlagsAcceptedBeforeAndAfterVerb(t *testing.T) {
	cases := [][]string{
		{"--cwd=/tmp", "list"},
		{"list", "--cwd=/tmp"},
		{"--cwd", "/tmp", "list"},
		{"list", "--cwd", "/tmp"},
	}
	for _, args := range cases {
		t.Run("args="+joinSpaces(args), func(t *testing.T) {
			r := ParseArgs(args, "/cwd")
			if !r.Ok {
				t.Fatalf("parse failed: %v", r.Err)
			}
			if r.Invocation.Command.Kind != KindList {
				t.Fatalf("want KindList, got %q", r.Invocation.Command.Kind)
			}
			if r.Invocation.Global.Cwd != "/tmp" {
				t.Fatalf("want cwd=/tmp, got %q", r.Invocation.Global.Cwd)
			}
		})
	}
}

func TestParseQuietJsonExclusive(t *testing.T) {
	r := ParseArgs([]string{"--json", "--quiet", "list"}, "/")
	if r.Ok {
		t.Fatalf("--json --quiet should be rejected; got Ok=%v", r.Ok)
	}
}

func TestParseUseRequiresProfile(t *testing.T) {
	r := ParseArgs([]string{"use"}, "/")
	if r.Ok {
		t.Fatalf("use without profile should be rejected")
	}
}

func TestParseOnDriftValues(t *testing.T) {
	cases := map[string]drift.GateChoice{
		"--on-drift=discard": drift.GateChoiceDiscard,
		"--on-drift=persist": drift.GateChoicePersist,
		"--on-drift=abort":   drift.GateChoiceAbort,
	}
	for arg, want := range cases {
		t.Run(arg, func(t *testing.T) {
			r := ParseArgs([]string{arg, "use", "x"}, "/")
			if !r.Ok || r.Invocation.Global.OnDrift != want {
				t.Fatalf("want %q, got Ok=%v drift=%q err=%v", want, r.Ok, r.Invocation.Global.OnDrift, r.Err)
			}
		})
	}
	bad := ParseArgs([]string{"--on-drift=nope", "use", "x"}, "/")
	if bad.Ok {
		t.Fatalf("--on-drift=nope should be rejected")
	}
}

func TestParseHookActions(t *testing.T) {
	r := ParseArgs([]string{"hook", "install"}, "/")
	if !r.Ok || r.Invocation.Command.Kind != KindHook || r.Invocation.Command.HookAction != HookInstall {
		t.Fatalf("hook install: %+v", r)
	}
	rfu := ParseArgs([]string{"hook", "uninstall", "--force"}, "/")
	if rfu.Ok {
		t.Fatalf("hook uninstall --force should be rejected (force is install-only)")
	}
	bad := ParseArgs([]string{"hook"}, "/")
	if bad.Ok {
		t.Fatalf("hook without action should be rejected")
	}
}

func TestParseDiffPositionals(t *testing.T) {
	r := ParseArgs([]string{"diff", "a", "b"}, "/")
	if !r.Ok || r.Invocation.Command.A != "a" || r.Invocation.Command.B != "b" {
		t.Fatalf("diff a b: %+v", r)
	}
	r1 := ParseArgs([]string{"diff", "a"}, "/")
	if !r1.Ok || r1.Invocation.Command.A != "a" || r1.Invocation.Command.B != "" {
		t.Fatalf("diff a: %+v", r1)
	}
	bad := ParseArgs([]string{"diff"}, "/")
	if bad.Ok {
		t.Fatalf("diff without args should be rejected")
	}
}

func TestParseCIEnvAutoDetect(t *testing.T) {
	t.Setenv("CI", "true")
	r := ParseArgs([]string{"list"}, "/")
	if !r.Ok || !r.Invocation.Global.NonInteractive {
		t.Fatalf("CI=true should set NonInteractive: %+v", r)
	}
}

func TestParseUnknownVerb(t *testing.T) {
	r := ParseArgs([]string{"nope"}, "/")
	if r.Ok || !contains(r.Err.Message, "unknown command") {
		t.Fatalf("expected 'unknown command' error: %+v", r)
	}
}

func TestParseHelpAfterVerb(t *testing.T) {
	r := ParseArgs([]string{"use", "--help"}, "/")
	if !r.Ok || r.Invocation.Command.Kind != KindHelp || r.Invocation.Command.HelpVerb != "use" {
		t.Fatalf("use --help should produce help{verb=use}: %+v", r)
	}
}

func TestParseWaitFlag(t *testing.T) {
	r1 := ParseArgs([]string{"--wait", "use", "x"}, "/")
	if !r1.Ok || r1.Invocation.Global.WaitMs != 30000 {
		t.Fatalf("--wait should set 30000ms: got %d", r1.Invocation.Global.WaitMs)
	}
	r2 := ParseArgs([]string{"--wait=5", "use", "x"}, "/")
	if !r2.Ok || r2.Invocation.Global.WaitMs != 5000 {
		t.Fatalf("--wait=5 should set 5000ms: got %d", r2.Invocation.Global.WaitMs)
	}
	bad := ParseArgs([]string{"--wait=-3", "use", "x"}, "/")
	if bad.Ok {
		t.Fatalf("--wait=-3 should be rejected")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func joinSpaces(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
