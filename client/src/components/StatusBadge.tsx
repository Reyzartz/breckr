import { Badge } from "brake-ui";
import type { RunStatus } from "../types/index.ts";
import { STATUS_BADGE_VARIANT } from "../constants/index.ts";

interface StatusBadgeProps {
  status: RunStatus | null | undefined;
}

export function StatusBadge({ status }: StatusBadgeProps) {
  if (!status) return <Badge variant="default">never run</Badge>;
  return <Badge variant={STATUS_BADGE_VARIANT[status]}>{status}</Badge>;
}
