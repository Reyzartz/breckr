/**
 * Serialize a task result for storage.
 *
 * A task's run() may return something JSON.stringify chokes on (a circular
 * reference, a BigInt). Recording a diagnostic beats throwing away an otherwise
 * good run, so this never throws.
 */
export function safeStringify(value: unknown): string {
  try {
    return JSON.stringify(value) ?? "null";
  } catch (err) {
    return JSON.stringify({
      _unserializable: err instanceof Error ? err.message : String(err),
    });
  }
}

/** Message plus stack when available, for the `error` column. */
export function describeError(err: unknown): string {
  if (err instanceof Error) return err.stack ?? err.message;
  return String(err);
}

/** Just the message, for log lines. */
export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
