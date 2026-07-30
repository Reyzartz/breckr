import { useEffect, useState } from "react";
import {
  Alert,
  Badge,
  Button,
  ConfirmActionButton,
  Divider,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Select,
  Text,
  Toggle,
} from "brake-ui";
import { Bell, FlaskConical, Plus, SquarePen, Trash2 } from "lucide-react";
import type {
  Channel,
  ChannelType,
  CreateChannelRequest,
  TestNotificationResponse,
  UpdateChannelRequest,
} from "../types/index.ts";
import { testDraftChannel } from "../services/monitor.service.ts";
import { ApiError, toErrorMessage } from "../apis/client.ts";
import {
  CHANNEL_FIELDS,
  CHANNEL_TYPE_LABEL,
  CHANNEL_TYPE_OPTIONS,
  MASK_PREFIX,
  type ChannelField,
} from "../constants/index.ts";

interface ChannelsModalProps {
  isOpen: boolean;
  channels: Channel[];
  onClose: () => void;
  onCreate: (input: CreateChannelRequest) => Promise<void>;
  onSave: (id: string, patch: UpdateChannelRequest) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
  onTest: (id: string) => Promise<void>;
  /** Id of the channel currently being tested, or null. */
  testingChannelId: string | null;
  /** Outcome of the last test, wherever it was started from. */
  notificationTest: TestNotificationResponse | null;
  onDismissTest: () => void;
}

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

/**
 * Manage delivery destinations.
 *
 * A modal rather than a page because the dashboard has no router — the same
 * `editing: Channel | "new" | null` shape the task list already uses.
 */
export function ChannelsModal({
  isOpen,
  channels,
  onClose,
  onCreate,
  onSave,
  onDelete,
  onTest,
  testingChannelId,
  notificationTest,
  onDismissTest,
}: ChannelsModalProps) {
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

  // Closing and reopening must not leave the last channel's credentials in the
  // form.
  useEffect(() => {
    if (!isOpen) return;
    setEditing(null);
    setFieldError(null);
    setFormError(null);
  }, [isOpen]);

  const openEditor = (target: Channel | "new") => {
    setEditing(target);
    setFieldError(null);
    setFormError(null);
    setDraftTest(null);
    onDismissTest();

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
    if (editing === null) return;

    setSaving(true);
    setFieldError(null);
    setFormError(null);

    try {
      if (editing === "new") {
        await onCreate({
          name: name.trim(),
          type,
          config: toConfig(type, draft),
          enabled,
        });
      } else {
        await onSave(editing.id, {
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
    onDismissTest();

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
    <Modal isOpen={isOpen} onClose={onClose} maxWidth="lg">
      <ModalHeader title="Notification channels" icon={Bell} />

      <ModalBody>
        <div className="grid max-h-[60vh] gap-4 overflow-y-auto pr-1">
          {notificationTest && (
            <Alert variant={notificationTest.ok ? "success" : "error"}>
              <span className="flex flex-wrap items-baseline gap-x-2">
                <span>
                  {notificationTest.ok
                    ? "Test delivered — check the destination."
                    : `Test not delivered (${notificationTest.status}). ${notificationTest.detail ?? ""}`}
                </span>
                <button
                  type="button"
                  className="cursor-pointer underline underline-offset-2"
                  onClick={onDismissTest}
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
                <div className="grid gap-2">
                  {channels.map((channel) => (
                    <div
                      key={channel.id}
                      className="flex flex-wrap items-center justify-between gap-2"
                    >
                      <div className="grid">
                        <span className="flex items-center gap-2">
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

                      <div className="flex items-center gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          icon={FlaskConical}
                          disabled={testingChannelId !== null}
                          onClick={() => void onTest(channel.id)}
                        >
                          {testingChannelId === channel.id ? "Sending…" : "Test"}
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
                          onConfirm={() => void onDelete(channel.id)}
                          aria-label={`Delete ${channel.name}`}
                        />
                      </div>
                    </div>
                  ))}
                </div>
              )}

              <Divider spacing="sm" />

              <Button
                size="sm"
                icon={Plus}
                onClick={() => {
                  openEditor("new");
                }}
              >
                Add channel
              </Button>
            </>
          ) : (
            <>
              <div className="grid gap-4 sm:grid-cols-2">
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

              <div className="grid gap-1">
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
                  {draftTest.ok
                    ? "Test delivered — check the destination."
                    : `Test not delivered (${draftTest.status}). ${draftTest.detail ?? ""}`}
                </Alert>
              )}
            </>
          )}
        </div>
      </ModalBody>

      <ModalFooter>
        {editing === null ? (
          <div className="flex w-full justify-end">
            <Button variant="ghost" onClick={onClose}>
              Done
            </Button>
          </div>
        ) : (
          <div className="flex w-full flex-wrap items-center justify-between gap-2">
            <Button
              variant="outlined"
              icon={FlaskConical}
              onClick={() => void handleTestDraft()}
              disabled={testingDraft || saving}
            >
              {testingDraft ? "Testing…" : "Send test"}
            </Button>

            <div className="flex items-center gap-2">
              <Button variant="ghost" onClick={closeEditor} disabled={saving}>
                Cancel
              </Button>
              <Button
                onClick={() => void handleSubmit()}
                disabled={saving || testingDraft}
              >
                {saving
                  ? "Saving…"
                  : editing === "new"
                    ? "Create channel"
                    : "Save changes"}
              </Button>
            </div>
          </div>
        )}
      </ModalFooter>
    </Modal>
  );
}
