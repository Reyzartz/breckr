import { Button, Input, Select, Text } from "broke-ui";
import { Trash2 } from "lucide-react";
import type {
  CompareOperator,
  Condition,
  ExtractKind,
} from "../types/index.ts";
import {
  EXTRACT_OPTIONS,
  OPERATORS_BY_KIND,
  OPERATOR_LABELS,
  VALUELESS_OPERATORS,
} from "../constants/index.ts";

/**
 * One condition's slice of the form state.
 *
 * Flat and string-typed like the rest of `FormState`, for the same reason:
 * every field can be driven straight from an input's value, and the structured
 * `Condition` the server wants is assembled once at the submit boundary.
 */
export interface ConditionFields {
  selector: string;
  waitForSelector: string;
  extract: ExtractKind;
  attribute: string;
  operator: CompareOperator;
  value: string;
}

export const BLANK_CONDITION: ConditionFields = {
  selector: "",
  waitForSelector: "",
  extract: "text",
  attribute: "",
  operator: "changed",
  value: "",
};

export function toConditionFields(condition: Condition): ConditionFields {
  return {
    selector: condition.selector,
    waitForSelector: condition.waitForSelector ?? "",
    extract: condition.extract,
    attribute: condition.attribute ?? "",
    operator: condition.operator,
    value: condition.value ?? "",
  };
}

/** Blank optionals are omitted so they never reach the server as "". */
export function toCondition(fields: ConditionFields): Condition {
  return {
    selector: fields.selector.trim(),
    extract: fields.extract,
    operator: fields.operator,
    ...(fields.waitForSelector.trim()
      ? { waitForSelector: fields.waitForSelector.trim() }
      : {}),
    ...(fields.extract === "attribute"
      ? { attribute: fields.attribute.trim() }
      : {}),
    ...(VALUELESS_OPERATORS.includes(fields.operator)
      ? {}
      : { value: fields.value.trim() }),
  };
}

/**
 * How the server names a field inside one condition.
 *
 * It has to, so that a complaint about the third condition lands on the third
 * row rather than on the first — which is the one row you can be sure is fine,
 * since validation stops at the first failure.
 */
export function conditionFieldName(
  index: number,
  field: keyof ConditionFields,
): string {
  return `conditions[${index}].${field}`;
}

interface ConditionFieldProps {
  index: number;
  value: ConditionFields;
  onChange: (patch: Partial<ConditionFields>) => void;
  /** Absent while the task has only one condition — there is nothing to remove. */
  onRemove?: () => void;
  /** The server's complaint about a field of *this* condition. */
  errorFor: (field: keyof ConditionFields) => string | undefined;
}

/**
 * One condition: what to select, what to pull out of it, and what would make
 * that interesting.
 *
 * The server is the authority on whether the combination is valid; this only
 * keeps the user from building one it would certainly reject — an operator that
 * cannot apply to the chosen extraction.
 */
export function ConditionField({
  index,
  value,
  onChange,
  onRemove,
  errorFor,
}: ConditionFieldProps) {
  const operators = OPERATORS_BY_KIND[value.extract];
  const needsValue = !VALUELESS_OPERATORS.includes(value.operator);

  /** Switching kind can strand an operator the new kind does not allow. */
  const handleExtract = (event: React.ChangeEvent<HTMLSelectElement>) => {
    const extract = event.target.value as ExtractKind;
    const allowed = OPERATORS_BY_KIND[extract];

    onChange({
      extract,
      ...(allowed.includes(value.operator)
        ? {}
        : { operator: allowed[0] as CompareOperator }),
    });
  };

  const bind = <K extends keyof ConditionFields>(key: K) => ({
    value: value[key],
    error: errorFor(key),
    onChange: (
      event: React.ChangeEvent<
        HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement
      >,
    ) => {
      onChange({ [key]: event.target.value } as Partial<ConditionFields>);
    },
  });

  return (
    <div className="grid grid-cols-1 gap-4 rounded-lg border border-(--border) p-3">
      <div className="flex items-center justify-between">
        <Text variant="caption" color="muted">
          Condition {index + 1}
        </Text>
        {onRemove && (
          <Button
            variant="ghost"
            size="sm"
            icon={Trash2}
            onClick={onRemove}
            aria-label={`Remove condition ${index + 1}`}
          >
            Remove
          </Button>
        )}
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Input
          label="CSS selector"
          {...bind("selector")}
          placeholder=".price"
          fullWidth
        />
        <Input
          label="Wait for selector (optional)"
          {...bind("waitForSelector")}
          info="Defaults to the selector above."
          fullWidth
        />
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Select
          label="Extract"
          value={value.extract}
          onChange={handleExtract}
          error={errorFor("extract")}
          fullWidth
        >
          {EXTRACT_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </Select>

        {value.extract === "attribute" && (
          <Input
            label="Attribute name"
            {...bind("attribute")}
            placeholder="href"
            fullWidth
          />
        )}
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Select label="Alert when it" {...bind("operator")} fullWidth>
          {operators.map((operator) => (
            <option key={operator} value={operator}>
              {OPERATOR_LABELS[operator]}
            </option>
          ))}
        </Select>

        {needsValue && (
          <Input
            label="Value"
            {...bind("value")}
            placeholder={value.extract === "number" ? "100" : "In stock"}
            fullWidth
          />
        )}
      </div>
    </div>
  );
}
