import type {
  ChannelType,
  CompareOperator,
  Condition,
  ExtractKind,
  MatchMode,
  NotificationReason,
  NotifyMode,
  RunStatus,
  Schedule,
  ScheduleFrequency,
  TaskSpec,
} from "../types/index.ts";

/** Rows in the dashboard's compact "recent runs" panel — the full, filterable,
 * paginated table lives on /runs. */
export const RECENT_RUNS_LIMIT = 8;

/** Runs per page in the history table. */
export const PAGE_SIZE = 25;

/**
 * Runtime values live here, not in `../types`, which is the types-only mirror
 * of the server's contract. Typing them against it keeps the two in step —
 * `server/internal/types` stays the authority, this copy just stops the form
 * offering a pairing the server would reject.
 */
export const RUN_STATUSES: readonly RunStatus[] = [
  "success",
  "failed",
  "running",
];

/** broke-ui Badge variants, keyed by run status. */
export const STATUS_BADGE_VARIANT: Record<
  RunStatus,
  "success" | "error" | "info"
> = {
  success: "success",
  failed: "error",
  running: "info",
};

/**
 * broke-ui Badge variants, keyed by notification outcome.
 *
 * `error` is red and `disabled` is neutral on purpose: an alert the transport
 * rejected is a fault to fix, while an alert nothing was configured to send is
 * just the state of the install.
 */
export const NOTIFICATION_BADGE_VARIANT: Record<
  NotificationReason,
  "success" | "error" | "default"
> = {
  sent: "success",
  disabled: "default",
  error: "error",
};

/** How each outcome reads in the UI. */
export const NOTIFICATION_LABEL: Record<NotificationReason, string> = {
  sent: "notified",
  disabled: "no channels",
  error: "notify failed",
};

// --- Channels --------------------------------------------------------------

/** The transports the form offers, in the order they appear. */
export const CHANNEL_TYPE_OPTIONS: readonly {
  value: ChannelType;
  label: string;
}[] = [
  { value: "telegram", label: "Telegram" },
  { value: "discord", label: "Discord" },
  { value: "slack", label: "Slack" },
  { value: "email", label: "Email (Gmail)" },
  { value: "webhook", label: "Custom webhook" },
];

export const CHANNEL_TYPE_LABEL: Record<ChannelType, string> = {
  telegram: "Telegram",
  discord: "Discord",
  slack: "Slack",
  webhook: "Webhook",
  email: "Email",
};

/**
 * One config field, as the form renders it.
 *
 * `secret` fields come back from the server masked rather than in full, so the
 * form leaves a masked value alone and the server keeps whatever is stored.
 */
export interface ChannelField {
  name: string;
  label: string;
  placeholder?: string;
  hint?: string;
  secret?: boolean;
  /** Rendered as a comma-separated text input and sent as an array. */
  list?: boolean;
  optional?: boolean;
}

/**
 * What each transport needs, mirroring the spec structs in
 * `server/internal/notifier/spec.go` — that file is the authority, and it
 * re-validates everything. The `name`s are the JSON keys it decodes.
 */
export const CHANNEL_FIELDS: Record<ChannelType, readonly ChannelField[]> = {
  telegram: [
    {
      name: "token",
      label: "Bot token",
      placeholder: "123456:ABC-DEF…",
      hint: "Create a bot with @BotFather to get one.",
      secret: true,
    },
    {
      name: "chat_id",
      label: "Chat ID",
      placeholder: "-1001234567890",
      hint: "Message @userinfobot to find yours.",
    },
  ],
  discord: [
    {
      name: "webhook_url",
      label: "Webhook URL",
      placeholder: "https://discord.com/api/webhooks/…",
      hint: "Server Settings → Integrations → Webhooks.",
      secret: true,
    },
  ],
  slack: [
    {
      name: "webhook_url",
      label: "Webhook URL",
      placeholder: "https://hooks.slack.com/services/…",
      hint: "Create one at api.slack.com/apps → Incoming Webhooks.",
      secret: true,
    },
  ],
  email: [
    {
      name: "username",
      label: "Username",
      placeholder: "you@gmail.com",
      hint: "Your full email address.",
    },
    {
      name: "app_password",
      label: "App password",
      hint: "Gmail rejects your account password — create an app password at myaccount.google.com/apppasswords.",
      secret: true,
    },
    {
      name: "to",
      label: "Send to",
      placeholder: "you@example.com, ops@example.com",
      hint: "Comma-separated.",
      list: true,
    },
    {
      name: "host",
      label: "SMTP host",
      placeholder: "smtp.gmail.com",
      optional: true,
    },
    {
      name: "port",
      label: "SMTP port",
      placeholder: "587",
      optional: true,
    },
  ],
  webhook: [
    {
      name: "url",
      label: "URL",
      placeholder: "https://example.com/hooks/breckr",
    },
    {
      name: "method",
      label: "Method",
      placeholder: "POST",
      hint: "POST or PUT.",
      optional: true,
    },
  ],
};

/** Masked secrets come back prefixed with this, and are left untouched on save. */
export const MASK_PREFIX = "••••";

