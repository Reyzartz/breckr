import test from "node:test";
import assert from "node:assert/strict";
import { createMutex, withTimeout, TimeoutError } from "./async.ts";

/**
 * The mutex is what stops two tasks colliding over a CDP server that accepts
 * only one connection at a time. node-cron's `noOverlap` covers a task
 * overlapping itself; nothing else covers different tasks firing on the same
 * minute.
 */

test("mutex serializes overlapping work", async () => {
  const runExclusive = createMutex();
  let active = 0;
  let peak = 0;

  const job = () =>
    runExclusive(async () => {
      active += 1;
      peak = Math.max(peak, active);
      await new Promise((resolve) => setTimeout(resolve, 40));
      active -= 1;
    });

  await Promise.all([job(), job(), job(), job(), job()]);

  assert.equal(peak, 1, "at most one run may hold the browser at a time");
});

test("a failing run does not poison the queue", async () => {
  const runExclusive = createMutex();

  await assert.rejects(
    runExclusive(() => Promise.reject(new Error("boom"))),
    /boom/,
    "the failure still surfaces to its own caller"
  );

  const after = await runExclusive(() => Promise.resolve("still alive"));
  assert.equal(after, "still alive", "later runs are unaffected");
});

test("mutex preserves submission order", async () => {
  const runExclusive = createMutex();
  const order: number[] = [];

  await Promise.all(
    [1, 2, 3].map((n) =>
      runExclusive(async () => {
        // Longer delay first: without the mutex these would finish reversed.
        await new Promise((resolve) => setTimeout(resolve, 30 - n * 10));
        order.push(n);
      })
    )
  );

  assert.deepEqual(order, [1, 2, 3]);
});

test("withTimeout rejects slow work and passes fast work through", async () => {
  await assert.rejects(
    withTimeout(new Promise((resolve) => setTimeout(resolve, 5000)), 50),
    TimeoutError
  );

  assert.equal(await withTimeout(Promise.resolve("fast"), 1000), "fast");
});

test("withTimeout surfaces the original error, not a timeout", async () => {
  await assert.rejects(
    withTimeout(Promise.reject(new Error("real failure")), 1000),
    /real failure/
  );
});
