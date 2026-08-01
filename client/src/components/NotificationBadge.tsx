import { Badge } from "broke-ui";
import type { Run } from "../types/index.ts";
import {
  NOTIFICATION_BADGE_VARIANT,
  NOTIFICATION_LABEL,
} from "../constants/index.ts";

interface NotificationBadgeProps {
  run: Pick<Run, "notification_status" | "notified">;
}

/**
 * Renders nothing when no alert was owed — the condition did not transition, so
 * there is no delivery to report and a badge would only add noise.
 */
export function NotificationBadge({ run }: NotificationBadgeProps) {
  // Runs recorded before delivery outcomes were persisted carry only the bool.
  // Falling back to it keeps their history labelled rather than silently blank.
  const status = run.notification_status ?? (run.notified ? "sent" : null);
  if (!status) return null;

  return (
    <Badge variant={NOTIFICATION_BADGE_VARIANT[status]}>
      {NOTIFICATION_LABEL[status]}
    </Badge>
  );
}
