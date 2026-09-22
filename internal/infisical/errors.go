package infisical

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/joaoseixas88/lazylock/internal/domain"
)

// APIError is what the TUI renders when a call fails. It deliberately carries
// the path without the query string: query values are where project ids and
// paths live, and an error pane is the wrong place for them.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Err        string // Infisical's "error"
	Message    string // Infisical's "message", flattened
	ReqID      string // Infisical's "reqId", which self-hosted debugging needs
}

func (e *APIError) Error() string {
	parts := []string{fmt.Sprintf("%s %s: %d", e.Method, e.Path, e.StatusCode)}
	if e.Message != "" {
		parts = append(parts, e.Message)
	} else if e.Err != "" {
		parts = append(parts, e.Err)
	} else {
		parts = append(parts, http.StatusText(e.StatusCode))
	}
	if e.ReqID != "" {
		parts = append(parts, "reqId "+e.ReqID)
	}
	return strings.Join(parts, ": ")
}

// Unwrap maps the statuses the UI reacts to onto shared sentinels.
func (e *APIError) Unwrap() error {
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return domain.ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	}
	return nil
}

var ErrForbidden = fmt.Errorf("infisical: permission denied")

// wireError is Infisical's error envelope. message is usually a string, but on
// a zod validation failure it is an array of issue objects, so it has to be
// decoded lazily or every 422 surfaces as a json unmarshal complaint instead of
// the real problem.
type wireError struct {
	StatusCode int             `json:"statusCode"`
	Message    json.RawMessage `json:"message"`
	Error      string          `json:"error"`
	ReqID      string          `json:"reqId"`
}

// flattenMessage renders the message field whatever shape it arrived in.
func flattenMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var issues []struct {
		Message  string `json:"message"`
		Path     []any  `json:"path"`
		Code     string `json:"code"`
		Expected any    `json:"expected"`
	}
	if err := json.Unmarshal(raw, &issues); err == nil && len(issues) > 0 {
		var parts []string
		for _, issue := range issues {
			if issue.Message != "" {
				parts = append(parts, issue.Message)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	return truncate(string(raw), 200)
}

func truncate(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
