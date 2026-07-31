package api

import (
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"breckr-server/internal/auth"
	"breckr-server/internal/types"
	"breckr-server/internal/utils"
)

type AuthHandler struct {
	logger   *log.Logger
	sessions *auth.Sessions
	throttle *auth.Throttle
}

func NewAuthHandler(logger *log.Logger, sessions *auth.Sessions, throttle *auth.Throttle) *AuthHandler {
	return &AuthHandler{logger: logger, sessions: sessions, throttle: throttle}
}

// HandleStatus reports whether a login is needed and whether this caller has
// already done it. Public, and the one call the dashboard makes before it knows
// either.
func (ah *AuthHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{
		"data": types.AuthStatusResponse{
			Required:      ah.sessions.Enabled(),
			Authenticated: auth.IsAuthenticated(r.Context()),
		},
	})
}

func (ah *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	// Defensive: with no password configured the dashboard never renders a login
	// page, so reaching this is a sign of a stale tab rather than a wrong guess.
	if !ah.sessions.Enabled() {
		utils.WriteError(w, http.StatusBadRequest, "Authentication is not enabled on this server.", "")
		return
	}

	key := auth.ClientKey(r)
	if allowed, retryAfter := ah.throttle.Allow(key); !allowed {
		minutes := int(math.Ceil(retryAfter.Minutes()))
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
		utils.WriteError(w, http.StatusTooManyRequests,
			"Too many attempts. Try again in "+strconv.Itoa(minutes)+" minute(s).", "")
		return
	}

	var request types.LoginRequest
	if err := utils.ReadRequestBody(r, &request); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Could not read the request body.", "")
		return
	}

	if !ah.sessions.PasswordMatches(request.Password) {
		ah.throttle.RecordFailure(key)
		// Every rejection is slow, not only the ones that close the window --
		// enough to make online guessing impractical, short enough that a typo
		// is not punished noticeably.
		time.Sleep(auth.FailureDelay)

		ah.logger.Printf("WARN: rejected a login from %s", key)
		utils.WriteError(w, http.StatusUnauthorized, "Incorrect password.", "password")
		return
	}

	// Otherwise four typos followed by the right password would leave the user
	// one mistake from locking themselves out of their own dashboard.
	ah.throttle.Reset(key)

	if err := ah.sessions.Issue(w, r); err != nil {
		ah.logger.Printf("ERROR: could not issue a session: %v", err)
		utils.WriteError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{
		"data": types.LoginResponse{OK: true},
	})
}

// HandleLogout clears the cookie. Public on purpose: a stale or unreadable
// session should always be droppable, and there is nothing to protect in
// throwing one away.
func (ah *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	ah.sessions.Clear(w, r)
	w.WriteHeader(http.StatusNoContent)
}
