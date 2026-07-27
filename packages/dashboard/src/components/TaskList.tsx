import { Button, Card, Text } from "brake-ui";
import { Plus } from "lucide-react";
import type { TaskWithStatus } from "@breckr/shared";
import { TaskCard } from "./TaskCard.tsx";

interface TaskListProps {
  tasks: TaskWithStatus[];
  onToggle: (id: string, enabled: boolean) => Promise<void>;
  onRunNow: (id: string) => Promise<void>;
  onEdit: (task: TaskWithStatus) => void;
  onDelete: (id: string) => Promise<void>;
  onCreate: () => void;
  busyTaskId: string | null;
}

export function TaskList({
  tasks,
  onToggle,
  onRunNow,
  onEdit,
  onDelete,
  onCreate,
  busyTaskId,
}: TaskListProps) {
  if (tasks.length === 0) {
    return (
      <Card>
        <div className="flex flex-col items-start gap-3">
          <Text color="muted">
            No tasks yet. A task watches one thing on one page — a price, a stock
            label, whether an element appears — and alerts you when it changes or
            crosses a threshold.
          </Text>
          <Button icon={Plus} onClick={onCreate}>
            Add your first task
          </Button>
        </div>
      </Card>
    );
  }

  return (
    <div className="grid gap-3">
      {tasks.map((task) => (
        <TaskCard
          key={task.id}
          task={task}
          onToggle={onToggle}
          onRunNow={onRunNow}
          onEdit={onEdit}
          onDelete={onDelete}
          busy={busyTaskId === task.id}
        />
      ))}
    </div>
  );
}
