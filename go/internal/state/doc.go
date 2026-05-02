// Package state provides the on-disk state primitives (atomic-rename,
// advisory file lock, fingerprint) and the materialize/reconcile/persist
// orchestration. F1 lands an empty scaffold; D4 (claude-code-profiles-chp)
// fills in the primitives, D5 the orchestration.
package state
