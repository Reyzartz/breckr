-- +goose Up
-- +goose StatementBegin
-- Why the alert did or did not go out. NULL means none was owed: the condition
-- did not transition on this run. `notified` alone cannot tell that apart from
-- a delivery Telegram rejected.
ALTER TABLE runs ADD COLUMN notification_status TEXT;

-- +goose StatementEnd
-- +goose StatementBegin
-- The failure reason, in the same words it was logged in. NULL when delivered.
ALTER TABLE runs ADD COLUMN notification_detail TEXT;

-- +goose StatementEnd
-- +goose StatementBegin
-- The exact body handed to the notifier, so "was it sent" is answerable by
-- reading what went out rather than by inferring it from a bool.
ALTER TABLE runs ADD COLUMN notification_message TEXT;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN notification_message;

-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN notification_detail;

-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN notification_status;

-- +goose StatementEnd
