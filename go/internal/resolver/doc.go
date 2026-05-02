// Package resolver discovers profiles, parses their manifests, walks the
// extends/includes graph, and produces a ResolvedPlan. F1 lands an empty
// scaffold; D1 (epic claude-code-profiles-93e) fills in the implementation
// against R1–R7, R35–R37, PR16, PR16a.
package resolver
