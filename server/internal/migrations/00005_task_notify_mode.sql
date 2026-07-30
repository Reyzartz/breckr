-- +goose Up
-- +goose StatementBegin
-- When a task alerts, given a condition that is met: 'transition' fires only on
-- the false -> true edge, 'always' fires on every matching run.
--
-- The default is 'transition' because that is what every task did before this
-- column existed. An existing row has to keep alerting exactly as it did, since
-- the alternative is an upgrade quietly turning a one-shot monitor into a
-- repeating one.
ALTER TABLE tasks ADD COLUMN notify_mode TEXT NOT NULL DEFAULT 'transition';

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
ALTER TABLE tasks DROP COLUMN notify_mode;

-- +goose StatementEnd
