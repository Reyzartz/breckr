package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"breckr-server/internal/crypto"
	"breckr-server/internal/types"
)

// StoredChannel is a channel with its config decrypted, as the notifier needs
// it. Only this package and the notifier ever see Config.
//
// The API layer takes types.Channel instead, which carries the redacted view --
// so there is no shape a handler could serialise that contains a live token.
type StoredChannel struct {
	types.Channel
	// Raw per-type config JSON. Nil when Broken.
	Config json.RawMessage
}

type CreateChannelInput struct {
	ID      string
	Name    string
	Type    types.ChannelType
	Config  json.RawMessage
	Enabled bool
}

// UpdateChannelInput patches a channel. Only the non-nil fields are written.
type UpdateChannelInput struct {
	Name    *string
	Config  json.RawMessage
	Enabled *bool
}

func (u UpdateChannelInput) IsEmpty() bool {
	return u.Name == nil && u.Config == nil && u.Enabled == nil
}

// AttemptInput is one channel's delivery outcome for one run.
type AttemptInput struct {
	// Empty when the channel was deleted between the send and this write.
	ChannelID   string
	ChannelName string
	ChannelType types.ChannelType
	Status      types.NotificationReason
	Detail      string
	Message     string
}

type ChannelStore interface {
	CreateChannel(input CreateChannelInput) (*StoredChannel, error)
	UpdateChannel(id string, patch UpdateChannelInput) (*StoredChannel, error)
	DeleteChannel(id string) (bool, error)
	GetChannel(id string) (*StoredChannel, error)
	ListChannels() ([]*StoredChannel, error)
	// ListChannelsForTask returns only enabled channels: a disabled channel is
	// one the user has muted, not one they have unlinked.
	ListChannelsForTask(taskID string) ([]*StoredChannel, error)
	ListChannelIDsForTask(taskID string) ([]string, error)
	// ListChannelIDsByTask is the batched form, for the dashboard's task list.
	ListChannelIDsByTask() (map[string][]string, error)
	SetTaskChannels(taskID string, channelIDs []string) error
	CountEnabledChannels() (int, error)
	RecordAttempts(runID int64, attempts []AttemptInput) error
	ListAttempts(runID int64) ([]*types.NotificationAttempt, error)
}

type SQLiteChannelStore struct {
	db     *sql.DB
	cipher *crypto.Cipher
}

func NewSQLiteChannelStore(db *sql.DB, cipher *crypto.Cipher) *SQLiteChannelStore {
	return &SQLiteChannelStore{db: db, cipher: cipher}
}

const channelColumns = `id, name, type, config_encrypted, enabled, created_at, updated_at`

const attemptColumns = `id, run_id, channel_id, channel_name, channel_type, status, detail, message, attempted_at`

// scanChannel decrypts at the row boundary, so nothing above this package
// handles ciphertext.
//
// A config that will not decrypt marks the channel Broken rather than failing
// the query -- the same call the task store makes for unparseable specs. One
// channel whose key changed must not take down the whole channel list, because
// that list is where you would go to fix it.
func (s *SQLiteChannelStore) scanChannel(row interface{ Scan(...any) error }) (*StoredChannel, error) {
	var (
		channel   StoredChannel
		encrypted string
		enabled   int
	)

	err := row.Scan(
		&channel.ID,
		&channel.Name,
		&channel.Type,
		&encrypted,
		&enabled,
		&channel.CreatedAt,
		&channel.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	channel.Enabled = enabled != 0

	plaintext, err := s.cipher.Decrypt(encrypted)
	if err != nil {
		channel.Broken = true
		return &channel, nil
	}
	if !json.Valid(plaintext) {
		channel.Broken = true
		return &channel, nil
	}

	channel.Config = json.RawMessage(plaintext)
	return &channel, nil
}

func scanAttempt(row interface{ Scan(...any) error }) (*types.NotificationAttempt, error) {
	var (
		attempt   types.NotificationAttempt
		channelID sql.NullString
		detail    sql.NullString
		message   sql.NullString
	)

	err := row.Scan(
		&attempt.ID,
		&attempt.RunID,
		&channelID,
		&attempt.ChannelName,
		&attempt.ChannelType,
		&attempt.Status,
		&detail,
		&message,
		&attempt.AttemptedAt,
	)
	if err != nil {
		return nil, err
	}

	attempt.ChannelID = nullString(channelID)
	attempt.Detail = nullString(detail)
	attempt.Message = nullString(message)

	return &attempt, nil
}

func (s *SQLiteChannelStore) CreateChannel(input CreateChannelInput) (*StoredChannel, error) {
	encrypted, err := s.cipher.Encrypt(input.Config)
	if err != nil {
		return nil, err
	}

	query := `
		INSERT INTO channels (id, name, type, config_encrypted, enabled, created_at, updated_at)
		    VALUES (?, ?, ?, ?, ?, ?, ?)`

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	at := now()
	_, err = s.db.ExecContext(ctx, query,
		input.ID,
		input.Name,
		string(input.Type),
		encrypted,
		fromBool(input.Enabled),
		at,
		at,
	)
	if err != nil {
		return nil, err
	}

	return s.GetChannel(input.ID)
}

func (s *SQLiteChannelStore) UpdateChannel(id string, patch UpdateChannelInput) (*StoredChannel, error) {
	assignments := []string{}
	args := []any{}

	if patch.Name != nil {
		assignments = append(assignments, "name = ?")
		args = append(args, *patch.Name)
	}
	if patch.Config != nil {
		encrypted, err := s.cipher.Encrypt(patch.Config)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, "config_encrypted = ?")
		args = append(args, encrypted)
	}
	if patch.Enabled != nil {
		assignments = append(assignments, "enabled = ?")
		args = append(args, fromBool(*patch.Enabled))
	}

	if len(assignments) > 0 {
		assignments = append(assignments, "updated_at = ?")
		args = append(args, now(), id)

		query := fmt.Sprintf("UPDATE channels SET %s WHERE id = ?", strings.Join(assignments, ", "))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return nil, err
		}
	}

	return s.GetChannel(id)
}

