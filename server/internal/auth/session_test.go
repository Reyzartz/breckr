package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

/*
The whole of the dashboard's access control is one cookie, so the failure modes
are what matter here: a tampered or expired token must be rejected rather than
half-trusted, a token minted under the old password must stop working the moment
the password changes -- that rotation is the only revocation a stateless token
gets -- and Clear must write attributes identical to Issue, because a browser
treats a cookie whose attributes differ as a different cookie and would silently
keep the session it was told to drop.
*/

const testPassword = "hunter2hunter2"

func newSessions(t *testing.T, password string) *Sessions {
	t.Helper()

	// A fixed key rather than a generated one: two Sessions built from the same
	// key and password must agree, which is what the rotation test compares
	// against.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	sessions, err := NewSessions(key, password, time.Hour, CookieSecureAuto)
	if err != nil {
		t.Fatalf("NewSessions: %v", err)
	}
	return sessions
}

// issue runs a login and returns the request a browser would send next.
func issue(t *testing.T, sessions *Sessions) *http.Request {
	t.Helper()

	recorder := httptest.NewRecorder()
	if err := sessions.Issue(recorder, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	next := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	for _, cookie := range recorder.Result().Cookies() {
		next.AddCookie(cookie)
	}
	return next
}

func TestAnIssuedCookieAuthenticates(t *testing.T) {
	sessions := newSessions(t, testPassword)

	if !sessions.Authenticated(issue(t, sessions)) {
		t.Fatal("a freshly issued cookie was not accepted")
	}
}

func TestARequestWithNoCookieIsNotAuthenticated(t *testing.T) {
	sessions := newSessions(t, testPassword)

	if sessions.Authenticated(httptest.NewRequest(http.MethodGet, "/api/tasks", nil)) {
		t.Fatal("a request with no cookie was accepted")
	}
}

func TestAnExpiredTokenIsRejected(t *testing.T) {
	sessions := newSessions(t, testPassword)

	token, err := sessions.mint(time.Now().Add(-time.Second))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	if sessions.verify(token) {
		t.Fatal("a token that expired a second ago was accepted")
	}
}

func TestATamperedTokenIsRejected(t *testing.T) {
	sessions := newSessions(t, testPassword)

	token, err := sessions.mint(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	payload, mac, _ := strings.Cut(token, ".")

	// Both halves, because they fail for different reasons: a changed payload
	// has to be caught by the MAC, and a changed MAC has to be caught by the
	// comparison.
	for name, tampered := range map[string]string{
		"payload": flipLast(payload) + "." + mac,
		"mac":     payload + "." + flipLast(mac),
		"no dot":  payload + mac,
		"empty":   "",
	} {
		if sessions.verify(tampered) {
			t.Errorf("a token with a tampered %s was accepted", name)
		}
	}
}

// A token minted under one password must not verify under another. This is what
// makes "change AUTH_PASSWORD and restart" a working log-out-everywhere.
func TestChangingThePasswordInvalidatesExistingTokens(t *testing.T) {
	original := newSessions(t, testPassword)

	token, err := original.mint(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	if !original.verify(token) {
		t.Fatal("the token did not verify under the password that minted it")
	}

	rotated := newSessions(t, "a-completely-different-password")
	if rotated.verify(token) {
		t.Fatal("a token minted under the old password still verifies after a rotation")
	}
}

func TestNoPasswordMeansEveryRequestIsAuthenticated(t *testing.T) {
	sessions := newSessions(t, "")

	if sessions.Enabled() {
		t.Fatal("Enabled reported true with no password configured")
	}
	if !sessions.Authenticated(httptest.NewRequest(http.MethodGet, "/api/tasks", nil)) {
		t.Fatal("a cookieless request was rejected with auth disabled")
	}
}

func TestPasswordMatchesOnlyTheConfiguredPassword(t *testing.T) {
	sessions := newSessions(t, testPassword)

	if !sessions.PasswordMatches(testPassword) {
		t.Fatal("the configured password did not match itself")
	}
	// A prefix specifically: hashing both sides first is what keeps the
	// comparison from returning early on a length mismatch.
	if sessions.PasswordMatches(testPassword[:5]) {
		t.Fatal("a prefix of the password matched")
	}
	if sessions.PasswordMatches("") {
		t.Fatal("an empty password matched")
	}
}

// `auto` is what lets a plain-HTTP LAN deployment log in at all: a Secure cookie
// over http is accepted by the browser and then dropped, and the next request
// 401s with nothing on screen to explain it.
func TestTheSecureFlagFollowsTheConfiguredMode(t *testing.T) {
	plain := httptest.NewRequest(http.MethodGet, "http://192.168.1.20:3000/", nil)

	forwarded := httptest.NewRequest(http.MethodGet, "http://192.168.1.20:3000/", nil)
	forwarded.Header.Set("X-Forwarded-Proto", "https")

	for name, tc := range map[string]struct {
		mode    CookieSecure
		request *http.Request
		want    bool
	}{
		"auto over plain http":    {CookieSecureAuto, plain, false},
		"auto behind a tls proxy": {CookieSecureAuto, forwarded, true},
		"always over plain http":  {CookieSecureAlways, plain, true},
		"never behind a proxy":    {CookieSecureNever, forwarded, false},
	} {
		sessions, err := NewSessions(make([]byte, 32), testPassword, time.Hour, tc.mode)
		if err != nil {
			t.Fatalf("NewSessions: %v", err)
		}
		if got := sessions.secureFor(tc.request); got != tc.want {
			t.Errorf("%s: Secure = %v, want %v", name, got, tc.want)
		}
	}
}

func TestClearWritesTheSameAttributesAsIssue(t *testing.T) {
	sessions := newSessions(t, testPassword)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)

	issued := httptest.NewRecorder()
	if err := sessions.Issue(issued, request); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	cleared := httptest.NewRecorder()
	sessions.Clear(cleared, request)

	before, after := issued.Result().Cookies()[0], cleared.Result().Cookies()[0]

	if before.Path != after.Path || before.SameSite != after.SameSite ||
		before.Secure != after.Secure || before.HttpOnly != after.HttpOnly {
		t.Fatalf("Clear wrote different attributes from Issue:\n issue: %+v\n clear: %+v", before, after)
	}
	if after.MaxAge >= 0 {
		t.Fatalf("the cleared cookie has MaxAge %d, which does not expire it", after.MaxAge)
	}
}

func TestParseCookieSecureRejectsAnythingElse(t *testing.T) {
	for _, valid := range []string{"auto", "AUTO", " true ", "false"} {
		if _, err := ParseCookieSecure(valid); err != nil {
			t.Errorf("ParseCookieSecure(%q): %v", valid, err)
		}
	}
	if _, err := ParseCookieSecure("yes"); err == nil {
		t.Fatal("ParseCookieSecure accepted \"yes\"")
	}
}

func flipLast(s string) string {
	if s == "" {
		return "x"
	}
	last := s[len(s)-1]
	if last == 'A' {
		return s[:len(s)-1] + "B"
	}
	return s[:len(s)-1] + "A"
}
