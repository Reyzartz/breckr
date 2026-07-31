import { useState } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Alert, Button, Text } from "brake-ui";
import { Plus } from "lucide-react";
import type { Run, TaskWithStatus } from "../types/index.ts";
import { TaskList } from "../components/TaskList.tsx";
import { TaskFormModal } from "../components/TaskFormModal.tsx";
import { RunDetail } from "../components/RunDetail.tsx";
import { RecentRuns } from "../components/RecentRuns.tsx";
import { useTasks } from "../hooks/useTasks.ts";
import { useChannels } from "../hooks/useChannels.ts";
import { useHealth } from "../hooks/useHealth.ts";
import { toErrorMessage } from "../services/api/index.ts";

export const Route = createFileRoute("/")({ component: Dashboard });

/**
 * Enabled tasks that alert nowhere.
 *
 * Orphaned tasks are excluded: they cannot run at all, so a missing channel is
 * not their most pressing problem, and the list already flags them.
 */
function silentTasks(tasks: TaskWithStatus[]): TaskWithStatus[] {
  return tasks.filter(
    (task) => task.enabled && !task.orphaned && task.channel_ids.length === 0
  );
}

function Dashboard() {
  const navigate = useNavigate();

  const {
    tasks,
    error: tasksError,
    isTaskBusy,
    toggleTaskEnabled,
    runTaskNow,
    deleteTask,
    createTask,
    updateTask,
  } = useTasks();
  const { channels, error: channelsError } = useChannels();
  const { health, error: healthError } = useHealth();

  const [selectedRun, setSelectedRun] = useState<Run | null>(null);
  // Null while closed. Editing carries the task; creating carries "new", which
  // distinguishes it from closed without a second flag.
  const [editing, setEditing] = useState<TaskWithStatus | "new" | null>(null);

  const pageError =
    tasksError ?? channelsError ?? (healthError ? toErrorMessage(healthError) : null);

  const openChannels = () => {
    setEditing(null);
    void navigate({ to: "/channels" });
  };

  return (
    <div className="flex h-full flex-col">
      {pageError && (
        <div className="mb-4">
          <Alert variant="error">{pageError}</Alert>
        </div>
      )}

      {health && !health.browser.reachable && (
        <div className="mb-4">
          <Alert variant="warning">
            Browser not reachable at <code>{health.browser.endpoint}</code> —
            tasks that need a page will fail. Start it with{" "}
            <code>docker compose up -d</code>. Browserless tasks still run.
          </Alert>
        </div>
      )}

      {/*
        A monitor that quietly never alerts is the failure this app exists to
        avoid, so say so up front rather than at the moment an alert was owed
        and did not arrive.
      */}
      {health && !health.notifications.configured && (
        <div className="mb-4">
          <Alert variant="warning">
            <span className="flex flex-wrap items-baseline gap-x-2">
              <span>
                No notification channels — conditions will still be checked
                and recorded, but no alert will be sent.
              </span>
              <button
                type="button"
                className="cursor-pointer underline underline-offset-2"
                onClick={openChannels}
              >
                Add a channel
              </button>
            </span>
          </Alert>
        </div>
      )}

      {/*
        A task that alerts nowhere is a silent monitor, which is the same
        failure as having no channels at all — just scoped to one task.
      */}
      {health?.notifications.configured && silentTasks(tasks).length > 0 && (
        <div className="mb-4">
          <Alert variant="warning">
            No channels selected for{" "}
            {silentTasks(tasks)
              .map((task) => task.name)
              .join(", ")}{" "}
            — {silentTasks(tasks).length === 1 ? "it" : "they"} will never
            alert. Edit the task to pick one.
          </Alert>
        </div>
      )}

      <div
        className="grid flex-1 gap-6 overflow-hidden"
        style={{ gridTemplateColumns: "repeat(auto-fit, minmax(400px, 1fr))" }}
      >
        <section>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Text variant="h4" as="h2">
              Tasks
            </Text>
            <Button
              size="sm"
              icon={Plus}
              onClick={() => {
                setEditing("new");
              }}
            >
              Add task
            </Button>
          </div>
          <div className="mt-3">
            <TaskList
              tasks={tasks}
              onToggle={toggleTaskEnabled}
              onRunNow={runTaskNow}
              onEdit={setEditing}
              onDelete={deleteTask}
              onCreate={() => {
                setEditing("new");
              }}
              isBusy={isTaskBusy}
            />
          </div>
        </section>

        <RecentRuns onSelectRun={setSelectedRun} />
      </div>

      <TaskFormModal
        isOpen={editing !== null}
        task={editing === "new" ? null : editing}
        channels={channels}
        onClose={() => {
          setEditing(null);
        }}
        onCreate={async (input) => {
          await createTask(input);
        }}
        onSave={async (id, patch) => {
          await updateTask(id, patch);
        }}
        onManageChannels={openChannels}
      />

      <RunDetail
        run={selectedRun}
        onClose={() => {
          setSelectedRun(null);
        }}
      />
    </div>
  );
}
