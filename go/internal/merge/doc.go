// Package merge transforms a ResolvedPlan into a stream of MergedFiles
// using the per-path strategy registry (R8 deep-merge, R9 concat, R10
// last-wins, R11 conflict, R12 hooks-by-event precedence). F1 lands an
// empty scaffold; D2 (epic claude-code-profiles-rig) fills it in.
package merge
