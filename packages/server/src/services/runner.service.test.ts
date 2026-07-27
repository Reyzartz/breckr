import test from "node:test";
import assert from "node:assert/strict";
import { runTask } from "./runner.service.ts";
import type { RunnerDependencies } from "./runner.service.ts";
import * as taskRepo from "../repositories/tasks.repository.ts";
import * as runRepo from "../repositories/runs.repository.ts";
import type { Logger, NotificationOutcome, ResolvedTask } from "../types/index.ts";

/**
 * The edge-trigger state machine.
 *
 * These assertions mirror the behavior verified by hand before the TypeScript
 * migration; they exist so a mechanical refactor cannot quietly change it. The
 * `error` vs `disabled` distinction in particular is easy to "tidy" into a bug:
 * a failed delivery still owes an alert, while a disabled notifier owes nothing.
 */

const quiet: Logger = { info() {}, warn() {}, error() {} };

/** Captures messages and lets each test force the delivery outcome. */
function createNotifier(outcome: NotificationOutcome) {
  const sent: string[] = [];
  const deps: RunnerDependencies = {
    sendNotification: (message: string) => {
      // A disabled notifier still "reports" the message (it logs it), so count
      // both delivered and disabled as observable alerts.
      if (outcome.delivered || outcome.reason === "disabled") sent.push(message);
      return Promise.resolve(outcome);
    },
  };
  return {
    deps,
    sent,
    set(next: NotificationOutcome) {
      outcome = next;
    },
  };
}

let taskCounter = 0;

/** A fresh browserless task with its own database row per test. */
function createTask(
  overrides: Partial<ResolvedTask<{ value: number }>> = {}
): { task: ResolvedTask<{ value: number }>; setValue(v: number): void } {
  const id = `_test_${String(++taskCounter)}`;
  let value = 500;

  const task: ResolvedTask<{ value: number }> = {
    id,
    name: `Test ${id}`,
    cron: "*/1 * * * *",
    needsBrowser: false,
    timeoutMs: 5000,
    run: () => ({ value }),
    condition: (result) => result.value < 100,
    notify: (result) => `value=${result.value}`,
    ...overrides,
  };

  // The runner reads edge-trigger state off the task row, so one has to exist.
  // Its spec is irrelevant here — these tests drive `task` directly rather than
  // going through the executor.
  taskRepo.createTask({
    id: task.id,
    name: task.name,
    cron_expr: task.cron,
    spec: {
      url: "https://example.com",
      selector: "#value",
      extract: "number",
      operator: "lt",
      value: "100",
    },
  });

  return {
    task,
    setValue(v: number) {
      value = v;
    },
  };
}

const isArmed = (id: string): boolean => Boolean(taskRepo.getTask(id)?.condition_met);

test("notifies once while the condition holds", async () => {
  const notifier = createNotifier({ delivered: true, reason: "sent" });
  const { task, setValue } = createTask();

  setValue(50);
  await runTask(task, "cron", quiet, notifier.deps);
  assert.equal(notifier.sent.length, 1, "first match should alert");

  setValue(40);
  await runTask(task, "cron", quiet, notifier.deps);
  setValue(30);
  await runTask(task, "cron", quiet, notifier.deps);

  assert.equal(notifier.sent.length, 1, "a held condition must not re-alert");
  assert.ok(isArmed(task.id), "state stays armed while the condition holds");
});

test("re-arms after the condition clears, then fires again", async () => {
  const notifier = createNotifier({ delivered: true, reason: "sent" });
  const { task, setValue } = createTask();

  setValue(50);
  await runTask(task, "cron", quiet, notifier.deps);
  assert.equal(notifier.sent.length, 1);

  setValue(500);
  await runTask(task, "cron", quiet, notifier.deps);
  assert.equal(notifier.sent.length, 1, "clearing must not alert");
  assert.equal(isArmed(task.id), false, "clearing re-arms the trigger");

  setValue(10);
  await runTask(task, "cron", quiet, notifier.deps);
  assert.equal(notifier.sent.length, 2, "a fresh transition alerts again");
});

