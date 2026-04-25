/**
 * Top-level package entry. Surface so far: resolver (E1) + merge (E2) +
 * state/materialize (E3) + drift (E4). E5 (CLI) and E6 (init/hook) extend
 * this surface.
 */

export * from "./resolver/index.js";
export * from "./merge/index.js";
export * from "./state/index.js";
export * from "./drift/index.js";
