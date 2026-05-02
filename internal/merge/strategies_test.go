package merge

import (
	"reflect"
	"testing"

	"github.com/htxryan/c3p/internal/resolver"
)

func TestGetStrategy_DeepMerge(t *testing.T) {
	got, err := GetStrategy(resolver.MergePolicyDeepMerge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(MergeStrategy(DeepMergeStrategy)).Pointer() {
		t.Fatalf("expected DeepMergeStrategy")
	}
}

func TestGetStrategy_Concat(t *testing.T) {
	got, err := GetStrategy(resolver.MergePolicyConcat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(MergeStrategy(ConcatStrategy)).Pointer() {
		t.Fatalf("expected ConcatStrategy")
	}
}

func TestGetStrategy_LastWins(t *testing.T) {
	got, err := GetStrategy(resolver.MergePolicyLastWins)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(MergeStrategy(LastWinsStrategy)).Pointer() {
		t.Fatalf("expected LastWinsStrategy")
	}
}

func TestGetStrategy_UnknownPolicy(t *testing.T) {
	if _, err := GetStrategy(resolver.MergePolicy("unknown")); err == nil {
		t.Fatal("want error for unknown policy, got nil")
	}
}
