package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"breckr-server/internal/types"
	"breckr-server/internal/utils"
)

type StartRunInput struct {
	TaskID        string
	TriggerSource types.TriggerSource
}

type CompleteRunInput struct {
	ID           int64
	Status       types.RunStatus
	ConditionMet bool
	Notified     bool
	// HasResult distinguishes "no result" from "a result that happens to be
	// nil" -- the former stores NULL, the latter stores the JSON null.
	HasResult bool
	Result    any
	Error     string
	// NotificationStatus is why an alert did or did not go out. Empty when none
	// was owed, which stores NULL -- "no alert was due" is not the same fact as
	// "an alert was due and went out".
	NotificationStatus types.NotificationReason
	// NotificationDetail is the failure reason. Empty when delivered.
	NotificationDetail string
	// NotificationMessage is the body handed to the notifier.
	NotificationMessage string
}

type ListRunsOptions struct {
	TaskID string
	Status types.RunStatus
	Limit  int
	Offset int
}

type RunStore interface {
	StartRun(input StartRunInput) (int64, error)
	CompleteRun(input CompleteRunInput) error
	GetRun(id int64) (*types.Run, error)
	ListRuns(opts ListRunsOptions) (*types.RunsResponse, error)
	GetLatestRunByTask() (map[string]*types.Run, error)
	GetLastSuccessfulResult(taskID string) any
	SweepInterruptedRuns() (int64, error)
	PruneOldRuns(retentionDays int) (int64, error)
}

type SQLiteRunStore struct {
	db *sql.DB
}

func NewSQLiteRunStore(db *sql.DB) *SQLiteRunStore {
	return &SQLiteRunStore{db: db}
}

const runColumns = `runs.id, runs.task_id, runs.started_at, runs.finished_at, runs.status,
	runs.condition_met, runs.notified, runs.trigger_source, runs.result_summary, runs.error,
	runs.notification_status, runs.notification_detail, runs.notification_message`

// scanRun converts a row, normalizing SQLite's 0/1 booleans at the boundary --
// which is what lets types.Run honestly declare `ConditionMet bool` instead of
// making every consumer remember to coerce.
func scanRun(row interface{ Scan(...any) error }, withTaskName bool) (*types.Run, error) {
	var (
		run                 types.Run
		finishedAt          sql.NullString
		conditionMet        int
		notified            int
		resultSummary       sql.NullString
		runError            sql.NullString
		notificationStatus  sql.NullString
		notificationDetail  sql.NullString
		notificationMessage sql.NullString
		taskName            sql.NullString
	)

	targets := []any{
		&run.ID,
		&run.TaskID,
		&run.StartedAt,
		&finishedAt,
		&run.Status,
		&conditionMet,
		&notified,
		&run.TriggerSource,
		&resultSummary,
		&runError,
		&notificationStatus,
		&notificationDetail,
		&notificationMessage,
	}
	if withTaskName {
		targets = append(targets, &taskName)
	}

	if err := row.Scan(targets...); err != nil {
		return nil, err
	}

	run.FinishedAt = nullString(finishedAt)
	run.ConditionMet = conditionMet != 0
	run.Notified = notified != 0
	run.ResultSummary = nullString(resultSummary)
	run.Error = nullString(runError)
	run.NotificationStatus = notificationReason(notificationStatus)
	run.NotificationDetail = nullString(notificationDetail)
	run.NotificationMessage = nullString(notificationMessage)
	run.TaskName = nullString(taskName)

	return &run, nil
}

// notificationReason keeps NULL distinct from a reason. A run written before
// this column existed, and a run that owed no alert, both read as nil rather
// than as the empty reason -- which is what lets the dashboard say "no alert
// was due" instead of showing an empty badge.
func notificationReason(value sql.NullString) *types.NotificationReason {
	if !value.Valid || value.String == "" {
		return nil
	}
	reason := types.NotificationReason(value.String)
	return &reason
}

