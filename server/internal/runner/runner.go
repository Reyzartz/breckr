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
	tasks      store.TaskStore
	runs       store.RunStore
	channels   store.ChannelStore
	browser    Browser
	dispatcher notifier.Dispatcher
	logger     *log.Logger
}

func New(
	tasks store.TaskStore,
	runs store.RunStore,
	channels store.ChannelStore,
	browser Browser,
	dispatcher notifier.Dispatcher,
	logger *log.Logger,
) *Runner {
	return &Runner{
		tasks:      tasks,
		runs:       runs,
		channels:   channels,
		browser:    browser,
		dispatcher: dispatcher,
		logger:     logger,
	}
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

	// Hoisted so the run row can record what was sent and what came back. The
	// zero value means no alert was owed, which is stored as NULL rather than
	// being flattened into "not notified".
	var outcome types.NotificationOutcome
	notifyMessage := ""

	switch {
	case conditionMet && !wasMet:
		notifyMessage = fmt.Sprintf("Task %q matched its condition.", definition.Name)
		if definition.Notify != nil {
			notifyMessage = definition.Notify(result)
		}

		fanout := r.dispatcher.DispatchTask(ctx, definition.ID, notifier.Message{
			Subject: fmt.Sprintf("breckr: %s", definition.Name),
			Body:    notifyMessage,
		})
		outcome = fanout.Aggregate
		notified = outcome.Delivered

		// Written before the run row so a failure here cannot leave the
		// aggregate claiming a breakdown that was never stored.
		r.recordAttempts(runID, fanout, notifyMessage)

		switch {
		case outcome.Delivered:
			// One channel getting through is enough. Retrying for the sake of a
			// failed channel would re-alert the ones that already worked.
			r.logError(r.tasks.MarkTaskNotified(definition.ID), "mark task notified")
		case outcome.Reason == types.NotificationDisabled:
			// Nothing owed, so arm as if sent -- dedup then behaves the same
			// whether or not any channel is attached.
			r.logError(r.tasks.SetTaskConditionMet(definition.ID, true), "arm task")
		}
		// Reason "error": every channel failed, so deliberately leave the state
		// disarmed and let the next run retry rather than swallowing the alert.

	case !conditionMet && wasMet:
		// Condition cleared -- re-arm so the next false -> true transition fires.
		r.logError(r.tasks.SetTaskConditionMet(definition.ID, false), "re-arm task")
	}

	r.complete(store.CompleteRunInput{
		ID:                  runID,
		Status:              types.RunStatusSuccess,
		ConditionMet:        conditionMet,
		Notified:            notified,
		HasResult:           true,
		Result:              result,
		NotificationStatus:  outcome.Reason,
		NotificationDetail:  outcome.Detail,
		NotificationMessage: notifyMessage,
	})

	r.logger.Printf("INFO: task run complete (task=%s run=%d conditionMet=%t notified=%t notify=%s trigger=%s)",
		definition.ID, runID, conditionMet, notified, notifyReason(outcome.Reason), triggerSource)

	return types.RunOutcome{
		RunID:        runID,
		Status:       types.RunStatusSuccess,
		ConditionMet: conditionMet,
		Notified:     notified,
	}
}

// notifyReason labels the log line, so an empty reason reads as "no alert was
// due" rather than as a blank field.
func notifyReason(reason types.NotificationReason) string {
	if reason == "" {
		return "none"
	}
	return string(reason)
}

// recordAttempts stores the per-channel breakdown behind the aggregate.
//
// The message is stamped onto every attempt rather than held once on the run,
// because "what did this channel actually receive" is the question a failed
// delivery raises, and truncation makes the answer differ per channel.
func (r *Runner) recordAttempts(runID int64, fanout notifier.Fanout, message string) {
	attempts := fanout.Attempts()
	if len(attempts) == 0 {
		return
	}

	for i := range attempts {
		attempts[i].Message = message
	}

	r.logError(r.channels.RecordAttempts(runID, attempts), "record notification attempts")
}

func (r *Runner) complete(input store.CompleteRunInput) {
	r.logError(r.runs.CompleteRun(input), "complete run")
}

func (r *Runner) logError(err error, what string) {
	if err != nil {
		r.logger.Printf("ERROR: could not %s: %v", what, err)
	}
}
