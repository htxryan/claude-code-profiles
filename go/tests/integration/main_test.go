package integration_test

import (
	"os"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// TestMain owns the shared c3p binary built by helpers.BinPath. The
// binary lives in os.TempDir() and is shared across every test in this
// package; t.Cleanup can't free it without invalidating the cache, so
// the suite-level TestMain removes it after m.Run() returns.
func TestMain(m *testing.M) {
	code := m.Run()
	helpers.CleanupBuiltBin()
	os.Exit(code)
}
