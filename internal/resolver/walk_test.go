package resolver_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/internal/resolver"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestWalkClaudeDir_LexSorted(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, tmp, "z.txt", "z")
	writeFile(t, tmp, "a.txt", "a")
	writeFile(t, tmp, "m/b.txt", "b")
	writeFile(t, tmp, "m/a.txt", "a")

	entries, err := resolver.WalkClaudeDir(tmp)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	got := make([]string, len(entries))
	for i, e := range entries {
		got[i] = e.RelPath
	}
	want := []string{"a.txt", "m/a.txt", "m/b.txt", "z.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestWalkClaudeDir_ReturnsEmptyForMissingDir(t *testing.T) {
	entries, err := resolver.WalkClaudeDir(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want empty, got %v", entries)
	}
}

func TestWalkClaudeDir_ReturnsEmptyForFileTarget(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := resolver.WalkClaudeDir(f)
	if err != nil {
		t.Fatalf("walk on file: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want empty, got %v", entries)
	}
}

func TestWalkProfileRoot_CLAUDEMd(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "CLAUDE.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := resolver.WalkProfileRoot(tmp)
	if err != nil {
		t.Fatalf("walkProfileRoot: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want one entry, got %v", entries)
	}
	if entries[0].RelPath != "CLAUDE.md" {
		t.Fatalf("want CLAUDE.md, got %q", entries[0].RelPath)
	}
}

func TestWalkProfileRoot_NoCLAUDEMd(t *testing.T) {
	tmp := t.TempDir()
	entries, err := resolver.WalkProfileRoot(tmp)
	if err != nil {
		t.Fatalf("walkProfileRoot: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want empty, got %v", entries)
	}
}

func TestIsDirectory(t *testing.T) {
	tmp := t.TempDir()
	if !resolver.IsDirectory(tmp) {
		t.Fatalf("tmp should be a directory")
	}
	f := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if resolver.IsDirectory(f) {
		t.Fatalf("file should not be reported as dir")
	}
	if resolver.IsDirectory(filepath.Join(tmp, "absent")) {
		t.Fatalf("missing path should not be reported as dir")
	}
}
