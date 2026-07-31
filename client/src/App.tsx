import { useState } from "react";
import { Alert, Button, Divider, Text } from "brake-ui";
import { Bell, Moon, Plus, RefreshCw, Sun, WifiOff } from "lucide-react";
import type { Run, TaskWithStatus } from "./types/index.ts";
import { TaskList } from "./components/TaskList.tsx";
import { TaskFormModal } from "./components/TaskFormModal.tsx";
import { ChannelsModal } from "./components/ChannelsModal.tsx";
import { RunHistory } from "./components/RunHistory.tsx";
import { RunDetail } from "./components/RunDetail.tsx";
import { useMonitorData } from "./hooks/useMonitorData.ts";
import { useRunFilters } from "./hooks/useRunFilters.ts";
import { useTheme } from "./hooks/useTheme.ts";

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

export default function App() {
  const { theme, toggleTheme } = useTheme();
  const { filters, setFilter, nextPage, previousPage } = useRunFilters();
  const {
    tasks,
    runs,
    health,
    channels,
    error,
    loading,
    connection,
    isTaskBusy,
    testingChannelId,
    notificationTest,
    refresh,
    toggleTask,
    runNow,
    runChannelTest,
    dismissNotificationTest,
    addTask,
    saveTask,
    removeTask,
    addChannel,
    saveChannel,
    removeChannel,
  } = useMonitorData(filters);

  const [selectedRun, setSelectedRun] = useState<Run | null>(null);

  // Null while closed. Editing carries the task; creating carries "new", which
  // distinguishes it from closed without a second flag.
  const [editing, setEditing] = useState<TaskWithStatus | "new" | null>(null);
  const [managingChannels, setManagingChannels] = useState(false);

  const openCreate = () => {
    setEditing("new");
  };

  const openChannels = () => {
    setManagingChannels(true);
  };

  return (
    <div className="mx-auto px-10 py-6 flex flex-col h-screen">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <Text variant="h2" as="h1">
            Web Task Monitor
          </Text>
          <Text variant="caption" color="muted">
            Scheduled browser checks, conditions, and alerts
          </Text>
        </div>

        <div className="flex items-center gap-2">
          {/*
            Shown only while disconnected. The dashboard has no polling loop, so
            a dropped socket means what is on screen has stopped updating —
            which the user has no other way to tell.
          */}
          {connection !== "open" && (
            <Text variant="caption" color="muted">
              <span className="inline-flex items-center gap-1.5">
                <WifiOff size={12} aria-hidden="true" />
                {connection === "connecting" ? "Connecting…" : "Reconnecting…"}
              </span>
            </Text>
          )}
          <Button size="sm" variant="ghost" icon={Bell} onClick={openChannels}>
            Channels
            {channels.length > 0 && ` (${String(channels.length)})`}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            icon={RefreshCw}
            onClick={() => void refresh()}
          >
            Refresh
          </Button>
          <Button
            size="sm"
            variant="ghost"
            icon={theme === "dark" ? Sun : Moon}
            onClick={toggleTheme}
            aria-label="Toggle theme"
          />
        </div>
      </header>

      <Divider spacing="md" />

      <div
        className="grid gap-6 flex-1 overflow-hidden"
        style={{
          gridTemplateColumns: "repeat( auto-fit, minmax(400px, 1fr) )",
        }}
      >
        <div>
          {error && (
            <div className="mb-4">
              <Alert variant="error">{error}</Alert>
            </div>
          )}

          {health && !health.browser.reachable && (
            <div className="mb-4">
              <Alert variant="warning">
                Browser not reachable at <code>{health.browser.endpoint}</code>{" "}
                — tasks that need a page will fail. Start it with{" "}
                <code>docker compose up -d</code>. Browserless tasks still run.
              </Alert>
            </div>
          )}

          {/*
            A monitor that quietly never alerts is the failure this app exists
            to avoid, so say so up front rather than at the moment an alert was
            owed and did not arrive.
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
          {health?.notifications.configured &&
            silentTasks(tasks).length > 0 && (
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

          <section>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <Text variant="h4" as="h2">
                Tasks
              </Text>
              <Button size="sm" icon={Plus} onClick={openCreate}>
                Add task
              </Button>
            </div>
            <div className="mt-3">
              <TaskList
                tasks={tasks}
                onToggle={toggleTask}
                onRunNow={runNow}
                onEdit={setEditing}
                onDelete={removeTask}
                onCreate={openCreate}
                isTaskBusy={isTaskBusy}
              />
            </div>
          </section>
        </div>

        <RunHistory
          data={runs}
          tasks={tasks}
          filters={filters}
          onFilterChange={setFilter}
          onNextPage={nextPage}
          onPreviousPage={previousPage}
          onSelectRun={setSelectedRun}
          loading={loading}
        />
      </div>

      <TaskFormModal
        isOpen={editing !== null}
        task={editing === "new" ? null : editing}
        channels={channels}
        onClose={() => {
          setEditing(null);
        }}
        onCreate={addTask}
        onSave={saveTask}
        onManageChannels={openChannels}
      />

      <ChannelsModal
        isOpen={managingChannels}
        channels={channels}
        onClose={() => {
          setManagingChannels(false);
          dismissNotificationTest();
        }}
        onCreate={addChannel}
        onSave={saveChannel}
        onDelete={removeChannel}
        onTest={runChannelTest}
        testingChannelId={testingChannelId}
        notificationTest={notificationTest}
        onDismissTest={dismissNotificationTest}
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
