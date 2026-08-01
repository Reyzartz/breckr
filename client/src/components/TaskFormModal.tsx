import { useEffect, useRef, useState } from "react";
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
} from "broke-ui";
import { FlaskConical, Plus, SquarePen } from "lucide-react";
import type {
  Channel,
  MatchMode,
  NotifyMode,
  Schedule,
  TaskSpec,
  TaskWithStatus,
  TestTaskResponse,
} from "../types/index.ts";
import { useTasks } from "../hooks/useTasks.ts";
import { ApiError, toErrorMessage } from "../services/api/index.ts";
import {
  DEFAULT_NOTIFY_MODE,
  DEFAULT_SCHEDULE,
  DEFAULT_SPEC,
  MATCH_MODE_OPTIONS,
  MAX_CONDITIONS,
  NOTIFY_MODE_HINTS,
  NOTIFY_MODE_OPTIONS,
} from "../constants/index.ts";
import { slugify } from "../utils/format.ts";
import { ScheduleField, type ScheduleFields } from "./ScheduleField.tsx";
import {
  BLANK_CONDITION,
  ConditionField,
  conditionFieldName,
  toCondition,
  toConditionFields,
  type ConditionFields,
} from "./ConditionField.tsx";
import { ChannelPicker } from "./ChannelPicker.tsx";

interface TaskFormModalProps {
  isOpen: boolean;
  /** The task being edited, or null to create a new one. */
  task: TaskWithStatus | null;
  /** Every channel, so the picker can offer them. */
  channels: Channel[];
  onClose: () => void;
  onCreate: (input: {
    id: string;
    name: string;
    schedule: Schedule;
    spec: TaskSpec;
    notify_mode: NotifyMode;
    channel_ids: string[];
  }) => Promise<void>;
  onSave: (
    id: string,
    patch: {
      name: string;
      schedule: Schedule;
      spec: TaskSpec;
      notify_mode: NotifyMode;
      channel_ids: string[];
    },
  ) => Promise<void>;
  /** Opens the channel manager from inside the form. */
  onManageChannels: () => void;
}

interface FormState extends ScheduleFields {
  id: string;
  name: string;
  url: string;
  match: MatchMode;
  /** One entry per condition, in the order they are checked. */
  conditions: ConditionFields[];
  message: string;
  notifyMode: NotifyMode;
}

const pad = (value: number): string => String(value).padStart(2, "0");

/**
 * Spread a schedule across the flat fields the builder edits.
 *
 * Every field gets a value, not just the ones this frequency uses, so
 * switching frequency never lands on a blank control.
 */
function toScheduleFields(schedule: Schedule): ScheduleFields {
  const base: ScheduleFields = {
    frequency: schedule.every,
    interval: "15",
    minuteOfHour: "0",
    time: "09:00",
    weekdays: [1],
    monthDay: "1",
    customCron: "",
  };

  switch (schedule.every) {
    case "minutes":
      return { ...base, interval: String(schedule.interval) };
    case "hours":
      return {
        ...base,
        interval: String(schedule.interval),
        minuteOfHour: String(schedule.minute),
      };
    case "day":
      return { ...base, time: `${pad(schedule.hour)}:${pad(schedule.minute)}` };
    case "week":
      return {
        ...base,
        time: `${pad(schedule.hour)}:${pad(schedule.minute)}`,
        weekdays: schedule.weekdays,
      };
    case "month":
      return {
        ...base,
        time: `${pad(schedule.hour)}:${pad(schedule.minute)}`,
        monthDay: String(schedule.day),
      };
    case "custom":
      return { ...base, customCron: schedule.cron };
  }
}

/**
 * Reassemble the schedule from those fields.
 *
 * Out-of-range numbers are passed through rather than clamped: the server
 * range-checks them and names `schedule`, which is the same path a hand-driven
 * API call takes, so there is one set of bounds rather than two.
 */
function toSchedule(form: FormState): Schedule {
  const int = (raw: string): number => Number.parseInt(raw, 10);
  const [hour = "", minute = ""] = form.time.split(":");

  switch (form.frequency) {
    case "minutes":
      return { every: "minutes", interval: int(form.interval) };
    case "hours":
      return {
        every: "hours",
        interval: int(form.interval),
        minute: int(form.minuteOfHour),
      };
    case "day":
      return { every: "day", hour: int(hour), minute: int(minute) };
    case "week":
      return {
        every: "week",
        weekdays: form.weekdays,
        hour: int(hour),
        minute: int(minute),
      };
    case "month":
      return {
        every: "month",
        day: int(form.monthDay),
        hour: int(hour),
        minute: int(minute),
      };
    case "custom":
      return { every: "custom", cron: form.customCron.trim() };
  }
}

/**
 * The fields `bind` can drive.
 *
 * `weekdays` is an array and the builder owns it, so excluding it keeps `bind`
 * from having to pretend an input's string value is one.
 */
type TextFieldKey = {
  [K in keyof FormState]: FormState[K] extends string ? K : never;
}[keyof FormState];

