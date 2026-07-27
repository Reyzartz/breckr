import puppeteer from "puppeteer-core";
import type { Browser, BrowserContext, Page } from "puppeteer-core";
import { config } from "../config/index.ts";
import { BROWSER_PROBE_TIMEOUT_MS } from "../constants/index.ts";
import { createMutex, withTimeout } from "../utils/async.ts";
import { errorMessage } from "../utils/json.ts";

/**
 * Every browser run goes through one mutex.
 *
 * Lightpanda's CDP server accepts one connection, one context and one page per
 * process, so concurrent tasks would fight over it. Chrome would tolerate more,
 * but serializing is correct for both and this is a low-volume monitor.
 */
const runExclusive = createMutex();

export interface BrowserHealth {
  reachable: boolean;
  version?: string;
  error?: string;
}

/**
 * Connect, hand a fresh page to `fn`, and tear everything down again.
 *
 * The timeout covers connection as well as execution — a browser that is down
 * makes connect() hang, which is exactly the case a run timeout must catch.
 *
 * We disconnect rather than close: the browser is a shared, long-lived process
 * we do not own, and closing it would kill every other task's browser too.
 */
export async function withPage<T>(
  timeoutMs: number,
  fn: (page: Page) => Promise<T> | T
): Promise<T> {
  return runExclusive(() =>
    withTimeout(
      (async () => {
        let browser: Browser | undefined;
        let context: BrowserContext | undefined;
        let page: Page | undefined;

        try {
          browser = await puppeteer.connect({ ...config.browserConnectOptions });

          // Lightpanda documents the context-per-session pattern, and on Chrome
          // it gives each run an isolated profile. Fall back for any CDP server
          // that doesn't implement Target.createBrowserContext.
          if (typeof browser.createBrowserContext === "function") {
            try {
              context = await browser.createBrowserContext();
              page = await context.newPage();
            } catch {
              context = undefined;
            }
          }
          page ??= await browser.newPage();

          return await fn(page);
        } finally {
          // Best-effort teardown: a failure here must not mask the real error
          // that brought us into `finally` in the first place.
          if (page) await page.close().catch(() => {});
          if (context) await context.close().catch(() => {});
          browser?.disconnect();
        }
      })(),
      timeoutMs
    )
  );
}

/** Run a task that needs no browser, still serialized and still time-boxed. */
export async function withoutPage<T>(
  timeoutMs: number,
  fn: () => Promise<T> | T
): Promise<T> {
  return runExclusive(() => withTimeout(Promise.resolve().then(fn), timeoutMs));
}

/** Cheap liveness probe for /api/health. */
export async function checkBrowserReachable(
  timeoutMs: number = BROWSER_PROBE_TIMEOUT_MS
): Promise<BrowserHealth> {
  try {
    const browser = await withTimeout(
      puppeteer.connect({ ...config.browserConnectOptions }),
      timeoutMs
    );
    const version = await browser.version().catch(() => "unknown");
    browser.disconnect();
    return { reachable: true, version };
  } catch (err) {
    return { reachable: false, error: errorMessage(err) };
  }
}
