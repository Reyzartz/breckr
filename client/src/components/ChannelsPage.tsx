import { useState } from "react";
import { Link } from "@tanstack/react-router";
import {
  Alert,
  Badge,
  Button,
  Card,
  ConfirmActionButton,
  Divider,
  Input,
  Select,
  Text,
  Toggle,
} from "brake-ui";
import { ArrowLeft, Bell, FlaskConical, Plus, SquarePen, Trash2 } from "lucide-react";
import type { Channel, ChannelType, TestNotificationResponse } from "../types/index.ts";
import { useChannels } from "../hooks/useChannels.ts";
import { ApiError, toErrorMessage } from "../services/api/index.ts";
import {
  CHANNEL_FIELDS,
  CHANNEL_TYPE_LABEL,
  CHANNEL_TYPE_OPTIONS,
  MASK_PREFIX,
  type ChannelField,
} from "../constants/index.ts";

/** Every config field as a string, which is what the inputs edit. */
type ConfigDraft = Record<string, string>;

/**
 * Seed the form from a channel's redacted config.
 *
 * Secrets arrive masked, and the mask is kept as the field's value on purpose:
 * it shows a credential is set, and the server treats a still-masked field as
 * "keep what is stored".
 */
function toConfigDraft(type: ChannelType, config: Record<string, unknown>): ConfigDraft {
  const draft: ConfigDraft = {};

  for (const field of CHANNEL_FIELDS[type]) {
    const value = config[field.name];
    draft[field.name] = Array.isArray(value)
      ? value.join(", ")
      : value == null
        ? ""
        : String(value);
  }

  return draft;
}

/**
 * Turn the draft back into the config the server decodes.
 *
 * Untouched masked secrets and blank optionals are dropped rather than sent: the
 * former would overwrite a working credential with a row of dots, and the latter
 * would override a server-side default with "".
 */
function toConfig(type: ChannelType, draft: ConfigDraft): Record<string, unknown> {
  const config: Record<string, unknown> = {};

  for (const field of CHANNEL_FIELDS[type]) {
    const raw = (draft[field.name] ?? "").trim();

    if (field.secret && raw.startsWith(MASK_PREFIX)) continue;
    if (raw === "" && (field.optional || field.secret)) continue;

    if (field.list) {
      config[field.name] = raw
        .split(",")
        .map((part) => part.trim())
        .filter(Boolean);
      continue;
    }

    // Ports are numbers in the spec struct; everything else is a string.
    config[field.name] = field.name === "port" ? Number(raw) : raw;
  }

  return config;
}

function outcomeMessage(outcome: TestNotificationResponse): string {
  return outcome.ok
    ? "Test delivered — check the destination."
    : `Test not delivered (${outcome.status}). ${outcome.detail ?? ""}`;
}

