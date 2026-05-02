// Package merge transforms a ResolvedPlan into a slice of MergedFiles
// using the per-path strategy registry (R8 deep-merge, R9 concat, R10
// last-wins, R12 hooks-by-event precedence). The package is FS-IO-free:
// callers supply a ReadFunc that returns each contributor file's bytes,
// keeping merge a pure transformation that can be exercised without disk.
//
// Conflict detection (R11) lives in the resolver — by the time merge runs
// on a ResolvedPlan, only mergeable conflicts (deep-merge, concat) and
// last-wins ancestor-only chains are present.
package merge
