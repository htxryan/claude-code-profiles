package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// pinnedBatSHA256 is the SHA-256 of HookScriptContentBat as of first ship.
// W1 fitness function: any change to the .bat bytes after first ship is a
// deliberate spec bump and MUST be accompanied by an update to this hash
// AND a note in the spec changelog. Drift here means a user who installed
// the hook on an older c3p will see a `--force`-required mismatch on
// reinstall; that's user-visible behavior, not implementation detail.
//
// To rotate (intentional bump): regenerate via
//
//	echo -n "$HookScriptContentBat" | shasum -a 256
//
// then update both this constant and the spec.
const pinnedBatSHA256 = "821e3e55fbb6c6ed1bf9b62e8ea9268e22f54d46aa59cfec0d154aa35dd4beff"

// TestPreCommitBat_BytesPinned guards the .bat companion against silent
// drift. The bat bytes are part of the cross-platform user contract (PR15)
// and MUST be byte-stable across releases.
func TestPreCommitBat_BytesPinned(t *testing.T) {
	sum := sha256.Sum256([]byte(HookScriptContentBat))
	got := hex.EncodeToString(sum[:])
	if got != pinnedBatSHA256 {
		t.Fatalf("HookScriptContentBat bytes have changed.\n got: %s\nwant: %s\nbytes:\n%q\n\nIf this change is intentional, update pinnedBatSHA256 in this file and note the bump in the spec.",
			got, pinnedBatSHA256, HookScriptContentBat)
	}
}

// TestPreCommitBat_ShapeInvariants documents the load-bearing surface of the
// .bat companion in case the byte hash is rotated. These are the contracts
// the .bat must satisfy for git/cmd interoperability and the S18 fail-open
// guarantee — independent of whitespace/comment shape changes.
func TestPreCommitBat_ShapeInvariants(t *testing.T) {
	body := HookScriptContentBat
	mustContain := []struct {
		needle string
		why    string
	}{
		{"@echo off", "suppress command echoing in git output"},
		{"where c3p", "S18: probe for c3p binary on PATH"},
		{"if errorlevel 1 exit /b 0", "S18: exit 0 when probe fails"},
		{"c3p drift --pre-commit-warn", "R25: invoke drift warner"},
		{"exit /b 0", "fail-open: never return non-zero"},
	}
	for _, m := range mustContain {
		if !strings.Contains(body, m.needle) {
			t.Errorf("HookScriptContentBat missing %q (%s)", m.needle, m.why)
		}
	}

	// CRLF line endings are required: cmd.exe parses .bat files line-by-line
	// and lone-LF endings have caused historical parser misbehaviour in
	// Git for Windows installs.
	if !strings.Contains(body, "\r\n") {
		t.Errorf("HookScriptContentBat must use CRLF line endings (cmd.exe parser stability)")
	}
	if strings.Contains(strings.ReplaceAll(body, "\r\n", ""), "\n") {
		t.Errorf("HookScriptContentBat contains lone LF — every newline must be CRLF")
	}
}
