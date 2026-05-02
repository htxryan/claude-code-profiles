// Package resolver discovers profiles, parses their manifests, walks the
// extends/includes graph, and produces a ResolvedPlan. ResolvedPlan is the
// load-bearing cross-epic interface — D2 (merge), D5 (state orchestration),
// D6 (drift), and D7 (CLI) consume this exact shape. Per the D1 fitness
// function (epic claude-code-profiles-93e) the schema is locked at
// schemaVersion=1 for >= 2 weeks once shipped.
//
// Public surface:
//
//   - Resolve(profileName, ResolveOptions) (*ResolvedPlan, error)
//   - ListProfiles(DiscoverOptions) ([]string, error)
//   - ProfileExists(name, projectRoot) bool
//   - PolicyFor / IsMergeable
//   - BuildPaths / IsValidProfileName / IsWindowsReservedName / ClassifyInclude
//
// Errors returned mirror the TS resolver: *MissingProfileError,
// *CycleError, *MissingIncludeError, *ConflictError,
// *InvalidManifestError, *PathTraversalError. PR16a rejects relative
// includes that escape projectRoot before any filesystem touch; PR16
// rejects Windows DOS-device names at every profile-name boundary.
package resolver