// --- Task form -------------------------------------------------------------

/**
 * The extraction kinds, labelled for the form.
 *
 * The server holds the same list in its own `constants/`; both are typed
 * against `ExtractKind`, so adding a kind to the shared contract breaks
 * whichever side has not been updated.
 */
export const EXTRACT_OPTIONS: readonly { value: ExtractKind; label: string }[] =
  [
    { value: "text", label: "Text of the element" },
    { value: "number", label: "Number in the text" },
    { value: "attribute", label: "An attribute" },
    { value: "count", label: "How many match" },
    { value: "exists", label: "Whether it exists" },
  ];

/** What every task did before the mode existed, and what the server defaults to. */
export const DEFAULT_NOTIFY_MODE: NotifyMode = "transition";

/** The alert modes the form offers, in the order they appear. */
export const NOTIFY_MODE_OPTIONS: readonly {
  value: NotifyMode;
  label: string;
}[] = [
  { value: "transition", label: "Once, when it starts matching" },
  { value: "always", label: "Every time it matches" },
];

/**
 * What each mode actually does, shown under the control.
 *
 * Spelled out rather than left to the label: "every time it matches" means
 * every *run*, which is the schedule's frequency — and that is the thing worth
 * knowing before picking it.
 */
export const NOTIFY_MODE_HINTS: Record<NotifyMode, string> = {
  transition:
    "You are alerted once when the condition becomes true, and not again until it goes back to false.",
  always: "You are alerted on every scheduled run where the condition is true.",
};

export const OPERATOR_LABELS: Record<CompareOperator, string> = {
  lt: "is less than",
  lte: "is at most",
  gt: "is greater than",
  gte: "is at least",
  eq: "equals",
  neq: "does not equal",
  contains: "contains",
  not_contains: "does not contain",
  is_true: "is present",
  is_false: "is missing",
  changed: "changed since the last run",
};

/**
 * Which operators the form offers per kind. Mirrors OPERATORS_BY_KIND on the
 * server, which is the authority — this only keeps the user from picking a
 * pairing that would be rejected on save.
 */
export const OPERATORS_BY_KIND: Record<
  ExtractKind,
  readonly CompareOperator[]
> = {
  text: ["eq", "neq", "contains", "not_contains", "changed"],
  number: ["lt", "lte", "gt", "gte", "eq", "neq", "changed"],
  attribute: ["eq", "neq", "contains", "not_contains", "changed"],
  count: ["lt", "lte", "gt", "gte", "eq", "neq", "changed"],
  exists: ["is_true", "is_false", "changed"],
};

/** Operators that test the value alone, so the form hides the value input. */
export const VALUELESS_OPERATORS: readonly CompareOperator[] = [
  "is_true",
  "is_false",
  "changed",
];

// --- Schedule builder ------------------------------------------------------

/** A sensible starting point for a new task: check quarter-hourly. */
export const DEFAULT_SCHEDULE: Schedule = { every: "minutes", interval: 15 };

/**
 * The frequencies the builder offers, in the order they appear.
 *
 * `custom` is last because it is the escape hatch: the server hands one back
 * for any stored expression the other five cannot express, and editing such a
 * task has to leave its cron alone.
 */
export const FREQUENCY_OPTIONS: readonly {
  value: ScheduleFrequency;
  label: string;
}[] = [
  { value: "minutes", label: "Every few minutes" },
  { value: "hours", label: "Every few hours" },
  { value: "day", label: "Every day" },
  { value: "week", label: "Every week" },
  { value: "month", label: "Every month" },
  { value: "custom", label: "Custom cron" },
];

/** Indexed by cron's day numbering, Sunday first. */
export const WEEKDAY_LABELS: readonly string[] = [
  "Sun",
  "Mon",
  "Tue",
  "Wed",
  "Thu",
  "Fri",
  "Sat",
];

/** Upper bound per frequency, matching the server's own range checks. */
export const MAX_INTERVAL: Record<"minutes" | "hours", number> = {
  minutes: 59,
  hours: 23,
};

/**
 * How the form offers to combine conditions. Mirrors MatchModes on the server.
 *
 * `all` is first because it is the default and the one a single-condition task
 * silently uses.
 */
export const MATCH_MODE_OPTIONS: readonly {
  value: MatchMode;
  label: string;
}[] = [
  { value: "all", label: "all of these are true" },
  { value: "any", label: "any of these is true" },
];

/**
 * Matches MaxConditions on the server, which is the authority — this only keeps
 * the form from offering an "Add" the save would reject.
 */
export const MAX_CONDITIONS = 10;

export const DEFAULT_CONDITION: Condition = {
  selector: "",
  extract: "text",
  operator: "changed",
};

export const DEFAULT_SPEC: TaskSpec = {
  url: "",
  match: "all",
  conditions: [DEFAULT_CONDITION],
};

export const THEMES = ["light", "dark"] as const;
export type Theme = (typeof THEMES)[number];

/** broke-ui switches theme off this attribute on any ancestor. */
export const THEME_ATTRIBUTE = "data-theme";
export const THEME_STORAGE_KEY = "breckr-theme";
