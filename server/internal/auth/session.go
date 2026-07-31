// Package auth implements the dashboard's optional shared-password session.
//
// One password rather than user accounts: this is a single-tenant self-hosted
// service, and what needs protecting is a dashboard that can drive a browser at
// arbitrary URLs. Anyone who needs per-user access should put an identity-aware
// proxy in front instead.
//
// Auth is off entirely when the password is empty. That is the right default for
// a deployment reachable only from loopback -- which is what the compose file
// still publishes -- and it keeps local development free of a login step. Every
// caller asks Enabled or Authenticated rather than looking at the password, so
// there is one disabled path rather than one per call site.
package auth

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	// CookieName is the session cookie.
	//
	// A cookie rather than a bearer token because of /api/events: the browser
	// WebSocket API cannot set request headers, but it attaches cookies to a
	// same-origin handshake for free. A header-based scheme would need a second
	// mechanism for the socket alone.
	CookieName = "breckr_session"

	// tokenContext domain-separates this MAC from any other use of the key.
	tokenContext = "breckr.session.v1|"

	// hkdfInfo labels the derived subkey. Bump the version if the token layout
	// changes, so old cookies fail to verify rather than being misread.
	hkdfInfo = "breckr session cookie v1"

	// nonceSize keeps two tokens minted in the same second distinct.
	nonceSize = 16
)

// CookieSecure is the tri-state behind AUTH_COOKIE_SECURE.
type CookieSecure string

const (
	// CookieSecureAuto sets Secure only when the request arrived over TLS.
	CookieSecureAuto CookieSecure = "auto"
	// CookieSecureAlways sets it unconditionally.
	CookieSecureAlways CookieSecure = "true"
	// CookieSecureNever never sets it.
	CookieSecureNever CookieSecure = "false"
)

// ParseCookieSecure validates the configured value.
func ParseCookieSecure(raw string) (CookieSecure, error) {
	switch CookieSecure(strings.ToLower(strings.TrimSpace(raw))) {
	case CookieSecureAuto:
		return CookieSecureAuto, nil
	case CookieSecureAlways:
		return CookieSecureAlways, nil
	case CookieSecureNever:
		return CookieSecureNever, nil
	default:
		return "", fmt.Errorf("AUTH_COOKIE_SECURE must be auto, true or false, got %q", raw)
	}
}

// Sessions mints and verifies session cookies.
type Sessions struct {
	password   string
	signingKey []byte
	ttl        time.Duration
	secure     CookieSecure
}

// NewSessions derives the signing key from the master key that already encrypts
// channel credentials, rather than introducing a second secret to generate,
// back up and lose.
//
// It derives rather than reuses, for two reasons. The AES-GCM key that protects
// stored credentials never itself signs anything, so a token-verification oracle
// cannot be turned against the credential cipher. And because the password is
// the HKDF salt, changing AUTH_PASSWORD rotates the signing key and invalidates
// every outstanding session -- which is the only revocation a stateless token
// gets, and is worth having for free.
func NewSessions(masterKey []byte, password string, ttl time.Duration, secure CookieSecure) (*Sessions, error) {
	salt := sha256.Sum256([]byte(password))

	signingKey, err := hkdf.Key(sha256.New, masterKey, salt[:], hkdfInfo, sha256.Size)
	if err != nil {
		return nil, fmt.Errorf("could not derive the session signing key: %w", err)
	}

	return &Sessions{password: password, signingKey: signingKey, ttl: ttl, secure: secure}, nil
}

// Enabled reports whether a password was configured at all.
func (s *Sessions) Enabled() bool { return s.password != "" }

// PasswordMatches compares in constant time.
//
// Both sides are hashed first so the comparison is independent of length as well
// as of content -- ConstantTimeCompare returns early on a length mismatch, which
// would otherwise leak the password's length.
func (s *Sessions) PasswordMatches(submitted string) bool {
	want := sha256.Sum256([]byte(s.password))
	got := sha256.Sum256([]byte(submitted))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}

// Authenticated reports whether the request carries a valid session.
//
// With no password configured every request is authenticated, so callers never
// need to ask whether auth is on before asking whether the caller is allowed.
func (s *Sessions) Authenticated(r *http.Request) bool {
	if !s.Enabled() {
		return true
	}

	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return false
	}
	return s.verify(cookie.Value)
}

// Issue sets a fresh session cookie.
func (s *Sessions) Issue(w http.ResponseWriter, r *http.Request) error {
	token, err := s.mint(time.Now().Add(s.ttl))
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		// Lax rather than Strict: Strict omits the cookie on a cross-site
		// top-level navigation, so following a link to the dashboard from a chat
		// message would bounce through /login despite a valid session. CSRF is
		// already covered -- every mutation is a JSON POST/PATCH/DELETE, which
		// is not a CORS-simple request and needs a preflight the CORS handler
		// refuses for unknown origins.
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureFor(r),
		MaxAge:   int(s.ttl.Seconds()),
	})
	return nil
}

// Clear expires the session cookie.
//
// Path, SameSite and Secure must match what Issue wrote exactly: a browser
// treats a cookie whose attributes differ as a different cookie and keeps the
// original, so a mismatch here silently fails to log anyone out.
func (s *Sessions) Clear(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureFor(r),
		MaxAge:   -1,
	})
}

// secureFor decides the Secure attribute for this request.
//
// `auto` exists because plain HTTP on a LAN is the common self-hosted case, and
// a hardcoded Secure makes login silently impossible there: the browser accepts
// the response, drops the cookie, and the next request 401s with nothing on
// screen to explain it.
//
// X-Forwarded-Proto is trusted here even though it is attacker-controlled,
// because it can only add a restriction. A spoofed header sets Secure on a
// cookie that did not need it; it cannot remove Secure from one that did.
func (s *Sessions) secureFor(r *http.Request) bool {
	switch s.secure {
	case CookieSecureAlways:
		return true
	case CookieSecureNever:
		return false
	default:
		return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
}

// mint builds `payload.mac`, where payload carries the expiry and a nonce.
func (s *Sessions) mint(expiry time.Time) (string, error) {
	raw := make([]byte, 8+nonceSize)
	binary.BigEndian.PutUint64(raw[:8], uint64(expiry.Unix()))

	if _, err := rand.Read(raw[8:]); err != nil {
		return "", fmt.Errorf("could not generate a session nonce: %w", err)
	}

	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + s.sign(payload), nil
}

// verify checks the MAC before the expiry, and reports nothing about which
// failed -- an unauthenticated request is unauthenticated either way.
func (s *Sessions) verify(token string) bool {
	payload, mac, found := strings.Cut(token, ".")
	if !found {
		return false
	}

	// Compared with hmac.Equal rather than ==, so a near-miss cannot be found
	// one byte at a time by timing the comparison.
	if !hmac.Equal([]byte(mac), []byte(s.sign(payload))) {
		return false
	}

	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || len(raw) != 8+nonceSize {
		return false
	}

	expiry := time.Unix(int64(binary.BigEndian.Uint64(raw[:8])), 0)
	return time.Now().Before(expiry)
}

func (s *Sessions) sign(payload string) string {
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(tokenContext))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
