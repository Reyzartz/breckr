/**
 * The rejection type every user-input validator throws.
 *
 * It lives here rather than beside one validator because both `spec.service`
 * and `schedule.service` raise it and `spec.service` calls into
 * `schedule.service` — owning it in either would make that a cycle.
 */

/** Distinguishes a bad request from a server fault at the route boundary. */
export class SpecValidationError extends Error {
  /** The offending field, so the dashboard can highlight it. */
  readonly field: string;

  constructor(field: string, message: string) {
    super(message);
    this.name = "SpecValidationError";
    this.field = field;
  }
}

export function fail(field: string, message: string): never {
  throw new SpecValidationError(field, message);
}
