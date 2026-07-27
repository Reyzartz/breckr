/**
 * The HTTP contract between the server and the dashboard.
 *
 * Types only. This package is symlinked into node_modules by npm workspaces,
 * and Node will not strip types from there — so a runtime export here would
 * fail at boot. Runtime constants belong in each package's own `constants/`,
 * typed against these declarations.
 */

export * from "./types.js";