// SweepInterruptedRuns resolves runs left dangling by a crash.
//
// A run row is written before the task executes, so a crash mid-run leaves a
// row stuck at 'running'. Resolve those at boot -- otherwise they stay "in
// progress" forever in the dashboard.
func (s *SQLiteRunStore) SweepInterruptedRuns() (int64, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := s.db.ExecContext(ctx, `
		UPDATE runs
		SET status = 'failed',
		    error = 'Interrupted: the server stopped while this run was in flight.',
		    finished_at = ?
		WHERE status = 'running'`, now())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SQLiteRunStore) PruneOldRuns(retentionDays int) (int64, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format(time.RFC3339Nano)

	result, err := s.db.ExecContext(ctx, `DELETE FROM runs WHERE started_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SQLiteRunStore) StartRun(input StartRunInput) (int64, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO runs (task_id, started_at, status, trigger_source)
		    VALUES (?, ?, 'running', ?)`,
		input.TaskID, now(), string(input.TriggerSource))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *SQLiteRunStore) CompleteRun(input CompleteRunInput) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var resultSummary any
	if input.HasResult {
		resultSummary = utils.SafeMarshal(input.Result)
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE runs SET
		    finished_at = ?,
		    status = ?,
		    condition_met = ?,
		    notified = ?,
		    result_summary = ?,
		    error = ?,
		    notification_status = ?,
		    notification_detail = ?,
		    notification_message = ?
		WHERE id = ?`,
		now(),
		string(input.Status),
		fromBool(input.ConditionMet),
		fromBool(input.Notified),
		resultSummary,
		emptyAsNull(input.Error),
		emptyAsNull(string(input.NotificationStatus)),
		emptyAsNull(input.NotificationDetail),
		emptyAsNull(input.NotificationMessage),
		input.ID,
	)
	return err
}

// emptyAsNull stores "" as SQL NULL, so "absent" and "present but empty" stay
// distinguishable on read.
func emptyAsNull(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// GetRun returns nil, nil when the run does not exist.
func (s *SQLiteRunStore) GetRun(id int64) (*types.Run, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	query := `SELECT ` + runColumns + `, tasks.name AS task_name
		FROM runs LEFT JOIN tasks ON tasks.id = runs.task_id
		WHERE runs.id = ?`

	run, err := scanRun(s.db.QueryRowContext(ctx, query, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return run, nil
}

// GetLastSuccessfulResult returns the stored result of this task's most recent
// successful run, or nil if it has never had one.
//
// Failed runs are skipped deliberately: an error says nothing about what the
// page holds, so comparing against one would report a change that never
// happened. Nil is the honest answer for a task that has not yet succeeded, and
// the `changed` operator treats it as "no change".
func (s *SQLiteRunStore) GetLastSuccessfulResult(taskID string) any {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var resultSummary sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT result_summary FROM runs
		WHERE task_id = ? AND status = 'success' AND result_summary IS NOT NULL
		ORDER BY id DESC
		LIMIT 1`, taskID).Scan(&resultSummary)
	if err != nil || !resultSummary.Valid {
		return nil
	}

	var decoded any
	if err := json.Unmarshal([]byte(resultSummary.String), &decoded); err != nil {
		return nil
	}
	return decoded
}

// GetLatestRunByTask returns the most recent run per task, for the "last run"
// column on the task list.
func (s *SQLiteRunStore) GetLatestRunByTask() (map[string]*types.Run, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	query := `SELECT ` + runColumns + ` FROM runs
		JOIN (
		    SELECT task_id, MAX(id) AS id FROM runs GROUP BY task_id
		) latest ON latest.id = runs.id`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	latest := map[string]*types.Run{}
	for rows.Next() {
		run, err := scanRun(rows, false)
		if err != nil {
			return nil, err
		}
		latest[run.TaskID] = run
	}

	return latest, rows.Err()
}

// ListRuns returns paginated run history. Filters are optional and compose;
// `total` reflects them so the dashboard can page correctly.
func (s *SQLiteRunStore) ListRuns(opts ListRunsOptions) (*types.RunsResponse, error) {
	where := []string{}
	args := []any{}

	if opts.TaskID != "" {
		where = append(where, "runs.task_id = ?")
		args = append(args, opts.TaskID)
	}
	if opts.Status != "" {
		where = append(where, "runs.status = ?")
		args = append(args, string(opts.Status))
	}

	clause := ""
	if len(where) > 0 {
		clause = "WHERE " + strings.Join(where, " AND ")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM runs %s", clause)
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`SELECT %s, tasks.name AS task_name
		FROM runs LEFT JOIN tasks ON tasks.id = runs.task_id
		%s
		ORDER BY runs.id DESC
		LIMIT ? OFFSET ?`, runColumns, clause)

	rows, err := s.db.QueryContext(ctx, query, append(args, opts.Limit, opts.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []*types.Run{}
	for rows.Next() {
		run, err := scanRun(rows, true)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &types.RunsResponse{
		Total:  total,
		Runs:   runs,
		Limit:  opts.Limit,
		Offset: opts.Offset,
	}, nil
}
