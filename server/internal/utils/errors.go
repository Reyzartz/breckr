package utils

import (
	"encoding/json"
	"fmt"
)

// ValidationError is the rejection type every user-input validator returns.
//
// It carries the offending field so the route boundary can answer 400 with
// {error, field} -- the dashboard shows the message against the control the
// user got wrong instead of as a page-level banner.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// Fail builds a *ValidationError. Returned rather than panicked so the whole
// rejection table stays ordinary Go control flow.
func Fail(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// SafeMarshal serializes a task result for storage, and never fails.
//
// A run's result may hold something json.Marshal chokes on. Recording a
// diagnostic beats throwing away an otherwise good run.
func SafeMarshal(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		fallback, _ := json.Marshal(map[string]string{"_unserializable": err.Error()})
		return string(fallback)
	}
	return string(encoded)
}

// ErrorMessage is the message alone, for log lines and API responses.
func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
