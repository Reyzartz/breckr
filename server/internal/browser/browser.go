// Package browser owns the CDP connection and the one mutex every run passes
// through.
package browser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"breckr-server/internal/config"
	"breckr-server/internal/types"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// teardownTimeout bounds the best-effort cleanup after a run.
//
// Lightpanda can take the better part of a minute to answer Target.closeTarget,
// and the pool mutex is held for all of it -- one run's teardown would stall
// every task queued behind it. Cleanup is best-effort by design, so giving up is
// the right answer.
const teardownTimeout = 2 * time.Second

// Pool serializes every browser run through one mutex.
//
// Lightpanda's CDP server accepts one connection, one context and one page per
// process, so concurrent tasks would fight over it. Chrome would tolerate more,
// but serializing is correct for both and this is a low-volume monitor.
//
// The cron scheduler's SkipIfStillRunning only stops a task overlapping
// *itself*; this covers the cross-task case.
type Pool struct {
	cfg *config.Config
	mu  sync.Mutex
}

func NewPool(cfg *config.Config) *Pool {
	return &Pool{cfg: cfg}
}

// ErrTimeout is what a run that outlived its budget fails with.
var ErrTimeout = errors.New("timed out")

// connection holds the websocket a run is using, so it can be closed from the
// outside.
//
// Necessary because a run that outlives its timeout is still executing when
// withDeadline gives up: the pool mutex is released at that point, and leaving
// the socket open would let the next run pile a second session onto a browser
// that serves one at a time.
type connection struct {
	mu sync.Mutex
	ws *cdp.WebSocket
}

func (c *connection) set(ws *cdp.WebSocket) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ws = ws
}

func (c *connection) close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ws != nil {
		_ = c.ws.Close()
		c.ws = nil
	}
}

// controlURL resolves the endpoint go-rod should connect to.
//
// Chrome's browser-level socket carries a per-launch UUID in its path, so its
// http:// address is resolved through /json/version at connect time rather than
// stored. Lightpanda serves CDP at a fixed ws:// URL and needs no resolution.
func (p *Pool) controlURL() (string, error) {
	if !p.cfg.Browser.NeedsResolve {
		return p.cfg.Browser.ControlURL, nil
	}

	resolved, err := launcher.ResolveURL(p.cfg.Browser.ControlURL)
	if err != nil {
		return "", fmt.Errorf("could not resolve the browser endpoint %s: %w",
			p.cfg.Browser.Endpoint, err)
	}
	return resolved, nil
}

// connect dials CDP and hands the socket to `conn` so the caller can close it.
//
// The websocket is dialled here rather than through rod.ControlURL because
// rod's own dialler keeps the socket private: cancelling its context stops the
// event loop but never closes the TCP connection. Against Lightpanda, which
// spawns a thread per session, that leaks until it answers every new connection
// with MaxThreadsReached.
//
// NoDefaultDevice turns off go-rod's per-page device emulation. A monitor reads
// a value out of the DOM; it has no use for a viewport, a touch model or a
// spoofed user agent, and Lightpanda answers each of those calls with a
// "not_implemented" warning. Dropping them removes three round trips per run.
func (p *Pool) connect(ctx context.Context, conn *connection) (*rod.Browser, error) {
	controlURL, err := p.controlURL()
	if err != nil {
		return nil, err
	}

	ws := &cdp.WebSocket{}
	if err := ws.Connect(ctx, controlURL, nil); err != nil {
		return nil, fmt.Errorf("could not connect to the browser at %s: %w",
			p.cfg.Browser.Endpoint, err)
	}
	conn.set(ws)

	browser := rod.New().Client(cdp.New().Start(ws)).Context(ctx).NoDefaultDevice()
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("could not start a CDP session at %s: %w",
			p.cfg.Browser.Endpoint, err)
	}

	return browser, nil
}

