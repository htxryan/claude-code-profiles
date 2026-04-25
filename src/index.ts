/**
 * Top-level package entry. Surface so far: resolver (E1) + merge (E2) +
 * state/materialize (E3). Future epics (E4 drift, E5 CLI) will extend this
 * surface.
 */

export * from "./resolver/index.js";
export * from "./merge/index.js";
export * from "./state/index.js";