// DeleteChannel removes a channel. Its task links go with it through the
// ON DELETE CASCADE on task_channels; its history survives with a null
// channel_id, keeping the name it was sent under.
func (s *SQLiteChannelStore) DeleteChannel(id string) (bool, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		return false, err
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed > 0, nil
}

// GetChannel returns nil, nil when the channel does not exist.
func (s *SQLiteChannelStore) GetChannel(id string) (*StoredChannel, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	query := `SELECT ` + channelColumns + ` FROM channels WHERE id = ?`

	channel, err := s.scanChannel(s.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return channel, nil
}

func (s *SQLiteChannelStore) ListChannels() ([]*StoredChannel, error) {
	query := `SELECT ` + channelColumns + ` FROM channels ORDER BY name`
	return s.queryChannels(query)
}

func (s *SQLiteChannelStore) ListChannelsForTask(taskID string) ([]*StoredChannel, error) {
	query := `
		SELECT ` + prefixed(channelColumns, "channels") + `
		FROM channels
		    JOIN task_channels ON task_channels.channel_id = channels.id
		WHERE task_channels.task_id = ? AND channels.enabled = 1
		ORDER BY channels.name`

	return s.queryChannels(query, taskID)
}

func (s *SQLiteChannelStore) queryChannels(query string, args ...any) ([]*StoredChannel, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	channels := []*StoredChannel{}
	for rows.Next() {
		channel, err := s.scanChannel(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}

	return channels, rows.Err()
}

// ListChannelIDsForTask includes disabled channels: the task form shows the
// links as saved, not as they would currently deliver.
func (s *SQLiteChannelStore) ListChannelIDsForTask(taskID string) ([]string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rows, err := s.db.QueryContext(ctx, `
		SELECT task_channels.channel_id
		FROM task_channels
		    JOIN channels ON channels.id = task_channels.channel_id
		WHERE task_channels.task_id = ?
		ORDER BY channels.name`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// SetTaskChannels replaces a task's links wholesale.
//
// Delete-then-insert inside one transaction, so a task is never briefly linked
// to nothing -- a cron tick landing in that window would skip the alert and,
// worse, arm the trigger as if it had been delivered.
func (s *SQLiteChannelStore) SetTaskChannels(taskID string, channelIDs []string) error {
	return withTx(s.db, func(tx *sql.Tx) error {
		return setTaskChannelsTx(tx, taskID, channelIDs)
	})
}

func setTaskChannelsTx(tx *sql.Tx, taskID string, channelIDs []string) error {
	if _, err := tx.Exec(`DELETE FROM task_channels WHERE task_id = ?`, taskID); err != nil {
		return err
	}

	for _, channelID := range channelIDs {
		_, err := tx.Exec(
			`INSERT INTO task_channels (task_id, channel_id) VALUES (?, ?)`,
			taskID, channelID,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// ListChannelIDsByTask returns every task's links in one query, so listing
// tasks costs one round trip rather than one per task.
func (s *SQLiteChannelStore) ListChannelIDsByTask() (map[string][]string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rows, err := s.db.QueryContext(ctx, `
		SELECT task_channels.task_id, task_channels.channel_id
		FROM task_channels
		    JOIN channels ON channels.id = task_channels.channel_id
		ORDER BY task_channels.task_id, channels.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byTask := map[string][]string{}
	for rows.Next() {
		var taskID, channelID string
		if err := rows.Scan(&taskID, &channelID); err != nil {
			return nil, err
		}
		byTask[taskID] = append(byTask[taskID], channelID)
	}

	return byTask, rows.Err()
}

func (s *SQLiteChannelStore) CountEnabledChannels() (int, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM channels WHERE enabled = 1`).Scan(&count)
	return count, err
}

// RecordAttempts writes the per-channel breakdown for one run, in one commit.
func (s *SQLiteChannelStore) RecordAttempts(runID int64, attempts []AttemptInput) error {
	if len(attempts) == 0 {
		return nil
	}

	at := now()

	return withTx(s.db, func(tx *sql.Tx) error {
		for _, attempt := range attempts {
			var channelID any
			if attempt.ChannelID != "" {
				channelID = attempt.ChannelID
			}

			_, err := tx.Exec(`
				INSERT INTO notification_attempts
				    (run_id, channel_id, channel_name, channel_type, status, detail, message, attempted_at)
				    VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				runID,
				channelID,
				attempt.ChannelName,
				string(attempt.ChannelType),
				string(attempt.Status),
				emptyToNull(attempt.Detail),
				emptyToNull(attempt.Message),
				at,
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteChannelStore) ListAttempts(runID int64) ([]*types.NotificationAttempt, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	query := `SELECT ` + attemptColumns + `
		FROM notification_attempts WHERE run_id = ? ORDER BY channel_name`

	rows, err := s.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	attempts := []*types.NotificationAttempt{}
	for rows.Next() {
		attempt, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}

	return attempts, rows.Err()
}

// prefixed qualifies a column list with its table, for queries that join.
func prefixed(columns, table string) string {
	parts := strings.Split(columns, ", ")
	for i, part := range parts {
		parts[i] = table + "." + part
	}
	return strings.Join(parts, ", ")
}

// emptyToNull stores an absent string as NULL rather than "", so "no detail" and
// "an empty detail" stay distinguishable in the column.
func emptyToNull(value string) any {
	if value == "" {
		return nil
	}
	return value
}
