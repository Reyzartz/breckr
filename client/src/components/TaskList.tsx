import { Button, Card, Text } from "brake-ui";
import { Plus } from "lucide-react";
import type { TaskWithStatus } from "../types/index.ts";
import { TaskCard } from "./TaskCard.tsx";

interface TaskListProps {
  tasks: TaskWithStatus[];
  onToggle: (id: string, enabled: boolean) => Promise<void>;
  onRunNow: (id: string) => Promise<void>;
  onEdit: (task: TaskWithStatus) => void;
  onDelete: (id: string) => Promise<void>;
  onCreate: () => void;
  isBusy: (id: string) => boolean;
}

export function TaskList({
  tasks,
  onToggle,
  onRunNow,
  onEdit,
  onDelete,
  onCreate,
  isBusy,
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
    /*
      Two columns exactly where the dashboard itself is one. Between md and xl
      the task list has the full page width to itself, and a single column of
      950px-wide cards puts a task's name and its Run button at opposite ends
      of the screen; past xl the dashboard takes the second column back for the
      run panel, so the cards return to one.
    */
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-1">
      {tasks.map((task) => (
        <TaskCard
          key={task.id}
          task={task}
          onToggle={onToggle}
          onRunNow={onRunNow}
          onEdit={onEdit}
          onDelete={onDelete}
          busy={isBusy(task.id)}
        />
      ))}
    </div>
  );
}
