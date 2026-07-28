package api

import (
	"log"
	"net/http"
	"strings"
	"time"

	"breckr-server/internal/executor"
	"breckr-server/internal/runner"
	"breckr-server/internal/scheduler"
	"breckr-server/internal/store"
	"breckr-server/internal/types"
	"breckr-server/internal/utils"
)

// Browser is the slice of the browser pool the test route needs.
type Browser interface {
	WithPage(timeout time.Duration, fn func(page types.Page) error) error
}

type TaskHandler struct {
	logger    *log.Logger
	taskStore store.TaskStore
	runStore  store.RunStore
	registry  *scheduler.Registry
	runner    *runner.Runner
	browser   Browser
	timeout   time.Duration
}

func NewTaskHandler(
	logger *log.Logger,
	taskStore store.TaskStore,
	runStore store.RunStore,
	registry *scheduler.Registry,
	taskRunner *runner.Runner,
	browser Browser,
	timeout time.Duration,
) *TaskHandler {
	return &TaskHandler{
		logger:    logger,
		taskStore: taskStore,
		runStore:  runStore,
		registry:  registry,
		runner:    taskRunner,
		browser:   browser,
		timeout:   timeout,
	}
}

// decorate turns a stored task into the shape the dashboard reads.
func (th *TaskHandler) decorate(task *types.Task, lastRun *types.Run) types.TaskWithStatus {
	return types.TaskWithStatus{
		Task: *task,
		// Derived rather than stored, so a row whose expression was written by
		// hand still opens in the form's builder -- as `custom`.
		Schedule: executor.FromCron(task.CronExpr),
		LastRun:  lastRun,
		NextRun:  th.registry.GetNextRun(task.ID),
		// A row can carry no usable spec -- written by the old file-based
		// registry, or corrupt JSON. The dashboard needs to know it can no
		// longer be run.
		Orphaned: th.registry.GetDefinition(task.ID) == nil,
	}
}

// fail answers a validation error as a 400 naming the field, and anything else
// as a 500. Every route that touches user input funnels through it.
func (th *TaskHandler) fail(w http.ResponseWriter, err error, what string) {
	if utils.WriteValidationError(w, err) {
		return
	}
	th.logger.Printf("ERROR: %s: %v", what, err)
	utils.WriteError(w, http.StatusInternalServerError, "internal server error", "")
}

func (th *TaskHandler) HandleGetAllTasks(w http.ResponseWriter, r *http.Request) {
	latestRuns, err := th.runStore.GetLatestRunByTask()
	if err != nil {
		th.fail(w, err, "GetLatestRunByTask")
		return
	}

	stored, err := th.taskStore.ListTasks()
	if err != nil {
		th.fail(w, err, "ListTasks")
		return
	}

	tasks := make([]types.TaskWithStatus, 0, len(stored))
	for _, task := range stored {
		tasks = append(tasks, th.decorate(task, latestRuns[task.ID]))
	}

	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{"data": tasks})
}

type createInput struct {
	id       string
	name     string
	cronExpr string
	spec     *types.TaskSpec
}

func validateCreate(body types.CreateTaskRequest) (*createInput, error) {
	id, err := executor.ValidateTaskID(body.ID)
	if err != nil {
		return nil, err
	}

	name, err := executor.ValidateName(body.Name)
	if err != nil {
		return nil, err
	}

	cronExpr, err := executor.ResolveCron(body.Schedule, optionalString(body.CronExpr))
	if err != nil {
		return nil, err
	}

	spec, err := executor.ValidateSpec(body.Spec)
	if err != nil {
		return nil, err
	}

	return &createInput{id: id, name: name, cronExpr: cronExpr, spec: spec}, nil
}

