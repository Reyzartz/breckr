// Package runner executes one task and records the outcome.
package runner

import (
	"context"
	"fmt"
	"log"
	"time"

	"breckr-server/internal/notifier"
	"breckr-server/internal/store"
	"breckr-server/internal/types"
)

// Browser is the slice of the browser pool the runner needs. An interface so
// the pipeline is testable with no browser at all.
type Browser interface {
	WithPage(timeout time.Duration, fn func(page types.Page) error) error
	WithoutPage(timeout time.Duration, fn func() error) error
}

type Runner struct {
	tasks    store.TaskStore
	runs     store.RunStore
	browser  Browser
	notifier notifier.Notifier
	logger   *log.Logger
}

func New(
	tasks store.TaskStore,
	runs store.RunStore,
	browser Browser,
	notify notifier.Notifier,
	logger *log.Logger,
) *Runner {
	return &Runner{tasks: tasks, runs: runs, browser: browser, notifier: notify, logger: logger}
}

// RunTask executes one task and records the outcome.
//
// The run row is written *before* execution so a crash or hang stays visible as
// 'running' instead of vanishing; the boot sweep resolves any left dangling.
//
// Never returns an error -- a failing task must not take down the scheduler or
// the HTTP request that triggered it. The failure is recorded on the run row
// instead.
func (r *Runner) RunTask(
	ctx context.Context,
	definition *types.ResolvedTask,
	triggerSource types.TriggerSource,
) types.RunOutcome {
	runID, err := r.runs.StartRun(store.StartRunInput{
		TaskID:        definition.ID,
		TriggerSource: triggerSource,
	})
	if err != nil {
		// Nothing was recorded, so there is no row to fail. Report it and stop.
		r.logger.Printf("ERROR: could not start a run for task %q: %v", definition.ID, err)
		return types.RunOutcome{Status: types.RunStatusFailed, Error: err.Error()}
	}

	var result *types.TaskResult

	if definition.NeedsBrowser {
		err = r.browser.WithPage(definition.Timeout, func(page types.Page) error {
			var runErr error
			result, runErr = definition.Run(page)
			return runErr
		})
	} else {
		// Browserless tasks never touch the page, so the argument is unused.
		err = r.browser.WithoutPage(definition.Timeout, func() error {
			var runErr error
			result, runErr = definition.Run(nil)
			return runErr
		})
	}

	if err != nil {
		r.complete(store.CompleteRunInput{
			ID:     runID,
			Status: types.RunStatusFailed,
			Error:  err.Error(),
		})
		r.logger.Printf("ERROR: task run failed (task=%s run=%d): %v", definition.ID, runID, err)
		// A failed run says nothing about the condition, so the armed state is
		// deliberately left untouched -- an error is not evidence it cleared.
		return types.RunOutcome{
			RunID:  runID,
			Status: types.RunStatusFailed,
			Error:  err.Error(),
		}
	}

	conditionMet := false
	if definition.Condition != nil {
		conditionMet, err = definition.Condition(result)
		if err != nil {
			// A failing condition is a bug in the task, not a browser failure:
			// the extraction worked, so keep the result and record the fault.
			r.complete(store.CompleteRunInput{
				ID:        runID,
				Status:    types.RunStatusFailed,
				HasResult: true,
				Result:    result,
				Error:     fmt.Sprintf("condition failed: %v", err),
			})
			r.logger.Printf("ERROR: task condition failed (task=%s run=%d): %v",
				definition.ID, runID, err)
			return types.RunOutcome{RunID: runID, Status: types.RunStatusFailed}
		}
	}

	// Edge-triggered: fire on the false -> true transition only, so a condition
	// that stays true doesn't notify on every interval. wasMet is read from the
	// database rather than memory so the state survives a restart.
	wasMet := false
	if persisted, err := r.tasks.GetTask(definition.ID); err == nil && persisted != nil {
		wasMet = persisted.ConditionMet
	}

	notified := false

	switch {
	case conditionMet && !wasMet:
		message := fmt.Sprintf("Task %q matched its condition.", definition.Name)
		if definition.Notify != nil {
			message = definition.Notify(result)
		}

		outcome := r.notifier.Send(ctx, message)
		notified = outcome.Delivered

		switch {
		case outcome.Delivered:
			r.logError(r.tasks.MarkTaskNotified(definition.ID), "mark task notified")
		case outcome.Reason == types.NotificationDisabled:
			// Nothing owed, so arm as if sent -- dedup then behaves the same
			// whether or not Telegram is configured.
			r.logError(r.tasks.SetTaskConditionMet(definition.ID, true), "arm task")
		}
		// Reason "error": deliberately leave the state disarmed so the next run
		// retries the alert rather than swallowing it forever.

	case !conditionMet && wasMet:
		// Condition cleared -- re-arm so the next false -> true transition fires.
		r.logError(r.tasks.SetTaskConditionMet(definition.ID, false), "re-arm task")
	}

	r.complete(store.CompleteRunInput{
		ID:           runID,
		Status:       types.RunStatusSuccess,
		ConditionMet: conditionMet,
		Notified:     notified,
		HasResult:    true,
		Result:       result,
	})

	r.logger.Printf("INFO: task run complete (task=%s run=%d conditionMet=%t notified=%t trigger=%s)",
		definition.ID, runID, conditionMet, notified, triggerSource)

	return types.RunOutcome{
		RunID:        runID,
		Status:       types.RunStatusSuccess,
		ConditionMet: conditionMet,
		Notified:     notified,
	}
}

func (r *Runner) complete(input store.CompleteRunInput) {
	r.logError(r.runs.CompleteRun(input), "complete run")
}

func (r *Runner) logError(err error, what string) {
	if err != nil {
		r.logger.Printf("ERROR: could not %s: %v", what, err)
	}
}
