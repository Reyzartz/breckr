import test from "node:test";
import assert from "node:assert/strict";
import type { TaskSpec } from "@breckr/shared";
import { db } from "./database.ts";
import * as taskRepo from "./tasks.repository.ts";
import * as runRepo from "./runs.repository.ts";

/**
 * Tasks are user data now, so the storage layer owes the same guarantees the
 * run history already had: a spec survives a round trip intact, a corrupt one
 * degrades instead of throwing, and deleting a task takes its history with it.
 */

const spec: TaskSpec = {
  url: "https://example.com/prices",
  selector: ".price",
  extract: "number",
  operator: "lt",
  value: "100",
  message: "Now {{value}}",
};

let counter = 0;
const nextId = (): string => `_task_${String(++counter)}`;

test("round-trips a spec through JSON", () => {
  const id = nextId();
  taskRepo.createTask({ id, name: "Prices", cron_expr: "*/15 * * * *", spec });

  assert.deepEqual(taskRepo.getTask(id)?.spec, spec);
});

test("a new task is enabled unless told otherwise", () => {
  const on = nextId();
  const off = nextId();

  taskRepo.createTask({ id: on, name: "On", cron_expr: "*/15 * * * *", spec });
  taskRepo.createTask({
    id: off,
    name: "Off",
    cron_expr: "*/15 * * * *",
    spec,
    enabled: false,
  });

  assert.equal(taskRepo.getTask(on)?.enabled, true);
  assert.equal(taskRepo.getTask(off)?.enabled, false);
});

test("a duplicate id is rejected by the primary key", () => {
  const id = nextId();
  taskRepo.createTask({ id, name: "First", cron_expr: "*/15 * * * *", spec });

  assert.throws(
    () => taskRepo.createTask({ id, name: "Second", cron_expr: "*/15 * * * *", spec }),
    /UNIQUE/
  );
});

test("updating patches only the fields present", () => {
  const id = nextId();
  taskRepo.createTask({ id, name: "Prices", cron_expr: "*/15 * * * *", spec });

  const updated = taskRepo.updateTask(id, { name: "Renamed" });

  assert.equal(updated?.name, "Renamed");
  assert.equal(updated?.cron_expr, "*/15 * * * *", "untouched fields survive");
  assert.deepEqual(updated?.spec, spec);
});

test("editing the spec re-arms the edge-trigger", () => {
  // The stored condition_met describes the *old* condition. Carrying it over
  // would let a stale "already alerted" flag swallow the first alert of the
  // new one — the exact silent failure this app exists to avoid.
  const id = nextId();
  taskRepo.createTask({ id, name: "Prices", cron_expr: "*/15 * * * *", spec });
  taskRepo.markTaskNotified(id);

  assert.equal(taskRepo.getTask(id)?.condition_met, true, "armed");

  taskRepo.updateTask(id, { spec: { ...spec, value: "50" } });

  assert.equal(taskRepo.getTask(id)?.condition_met, false, "re-armed by the edit");
});

test("renaming a task leaves the edge-trigger alone", () => {
  const id = nextId();
  taskRepo.createTask({ id, name: "Prices", cron_expr: "*/15 * * * *", spec });
  taskRepo.markTaskNotified(id);

  taskRepo.updateTask(id, { name: "Renamed" });

  assert.equal(
    taskRepo.getTask(id)?.condition_met,
    true,
    "a cosmetic change must not re-alert"
  );
});

test("deleting a task takes its run history with it", () => {
  const id = nextId();
  taskRepo.createTask({ id, name: "Prices", cron_expr: "*/15 * * * *", spec });
  const runId = runRepo.startRun({ taskId: id, triggerSource: "manual" });

  assert.ok(runRepo.getRun(runId), "run exists before the delete");

  assert.equal(taskRepo.deleteTask(id), true);

  assert.equal(taskRepo.getTask(id), null);
  assert.equal(runRepo.getRun(runId), null, "cascaded through the foreign key");
});

test("deleting an unknown task reports that nothing happened", () => {
  assert.equal(taskRepo.deleteTask("never-existed"), false);
});

test("a legacy row with no spec reads back as null instead of throwing", () => {
  // Written by the old file-based registry, before specs existed. It keeps its
  // history and surfaces as orphaned rather than breaking GET /api/tasks.
  const id = nextId();
  db.prepare(
    `INSERT INTO tasks (id, name, cron_expr, enabled) VALUES (?, ?, '*/15 * * * *', 1)`
  ).run(id, "Legacy");

  assert.equal(taskRepo.getTask(id)?.spec, null);
});

test("a corrupt spec degrades to null rather than throwing", () => {
  const id = nextId();
  taskRepo.createTask({ id, name: "Prices", cron_expr: "*/15 * * * *", spec });
  db.prepare(`UPDATE tasks SET spec = '{not json' WHERE id = ?`).run(id);

  assert.equal(taskRepo.getTask(id)?.spec, null);
  assert.doesNotThrow(() => taskRepo.listTasks());
});

test("getLastSuccessfulResult ignores failed runs", () => {
  // A failed run says nothing about what the page holds, so `changed` must not
  // compare against one — it would report a change that never happened.
  const id = nextId();
  taskRepo.createTask({ id, name: "Prices", cron_expr: "*/15 * * * *", spec });

  const good = runRepo.startRun({ taskId: id, triggerSource: "cron" });
  runRepo.completeRun({ id: good, status: "success", result: { value: 42 } });

  const bad = runRepo.startRun({ taskId: id, triggerSource: "cron" });
  runRepo.completeRun({ id: bad, status: "failed", error: "boom" });

  assert.deepEqual(runRepo.getLastSuccessfulResult(id), { value: 42 });
});

test("getLastSuccessfulResult is null before the first success", () => {
  const id = nextId();
  taskRepo.createTask({ id, name: "Prices", cron_expr: "*/15 * * * *", spec });

  assert.equal(runRepo.getLastSuccessfulResult(id), null);
});
