import { useState } from "react";
import {
  Badge,
  Button,
  Card,
  Dropdown,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Text,
  Toggle,
} from "brake-ui";
import {
  BellRing,
  Clock,
  EllipsisVertical,
  Play,
  SquarePen,
  Trash2,
} from "lucide-react";
import type { TaskWithStatus } from "../types/index.ts";
import { StatusBadge } from "./StatusBadge.tsx";
import {
  absoluteTime,
  describeSchedule,
  firstLine,
  hostname,
  timeAgo,
} from "../utils/format.ts";

interface TaskCardProps {
  task: TaskWithStatus;
  onToggle: (id: string, enabled: boolean) => Promise<void>;
  onRunNow: (id: string) => Promise<void>;
  onEdit: (task: TaskWithStatus) => void;
  onDelete: (id: string) => Promise<void>;
  busy: boolean;
}

export function TaskCard({
  task,
  onToggle,
  onRunNow,
  onEdit,
  onDelete,
  busy,
}: TaskCardProps) {
  const [pendingEnabled, setPendingEnabled] = useState<boolean | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  // Reflect the click immediately, then defer to the server's answer.
  const enabled = pendingEnabled ?? task.enabled;

  const handleToggle = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const next = event.target.checked;
    setPendingEnabled(next);
    try {
      await onToggle(task.id, next);
    } finally {
      setPendingEnabled(null);
    }
  };

  const status = task.last_run?.status;
  const variant =
    status === "failed" ? "errored" : task.condition_met ? "warning" : "default";

  return (
    <Card variant={variant}>
      {/*
        Title and the overflow menu share the top line at every width — the
        menu is the one control that never changes size, so anchoring it top
        right keeps a column of them aligned down the list.
      */}
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 flex-col gap-1.5">
          <Text variant="h5" as="h3" className="break-words">
            {task.name}
          </Text>

          <div className="flex flex-wrap items-center gap-2">
            <StatusBadge status={status} />

            {task.condition_met && (
              <Badge variant="warning">
                <span className="inline-flex items-center gap-1">
                  <BellRing size={12} aria-hidden="true" />
                  condition met
                </span>
              </Badge>
            )}

            {task.orphaned && <Badge variant="error">no definition</Badge>}
          </div>
        </div>

        <Dropdown
          trigger={
            <Button
              size="sm"
              variant="ghost"
              icon={EllipsisVertical}
              disabled={busy}
              aria-label={`More actions for ${task.name}`}
            />
          }
          items={[
            // A row with no definition has nothing to edit; it can only
            // be deleted, which is why that item is omitted here.
            ...(task.orphaned
              ? []
              : [
                  {
                    label: "Edit",
                    icon: SquarePen,
                    onClick: () => onEdit(task),
                  },
                ]),
            {
              label: "Delete",
              icon: Trash2,
              variant: "danger" as const,
              onClick: () => setConfirmingDelete(true),
            },
          ]}
        />
      </div>

      <Modal
        isOpen={confirmingDelete}
        onClose={() => setConfirmingDelete(false)}
        maxWidth="sm"
      >
        <ModalHeader title={`Delete ${task.name}?`} />

        <ModalBody borderless>
          <Text variant="body" color="muted">
            This also deletes its run history. It cannot be undone.
          </Text>
        </ModalBody>

        {/* One row — ModalFooter clips anything past max-h-14. */}
        <ModalFooter className="flex justify-end gap-2">
          <Button
            onClick={() => setConfirmingDelete(false)}
            color="secondary"
            variant="text"
          >
            Cancel
          </Button>

          <Button
            onClick={() => {
              setConfirmingDelete(false);
              void onDelete(task.id);
            }}
            color="danger"
          >
            Delete
          </Button>
        </ModalFooter>
      </Modal>

      {/*
        Sentence case, not the uppercase caption the rest of the app uses for
        labels: these are facts about the task, and a phone-width column of
        tracked-out capitals is markedly slower to scan.
      */}
      <div className="mt-3 flex flex-col gap-1">
        <Text variant="small" color="muted" as="div">
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span>
              {/* A custom schedule has only its expression to show, so it keeps
                  the monospace the other five no longer need. */}
              {task.schedule.every === "custom" ? (
                <code>{task.cron_expr}</code>
              ) : (
                describeSchedule(task.schedule)
              )}
            </span>

            <span
              className="inline-flex items-center gap-1"
              title={absoluteTime(task.next_run)}
            >
              <Clock size={12} aria-hidden="true" />
              {task.next_run ? `next ${timeAgo(task.next_run)}` : "not scheduled"}
            </span>

            {task.last_run && (
              <span title={absoluteTime(task.last_run.started_at)}>
                last run {timeAgo(task.last_run.started_at)}
              </span>
            )}
          </span>
        </Text>

        {task.spec && (
          <Text variant="small" color="muted" as="div" className="min-w-0">
            {/* The first selector, plus a count — a card is a summary, and the
                full list is one click away in the form. */}
            <span className="block truncate font-mono" title={task.spec.url}>
              {task.spec.conditions[0]?.selector} on {hostname(task.spec.url)}
              {task.spec.conditions.length > 1 &&
                ` +${task.spec.conditions.length - 1} more`}
            </span>
          </Text>
        )}

        {/*
          The id only matters when you are reaching for the API or matching a
          log line, neither of which happens on a phone — and on a card that
          already shows the name it is the least useful line present.
        */}
        <Text variant="small" color="muted" as="div" className="hidden sm:block">
          <code>{task.id}</code>
        </Text>
      </div>

      {task.last_run?.error && (
        <Text variant="small" color="error" as="div">
          <span className="mt-2 block truncate font-mono">
            {firstLine(task.last_run.error)}
          </span>
        </Text>
      )}

      {/*
        The two controls that change something live in a footer, split apart
        and given a rule above them: on a phone this is the row the thumb goes
        for, and it should not be mistakable for the metadata above it.
      */}
      <div className="mt-3 flex items-center justify-between gap-3 border-t border-border pt-3">
        <Toggle
          checked={enabled}
          // brake-ui's Toggle does not propagate the DOM handler's parameter
          // type through its props, so annotate explicitly.
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => void handleToggle(e)}
          disabled={task.orphaned}
          label={enabled ? "Enabled" : "Disabled"}
          size="sm"
        />
        <Button
          size="sm"
          variant="outlined"
          icon={Play}
          onClick={() => void onRunNow(task.id)}
          disabled={busy || task.orphaned}
        >
          {busy ? "Running…" : "Run now"}
        </Button>
      </div>
    </Card>
  );
}
