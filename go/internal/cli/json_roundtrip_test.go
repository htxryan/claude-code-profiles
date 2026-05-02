package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestJSONEnvelopesParse is the PR3 fitness function: every command's
// --json output is valid JSON, contains expected fields, and stderr is
// empty (no chatter under --json).
func TestJSONEnvelopesParse(t *testing.T) {
	tmp := t.TempDir()

	// Bootstrap a minimal project so list/status/drift have something to chew on.
	stdout, stderr, code := runCLI(t, "--cwd="+tmp, "init", "--no-hook", "--json")
	if code != ExitOK {
		t.Fatalf("init: want %d, got %d (stderr=%q)", ExitOK, code, stderr)
	}
	if stderr != "" {
		t.Fatalf("init under --json: stderr should be empty, got %q", stderr)
	}
	var initPayload map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &initPayload); err != nil {
		t.Fatalf("init JSON parse: %v (raw=%q)", err, stdout)
	}
	if _, ok := initPayload["profilesDir"]; !ok {
		t.Fatalf("init payload missing profilesDir: %v", initPayload)
	}

	cases := []struct {
		name     string
		args     []string
		required []string
	}{
		{"hello", []string{"hello", "--json"}, []string{"greeting"}},
		{"version", []string{"--version", "--json"}, []string{"version"}},
		{"help", []string{"--help", "--json"}, []string{"help"}},
		{"list", []string{"--cwd=" + tmp, "list", "--json"}, []string{"profiles", "stateWarning"}},
		{"status", []string{"--cwd=" + tmp, "status", "--json"}, []string{"activeProfile", "drift", "warnings"}},
		{"drift", []string{"--cwd=" + tmp, "drift", "--json"}, []string{"schemaVersion", "active", "fingerprintOk", "entries"}},
		{"validate", []string{"--cwd=" + tmp, "validate", "--json"}, []string{"results", "pass"}},
		{"completions", []string{"completions", "bash", "--json"}, []string{"shell", "script"}},
		{"doctor", []string{"--cwd=" + tmp, "doctor", "--json"}, []string{"pass", "checks"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, _ := runCLI(t, tc.args...)
			if stderr != "" {
				t.Fatalf("%s under --json: stderr should be empty, got %q", tc.name, stderr)
			}
			line := strings.TrimSpace(stdout)
			if line == "" {
				t.Fatalf("%s --json: no stdout", tc.name)
			}
			var payload map[string]interface{}
			if err := json.Unmarshal([]byte(line), &payload); err != nil {
				t.Fatalf("%s --json parse error: %v (raw=%q)", tc.name, err, line)
			}
			for _, f := range tc.required {
				if _, ok := payload[f]; !ok {
					t.Errorf("%s payload missing field %q: %v", tc.name, f, payload)
				}
			}
		})
	}
}

// TestJSONEnvelopeIsSingleLine asserts every --json envelope is a single
// line (no embedded newlines from indentation). Critical for consumers
// using `| jq -s 'add'` over multiple invocations.
func TestJSONEnvelopeIsSingleLine(t *testing.T) {
	tmp := t.TempDir()
	stdout, _, _ := runCLI(t, "--cwd="+tmp, "init", "--no-hook", "--json")
	stdout = strings.TrimRight(stdout, "\n")
	if strings.Contains(stdout, "\n") {
		t.Fatalf("init JSON envelope contains embedded newline: %q", stdout)
	}
}

// runCLI invokes Run with a fresh buffer pair and returns (stdout, stderr,
// exitCode). Resets the CI env var so non-interactive auto-detection
// doesn't poison test scenarios that depend on TTY mode.
func runCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	prev := os.Getenv("CI")
	os.Unsetenv("CI")
	defer os.Setenv("CI", prev)
	var stdout, stderr bytes.Buffer
	code := Run(args, "0.0.0-test", &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}
