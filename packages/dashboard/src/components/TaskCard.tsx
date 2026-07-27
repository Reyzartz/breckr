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
import type { TaskWithStatus } from "@breckr/shared";
import { StatusBadge } from "./StatusBadge.tsx";
import { absoluteTime, firstLine, hostname, timeAgo } from "../utils/format.ts";

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
    status === "failed"
      ? "errored"
      : task.condition_met
        ? "warning"
        : "default";

  return (
    <Card variant={variant}>
      <div className="flex items-center gap-2 mb-2">
        <div className="flex flex-col gap-0.5">
          <Text variant="h5" as="h3">
            {task.name}
          </Text>

          <div className="mt-1 flex items-center gap-2">
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

        <div className="flex shrink-0 items-center gap-3 flex-1 justify-end">
          <Toggle
            checked={enabled}
            // brake-ui's Toggle does not propagate the DOM handler's parameter
            // type through its props, so annotate explicitly.
            onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
              void handleToggle(e)
            }
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

        <ModalFooter className="flex-end flex gap-2">
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

      <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1">
        <Text variant="caption" color="muted">
          <code>{task.id}</code>
        </Text>
        <Text variant="caption" color="muted">
          <code>{task.cron_expr}</code>
        </Text>
        {task.spec && (
          <Text variant="caption" color="muted">
            <span className="truncate" title={task.spec.url}>
              {task.spec.selector} on {hostname(task.spec.url)}
            </span>
          </Text>
        )}
        {task.last_run && (
          <Text variant="caption" color="muted">
            <span title={absoluteTime(task.last_run.started_at)}>
              last run {timeAgo(task.last_run.started_at)}
            </span>
          </Text>
        )}
        <Text variant="caption" color="muted">
          <span
            className="inline-flex items-center gap-1"
            title={absoluteTime(task.next_run)}
          >
            <Clock size={12} aria-hidden="true" />
            {task.next_run ? `next ${timeAgo(task.next_run)}` : "not scheduled"}
          </span>
        </Text>
      </div>

      {task.last_run?.error && (
        <Text variant="caption" color="error">
          <span className="mt-2 block truncate font-mono">
            {firstLine(task.last_run.error)}
          </span>
        </Text>
      )}
    </Card>
  );
}