// WithPage connects, hands a fresh page to fn, and tears everything down again.
//
// The timeout covers connection as well as execution -- a browser that is down
// makes connect hang, which is exactly the case a run timeout must catch.
func (p *Pool) WithPage(timeout time.Duration, fn func(page types.Page) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn := &connection{}
	// This is the disconnect, and it runs even when the body is still stuck. We
	// never call Browser.Close: the browser is a shared, long-lived process we
	// do not own, and closing it would kill every other task's browser too.
	defer conn.close()

	return withDeadline(ctx, timeout, func() error {
		browser, err := p.connect(ctx, conn)
		if err != nil {
			return err
		}

		// Lightpanda documents the context-per-session pattern, and on Chrome it
		// gives each run an isolated profile. Fall back for any CDP server that
		// does not implement Target.createBrowserContext.
		target := browser
		var incognito *rod.Browser
		if isolated, err := browser.Incognito(); err == nil {
			incognito = isolated
			target = isolated
		}

		page, err := target.Page(proto.TargetCreateTarget{})
		if err != nil {
			return fmt.Errorf("could not open a page: %w", err)
		}
		defer teardown(incognito, page)

		return fn(&rodPage{page: page.Context(ctx)})
	})
}

// teardown releases the page and the isolated context, bounded and best-effort.
//
// Disposing the browser context is both faster and more thorough than closing
// the page -- it takes the pages inside it with it, and on Chrome it is what
// stops a per-run context leaking after we disconnect. Closing the page is only
// the fallback for a CDP server that had no context to give us.
//
// Failures are ignored on purpose: a teardown error must not mask the real error
// that brought us here.
func teardown(incognito *rod.Browser, page *rod.Page) {
	if incognito != nil {
		_ = proto.TargetDisposeBrowserContext{
			BrowserContextID: incognito.BrowserContextID,
		}.Call(incognito.Timeout(teardownTimeout))
		return
	}

	_ = page.Timeout(teardownTimeout).Close()
}

// WithoutPage runs a task that needs no browser, still serialized and still
// time-boxed.
func (p *Pool) WithoutPage(timeout time.Duration, fn func() error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return withDeadline(ctx, timeout, fn)
}

// withDeadline races fn against ctx, so a call that ignores cancellation still
// fails on time rather than pinning the mutex forever.
//
// The goroutine may outlive the return; it writes to a buffered channel so it
// cannot leak on a send.
func withDeadline(ctx context.Context, timeout time.Duration, fn func() error) error {
	done := make(chan error, 1)

	go func() {
		done <- fn()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// A result that landed in the same instant the deadline expired is a
		// real result -- select picks at random between two ready cases, so
		// check once more before calling it a timeout.
		select {
		case err := <-done:
			return err
		default:
		}
		return fmt.Errorf("%w after %s", ErrTimeout, timeout)
	}
}

// CheckReachable is the cheap liveness probe behind /api/health.
//
// It takes the pool mutex like any other browser work, and a browser that
// serves one session at a time cannot answer a probe and a run at once -- which
// is why the server probes this on one schedule of its own
// (Application.watchBrowserHealth) rather than letting every open dashboard ask
// on a timer.
func (p *Pool) CheckReachable(timeout time.Duration) types.BrowserHealth {
	p.mu.Lock()
	defer p.mu.Unlock()

	health := types.BrowserHealth{Endpoint: p.cfg.Browser.Endpoint}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn := &connection{}
	defer conn.close()

	err := withDeadline(ctx, timeout, func() error {
		browser, err := p.connect(ctx, conn)
		if err != nil {
			return err
		}

		version, err := proto.BrowserGetVersion{}.Call(browser)
		if err != nil {
			// Reachable, but it would not say what it is. Not worth failing the
			// probe over -- the connection is what the dashboard cares about.
			health.Version = "unknown"
			return nil
		}
		health.Version = version.Product
		return nil
	})
	if err != nil {
		return types.BrowserHealth{Endpoint: p.cfg.Browser.Endpoint, Error: err.Error()}
	}

	health.Reachable = true
	return health
}
