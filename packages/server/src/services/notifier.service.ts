import { config } from "../config/index.ts";
import {
  TELEGRAM_API_BASE,
  TELEGRAM_TIMEOUT_MS,
  TELEGRAM_MAX_MESSAGE_LENGTH,
  TELEGRAM_TRUNCATION_SUFFIX,
} from "../constants/index.ts";
import { errorMessage } from "../utils/json.ts";
import type { Logger, NotificationOutcome } from "../types/index.ts";

function truncate(text: string): string {
  if (text.length <= TELEGRAM_MAX_MESSAGE_LENGTH) return text;
  return (
    text.slice(0, TELEGRAM_MAX_MESSAGE_LENGTH - TELEGRAM_TRUNCATION_SUFFIX.length) +
    TELEGRAM_TRUNCATION_SUFFIX
  );
}

/**
 * Send a Telegram message.
 *
 * Never throws: a notification failure must not fail an otherwise-successful
 * run, and must not prevent the run being recorded.
 *
 * The caller needs to tell two non-delivery cases apart, because they demand
 * opposite handling of the edge-trigger state:
 *
 *   reason 'error'    — Telegram was configured but the send broke. The alert
 *                       is still owed, so the caller must NOT advance the armed
 *                       state; the next run retries.
 *   reason 'disabled' — no token configured, so there is nothing to retry and
 *                       nothing owed. The caller advances state as if sent,
 *                       which keeps dedup behaving identically with and without
 *                       Telegram set up.
 */
export async function sendNotification(
  message: string,
  logger: Logger = console
): Promise<NotificationOutcome> {
  const text = truncate(String(message));

  if (!config.telegram.enabled) {
    logger.warn(
      { notification: text },
      "Telegram not configured — notification logged instead of sent"
    );
    return { delivered: false, reason: "disabled" };
  }

  const url = `${TELEGRAM_API_BASE}/bot${config.telegram.token}/sendMessage`;

  try {
    const response = await fetch(url, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        chat_id: config.telegram.chatId,
        text,
        disable_web_page_preview: true,
      }),
      signal: AbortSignal.timeout(TELEGRAM_TIMEOUT_MS),
    });

    if (!response.ok) {
      // Telegram puts the useful reason in the body, not the status text.
      const body = await response.text().catch(() => "");
      logger.error(
        { status: response.status, body: body.slice(0, 500) },
        "Telegram rejected the notification"
      );
      return { delivered: false, reason: "error" };
    }

    return { delivered: true, reason: "sent" };
  } catch (err) {
    logger.error(
      { err: errorMessage(err) },
      "Failed to reach Telegram — notification will be retried on the next run"
    );
    return { delivered: false, reason: "error" };
  }
}
