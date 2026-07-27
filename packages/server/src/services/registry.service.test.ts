import test from "node:test";
import assert from "node:assert/strict";
import type { Task, TaskSpec } from "@breckr/shared";
import * as registry from "./registry.service.ts";
import * as taskRepo from "../repositories/tasks.repository.ts";
import type { Logger } from "../types/index.ts";

/**
 * The live cron registry.
 *
 * Tasks are authored from the dashboard now, so the registry has to be mutable
 * at run time: a task saved at 10:05 must start firing without a restart, an
 * edited schedule must take effect immediately, and a deleted task must stop.
 * Getting any of those wrong produces a monitor that looks armed and isn't.
 */

const quiet: Logger = { info() {}, warn() {}, error() {} };

const spec: TaskSpec = {
  url: "https://example.com",
  selector: ".price",
  extract: "number",
  operator: "lt",
  value: "100",
};

let counter = 0;

function seed(overrides: { cron_expr?: string; enabled?: boolean } = {}): Task {
  const id = `_reg_${String(++counter)}`;
  return taskRepo.createTask({
    id,
    name: `Registry ${id}`,
    cron_expr: "*/15 * * * *",
    spec,
    ...overrides,
  });
}

// Every test needs the trigger handler installed; scheduleAll also arms
// whatever earlier tests left in the database, which is harmless here.
test.before(() => {
  registry.scheduleAll(() => {}, quiet);
});

test.after(() => {
  registry.destroyAll();
});

test("registers a task and schedules it immediately", () => {
  const task = seed();

  assert.equal(registry.register(task, quiet), true);
  assert.ok(registry.getDefinition(task.id), "compiled definition is available");
  assert.ok(registry.getNextRun(task.id), "an enabled task reports a next fire");
});

test("a disabled task is armed stopped", () => {
  // cron.schedule() arms on construction, so a task stored as disabled has to
  // be stopped right back down or it would fire despite the dashboard toggle.
  const task = seed({ enabled: false });

  registry.register(task, quiet);

  assert.equal(registry.getNextRun(task.id), null, "not scheduled while disabled");
  assert.ok(registry.getDefinition(task.id), "but still known, so it can be run now");
});

test("compiles the stored spec into the definition the runner consumes", () => {
  const task = seed();
  registry.register(task, quiet);

  const definition = registry.getDefinition(task.id);

  assert.equal(definition?.cron, task.cron_expr);
  assert.equal(definition?.name, task.name);
  assert.equal(definition?.needsBrowser, true, "a spec always reads a page");
  assert.equal(typeof definition?.condition, "function");
  assert.equal(typeof definition?.notify, "function");
});

test("refuses a row with no usable spec instead of throwing", () => {
  // A legacy row, or one whose JSON no longer parses. It keeps its history and
  // shows up as orphaned; it must not take down the boot or the request.
  const task = { ...seed(), spec: null };

  assert.equal(registry.register(task, quiet), false);
  assert.equal(registry.getDefinition(task.id), null);
});

test("rescheduling swaps the cron expression on a live task", () => {
  // node-cron cannot change an expression in place, so this is the one path
  // that proves an edited schedule actually takes effect without a restart.
  const task = seed({ cron_expr: "0 0 1 1 *" });
  registry.register(task, quiet);

  const before = registry.getNextRun(task.id);

  const updated = taskRepo.updateTask(task.id, { cron_expr: "*/1 * * * *" });
  assert.ok(updated);
  registry.reschedule(updated, quiet);

  const after = registry.getNextRun(task.id);

  assert.ok(after);
  assert.notEqual(after, before, "the next fire moved with the new expression");
  assert.ok(
    new Date(after).getTime() - Date.now() < 61_000,
    "the new every-minute schedule fires within the minute"
  );
});

test("registering the same id twice leaves exactly one schedule", () => {
  const task = seed();

  registry.register(task, quiet);
  registry.register(task, quiet);

  const matches = registry.listIds().filter((id) => id === task.id);
  assert.equal(matches.length, 1, "the previous handle must be destroyed, not orphaned");
});

test("unregistering stops the task and forgets it", () => {
  const task = seed();
  registry.register(task, quiet);

  registry.unregister(task.id);

  assert.equal(registry.getDefinition(task.id), null);
  assert.equal(registry.getNextRun(task.id), null);
  assert.ok(!registry.listIds().includes(task.id));
});

test("unregistering an unknown id is a no-op", () => {
  assert.doesNotThrow(() => {
    registry.unregister("never-existed");
  });
});

test("setEnabled toggles the schedule and persists the choice", () => {
  const task = seed();
  registry.register(task, quiet);

  assert.equal(registry.setEnabled(task.id, false), true);
  assert.equal(registry.getNextRun(task.id), null);
  assert.equal(taskRepo.getTask(task.id)?.enabled, false, "persisted, not just in memory");

  assert.equal(registry.setEnabled(task.id, true), true);
  assert.ok(registry.getNextRun(task.id));
  assert.equal(taskRepo.getTask(task.id)?.enabled, true);
});

test("setEnabled reports failure for a task that was never scheduled", () => {
  assert.equal(registry.setEnabled("never-existed", true), false);
});
