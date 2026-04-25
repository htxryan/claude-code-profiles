/**
 * Top-level package entry. Surface so far: resolver (E1) + merge (E2) +
 * state/materialize (E3) + drift (E4) + cli (E5). E6 (init/hook) extends
 * this surface.
 *
 * Note on naming collisions: E1 and E5 both export an `OnDriftFlag`-shaped
 * type via different paths — the resolver doesn't own one, so this is fine.
 * If a future epic adds a name clash, prefer importing from the sub-path
 * (e.g. `claude-code-profiles/cli`).
 */

export * from "./resolver/index.js";
export * from "./merge/index.js";
export * from "./state/index.js";
export * from "./drift/index.js";
export * as cli from "./cli/index.js";
