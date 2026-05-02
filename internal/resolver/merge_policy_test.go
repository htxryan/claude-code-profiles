package resolver_test

import (
	"testing"

	"github.com/htxryan/claude-code-config-profiles/internal/resolver"
)

func TestPolicyFor(t *testing.T) {
	cases := []struct {
		path string
		want resolver.MergePolicy
	}{
		{"settings.json", resolver.MergePolicyDeepMerge},
		{"agents/settings.json", resolver.MergePolicyDeepMerge},
		{"CLAUDE.md", resolver.MergePolicyConcat},
		{"docs/x.md", resolver.MergePolicyConcat},
		{"docs/X.MD", resolver.MergePolicyConcat},
		{"agents/foo.json", resolver.MergePolicyLastWins},
		{"hooks/precommit.sh", resolver.MergePolicyLastWins},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := resolver.PolicyFor(tc.path); got != tc.want {
				t.Fatalf("PolicyFor(%q): want %q, got %q", tc.path, tc.want, got)
			}
		})
	}
}

func TestIsMergeable(t *testing.T) {
	if !resolver.IsMergeable("settings.json") {
		t.Fatalf("settings.json must be mergeable")
	}
	if !resolver.IsMergeable("CLAUDE.md") {
		t.Fatalf("CLAUDE.md must be mergeable")
	}
	if resolver.IsMergeable("agents/foo.json") {
		t.Fatalf("agents/foo.json must NOT be mergeable")
	}
}
