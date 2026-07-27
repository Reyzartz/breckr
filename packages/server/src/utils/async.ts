export class TimeoutError extends Error {
  constructor(ms: number) {
    super(`Timed out after ${ms}ms`);
    this.name = "TimeoutError";
  }
}

/**
 * Reject if `promise` has not settled within `ms`.
 *
 * The timer is unref'd so a pending timeout never keeps the process alive, and
 * cleared on settle so a fast result does not leave one dangling.
 */
export function withTimeout<T>(promise: Promise<T>, ms: number): Promise<T> {
  let timer: NodeJS.Timeout | undefined;

  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => {
      reject(new TimeoutError(ms));
    }, ms);
    timer.unref?.();
  });

  return Promise.race([promise, timeout]).finally(() => {
    clearTimeout(timer);
  });
}

/**
 * Serializes work through a promise chain.
 *
 * Lightpanda's CDP server accepts one connection, one context and one page per
 * process, so two tasks whose schedules collide would fight over it. node-cron's
 * `noOverlap` only stops a task overlapping *itself*; this covers the cross-task
 * case. Volume is low, so queueing costs nothing.
 */
export function createMutex(): <T>(fn: () => Promise<T>) => Promise<T> {
  let tail: Promise<unknown> = Promise.resolve();

  return function runExclusive<T>(fn: () => Promise<T>): Promise<T> {
    const run = tail.then(fn);
    // Swallow the result on the chain itself so one failure cannot poison the
    // queue for every later caller — `run` still rejects for its own caller.
    tail = run.then(
      () => {},
      () => {}
    );
    return run;
  };
}
