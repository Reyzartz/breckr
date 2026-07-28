import {
  Badge,
  Button,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Text,
} from "brake-ui";
import type { Run } from "../types/index.ts";
import { StatusBadge } from "./StatusBadge.tsx";
import { NotificationBadge } from "./NotificationBadge.tsx";
import { absoluteTime, duration, prettyJson } from "../utils/format.ts";

interface RunDetailProps {
  run: Run | null;
  onClose: () => void;
}

export function RunDetail({ run, onClose }: RunDetailProps) {
  if (!run) return null;

  return (
    <Modal isOpen onClose={onClose} maxWidth="lg">
      <ModalHeader title={`Run #${String(run.id)} — ${run.task_name ?? run.task_id}`} />

      <ModalBody>
        <div className="flex flex-wrap items-center gap-2">
          <StatusBadge status={run.status} />
          {run.condition_met && <Badge variant="info">condition met</Badge>}
          <NotificationBadge run={run} />
          <Badge variant="default">{run.trigger_source}</Badge>
        </div>

        <dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5 text-sm">
          <Field label="Started" value={absoluteTime(run.started_at)} />
          <Field label="Finished" value={absoluteTime(run.finished_at)} />
          <Field
            label="Duration"
            value={duration(run.started_at, run.finished_at) ?? "—"}
          />
          <Field label="Task ID" value={run.task_id} mono />
        </dl>

        {run.result_summary && (
          <Section title="Result">
            <pre className="max-h-72 overflow-auto rounded-md bg-background-secondary p-3 font-mono text-xs whitespace-pre-wrap text-text">
              {prettyJson(run.result_summary)}
            </pre>
          </Section>
        )}

        {run.error && (
          <Section title="Error">
            <pre className="max-h-72 overflow-auto rounded-md bg-error-bg p-3 font-mono text-xs whitespace-pre-wrap text-error-text">
              {run.error}
            </pre>
          </Section>
        )}

        {/*
          Only shown when an alert was actually owed. The message body is the
          answer to "was it sent" — you read what went out rather than inferring
          it from a badge.
        */}
        {run.notification_status && (
          <Section title="Notification">
            {run.notification_detail && (
              <pre className="mb-2 max-h-40 overflow-auto rounded-md bg-error-bg p-3 font-mono text-xs whitespace-pre-wrap text-error-text">
                {run.notification_detail}
              </pre>
            )}
            {run.notification_message && (
              <pre className="max-h-40 overflow-auto rounded-md bg-background-secondary p-3 font-mono text-xs whitespace-pre-wrap text-text">
                {run.notification_message}
              </pre>
            )}
          </Section>
        )}
      </ModalBody>

      <ModalFooter>
        <Button variant="outlined" onClick={onClose}>
          Close
        </Button>
      </ModalFooter>
    </Modal>
  );
}

function Field({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <>
      <dt className="text-text-muted">{label}</dt>
      <dd className={mono ? "font-mono" : undefined}>{value}</dd>
    </>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mt-4">
      <Text variant="caption" color="muted">
        {title}
      </Text>
      <div className="mt-1">{children}</div>
    </div>
  );
}