func (th *TaskHandler) HandleCreateTask(w http.ResponseWriter, r *http.Request) {
	var body types.CreateTaskRequest
	if err := utils.ReadRequestBody(r, &body); err != nil {
		th.logger.Printf("ERROR: decoding create task request body: %v", err)
		utils.WriteError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	input, err := validateCreate(body)
	if err != nil {
		th.fail(w, err, "validating create task request")
		return
	}

	existing, err := th.taskStore.GetTask(input.id)
	if err != nil {
		th.fail(w, err, "GetTask")
		return
	}
	if existing != nil {
		utils.WriteError(w, http.StatusConflict, "Task \""+input.id+"\" already exists.", "id")
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	created, err := th.taskStore.CreateTask(store.CreateTaskInput{
		ID:       input.id,
		Name:     input.name,
		CronExpr: input.cronExpr,
		Spec:     input.spec,
		Enabled:  enabled,
	})
	if err != nil {
		th.fail(w, err, "CreateTask")
		return
	}

	// Arm it now rather than at the next boot -- a task you just saved is
	// expected to start running, not to wait for a restart.
	scheduled := th.registry.Register(created)

	response := th.decorate(created, nil)
	response.Orphaned = !scheduled

	utils.WriteJSONResponse(w, http.StatusCreated, utils.Envelope{"data": response})
}

func (th *TaskHandler) HandleUpdateTask(w http.ResponseWriter, r *http.Request) {
	id := utils.ReadIDParam(r)

	existing, err := th.taskStore.GetTask(id)
	if err != nil {
		th.fail(w, err, "GetTask")
		return
	}
	if existing == nil {
		utils.WriteError(w, http.StatusNotFound, "Unknown task \""+id+"\".", "")
		return
	}

	var body types.UpdateTaskRequest
	if err := utils.ReadRequestBody(r, &body); err != nil {
		th.logger.Printf("ERROR: decoding update task request body: %v", err)
		utils.WriteError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	patch, err := buildPatch(body)
	if err != nil {
		th.fail(w, err, "validating update task request")
		return
	}

	enabled := existing.Enabled

	switch {
	case !patch.IsEmpty():
		if _, err := th.taskStore.UpdateTask(id, patch); err != nil {
			th.fail(w, err, "UpdateTask")
			return
		}

		// Written to the row *before* rescheduling: Register reads `enabled`
		// off the stored task to decide whether to arm the fresh entry, so a
		// toggle applied afterwards would be undone by it.
		if body.Enabled != nil {
			enabled = *body.Enabled
			if err := th.taskStore.SetTaskEnabled(id, enabled); err != nil {
				th.logger.Printf("ERROR: SetTaskEnabled: %v", err)
			}
		}

		// There is no way to swap an expression on a live cron entry, so the
		// schedule is rebuilt from the row we just wrote.
		updated, err := th.taskStore.GetTask(id)
		if err == nil && updated != nil {
			th.registry.Reschedule(updated)
		}

	case body.Enabled != nil:
		// SetEnabled owns both the cron entry and the row, so there is nothing
		// to write here first.
		enabled = *body.Enabled
		if !th.registry.SetEnabled(id, enabled) {
			utils.WriteError(w, http.StatusConflict,
				"Task \""+id+"\" has no usable spec and cannot be scheduled.", "")
			return
		}
	}

	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{
		"data": types.UpdateTaskResponse{
			ID:      id,
			Enabled: enabled,
			NextRun: th.registry.GetNextRun(id),
		},
	})
}

func buildPatch(body types.UpdateTaskRequest) (store.UpdateTaskInput, error) {
	patch := store.UpdateTaskInput{}

	if body.Name != nil {
		name, err := executor.ValidateName(*body.Name)
		if err != nil {
			return patch, err
		}
		patch.Name = &name
	}

	if body.Schedule != nil || body.CronExpr != nil {
		cronExpr, err := executor.ResolveCron(body.Schedule, body.CronExpr)
		if err != nil {
			return patch, err
		}
		patch.CronExpr = &cronExpr
	}

	if body.Spec != nil {
		spec, err := executor.ValidateSpec(body.Spec)
		if err != nil {
			return patch, err
		}
		patch.Spec = spec
	}

	return patch, nil
}

func (th *TaskHandler) HandleDeleteTask(w http.ResponseWriter, r *http.Request) {
	id := utils.ReadIDParam(r)

	existing, err := th.taskStore.GetTask(id)
	if err != nil {
		th.fail(w, err, "GetTask")
		return
	}
	if existing == nil {
		utils.WriteError(w, http.StatusNotFound, "Unknown task \""+id+"\".", "")
		return
	}

	th.registry.Unregister(id)

	// Run history goes with it, through the ON DELETE CASCADE on runs.task_id.
	if _, err := th.taskStore.DeleteTask(id); err != nil {
		th.fail(w, err, "DeleteTask")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleTestTask runs a draft spec once without saving it.
//
// Writes no run row and sends no notification, so it can be pressed freely while
// getting a selector right. It still queues behind the browser mutex like any
// other run.
func (th *TaskHandler) HandleTestTask(w http.ResponseWriter, r *http.Request) {
	var body types.TestTaskRequest
	if err := utils.ReadRequestBody(r, &body); err != nil {
		th.logger.Printf("ERROR: decoding test task request body: %v", err)
		utils.WriteError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	spec, err := executor.ValidateSpec(body.Spec)
	if err != nil {
		th.fail(w, err, "validating test task request")
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "Untitled task"
	}

	var (
		result       *types.TaskResult
		conditionMet bool
		message      string
	)

	runErr := th.browser.WithPage(th.timeout, func(page types.Page) error {
		var err error
		result, conditionMet, message, err = executor.TestSpec(page, spec, name)
		return err
	})

	if runErr != nil {
		// A failing draft is the expected case while iterating on a selector --
		// report it as a result, not as a 500.
		th.logger.Printf("INFO: task test run failed: %v", runErr)
		utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{
			"data": types.TestTaskResponse{OK: false, Error: runErr.Error()},
		})
		return
	}

	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{
		"data": types.TestTaskResponse{
			OK:           true,
			Result:       result,
			ConditionMet: &conditionMet,
			Message:      message,
		},
	})
}

func (th *TaskHandler) HandleRunTaskNow(w http.ResponseWriter, r *http.Request) {
	id := utils.ReadIDParam(r)

	definition := th.registry.GetDefinition(id)
	if definition == nil {
		utils.WriteError(w, http.StatusNotFound, "Unknown task \""+id+"\".", "")
		return
	}

	// Deliberately runs even when the task is disabled -- "run now" is an
	// explicit manual override, and it's how you test a task before enabling
	// it. It still queues behind the mutex like any scheduled run.
	outcome := th.runner.RunTask(r.Context(), definition, types.TriggerManual)

	utils.WriteJSONResponse(w, http.StatusAccepted, utils.Envelope{"data": outcome})
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
