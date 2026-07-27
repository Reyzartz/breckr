import { useEffect, useMemo, useRef, useState } from "react";
import {
  Alert,
  Button,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Select,
  Text,
  Textarea,
} from "brake-ui";
import { FlaskConical, Plus, SquarePen } from "lucide-react";
import type {
  CompareOperator,
  ExtractKind,
  TaskSpec,
  TaskWithStatus,
  TestTaskResponse,
} from "@breckr/shared";
import { testTask } from "../services/monitor.service.ts";
import { ApiError, toErrorMessage } from "../apis/client.ts";
import {
  DEFAULT_CRON,
  DEFAULT_SPEC,
  EXTRACT_OPTIONS,
  OPERATORS_BY_KIND,
  OPERATOR_LABELS,
  VALUELESS_OPERATORS,
} from "../constants/index.ts";
import { slugify } from "../utils/format.ts";

interface TaskFormModalProps {
  isOpen: boolean;
  /** The task being edited, or null to create a new one. */
  task: TaskWithStatus | null;
  onClose: () => void;
  onCreate: (input: {
    id: string;
    name: string;
    cron_expr: string;
    spec: TaskSpec;
  }) => Promise<void>;
  onSave: (
    id: string,
    patch: { name: string; cron_expr: string; spec: TaskSpec }
  ) => Promise<void>;
}

interface FormState {
  id: string;
  name: string;
  cron_expr: string;
  url: string;
  selector: string;
  waitForSelector: string;
  extract: ExtractKind;
  attribute: string;
  operator: CompareOperator;
  value: string;
  message: string;
}

function toFormState(task: TaskWithStatus | null): FormState {
  const spec = task?.spec ?? DEFAULT_SPEC;

  return {
    id: task?.id ?? "",
    name: task?.name ?? "",
    cron_expr: task?.cron_expr ?? DEFAULT_CRON,
    url: spec.url,
    selector: spec.selector,
    waitForSelector: spec.waitForSelector ?? "",
    extract: spec.extract,
    attribute: spec.attribute ?? "",
    operator: spec.operator,
    value: spec.value ?? "",
    message: spec.message ?? "",
  };
}

/** Blank optionals are omitted so they never reach the server as "". */
function toSpec(form: FormState): TaskSpec {
  return {
    url: form.url.trim(),
    selector: form.selector.trim(),
    extract: form.extract,
    operator: form.operator,
    ...(form.waitForSelector.trim()
      ? { waitForSelector: form.waitForSelector.trim() }
      : {}),
    ...(form.extract === "attribute" ? { attribute: form.attribute.trim() } : {}),
    ...(VALUELESS_OPERATORS.includes(form.operator) ? {} : { value: form.value.trim() }),
    ...(form.message.trim() ? { message: form.message.trim() } : {}),
  };
}

/**
 * Create or edit one task.
 *
 * The server is the authority on whether a spec is valid; this only keeps the
 * user from building a combination it would certainly reject (an operator that
 * cannot apply to the chosen extraction), and renders the server's field-level
 * complaint against the control it names.
 */
