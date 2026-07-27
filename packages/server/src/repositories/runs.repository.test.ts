import test from "node:test";
import assert from "node:assert/strict";
import { db } from "./database.ts";
import * as runRepo from "./runs.repository.ts";
import * as taskRepo from "./tasks.repository.ts";

/**
 * Storage-layer guarantees the dashboard depends on: crashed runs get resolved,
 * old history is pruned, filters compose, and SQLite's 0/1 never leaks out.
 */

let counter = 0;
function seedTask(): string {
  const id = `_repo_${String(++counter)}`;
  taskRepo.createTask({
    id,
    name: `Repo ${id}`,
    cron_expr: "*/1 * * * *",
    spec: {
      url: "https://example.com",
      selector: "#value",
      extract: "text",
      operator: "changed",
    },
  });
  return id;
}

test("boot sweep resolves runs interrupted by a crash", () => {
  const taskId = seedTask();
  const runId = runRepo.startRun({ taskId, triggerSource: "cron" });

  assert.equal(runRepo.getRun(runId)?.status, "running", "written before execution");

  const swept = runRepo.sweepInterruptedRuns();
  assert.ok(swept >= 1);

  const row = runRepo.getRun(runId);
  assert.equal(row?.status, "failed", "a dangling run must not stay 'running' forever");
  assert.match(row?.error ?? "", /Interrupted/);
  assert.notEqual(row?.finished_at, null);
});

test("retention prunes only runs older than the cutoff", () => {
  const taskId = seedTask();
  const fresh = runRepo.startRun({ taskId, triggerSource: "cron" });
  runRepo.completeRun({ id: fresh, status: "success", result: { ok: true } });

  const old = new Date(Date.now() - 60 * 24 * 3600 * 1000).toISOString();
  db.prepare(
    `INSERT INTO runs (task_id, started_at, finished_at, status, trigger_source)
     VALUES (?, ?, ?, 'success', 'cron')`
  ).run(taskId, old, old);

  const pruned = runRepo.pruneOldRuns(30);
  assert.ok(pruned >= 1, "the ancient row is removed");
  assert.notEqual(runRepo.getRun(fresh), null, "the recent row survives");
});

test("booleans come back as booleans, not 0/1", () => {
  const taskId = seedTask();
  const runId = runRepo.startRun({ taskId, triggerSource: "manual" });
  runRepo.completeRun({
    id: runId,
    status: "success",
    conditionMet: true,
    notified: false,
    result: { price: 42 },
  });

  const row = runRepo.getRun(runId);
  assert.equal(row?.condition_met, true);
  assert.equal(row?.notified, false);
  assert.equal(typeof row?.notified, "boolean", "false must be a boolean, not 0");
});

test("an unserializable result is stored as a diagnostic, not lost", () => {
  const taskId = seedTask();
  const runId = runRepo.startRun({ taskId, triggerSource: "cron" });

  const circular: Record<string, unknown> = {};
  circular["self"] = circular;
  runRepo.completeRun({ id: runId, status: "success", result: circular });

  assert.match(runRepo.getRun(runId)?.result_summary ?? "", /_unserializable/);
});

test("filters compose and total reflects them", () => {
  const taskId = seedTask();
  const ok = runRepo.startRun({ taskId, triggerSource: "cron" });
  runRepo.completeRun({ id: ok, status: "success", result: {} });
  const bad = runRepo.startRun({ taskId, triggerSource: "cron" });
  runRepo.completeRun({ id: bad, status: "failed", error: "nope" });

  const failed = runRepo.listRuns({ taskId, status: "failed" });
  assert.equal(failed.total, 1);
  assert.equal(failed.runs[0]?.id, bad);

  const all = runRepo.listRuns({ taskId });
  assert.equal(all.total, 2);
});

test("pagination returns disjoint pages, newest first", () => {
  const taskId = seedTask();
  const ids: number[] = [];
  for (let i = 0; i < 5; i += 1) {
    const id = runRepo.startRun({ taskId, triggerSource: "cron" });
    runRepo.completeRun({ id, status: "success", result: { i } });
    ids.push(id);
  }

  const first = runRepo.listRuns({ taskId, limit: 2, offset: 0 });
  const second = runRepo.listRuns({ taskId, limit: 2, offset: 2 });

  assert.equal(first.total, 5, "total counts all matches, not just this page");
  assert.equal(first.runs.length, 2);
  assert.equal(first.runs[0]?.id, ids[4], "newest first");
  assert.equal(
    first.runs.filter((r) => second.runs.some((s) => s.id === r.id)).length,
    0,
    "pages must not overlap"
  );
});

test("latest-run map reports the most recent run per task", () => {
  const taskId = seedTask();
  const older = runRepo.startRun({ taskId, triggerSource: "cron" });
  runRepo.completeRun({ id: older, status: "success", result: {} });
  const newer = runRepo.startRun({ taskId, triggerSource: "manual" });
  runRepo.completeRun({ id: newer, status: "failed", error: "x" });

  assert.equal(runRepo.getLatestRunByTask().get(taskId)?.id, newer);
});
