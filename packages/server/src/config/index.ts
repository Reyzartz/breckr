import fs from "node:fs";
import path from "node:path";
import dotenv from "dotenv";
import { findRepoRoot } from "../utils/paths.ts";

const ROOT = findRepoRoot(import.meta.dirname);

dotenv.config({ path: path.join(ROOT, ".env"), quiet: true });

function required(name: string): string {
  const value = process.env[name];
  if (!value?.trim()) {
    throw new Error(
      `Missing required env var ${name}. Copy .env.example to .env and fill it in.`
    );
  }
  return value.trim();
}

function optional(name: string, fallback: string): string {
  const value = process.env[name];
  return value?.trim() ? value.trim() : fallback;
}

function integer(name: string, fallback: number): number {
  const raw = process.env[name];
  if (!raw?.trim()) return fallback;
  const parsed = Number(raw);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    throw new Error(`${name} must be a positive integer, got "${raw}".`);
  }
  return parsed;
}

const timezone = optional("TZ", "UTC");

// Fail at boot rather than on the first cron tick: node-cron accepts any string
// here and only resolves the zone when a schedule fires.
try {
  new Intl.DateTimeFormat("en-US", { timeZone: timezone });
} catch {
  throw new Error(`TZ is not a valid IANA timezone: "${timezone}".`);
}

const dbPath = path.resolve(ROOT, optional("DB_PATH", "./data/monitor.db"));
fs.mkdirSync(path.dirname(dbPath), { recursive: true });

const telegramToken = optional("TELEGRAM_BOT_TOKEN", "");
const telegramChatId = optional("TELEGRAM_CHAT_ID", "");

// One half without the other is a misconfiguration, not a disabled notifier —
// surface it at boot instead of silently never alerting.
if (telegramToken && !telegramChatId) {
  throw new Error(
    "TELEGRAM_BOT_TOKEN is set but TELEGRAM_CHAT_ID is empty. Set both, or neither to disable notifications."
  );
}
if (telegramChatId && !telegramToken) {
  throw new Error(
    "TELEGRAM_CHAT_ID is set but TELEGRAM_BOT_TOKEN is empty. Set both, or neither to disable notifications."
  );
}

const browserEndpoint = required("BROWSER_WS_ENDPOINT");

// The endpoint is handed straight to puppeteer.connect, which throws an opaque
// error on a malformed URL. Check the shape here where we can explain it.
//
// Both schemes are accepted because the two browsers address differently:
// Lightpanda serves CDP at a fixed ws:// URL, while Chrome's browser-level
// socket carries a per-launch UUID path. Passing Chrome's http:// address lets
// puppeteer resolve that itself via /json/version, so swapping engines stays a
// one-line change instead of hunting for a UUID after every restart.
if (!/^(wss?|https?):\/\//.test(browserEndpoint)) {
  throw new Error(
    `BROWSER_WS_ENDPOINT must be a ws://, wss://, http:// or https:// URL, got "${browserEndpoint}".`
  );
}

const browserConnectOptions = /^wss?:\/\//.test(browserEndpoint)
  ? { browserWSEndpoint: browserEndpoint }
  : { browserURL: browserEndpoint };

export const config = {
  rootDir: ROOT,
  browserWsEndpoint: browserEndpoint,
  /** ws:// -> browserWSEndpoint (Lightpanda), http:// -> browserURL (Chrome). */
  browserConnectOptions,
  telegram: {
    token: telegramToken,
    chatId: telegramChatId,
    enabled: Boolean(telegramToken && telegramChatId),
  },
  port: integer("PORT", 3000),
  host: optional("HOST", "127.0.0.1"),
  isProduction: optional("NODE_ENV", "development") === "production",
  dbPath,
  retentionDays: integer("RUN_RETENTION_DAYS", 30),
  timezone,
  defaultTimeoutMs: integer("DEFAULT_TIMEOUT_MS", 30_000),
  dashboardDist: path.resolve(ROOT, "packages/dashboard/dist"),
} as const;

export type Config = typeof config;
