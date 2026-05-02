// Package drift detects and applies drift between the materialized .claude/
// state and the resolved sources, with the gate state machine
// (discard/persist/abort/no-drift-proceed). F1 lands an empty scaffold; D6
// fills it in against R18–R26, R46.
package drift
