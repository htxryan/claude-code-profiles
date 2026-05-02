package resolver_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/htxryan/c3p/internal/resolver"
)

func TestListProfiles_EmptyWhenNoProfilesDir(t *testing.T) {
	tmp := t.TempDir()
	got, err := resolver.ListProfiles(resolver.DiscoverOptions{ProjectRoot: tmp})
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestListProfiles_SkipsHiddenAndUnderscore(t *testing.T) {
	tmp := t.TempDir()
	mk := func(name string) {
		if err := os.MkdirAll(filepath.Join(tmp, ".claude-profiles", name), 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", name, err)
		}
	}
	mk("alpha")
	mk("beta")
	mk("_components")
	mk(".hidden")

	got, err := resolver.ListProfiles(resolver.DiscoverOptions{ProjectRoot: tmp})
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	want := []string{"alpha", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestListProfiles_SortsLex(t *testing.T) {
	tmp := t.TempDir()
	for _, n := range []string{"zeta", "alpha", "mu"} {
		if err := os.MkdirAll(filepath.Join(tmp, ".claude-profiles", n), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	got, err := resolver.ListProfiles(resolver.DiscoverOptions{ProjectRoot: tmp})
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	want := []string{"alpha", "mu", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestProfileExists(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".claude-profiles", "p"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !resolver.ProfileExists("p", tmp) {
		t.Fatalf("p should exist")
	}
	if resolver.ProfileExists("ghost", tmp) {
		t.Fatalf("ghost should not exist")
	}
}