/** Manage delivery destinations. */
export function ChannelsPage() {
  const {
    channels,
    error,
    createChannel,
    updateChannel,
    deleteChannel,
    testChannel,
    channelBeingTested,
    testResult,
    dismissTestResult,
    testDraftChannel,
  } = useChannels();

  const [editing, setEditing] = useState<Channel | "new" | null>(null);
  const [name, setName] = useState("");
  const [type, setType] = useState<ChannelType>("telegram");
  const [draft, setDraft] = useState<ConfigDraft>({});
  const [enabled, setEnabled] = useState(true);
  const [fieldError, setFieldError] = useState<{
    field: string;
    message: string;
  } | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [testingDraft, setTestingDraft] = useState(false);
  /** Outcome of testing the *unsaved* form, kept separate from the list's test. */
  const [draftTest, setDraftTest] = useState<TestNotificationResponse | null>(
    null
  );

  const openEditor = (target: Channel | "new") => {
    setEditing(target);
    setFieldError(null);
    setFormError(null);
    setDraftTest(null);
    dismissTestResult();

    if (target === "new") {
      setName("");
      setType("telegram");
      setDraft({});
      setEnabled(true);
      return;
    }

    setName(target.name);
    setType(target.type);
    setDraft(toConfigDraft(target.type, target.config));
    setEnabled(target.enabled);
  };

  const closeEditor = () => {
    setEditing(null);
    setFieldError(null);
    setFormError(null);
  };

  const setField = (field: string, value: string) => {
    setDraft((current) => ({ ...current, [field]: value }));
    setFieldError((current) =>
      current?.field === `config.${field}` ? null : current
    );
  };

  const errorFor = (field: string): string | undefined =>
    fieldError?.field === field ? fieldError.message : undefined;

  /** Server field paths are dotted (`config.token`); inputs are named bare. */
  const configErrorFor = (field: string): string | undefined =>
    errorFor(`config.${field}`);

  const handleError = (err: unknown) => {
    if (err instanceof ApiError && err.field) {
      setFieldError({ field: err.field, message: err.message });
    } else {
      setFormError(toErrorMessage(err));
    }
  };

  const handleSubmit = async () => {
    setSaving(true);
    setFieldError(null);
    setFormError(null);

    try {
      if (editing === "new") {
        await createChannel({
          name: name.trim(),
          type,
          config: toConfig(type, draft),
          enabled,
        });
      } else if (editing) {
        await updateChannel(editing.id, {
          name: name.trim(),
          config: toConfig(type, draft),
          enabled,
        });
      }
      closeEditor();
    } catch (err) {
      handleError(err);
    } finally {
      setSaving(false);
    }
  };

  /**
   * Test the config in the form rather than the saved one, so a wrong token is
   * caught before it is stored and while the field is still in front of you.
   */
  const handleTestDraft = async () => {
    setTestingDraft(true);
    setFieldError(null);
    setFormError(null);
    dismissTestResult();

    try {
      setDraftTest(await testDraftChannel({ type, config: toConfig(type, draft) }));
    } catch (err) {
      handleError(err);
    } finally {
      setTestingDraft(false);
    }
  };

  const renderField = (field: ChannelField) => (
    <Input
      key={field.name}
      label={field.optional ? `${field.label} (optional)` : field.label}
      value={draft[field.name] ?? ""}
      onChange={(event: React.ChangeEvent<HTMLInputElement>) => {
        setField(field.name, event.target.value);
      }}
      error={configErrorFor(field.name)}
      placeholder={field.placeholder}
      info={field.hint}
      fullWidth
    />
  );

  return (
    <div className="flex h-full flex-col gap-3 sm:gap-4">
      <div className="flex items-center gap-3">
        <Link
          to="/"
          className="flex items-center gap-1 py-1 text-sm text-text-muted transition-colors hover:text-text"
        >
          <ArrowLeft size={14} aria-hidden="true" />
          Dashboard
        </Link>
      </div>

      <div className="flex items-center gap-2">
        <Bell size={20} aria-hidden="true" className="shrink-0" />
        <Text variant="h3" as="h2">
          Notification channels
        </Text>
      </div>

      {error && <Alert variant="error">{error}</Alert>}

      <Card size="lg" className="max-w-2xl">
        <div className="grid grid-cols-1 gap-4">
          {testResult && (
            <Alert variant={testResult.ok ? "success" : "error"}>
              <span className="flex flex-wrap items-baseline gap-x-2">
                <span>{outcomeMessage(testResult)}</span>
                <button
                  type="button"
                  className="cursor-pointer underline underline-offset-2"
                  onClick={dismissTestResult}
                >
                  Dismiss
                </button>
              </span>
            </Alert>
          )}

          {editing === null ? (
            <>
              {channels.length === 0 ? (
                <Text variant="caption" color="muted">
                  No channels yet. Tasks will check their conditions and record
                  history, but nothing will be sent anywhere.
                </Text>
              ) : (
                <div className="grid grid-cols-1 gap-2">
                  {channels.map((channel) => (
                    /*
                      A channel is a name plus three icon actions. Below sm the
                      two halves stack and the actions sit at the left edge,
                      which keeps them a full-size row rather than three ghost
                      buttons squeezed against the right margin.
                    */
                    <div
                      key={channel.id}
                      className="flex flex-col gap-2 border-b border-border pb-2 last:border-b-0 last:pb-0 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between sm:border-b-0 sm:pb-0"
                    >
                      <div className="grid min-w-0 grid-cols-1">
                        <span className="flex flex-wrap items-center gap-2">
                          <Text>{channel.name}</Text>
                          <Badge variant="default">
                            {CHANNEL_TYPE_LABEL[channel.type]}
                          </Badge>
                          {!channel.enabled && (
                            <Badge variant="default">muted</Badge>
                          )}
                          {channel.broken && (
                            <Badge variant="error">needs credentials</Badge>
                          )}
                        </span>
                        {channel.broken && (
                          <Text variant="caption" color="error">
                            Its stored credentials could not be read — re-enter
                            them.
                          </Text>
                        )}
                      </div>

                      <div className="flex shrink-0 items-center gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          icon={FlaskConical}
                          disabled={channelBeingTested !== null}
                          onClick={() => {
                            testChannel(channel.id);
                          }}
                        >
                          {channelBeingTested === channel.id ? "Sending…" : "Test"}
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          icon={SquarePen}
                          onClick={() => {
                            openEditor(channel);
                          }}
                          aria-label={`Edit ${channel.name}`}
                        />
                        <ConfirmActionButton
                          size="sm"
                          variant="ghost"
                          color="danger"
                          icon={Trash2}
                          title={`Delete ${channel.name}?`}
                          message="Any task alerting only through this channel will stop alerting. Its run history is kept."
                          confirmText="Delete"
                          isDestructiveAction
                          onConfirm={() => void deleteChannel(channel.id)}
                          aria-label={`Delete ${channel.name}`}
                        />
                      </div>
                    </div>
                  ))}
                </div>
              )}

              <Divider spacing="sm" />

              <div>
                <Button
                  size="sm"
                  icon={Plus}
                  onClick={() => {
                    openEditor("new");
                  }}
                >
                  Add channel
                </Button>
              </div>
            </>
          ) : (
            <>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <Input
                  label="Name"
                  value={name}
                  onChange={(event: React.ChangeEvent<HTMLInputElement>) => {
                    setName(event.target.value);
                    setFieldError((current) =>
                      current?.field === "name" ? null : current
                    );
                  }}
                  error={errorFor("name")}
                  placeholder="Team Slack"
                  info="How it appears when picking channels for a task."
                  fullWidth
                />

                <Select
                  label="Type"
                  value={type}
                  onChange={(event: React.ChangeEvent<HTMLSelectElement>) => {
                    // Each type has its own fields, so the draft cannot carry
                    // over — a Slack URL is not a Telegram token.
                    setType(event.target.value as ChannelType);
                    setDraft({});
                    setFieldError(null);
                    setDraftTest(null);
                  }}
                  error={errorFor("type")}
                  disabled={editing !== "new"}
                  info={
                    editing === "new"
                      ? undefined
                      : "Create a new channel to use a different transport."
                  }
                  fullWidth
                >
                  {CHANNEL_TYPE_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </Select>
              </div>

              {CHANNEL_FIELDS[type].map(renderField)}

              <div className="grid grid-cols-1 gap-1">
                <Toggle
                  label="Enabled"
                  checked={enabled}
                  onChange={() => {
                    setEnabled((current) => !current);
                  }}
                />
                <Text variant="caption" color="muted">
                  A muted channel stays attached to its tasks but is skipped when
                  alerting.
                </Text>
              </div>

              {formError && <Alert variant="error">{formError}</Alert>}

              {draftTest && (
                <Alert variant={draftTest.ok ? "success" : "error"}>
                  {outcomeMessage(draftTest)}
                </Alert>
              )}

              {/* Same footer shape as the task form, for the same reason. */}
              <div className="flex w-full flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
                <Button
                  variant="outlined"
                  icon={FlaskConical}
                  onClick={() => void handleTestDraft()}
                  disabled={testingDraft || saving}
                  fullWidth
                  className="sm:w-auto"
                >
                  {testingDraft ? "Testing…" : "Send test"}
                </Button>

                <div className="flex items-center gap-2">
                  <Button
                    variant="ghost"
                    onClick={closeEditor}
                    disabled={saving}
                    fullWidth
                    className="sm:w-auto"
                  >
                    Cancel
                  </Button>
                  <Button
                    onClick={() => void handleSubmit()}
                    disabled={saving || testingDraft}
                    fullWidth
                    className="sm:w-auto"
                  >
                    {saving
                      ? "Saving…"
                      : editing === "new"
                        ? "Create channel"
                        : "Save changes"}
                  </Button>
                </div>
              </div>
            </>
          )}
        </div>
      </Card>
    </div>
  );
}
