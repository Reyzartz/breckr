package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"breckr-server/internal/types"
	"breckr-server/internal/utils"
)

type CreateTaskInput struct {
	ID       string
	Name     string
	CronExpr string
	Spec     *types.TaskSpec
	// When to alert while the condition is met. Empty falls back to the
	// column's own default, so a caller that does not care can leave it unset.
	NotifyMode types.NotifyMode
	Enabled    bool
	// Channels this task alerts to. Written in the same transaction as the task
	// itself -- a task that exists without its links would notify nowhere, and
	// the arming logic would record that as "nothing owed".
	ChannelIDs []string
}

// UpdateTaskInput patches a task. Only the non-nil fields are written; the rest
// keep their stored values.
type UpdateTaskInput struct {
	Name       *string
	CronExpr   *string
	Spec       *types.TaskSpec
	NotifyMode *types.NotifyMode
	// Nil leaves the links alone; a non-nil pointer replaces them wholesale,
	// including with an empty slice to detach every channel.
	ChannelIDs *[]string
}

func (u UpdateTaskInput) IsEmpty() bool {
	return u.Name == nil && u.CronExpr == nil && u.Spec == nil &&
		u.NotifyMode == nil && u.ChannelIDs == nil
}

type TaskStore interface {
	CreateTask(input CreateTaskInput) (*types.Task, error)
	UpdateTask(id string, patch UpdateTaskInput) (*types.Task, error)
	DeleteTask(id string) (bool, error)
	GetTask(id string) (*types.Task, error)
	ListTasks() ([]*types.Task, error)
	SetTaskEnabled(id string, enabled bool) error
	SetTaskConditionMet(id string, met bool) error
	MarkTaskNotified(id string) error
}

type SQLiteTaskStore struct {
	db *sql.DB
}

func NewSQLiteTaskStore(db *sql.DB) *SQLiteTaskStore {
	return &SQLiteTaskStore{db: db}
}

const taskColumns = `id, name, cron_expr, enabled, condition_met, notify_mode, last_notified_at, spec`

// parseSpec reads a stored spec back, returning nil rather than an error when
// the JSON no longer parses.
//
// The task then shows up as orphaned and can be deleted from the dashboard --
// which beats a corrupt row taking down every request that lists tasks.
func parseSpec(raw sql.NullString) *types.TaskSpec {
	if !raw.Valid || raw.String == "" {
		return nil
	}

	var spec types.TaskSpec
	if err := json.Unmarshal([]byte(raw.String), &spec); err != nil {
		return nil
	}
	return &spec
}

// parseNotifyMode reads a stored mode back, falling back to the default for a
// value the current build no longer recognises.
//
// The same reasoning as parseSpec: an unreadable row should degrade to the
// conservative behavior -- alerting once -- rather than surface as an error on
// every request that lists tasks.
func parseNotifyMode(raw string) types.NotifyMode {
	if !types.IsNotifyMode(raw) {
		return types.DefaultNotifyMode
	}
	return types.NotifyMode(raw)
}

func scanTask(row interface{ Scan(...any) error }) (*types.Task, error) {
	var (
		task           types.Task
		enabled        int
		conditionMet   int
		notifyMode     string
		lastNotifiedAt sql.NullString
		spec           sql.NullString
	)

	err := row.Scan(
		&task.ID,
		&task.Name,
		&task.CronExpr,
		&enabled,
		&conditionMet,
		&notifyMode,
		&lastNotifiedAt,
		&spec,
	)
	if err != nil {
		return nil, err
	}

	task.Enabled = enabled != 0
	task.ConditionMet = conditionMet != 0
	task.NotifyMode = parseNotifyMode(notifyMode)
	task.LastNotifiedAt = nullString(lastNotifiedAt)
	task.Spec = parseSpec(spec)

	return &task, nil
}

