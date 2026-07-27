import type { TriggerSource } from "@breckr/shared";
import * as runs from "../repositories/runs.repository.ts";
import * as tasks from "../repositories/tasks.repository.ts";
import { withPage, withoutPage } from "./browser.service.ts";
import { sendNotification } from "./notifier.service.ts";
import { describeError, errorMessage } from "../utils/json.ts";
import type { Logger, ResolvedTask, RunRecord } from "../types/index.ts";

/**
 * Injected so the edge-trigger state machine can be tested against every
 * delivery outcome. ESM bindings cannot be monkey-patched the way CommonJS
 * exports could, so the seam is explicit.
 */
export interface RunnerDependencies {
  sendNotification: typeof sendNotification;
}

const defaultDependencies: RunnerDependencies = { sendNotification };

/**
 * Execute one task and record the outcome.
 *
 * The run row is written *before* execution so a crash or hang stays visible as
 * 'running' instead of vanishing; the boot sweep resolves any left dangling.
 *
 * Never throws — a failing task must not take down the scheduler or the HTTP
 * request that triggered it. The failure is recorded on the run row instead.
 */
export async function runTask<TResult>(
  definition: ResolvedTask<TResult>,
  triggerSource: TriggerSource = "cron",
  logger: Logger = console,
  deps: RunnerDependencies = defaultDependencies
): Promise<RunRecord> {
  const runId = runs.startRun({ taskId: definition.id, triggerSource });

  // Generic in the result type because `condition` and `notify` consume it:
  // a non-generic `ResolvedTask<unknown>` parameter would reject every
  // concretely-typed task, since those callbacks are contravariant.
  let result: TResult;
  try {
    result = definition.needsBrowser
      ? await withPage(definition.timeoutMs, (page) => definition.run(page))
      : // Browserless tasks never touch the page, so the argument is unused.
        await withoutPage(definition.timeoutMs, () =>
          definition.run(undefined as never)
        );
  } catch (err) {
    runs.completeRun({ id: runId, status: "failed", error: describeError(err) });
    logger.error(
      { taskId: definition.id, runId, err: errorMessage(err) },
      "Task run failed"
    );
    // A failed run says nothing about the condition, so the armed state is
    // deliberately left untouched — an error is not evidence it cleared.
    return {
      runId,
      status: "failed",
      conditionMet: false,
      notified: false,
      error: errorMessage(err),
    };
  }

  let conditionMet = false;
  try {
    conditionMet = definition.condition ? Boolean(definition.condition(result)) : false;
  } catch (err) {
    // A throwing condition is a bug in the task, not a browser failure: the
    // extraction worked, so keep the result and record the fault.
    runs.completeRun({
      id: runId,
      status: "failed",
      result,
      error: `condition() threw: ${describeError(err)}`,
    });
    logger.error(
      { taskId: definition.id, runId, err: errorMessage(err) },
      "Task condition threw"
    );
    return { runId, status: "failed", conditionMet: false, notified: false };
  }

  // Edge-triggered: fire on the false -> true transition only, so a condition
  // that stays true doesn't notify on every interval. `wasMet` is read from the
  // database rather than memory so the state survives a restart.
  const persisted = tasks.getTask(definition.id);
  const wasMet = Boolean(persisted?.condition_met);

  let notified = false;

  if (conditionMet && !wasMet) {
    let message: string;
    try {
      message = definition.notify
        ? definition.notify(result)
        : `Task "${definition.name}" matched its condition.`;
    } catch (err) {
      message = `Task "${definition.name}" matched, but notify() threw: ${errorMessage(err)}`;
      logger.error(
        { taskId: definition.id, runId, err: errorMessage(err) },
        "Task notify() threw"
      );
    }

    const outcome = await deps.sendNotification(message, logger);
    notified = outcome.delivered;

    if (outcome.delivered) {
      tasks.markTaskNotified(definition.id);
    } else if (outcome.reason === "disabled") {
      // Nothing owed, so arm as if sent — dedup then behaves the same whether
      // or not Telegram is configured.
      tasks.setTaskConditionMet(definition.id, true);
    }
    // reason 'error': deliberately leave the state disarmed so the next run
    // retries the alert rather than swallowing it forever.
  } else if (!conditionMet && wasMet) {
    // Condition cleared — re-arm so the next false -> true transition fires.
    tasks.setTaskConditionMet(definition.id, false);
  }

  runs.completeRun({ id: runId, status: "success", conditionMet, notified, result });

  logger.info(
    { taskId: definition.id, runId, conditionMet, notified, triggerSource },
    "Task run complete"
  );

  return { runId, status: "success", conditionMet, notified };
}
