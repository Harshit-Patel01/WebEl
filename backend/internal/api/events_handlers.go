package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/opendeploy/opendeploy/internal/services"
)

type eventsHandlers struct {
	eventSvc *services.EventService
}

// defaultEventLines matches the previous per-request default.
const defaultEventLines = 100

// maxEventLines caps a single response so one request cannot force a huge
// serialize+snapshot on a low-resource host.
const maxEventLines = 1000

func (h *eventsHandlers) getEvents(w http.ResponseWriter, r *http.Request) {
	lines := requestLines(r)

	resp := h.eventSvc.GetEventsFiltered(lines, requestSince(r), requestUntil(r))
	respondOK(w, resp)
}

// downloadEvents streams the current buffer as a plain-text log. It honours the
// same lines/since/until parameters as getEvents so the download matches
// whatever range the UI is showing.
func (h *eventsHandlers) downloadEvents(w http.ResponseWriter, r *http.Request) {
	resp := h.eventSvc.GetEventsFiltered(requestLines(r), requestSince(r), requestUntil(r))
	body := h.eventSvc.FormatEventsPlain(resp)

	filename := "opendeploy-logs.txt"
	if src := r.URL.Query().Get("source"); src != "" && src != "ALL" {
		filename = fmt.Sprintf("opendeploy-logs-%s-%s.log",
			sanitizeFilename(src), time.Now().UTC().Format("20060102-150405"))
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// requestLines reads ?lines=, clamped to [1, maxEventLines].
func requestLines(r *http.Request) int {
	lines := defaultEventLines
	if q := r.URL.Query().Get("lines"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			lines = n
		}
	}
	if lines > maxEventLines {
		lines = maxEventLines
	}
	return lines
}

// requestSince reads ?since= as minutes ago, or an RFC3339 timestamp.
func requestSince(r *http.Request) time.Time {
	return parseTimeParam(r.URL.Query().Get("since"))
}

// requestUntil reads ?until= as an RFC3339 timestamp.
func requestUntil(r *http.Request) time.Time {
	return parseTimeParam(r.URL.Query().Get("until"))
}

// parseTimeParam accepts either a bare number of minutes before now ("10") or
// an RFC3339 stamp. Anything unparseable means "no bound", which is safer than
// silently defaulting to a range the caller did not ask for.
func parseTimeParam(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	if mins, err := strconv.Atoi(v); err == nil {
		if mins <= 0 {
			return time.Time{}
		}
		return time.Now().Add(-time.Duration(mins) * time.Minute)
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t
	}
	return time.Time{}
}

// sanitizeFilename keeps a source name safe for a Content-Disposition header.
func sanitizeFilename(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "all"
	}
	return string(out)
}
