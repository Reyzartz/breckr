-- +goose Up
-- +goose StatementBegin

-- IF NOT EXISTS is deliberate. A database written by the previous Node server
-- already has this table and has never been stamped by goose, so the first Up
-- against an existing file has to be a no-op that only records the version.
--
-- spec/created_at/updated_at are folded in here; the Node server added them
-- with a runtime ALTER TABLE loop. They stay nullable on purpose: a row written
-- by the old file-based registry has no spec and cannot get one, but it still
-- owns run history, so it surfaces in the dashboard as orphaned rather than
-- vanishing.
CREATE TABLE IF NOT EXISTS tasks (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    cron_expr        TEXT NOT NULL,
    enabled          INTEGER NOT NULL DEFAULT 1,
    condition_met    INTEGER NOT NULL DEFAULT 0,
    last_notified_at TEXT,
    spec             TEXT,
    created_at       TEXT,
    updated_at       TEXT
);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS tasks;

-- +goose StatementEnd
