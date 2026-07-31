/**
 * Query key roots, one per resource.
 *
 * A hook that needs a narrower key (a run's filters, one run's id) spreads the
 * root and appends: `[...QueryKeys.runs, filters]`. That keeps every key for a
 * resource nested under the same root, so `invalidateQueries({ queryKey:
 * QueryKeys.runs })` reaches every filtered variant at once.
 */
export const QueryKeys = {
  tasks: ["tasks"] as const,
  runs: ["runs"] as const,
  health: ["health"] as const,
  channels: ["channels"] as const,
  auth: ["auth"] as const,
} as const;
