package resolver_test

import (
	stderrors "errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
	"github.com/htxryan/c3p/internal/resolver"
)

func writeManifest(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profile.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLoadManifest_MissingProducesWarning(t *testing.T) {
	tmp := t.TempDir()
	res, err := resolver.LoadManifest(tmp, "p")
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Code != resolver.WarningMissingManifest {
		t.Fatalf("want one MissingManifest warning, got %v", res.Warnings)
	}
}

func TestLoadManifest_ParsesAllFields(t *testing.T) {
	tmp := t.TempDir()
	writeManifest(t, tmp, `{
		"name": "p",
		"description": "desc",
		"extends": "base",
		"includes": ["a", "b"],
		"tags": ["t1", "t2"]
	}`)
	res, err := resolver.LoadManifest(tmp, "p")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := resolver.ProfileManifest{
		Name: "p", Description: "desc", Extends: "base",
		Includes: []string{"a", "b"}, Tags: []string{"t1", "t2"},
	}
	if !reflect.DeepEqual(res.Manifest, want) {
		t.Fatalf("want %+v, got %+v", want, res.Manifest)
	}
}

func TestLoadManifest_UnknownFieldWarns(t *testing.T) {
	tmp := t.TempDir()
	writeManifest(t, tmp, `{"weird": 1}`)
	res, err := resolver.LoadManifest(tmp, "p")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	found := 0
	for _, w := range res.Warnings {
		if w.Code == resolver.WarningUnknownManifestField {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("want 1 UnknownManifestField warning, got %d (%v)", found, res.Warnings)
	}
}

func TestLoadManifest_RejectsInvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	writeManifest(t, tmp, `{not valid json`)
	_, err := resolver.LoadManifest(tmp, "p")
	var ime *pipelineerrors.InvalidManifestError
	if !stderrors.As(err, &ime) {
		t.Fatalf("want InvalidManifestError, got %v", err)
	}
}

func TestLoadManifest_RejectsNonObjectRoot(t *testing.T) {
	tmp := t.TempDir()
	writeManifest(t, tmp, `[1, 2, 3]`)
	_, err := resolver.LoadManifest(tmp, "p")
	var ime *pipelineerrors.InvalidManifestError
	if !stderrors.As(err, &ime) {
		t.Fatalf("want InvalidManifestError, got %v", err)
	}
}

func TestLoadManifest_RejectsTypeMismatch(t *testing.T) {
	tmp := t.TempDir()
	writeManifest(t, tmp, `{"extends": 42}`)
	_, err := resolver.LoadManifest(tmp, "p")
	var ime *pipelineerrors.InvalidManifestError
	if !stderrors.As(err, &ime) {
		t.Fatalf("want InvalidManifestError, got %v", err)
	}
}

func TestLoadManifest_RejectsIncludesNonStringElement(t *testing.T) {
	tmp := t.TempDir()
	writeManifest(t, tmp, `{"includes": ["ok", 42]}`)
	_, err := resolver.LoadManifest(tmp, "p")
	var ime *pipelineerrors.InvalidManifestError
	if !stderrors.As(err, &ime) {
		t.Fatalf("want InvalidManifestError, got %v", err)
	}
}
