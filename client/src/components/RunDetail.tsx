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
import { useRun } from "../hooks/useRuns.ts";
import { StatusBadge } from "./StatusBadge.tsx";
import { NotificationBadge } from "./NotificationBadge.tsx";
import { absoluteTime, duration, prettyJson } from "../utils/format.ts";
import {
  CHANNEL_TYPE_LABEL,
  NOTIFICATION_BADGE_VARIANT,
} from "../constants/index.ts";

interface RunDetailProps {
  run: Run | null;
  onClose: () => void;
}

export function RunDetail({ run, onClose }: RunDetailProps) {
  /**
   * The per-channel breakdown, fetched on open.
   *
   * The run list does not carry attempts — one extra query per row would be paid
   * on every poll to render a badge that does not show them. So the detail view
   * asks for its own, keyed on the run that was clicked; `run` itself already
   * has everything else needed to render immediately.
   *
   * A failed fetch degrades to the aggregate `run` already carries rather than
   * an error over a run that displays fine without it — `attempts` just stays
   * empty.
   */
  const { run: detailed } = useRun(run?.id ?? null);
  const attempts = detailed?.attempts ?? [];

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
            {/*
              With fan-out, the aggregate can only say "something got through".
              Which channel failed is the question you actually have, so the
              per-channel rows come first.
            */}
            {attempts.length > 0 && (
              <div className="mb-2 grid gap-1">
                {attempts.map((attempt) => (
                  <div
                    key={attempt.id}
                    className="flex flex-wrap items-baseline gap-2"
                  >
                    <Badge variant={NOTIFICATION_BADGE_VARIANT[attempt.status]}>
                      {attempt.status}
                    </Badge>
                    <Text as="span">{attempt.channel_name}</Text>
                    <Text variant="caption" color="muted" as="span">
                      {CHANNEL_TYPE_LABEL[attempt.channel_type]}
                      {attempt.channel_id === null && " · deleted since"}
                    </Text>
                    {attempt.detail && (
                      <Text variant="caption" color="error" as="span">
                        {attempt.detail}
                      </Text>
                    )}
                  </div>
                ))}
              </div>
            )}

            {/* The aggregate detail, for the disabled case that has no rows. */}
            {run.notification_detail && attempts?.length === 0 && (
              <pre className="mb-2 max-h-40 overflow-auto rounded-md bg-error-bg p-3 font-mono text-xs whitespace-pre-wrap text-error-text">
                {run.notification_detail}
              </pre>
            )}

            {/*
              The message body is the answer to "was it sent" — you read what
              went out rather than inferring it from a badge.
            */}
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