func (s *SQLiteTaskStore) CreateTask(input CreateTaskInput) (*types.Task, error) {
	query := `
		INSERT INTO tasks (id, name, cron_expr, spec, notify_mode, enabled, created_at, updated_at)
		    VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	at := now()
	notifyMode := parseNotifyMode(string(input.NotifyMode))

	err := withTx(s.db, func(tx *sql.Tx) error {
		_, err := tx.Exec(query,
			input.ID,
			input.Name,
			input.CronExpr,
			utils.SafeMarshal(input.Spec),
			string(notifyMode),
			fromBool(input.Enabled),
			at,
			at,
		)
		if err != nil {
			return err
		}

		return setTaskChannelsTx(tx, input.ID, input.ChannelIDs)
	})
	if err != nil {
		return nil, err
	}

	return s.GetTask(input.ID)
}

// UpdateTask patches a task, returning it as stored afterwards.
//
// Editing the spec deliberately re-arms the edge-trigger: the persisted
// condition_met describes the *old* condition, and carrying it over would let a
// stale "already alerted" flag swallow the first alert of the new one.
//
// Changing only notify_mode deliberately does *not*. The condition is unchanged,
// so the stored state still describes it -- and re-arming would mean switching a
// currently-matching task to "transition" fires one more alert for a transition
// that already happened.
func (s *SQLiteTaskStore) UpdateTask(id string, patch UpdateTaskInput) (*types.Task, error) {
	assignments := []string{}
	args := []any{}

	if patch.Name != nil {
		assignments = append(assignments, "name = ?")
		args = append(args, *patch.Name)
	}
	if patch.CronExpr != nil {
		assignments = append(assignments, "cron_expr = ?")
		args = append(args, *patch.CronExpr)
	}
	if patch.Spec != nil {
		assignments = append(assignments, "spec = ?", "condition_met = 0")
		args = append(args, utils.SafeMarshal(patch.Spec))
	}
	if patch.NotifyMode != nil {
		assignments = append(assignments, "notify_mode = ?")
		args = append(args, string(*patch.NotifyMode))
	}

	if len(assignments) > 0 || patch.ChannelIDs != nil {
		err := withTx(s.db, func(tx *sql.Tx) error {
			if len(assignments) > 0 {
				assignments = append(assignments, "updated_at = ?")
				args = append(args, now(), id)

				query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", strings.Join(assignments, ", "))
				if _, err := tx.Exec(query, args...); err != nil {
					return err
				}
			}

			if patch.ChannelIDs != nil {
				return setTaskChannelsTx(tx, id, *patch.ChannelIDs)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return s.GetTask(id)
}

// DeleteTask removes a task. Run history goes with it, through the
// ON DELETE CASCADE on runs.task_id.
func (s *SQLiteTaskStore) DeleteTask(id string) (bool, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return false, err
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed > 0, nil
}

// GetTask returns nil, nil when the task does not exist.
func (s *SQLiteTaskStore) GetTask(id string) (*types.Task, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	query := `SELECT ` + taskColumns + ` FROM tasks WHERE id = ?`

	task, err := scanTask(s.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (s *SQLiteTaskStore) ListTasks() ([]*types.Task, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	query := `SELECT ` + taskColumns + ` FROM tasks ORDER BY name`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := []*types.Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	return tasks, rows.Err()
}

func (s *SQLiteTaskStore) SetTaskEnabled(id string, enabled bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET enabled = ? WHERE id = ?`, fromBool(enabled), id)
	return err
}

// SetTaskConditionMet advances or re-arms the edge-trigger state.
func (s *SQLiteTaskStore) SetTaskConditionMet(id string, met bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET condition_met = ? WHERE id = ?`, fromBool(met), id)
	return err
}

// MarkTaskNotified arms the trigger and stamps the delivery time, after a
// successful send.
func (s *SQLiteTaskStore) MarkTaskNotified(id string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET condition_met = 1, last_notified_at = ? WHERE id = ?`, now(), id)
	return err
}
