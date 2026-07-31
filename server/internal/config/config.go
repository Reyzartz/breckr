// Package config loads and validates every environment-driven setting.
//
// Everything is validated at boot rather than at first use. A bad timezone or a
// malformed browser endpoint are misconfigurations, and surfacing them on the
// first cron tick -- possibly hours later, possibly at 4am -- is exactly when
// you least want to find out.
//
// Notification credentials are deliberately absent: channels are rows the user
// manages from the dashboard, not environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"breckr-server/internal/auth"

	"github.com/joho/godotenv"
)

// minPasswordLength is the shortest AUTH_PASSWORD worth accepting. Short enough
// to guess is the same as unset, but silently so.
const minPasswordLength = 8

type Config struct {
	Server   ServerConfig
	Client   ClientConfig
	Database DatabaseConfig
	Browser  BrowserConfig
	Security SecurityConfig
	Runtime  RuntimeConfig
}

type ServerConfig struct {
	Host string
	Port int
	// Directory holding the built dashboard. Empty or missing means the API
	// runs headless and the dashboard is served by Vite in development.
	ClientDist string
}

type ClientConfig struct {
	AllowedOrigins []string
}

type DatabaseConfig struct {
	Path string
}

type BrowserConfig struct {
	// As configured, reported by /api/health.
	Endpoint string
	// What go-rod connects to. For an http:// endpoint this is resolved to the
	// real websocket URL at connect time -- Chrome's browser socket carries a
	// per-launch UUID, so the http:// address is the only stable thing to hold.
	ControlURL string
	// True when Endpoint was http(s):// and ControlURL still needs resolving.
	NeedsResolve   bool
	DefaultTimeout time.Duration
}

type SecurityConfig struct {
	// KeyFile holds the master key that encrypts channel credentials at rest.
	// It defaults to sitting beside the database, because the two are a pair:
	// backing up one without the other leaves unreadable channels.
	KeyFile string
	// AuthPassword is the one shared password the dashboard asks for. Empty --
	// the default -- disables authentication entirely, which is correct for a
	// deployment reachable only from loopback and keeps local development free
	// of a login step.
	AuthPassword string
	// SessionTTL is how long a session cookie stays valid.
	SessionTTL time.Duration
	// CookieSecure decides the cookie's Secure attribute. See auth.CookieSecure.
	CookieSecure auth.CookieSecure
}

type RuntimeConfig struct {
	// Cron expressions are matched against wall-clock time here, so a schedule
	// means what you intended regardless of the host's locale.
	Timezone      string
	Location      *time.Location
	RetentionDays int
}

