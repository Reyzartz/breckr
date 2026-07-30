// Package utils holds the small helpers shared across the HTTP layer: the JSON
// response envelope, request decoding, query-param reading, and the validation
// error type every user-input validator returns.
package utils

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/schema"
)

// NewID mints an identifier for rows the user does not name themselves.
//
// Random rather than sequential because it appears in URLs: channel ids should
// not let anyone count how many you have.
func NewID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		// crypto/rand does not fail in practice; a timestamp still yields a
		// usable unique id if it ever does.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(raw)
}

// Timestamp is the format every stored and reported time uses.
func Timestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// Envelope is the shape of every JSON response body: {"data": ...} on success,
// {"error": ..., "field": ...} on failure.
type Envelope map[string]any

func WriteJSONResponse(w http.ResponseWriter, status int, data Envelope) error {
	js, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	js = append(js, '\n')

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(js)

	return err
}

// WriteError is the failure half of the envelope. `field` is optional and only
// set for validation failures -- the dashboard renders the message against that
// control rather than as a page-level banner.
func WriteError(w http.ResponseWriter, status int, message, field string) {
	envelope := Envelope{"error": message}
	if field != "" {
		envelope["field"] = field
	}
	_ = WriteJSONResponse(w, status, envelope)
}

// WriteValidationError renders a *ValidationError as a 400 naming the offending
// field, and reports whether it handled the error. Anything else is the
// caller's to deal with.
func WriteValidationError(w http.ResponseWriter, err error) bool {
	var ve *ValidationError
	if errors.As(err, &ve) {
		WriteError(w, http.StatusBadRequest, ve.Message, ve.Field)
		return true
	}
	return false
}

func ReadRequestBody(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	return dec.Decode(dst)
}

func ReadIDParam(r *http.Request) string {
	return chi.URLParam(r, "id")
}

func ReadInt64Param(r *http.Request, key string) (int64, error) {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return 0, fmt.Errorf("invalid %s parameter", key)
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s parameter", key)
	}
	return value, nil
}

func ReadStringQueryParam(r *http.Request, key, defaultValue string) string {
	if value := r.URL.Query().Get(key); value != "" {
		return value
	}
	return defaultValue
}

func ReadIntQueryParam(r *http.Request, key string, defaultValue int) (int, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return defaultValue, nil
	}
	return strconv.Atoi(raw)
}

func QueryParamsDecoder[T any](r *http.Request, dst *T) error {
	decoder := schema.NewDecoder()
	decoder.IgnoreUnknownKeys(true)

	return decoder.Decode(dst, r.URL.Query())
}
