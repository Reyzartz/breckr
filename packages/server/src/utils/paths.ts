import fs from "node:fs";
import path from "node:path";

/**
 * Walk up from `startDir` to the workspace root.
 *
 * The obvious version of this is `path.resolve(import.meta.dirname, "../../..")`,
 * which silently breaks the moment a file moves between `src/` and `dist/` —
 * the two layouts sit at different depths. Searching for the marker instead is
 * depth-independent, so the same code is correct when running stripped from
 * `src/` in development and compiled from `dist/` in production.
 */
export function findRepoRoot(startDir: string): string {
  let dir = startDir;

  for (;;) {
    const manifest = path.join(dir, "package.json");
    if (fs.existsSync(manifest)) {
      try {
        const parsed: unknown = JSON.parse(fs.readFileSync(manifest, "utf8"));
        if (
          typeof parsed === "object" &&
          parsed !== null &&
          "workspaces" in parsed
        ) {
          return dir;
        }
      } catch {
        // A malformed package.json higher up the tree should not stop the walk.
      }
    }

    const parent = path.dirname(dir);
    if (parent === dir) {
      throw new Error(
        `Could not locate the workspace root above ${startDir} (no package.json declaring "workspaces").`
      );
    }
    dir = parent;
  }
}