function toFormState(task: TaskWithStatus | null): FormState {
  const spec = task?.spec ?? DEFAULT_SPEC;

  return {
    id: task?.id ?? "",
    name: task?.name ?? "",
    ...toScheduleFields(task?.schedule ?? DEFAULT_SCHEDULE),
    url: spec.url,
    match: spec.match ?? "all",
    // A task stored before conditions became a list has already been hoisted
    // into one by the server, so there is only one shape to read here.
    conditions: spec.conditions.map(toConditionFields),
    message: spec.message ?? "",
    // Not part of the spec: it is alert policy, and it survives a spec edit.
    notifyMode: task?.notify_mode ?? DEFAULT_NOTIFY_MODE,
  };
}

/** Blank optionals are omitted so they never reach the server as "". */
function toSpec(form: FormState): TaskSpec {
  return {
    url: form.url.trim(),
    match: form.match,
    conditions: form.conditions.map(toCondition),
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
  channels,
  onClose,
  onCreate,
  onSave,
  onManageChannels,
}: TaskFormModalProps) {
  const isEditing = task !== null;
  const { testTask } = useTasks();

  const [form, setForm] = useState<FormState>(() => toFormState(task));
  // Held outside FormState, which is flat and string-typed so `bind` can drive
  // it — the same reason `weekdays` lives in the schedule builder.
  const [channelIds, setChannelIds] = useState<string[]>(
    () => task?.channel_ids ?? [],
  );
  const [fieldError, setFieldError] = useState<{
    field: string;
    message: string;
  } | null>(null);
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
    setChannelIds(task?.channel_ids ?? []);
    setFieldError(null);
    setFormError(null);
    setTestResult(null);
    setIdTouched(task !== null);
  }, [isOpen, task]);

  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((current) => ({ ...current, [key]: value }));
    setFieldError((current) => (current?.field === key ? null : current));
  };

  const errorFor = (field: string): string | undefined =>
    fieldError?.field === field ? fieldError.message : undefined;

  /**
   * Value, handler and error for one text field.
   *
   * The event has to be annotated explicitly: broke-ui's controls do not
   * propagate the DOM handler's parameter type through their props, so an
   * inline arrow infers `any` and fails under noImplicitAny. Binding it once
   * here beats repeating the annotation on every control.
   */
  const bind = <K extends TextFieldKey>(key: K) => ({
    value: form[key],
    error: errorFor(key),
    onChange: (
      event: React.ChangeEvent<
        HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement
      >,
    ) => {
      set(key, event.target.value as FormState[K]);
    },
  });

  /** The builder owns several fields at once — switching frequency can clamp. */
  const handleScheduleChange = (patch: Partial<ScheduleFields>) => {
    setForm((current) => ({ ...current, ...patch }));
    setFieldError((current) =>
      current?.field === "schedule" ? null : current,
    );
  };

  const handleNameChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const name = event.target.value;
    setForm((current) => ({
      ...current,
      name,
      ...(idTouched || isEditing ? {} : { id: slugify(name) }),
    }));
    setFieldError(null);
  };

  /**
   * Patch one condition.
   *
   * The row owns several fields at once — switching the extraction can strand
   * an operator the new kind does not allow — so it sends a patch rather than a
   * single value, the same way the schedule builder does.
   */
  const patchCondition = (index: number, patch: Partial<ConditionFields>) => {
    setForm((current) => ({
      ...current,
      conditions: current.conditions.map((condition, at) =>
        at === index ? { ...condition, ...patch } : condition,
      ),
    }));
    setFieldError((current) =>
      current?.field.startsWith(`conditions[${index}]`) ? null : current,
    );
  };

  const addCondition = () => {
    setForm((current) => ({
      ...current,
      conditions: [...current.conditions, { ...BLANK_CONDITION }],
    }));
    setFieldError((current) =>
      current?.field === "conditions" ? null : current,
    );
  };

  /**
   * Drop one condition.
   *
   * Any message placeholder past the new last condition would be rejected on
   * save, but the error names `message` and says how many conditions there are
   * — which is enough to act on, and beats rewriting a template the user wrote.
   */
  const removeCondition = (index: number) => {
    setForm((current) => ({
      ...current,
      conditions: current.conditions.filter((_, at) => at !== index),
    }));
    // Positions shift, so any complaint about a condition now points at the
    // wrong row.
    setFieldError((current) =>
      current?.field.startsWith("conditions") ? null : current,
    );
  };

  const handleNotifyModeChange = (
    event: React.ChangeEvent<HTMLSelectElement>,
  ) => {
    set("notifyMode", event.target.value as NotifyMode);
    setFieldError((current) =>
      current?.field === "notify_mode" ? null : current,
    );
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
        await testTask({
          name: form.name || "Untitled task",
          spec: toSpec(form),
        }),
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
      schedule: toSchedule(form),
      spec: toSpec(form),
      notify_mode: form.notifyMode,
      channel_ids: channelIds,
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

  return (
    <Modal isOpen={isOpen} onClose={onClose} maxWidth="lg">
      <ModalHeader
        title={isEditing ? `Edit ${task.name}` : "New task"}
        icon={isEditing ? SquarePen : Plus}
      />

      <ModalBody>
        {/*
          Taller on a phone, where the modal is nearly the whole screen and
          every capped vh is a field the user has to scroll for. It stays under
          ModalBody's own 70vh cap either way, so this stays the one scroller
          rather than nesting inside a second one.
        */}
        <div
          ref={scrollRef}
          className="grid max-h-[62vh] grid-cols-1 gap-4 overflow-y-auto pr-1 sm:max-h-[60vh]"
        >
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
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

          <ScheduleField
            value={form}
            onChange={handleScheduleChange}
            error={errorFor("schedule")}
          />

          <Input
            label="URL"
            {...bind("url")}
            placeholder="https://example.com/product"
            fullWidth
          />

          <div className="grid grid-cols-1 gap-3">
            <Select
              label="Alert when"
              {...bind("match")}
              error={errorFor("match")}
              info={
                form.conditions.length > 1
                  ? undefined
                  : "Applies once there is more than one condition."
              }
              fullWidth
            >
              {MATCH_MODE_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </Select>

            {form.conditions.map((condition, index) => (
              <ConditionField
                key={index}
                index={index}
                value={condition}
                onChange={(patch) => patchCondition(index, patch)}
                // A task needs at least one condition, so the last one cannot go.
                onRemove={
                  form.conditions.length > 1
                    ? () => removeCondition(index)
                    : undefined
                }
                errorFor={(field) => errorFor(conditionFieldName(index, field))}
              />
            ))}

            {errorFor("conditions") && (
              <Alert variant="error">{errorFor("conditions")}</Alert>
            )}

            <div>
              <Button
                variant="outlined"
                size="sm"
                icon={Plus}
                onClick={addCondition}
                disabled={form.conditions.length >= MAX_CONDITIONS}
              >
                Add condition
              </Button>
            </div>
          </div>

          <ChannelPicker
            channels={channels}
            value={channelIds}
            onChange={(next) => {
              setChannelIds(next);
              setFieldError((current) =>
                current?.field === "channel_ids" ? null : current,
              );
            }}
            error={errorFor("channel_ids")}
            onManageChannels={onManageChannels}
          />

          <Select
            label="Alert me"
            value={form.notifyMode}
            onChange={handleNotifyModeChange}
            // The server names this field in snake_case, so `bind`'s key-based
            // lookup would not find its complaint.
            error={errorFor("notify_mode")}
            info={NOTIFY_MODE_HINTS[form.notifyMode]}
            fullWidth
          >
            {NOTIFY_MODE_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </Select>

          <Textarea
            label="Alert message (optional)"
            {...bind("message")}
            rows={2}
            placeholder="{{name}}: now {{value}} at {{url}}"
            fullWidth
          />
          <Text variant="caption" color="muted">
            Placeholders: <code>{"{{value}}"}</code> <code>{"{{raw}}"}</code>{" "}
            <code>{"{{url}}"}</code> <code>{"{{name}}"}</code>
            {form.conditions.length > 1 && (
              <>
                . Use <code>{"{{value1}}"}</code> …{" "}
                <code>{`{{value${form.conditions.length}}}`}</code> to name one
                condition; <code>{"{{value}}"}</code> is the first
              </>
            )}
            .
          </Text>

          {formError && <Alert variant="error">{formError}</Alert>}

          {testResult?.ok && (
            <Alert variant={testResult.conditionMet ? "warning" : "success"}>
              <div className="grid grid-cols-1 gap-1">
                {/* Per condition, so a task watching several says which
                    selector produced which value rather than only the first. */}
                {(testResult.result?.checks ?? []).map((check, index) => (
                  <div key={check.key}>
                    Extracted <code>{JSON.stringify(check.value)}</code> from{" "}
                    <code>{form.conditions[index]?.selector}</code>
                    {form.conditions.length > 1 &&
                      ` — ${check.met ? "matches" : "does not match"}`}
                    .
                  </div>
                ))}
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
        {/*
          One row at every width: ModalFooter clips itself to max-h-14, so a
          second row of buttons would be silently cut off rather than wrapped.
          Test keeps the left edge and the pair that closes the form keeps the
          right, which is where the thumb already is.
        */}
        <div className="flex w-full items-center gap-2">
          <Button
            variant="outlined"
            icon={FlaskConical}
            onClick={() => void handleTest()}
            disabled={testing || saving}
            className="shrink-0"
          >
            {testing ? "Testing…" : "Test"}
          </Button>

          <div className="ml-auto flex min-w-0 items-center gap-2">
            <Button variant="ghost" onClick={onClose} disabled={saving}>
              Cancel
            </Button>
            <Button
              onClick={() => void handleSubmit()}
              disabled={saving || testing}
            >
              {saving ? "Saving…" : isEditing ? "Save changes" : "Create task"}
            </Button>
          </div>
        </div>
      </ModalFooter>
    </Modal>
  );
}
