import Database from "better-sqlite3";
import { config } from "../config/index.ts";

export const db: Database.Database = new Database(config.dbPath);

// WAL lets the dashboard read run history while a task is mid-write.
db.pragma("journal_mode = WAL");
db.pragma("foreign_keys = ON");

db.exec(`
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

  CREATE TABLE IF NOT EXISTS runs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id        TEXT NOT NULL,
    started_at     TEXT NOT NULL,
    finished_at    TEXT,
    status         TEXT NOT NULL,
    condition_met  INTEGER NOT NULL DEFAULT 0,
    notified       INTEGER NOT NULL DEFAULT 0,
    trigger_source TEXT NOT NULL DEFAULT 'cron',
    result_summary TEXT,
    error          TEXT,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
  );

  CREATE INDEX IF NOT EXISTS idx_runs_task_started ON runs(task_id, started_at DESC);
  CREATE INDEX IF NOT EXISTS idx_runs_started ON runs(started_at DESC);
`);

/**
 * Bring a database created before tasks moved out of files up to date.
 *
 * The columns are nullable on purpose. A row written by the old file-based
 * registry has no spec and cannot get one, but it still owns run history —
 * adding the column rather than dropping the row keeps that history, and the
 * task surfaces in the dashboard as orphaned instead of vanishing.
 */
const existingTaskColumns = new Set(
  (db.prepare(`PRAGMA table_info(tasks)`).all() as { name: string }[]).map(
    (column) => column.name
  )
);

for (const column of ["spec", "created_at", "updated_at"]) {
  if (!existingTaskColumns.has(column)) {
    db.exec(`ALTER TABLE tasks ADD COLUMN ${column} TEXT`);
  }
}

/** All timestamps are ISO-8601 UTC, written from one place so they agree. */
export function now(): string {
  return new Date().toISOString();
}

/** SQLite has no boolean type; rows come back as 0/1. */
export function toBoolean(value: number | boolean): boolean {
  return Boolean(value);
}

export function fromBoolean(value: boolean): number {
  return value ? 1 : 0;
}

export function closeDatabase(): void {
  db.close();
}
