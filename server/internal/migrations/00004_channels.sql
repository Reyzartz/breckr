-- +goose Up
-- +goose StatementBegin
-- A named delivery destination the user manages from the dashboard, replacing
-- the single env-configured Telegram bot. `config_encrypted` is the whole
-- per-type config blob sealed with the key file beside this database, so a
-- leaked .db is not a leaked bot token.
CREATE TABLE IF NOT EXISTS channels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL,
    config_encrypted TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- +goose StatementEnd
-- +goose StatementBegin
-- Which channels a task alerts to. A task with no rows here notifies nowhere,
-- which the dashboard warns about rather than treating as an error.
CREATE TABLE IF NOT EXISTS task_channels (
    task_id TEXT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, channel_id)
);

-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_task_channels_channel ON task_channels (channel_id);

-- +goose StatementEnd
-- +goose StatementBegin
-- One row per (run, channel): with fan-out, the aggregate on `runs` can only say
-- "something got through", and "which one failed" is the question you actually
-- have at 4am.
--
-- channel_name and channel_type are copied rather than joined so history still
-- reads correctly after the channel is deleted -- the same reason `runs` keeps
-- its own error text instead of pointing at a task.
CREATE TABLE IF NOT EXISTS notification_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    channel_id TEXT REFERENCES channels (id) ON DELETE SET NULL,
    channel_name TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    status TEXT NOT NULL,
    detail TEXT,
    message TEXT,
    attempted_at TEXT NOT NULL,
    UNIQUE (run_id, channel_id)
);

-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_attempts_run ON notification_attempts (run_id);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS notification_attempts;

-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS task_channels;

-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS channels;

-- +goose StatementEnd