test("a failed delivery is retried on the next run, not swallowed", async () => {
  const notifier = createNotifier({ delivered: false, reason: "error" });
  const { task, setValue } = createTask();

  setValue(20);
  const first = await runTask(task, "cron", quiet, notifier.deps);

  assert.equal(first.notified, false, "the run must not claim it notified");
  assert.equal(
    isArmed(task.id),
    false,
    "state must stay disarmed so the alert is retried"
  );

  notifier.set({ delivered: true, reason: "sent" });
  await runTask(task, "cron", quiet, notifier.deps);
  assert.equal(notifier.sent.length, 1, "the retry delivers");
  assert.ok(isArmed(task.id));
});

test("a disabled notifier dedups exactly like a working one", async () => {
  const notifier = createNotifier({ delivered: false, reason: "disabled" });
  const { task, setValue } = createTask();

  setValue(5);
  await runTask(task, "cron", quiet, notifier.deps);
  assert.equal(notifier.sent.length, 1, "logs the first match");

  setValue(4);
  await runTask(task, "cron", quiet, notifier.deps);
  setValue(3);
  await runTask(task, "cron", quiet, notifier.deps);

  assert.equal(
    notifier.sent.length,
    1,
    "nothing is owed, so state advances and dedup holds"
  );
});

test("a failed run leaves the armed state untouched", async () => {
  const notifier = createNotifier({ delivered: true, reason: "sent" });
  const { task, setValue } = createTask();

  setValue(50);
  await runTask(task, "cron", quiet, notifier.deps);
  const armedBefore = isArmed(task.id);

  const exploding: ResolvedTask<{ value: number }> = {
    ...task,
    run: () => {
      throw new Error("browser exploded");
    },
  };
  const outcome = await runTask(exploding, "cron", quiet, notifier.deps);

  assert.equal(outcome.status, "failed");
  assert.equal(
    isArmed(task.id),
    armedBefore,
    "an error is not evidence the condition cleared"
  );
});

test("a throwing condition fails the run but keeps the result", async () => {
  const notifier = createNotifier({ delivered: true, reason: "sent" });
  const { task } = createTask({
    condition: () => {
      throw new Error("bad condition");
    },
  });

  const outcome = await runTask(task, "cron", quiet, notifier.deps);
  const row = runRepo.getRun(outcome.runId);

  assert.equal(outcome.status, "failed");
  assert.notEqual(row?.result_summary, null, "the extracted result is retained");
  assert.match(row?.error ?? "", /condition\(\) threw/);
});

test("a run that outlives its timeout fails promptly", async () => {
  const notifier = createNotifier({ delivered: true, reason: "sent" });
  const { task } = createTask({
    timeoutMs: 150,
    run: () => new Promise((resolve) => setTimeout(() => { resolve({ value: 1 }); }, 5000)),
  });

  const startedAt = Date.now();
  const outcome = await runTask(task, "cron", quiet, notifier.deps);
  const elapsed = Date.now() - startedAt;

  assert.equal(outcome.status, "failed");
  assert.ok(elapsed < 1500, `gave up promptly (took ${String(elapsed)}ms)`);
  assert.match(runRepo.getRun(outcome.runId)?.error ?? "", /Timed out/);
});

test("run rows expose real booleans, not SQLite 0/1", async () => {
  const notifier = createNotifier({ delivered: true, reason: "sent" });
  const { task, setValue } = createTask();

  setValue(5);
  const outcome = await runTask(task, "cron", quiet, notifier.deps);
  const row = runRepo.getRun(outcome.runId);

  assert.equal(typeof row?.condition_met, "boolean");
  assert.equal(typeof row?.notified, "boolean");
  assert.equal(row?.condition_met, true);
  assert.equal(row?.notified, true);
});
