package integration_test

import (
	"os"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/tests/integration/helpers"
)

// TestMain owns the shared c3p binary built by helpers.BinPath plus the
// stripped binary cached by perf_test.go::buildStrippedBin. Both live in
// os.TempDir() and are shared across every test in this package;
// t.Cleanup can't free them without invalidating the cache, so the
// suite-level TestMain removes them after m.Run() returns.
func TestMain(m *testing.M) {
	code := m.Run()
	helpers.CleanupBuiltBin()
	cleanupStrippedBin()
	os.Exit(code)
}
