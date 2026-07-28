import { useState } from "react";
import { Alert, Button, Divider, Text } from "brake-ui";
import { Moon, Plus, RefreshCw, Sun } from "lucide-react";
import type { Run, TaskWithStatus } from "./types/index.ts";
import { TaskList } from "./components/TaskList.tsx";
import { TaskFormModal } from "./components/TaskFormModal.tsx";
import { RunHistory } from "./components/RunHistory.tsx";
import { RunDetail } from "./components/RunDetail.tsx";
import { useMonitorData } from "./hooks/useMonitorData.ts";
import { useRunFilters } from "./hooks/useRunFilters.ts";
import { useTheme } from "./hooks/useTheme.ts";

export default function App() {
  const { theme, toggleTheme } = useTheme();
  const { filters, setFilter, nextPage, previousPage } = useRunFilters();
  const {
    tasks,
    runs,
    health,
    error,
    loading,
    busyTaskId,
    refresh,
    toggleTask,
    runNow,
    addTask,
    saveTask,
    removeTask,
  } = useMonitorData(filters);

  const [selectedRun, setSelectedRun] = useState<Run | null>(null);

  // Null while closed. Editing carries the task; creating carries "new", which
  // distinguishes it from closed without a second flag.
  const [editing, setEditing] = useState<TaskWithStatus | "new" | null>(null);

  const openCreate = () => {
    setEditing("new");
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
                busyTaskId={busyTaskId}
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
        onClose={() => {
          setEditing(null);
        }}
        onCreate={addTask}
        onSave={saveTask}
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