export function TaskFormModal({
  isOpen,
  task,
  onClose,
  onCreate,
  onSave,
}: TaskFormModalProps) {
  const isEditing = task !== null;

  const [form, setForm] = useState<FormState>(() => toFormState(task));
  const [fieldError, setFieldError] = useState<{ field: string; message: string } | null>(
    null
  );
  const [formError, setFormError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<TestTaskResponse | null>(null);
  const [testing, setTesting] = useState(false);
  const [saving, setSaving] = useState(false);
  // While false, the id tracks the name. The first manual edit stops that, so
  // typing an id and then fixing a typo in the name cannot silently undo it.
  const [idTouched, setIdTouched] = useState(false);

  /**
   * The form's own scroll container.
   *
   * The modal panel clips its overflow, so a form taller than the viewport
   * would put the test result somewhere the user cannot reach — pressing Test
   * would look like it did nothing. Scrolling is owned here, and the result is
   * scrolled to as soon as it lands.
   */
  const scrollRef = useRef<HTMLDivElement>(null);

  const revealResult = () => {
    const element = scrollRef.current;
    if (!element) return;
    // After paint, so scrollHeight includes the result that was just added.
    requestAnimationFrame(() => {
      element.scrollTo({ top: element.scrollHeight, behavior: "smooth" });
    });
  };

  // Reopening for a different task has to reset every field, including the
  // stale test result from the last one.
  useEffect(() => {
    if (!isOpen) return;
    setForm(toFormState(task));
    setFieldError(null);
    setFormError(null);
    setTestResult(null);
    setIdTouched(task !== null);
  }, [isOpen, task]);

  const operators = OPERATORS_BY_KIND[form.extract];
  const needsValue = !VALUELESS_OPERATORS.includes(form.operator);

  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((current) => ({ ...current, [key]: value }));
    setFieldError((current) => (current?.field === key ? null : current));
  };

  const errorFor = (field: string): string | undefined =>
    fieldError?.field === field ? fieldError.message : undefined;

  /**
   * Value, handler and error for one text field.
   *
   * The event has to be annotated explicitly: brake-ui's controls do not
   * propagate the DOM handler's parameter type through their props, so an
   * inline arrow infers `any` and fails under noImplicitAny. Binding it once
   * here beats repeating the annotation on every control.
   */
  const bind = <K extends keyof FormState>(key: K) => ({
    value: form[key],
    error: errorFor(key),
    onChange: (
      event: React.ChangeEvent<
        HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement
      >
    ) => {
      set(key, event.target.value as FormState[K]);
    },
  });

  const handleNameChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const name = event.target.value;
    setForm((current) => ({
      ...current,
      name,
      ...(idTouched || isEditing ? {} : { id: slugify(name) }),
    }));
    setFieldError(null);
  };

  /** Switching kind can strand an operator the new kind does not allow. */
  const handleExtractChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
    const extract = event.target.value as ExtractKind;
    setForm((current) => {
      const allowed = OPERATORS_BY_KIND[extract];
      return {
        ...current,
        extract,
        operator: allowed.includes(current.operator)
          ? current.operator
          : (allowed[0] as CompareOperator),
      };
    });
    setFieldError(null);
  };

  const handleIdChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    setIdTouched(true);
    set("id", event.target.value);
  };

  const handleTest = async () => {
    setTesting(true);
    setTestResult(null);
    setFormError(null);
    try {
      setTestResult(
        await testTask({ name: form.name || "Untitled task", spec: toSpec(form) })
      );
      revealResult();
    } catch (err) {
      // A 400 here is the spec failing validation before it ever ran.
      if (err instanceof ApiError && err.field) {
        setFieldError({ field: err.field, message: err.message });
      } else {
        setFormError(toErrorMessage(err));
        revealResult();
      }
    } finally {
      setTesting(false);
    }
  };

  const handleSubmit = async () => {
    setSaving(true);
    setFieldError(null);
    setFormError(null);

    const payload = {
      name: form.name.trim(),
      cron_expr: form.cron_expr.trim(),
      spec: toSpec(form),
    };

    try {
      if (isEditing) await onSave(task.id, payload);
      else await onCreate({ id: form.id.trim(), ...payload });
      onClose();
    } catch (err) {
      if (err instanceof ApiError && err.field) {
        setFieldError({ field: err.field, message: err.message });
      } else {
        setFormError(toErrorMessage(err));
      }
    } finally {
      setSaving(false);
    }
  };

  const operatorOptions = useMemo(
    () =>
      operators.map((operator) => (
        <option key={operator} value={operator}>
          {OPERATOR_LABELS[operator]}
        </option>
      )),
    [operators]
  );

  return (
    <Modal isOpen={isOpen} onClose={onClose} maxWidth="lg">
      <ModalHeader
        title={isEditing ? `Edit ${task.name}` : "New task"}
        icon={isEditing ? SquarePen : Plus}
      />

      <ModalBody>
        <div ref={scrollRef} className="grid max-h-[60vh] gap-4 overflow-y-auto pr-1">
          <div className="grid gap-4 sm:grid-cols-2">
            <Input
              label="Name"
              value={form.name}
              onChange={handleNameChange}
              error={errorFor("name")}
              placeholder="Price check"
              fullWidth
            />
            <Input
              label="ID"
              value={form.id}
              onChange={handleIdChange}
              error={errorFor("id")}
              disabled={isEditing}
              info={
                isEditing
                  ? "Run history is keyed on the ID, so it cannot be changed."
                  : "Letters, digits, dot, underscore and dash."
              }
              fullWidth
            />
          </div>

          <Input
            label="Schedule (cron)"
            {...bind("cron_expr")}
            info="Standard 5-field cron, in the server's timezone."
            fullWidth
          />

          <Input
            label="URL"
            {...bind("url")}
            placeholder="https://example.com/product"
            fullWidth
          />

          <div className="grid gap-4 sm:grid-cols-2">
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

          <div className="grid gap-4 sm:grid-cols-2">
            <Select
              label="Extract"
              value={form.extract}
              onChange={handleExtractChange}
              error={errorFor("extract")}
              fullWidth
            >
              {EXTRACT_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </Select>

            {form.extract === "attribute" && (
              <Input
                label="Attribute name"
                {...bind("attribute")}
                placeholder="href"
                fullWidth
              />
            )}
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <Select label="Alert when it" {...bind("operator")} fullWidth>
              {operatorOptions}
            </Select>

            {needsValue && (
              <Input
                label="Value"
                {...bind("value")}
                placeholder={form.extract === "number" ? "100" : "In stock"}
                fullWidth
              />
            )}
          </div>

          <Textarea
            label="Alert message (optional)"
            {...bind("message")}
            rows={2}
            placeholder="{{name}}: now {{value}} at {{url}}"
            fullWidth
          />
          <Text variant="caption" color="muted">
            Placeholders: <code>{"{{value}}"}</code> <code>{"{{raw}}"}</code>{" "}
            <code>{"{{url}}"}</code> <code>{"{{name}}"}</code>. You are alerted once
            when the condition becomes true, and not again until it goes back to false.
          </Text>

          {formError && <Alert variant="error">{formError}</Alert>}

          {testResult?.ok && (
            <Alert variant={testResult.conditionMet ? "warning" : "success"}>
              <div className="grid gap-1">
                <div>
                  Extracted <code>{JSON.stringify(testResult.result?.value)}</code> from{" "}
                  <code>{form.selector}</code>.
                </div>
                <div>
                  {testResult.conditionMet
                    ? `Condition matches — it would alert: "${testResult.message ?? ""}"`
                    : "Condition does not match, so it would stay quiet."}
                </div>
              </div>
            </Alert>
          )}

          {testResult && !testResult.ok && (
            <Alert variant="error">
              <span className="font-mono text-xs">{testResult.error}</span>
            </Alert>
          )}
        </div>
      </ModalBody>

      <ModalFooter>
        <div className="flex w-full flex-wrap items-center justify-between gap-2">
          <Button
            variant="outlined"
            icon={FlaskConical}
            onClick={() => void handleTest()}
            disabled={testing || saving}
          >
            {testing ? "Testing…" : "Test"}
          </Button>

          <div className="flex items-center gap-2">
            <Button variant="ghost" onClick={onClose} disabled={saving}>
              Cancel
            </Button>
            <Button onClick={() => void handleSubmit()} disabled={saving || testing}>
              {saving ? "Saving…" : isEditing ? "Save changes" : "Create task"}
            </Button>
          </div>
        </div>
      </ModalFooter>
    </Modal>
  );
}