// Load reads .env, then the environment, and validates the result.
func Load() (*Config, error) {
	root := loadDotEnv()

	browserEndpoint, err := required("BROWSER_WS_ENDPOINT")
	if err != nil {
		// The one setting with no sensible default, and the one most likely to
		// stop a first run -- so say what it is for rather than only that it is
		// missing.
		return nil, fmt.Errorf(
			"%w. It is the CDP address of the browser to drive: run `docker compose up -d lightpanda` "+
				"and set ws://127.0.0.1:9222, or use the breckr:standalone image, which bundles a browser "+
				"and sets this for you",
			err,
		)
	}

	// Handed straight to the CDP client, which fails opaquely on a malformed
	// URL. Check the shape here, where we can explain it.
	//
	// Both schemes are accepted because the two browsers address differently:
	// Lightpanda serves CDP at a fixed ws:// URL, while Chrome's browser-level
	// socket carries a per-launch UUID path. Passing Chrome's http:// address
	// lets us resolve that ourselves via /json/version, so swapping engines
	// stays a one-line change instead of hunting for a UUID after every restart.
	parsed, err := url.Parse(browserEndpoint)
	if err != nil {
		return nil, fmt.Errorf("BROWSER_WS_ENDPOINT is not a valid URL: %q", browserEndpoint)
	}
	var needsResolve bool
	switch parsed.Scheme {
	case "ws", "wss":
	case "http", "https":
		needsResolve = true
	default:
		return nil, fmt.Errorf(
			"BROWSER_WS_ENDPOINT must be a ws://, wss://, http:// or https:// URL, got %q",
			browserEndpoint,
		)
	}

	authPassword := optional("AUTH_PASSWORD", "")
	if authPassword != "" && len(authPassword) < minPasswordLength {
		return nil, fmt.Errorf(
			"AUTH_PASSWORD must be at least %d characters, got %d. Leave it unset to disable authentication",
			minPasswordLength, len(authPassword),
		)
	}

	sessionTTLHours, err := integer("AUTH_SESSION_TTL_HOURS", 720)
	if err != nil {
		return nil, err
	}

	cookieSecure, err := auth.ParseCookieSecure(optional("AUTH_COOKIE_SECURE", string(auth.CookieSecureAuto)))
	if err != nil {
		return nil, err
	}

	// This list is both the CORS allowlist and the websocket handshake's origin
	// patterns. With a session cookie in play and AllowCredentials already set,
	// a wildcard would let any page on the internet drive this API with the
	// user's own session -- so it stops being a convenience and starts being a
	// CSRF hole the moment a password exists.
	allowedOrigins := strings.Split(optional("CLIENT_ALLOWED_ORIGIN", "http://localhost:5173"), ",")
	if authPassword != "" {
		for _, origin := range allowedOrigins {
			if strings.TrimSpace(origin) == "*" {
				return nil, fmt.Errorf(
					"CLIENT_ALLOWED_ORIGIN cannot be * while AUTH_PASSWORD is set: " +
						"any site could then make authenticated requests with the signed-in user's cookie. " +
						"List the origins the dashboard is served from instead",
				)
			}
		}
	}

	timezone := optional("TZ", "UTC")
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("TZ is not a valid IANA timezone: %q", timezone)
	}

	port, err := integer("PORT", 3000)
	if err != nil {
		return nil, err
	}
	retentionDays, err := integer("RUN_RETENTION_DAYS", 30)
	if err != nil {
		return nil, err
	}
	timeoutMs, err := integer("DEFAULT_TIMEOUT_MS", 30000)
	if err != nil {
		return nil, err
	}

	// Relative paths resolve against the directory the .env was found in, so
	// DB_PATH=./data/monitor.db keeps pointing at the repo-root data/ whether
	// the binary runs from the repo root or from server/.
	dbPath := resolveAgainst(root, optional("DB_PATH", "./data/monitor.db"))
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("could not create the database directory: %w", err)
	}

	clientDist := resolveAgainst(root, optional("CLIENT_DIST", "./client/dist"))

	// Defaulting beside the database keeps the two together through a `cp
	// data/` backup, which is the only backup this app can assume anyone does.
	keyFile := resolveAgainst(root, optional(
		"SECRET_KEY_FILE",
		filepath.Join(filepath.Dir(dbPath), "secret.key"),
	))

	return &Config{
		Server: ServerConfig{
			Host:       optional("HOST", "127.0.0.1"),
			Port:       port,
			ClientDist: clientDist,
		},
		Client:   ClientConfig{AllowedOrigins: allowedOrigins},
		Database: DatabaseConfig{Path: dbPath},
		Browser: BrowserConfig{
			Endpoint:       browserEndpoint,
			ControlURL:     browserEndpoint,
			NeedsResolve:   needsResolve,
			DefaultTimeout: time.Duration(timeoutMs) * time.Millisecond,
		},
		Security: SecurityConfig{
			KeyFile:      keyFile,
			AuthPassword: authPassword,
			SessionTTL:   time.Duration(sessionTTLHours) * time.Hour,
			CookieSecure: cookieSecure,
		},
		Runtime: RuntimeConfig{
			Timezone:      timezone,
			Location:      location,
			RetentionDays: retentionDays,
		},
	}, nil
}

// loadDotEnv finds the repo root by looking for a .env beside the working
// directory and then one level up, and returns it.
//
// The one level up matters: `make start-server` runs from server/, while the
// container runs from the image root. Returning the directory the file was
// found in is what lets DB_PATH stay written relative to the repo root in both.
func loadDotEnv() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	for _, dir := range []string{cwd, filepath.Dir(cwd)} {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			// Ignore the error: a .env that exists but cannot be parsed is
			// worth failing on, but godotenv also reports an already-set
			// variable, which is normal in a container.
			_ = godotenv.Load(candidate)
			return dir
		}
	}

	// No .env at all is fine -- the container passes everything through the
	// environment. Resolve relative paths against the working directory.
	return cwd
}

func resolveAgainst(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, path)
}

func required(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		// Named rather than pointed at .env, because the audience is as likely to
		// be someone running `docker run -e` as someone in a clone of the repo.
		return "", fmt.Errorf(
			"missing required env var %s. Set it in the environment, or copy .env.example to .env and fill it in",
			name,
		)
	}
	return value, nil
}

func optional(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func integer(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, raw)
	}
	return parsed, nil
}
