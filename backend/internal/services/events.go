package services

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type NormalizedEvent struct {
	ID      string `json:"id"`
	TS      string `json:"ts"`
	Level   string `json:"level"`
	Source  string `json:"source"`
	Event   string `json:"event"`
	Message string `json:"message"`
	Summary string `json:"summary,omitempty"`
}

type LogStats struct {
	Total    int `json:"total"`
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Success  int `json:"success"`
	Info     int `json:"info"`
}

type EventsResponse struct {
	Events []NormalizedEvent `json:"events"`
	Stats  LogStats          `json:"stats"`
}

// bufferedEvent pairs a normalized event with its parsed timestamp so range
// filters do not re-parse strings on every request.
type bufferedEvent struct {
	ev NormalizedEvent
	at time.Time
}

// ringBuffer is a fixed-capacity, in-memory event store. Adding an ID already
// present is a no-op, so the collector can re-read overlapping journal windows
// without duplicating rows. When the ring wraps, the evicted ID is dropped from
// the seen-set, which keeps that set exactly in sync with the ring and bounded.
type ringBuffer struct {
	mu     sync.Mutex
	buf    []bufferedEvent
	seen   map[string]struct{}
	next   int
	filled bool
}

func newRingBuffer(capacity int) *ringBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &ringBuffer{
		buf:  make([]bufferedEvent, capacity),
		seen: make(map[string]struct{}, capacity),
	}
}

// add stores e unless its ID is already held. Returns false when deduplicated.
func (r *ringBuffer) add(e bufferedEvent) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, dup := r.seen[e.ev.ID]; dup {
		return false
	}
	if r.filled {
		delete(r.seen, r.buf[r.next].ev.ID)
	}
	r.buf[r.next] = e
	r.seen[e.ev.ID] = struct{}{}
	r.next++
	if r.next == len(r.buf) {
		r.next = 0
		r.filled = true
	}
	return true
}

// snapshot returns the held events oldest first.
func (r *ringBuffer) snapshot() []bufferedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	n, start := r.next, 0
	if r.filled {
		n, start = len(r.buf), r.next
	}
	out := make([]bufferedEvent, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, r.buf[(start+i)%len(r.buf)])
	}
	return out
}

// eventTimeLayouts covers nginx access logs ("10/Oct/2026:13:00:00 +0000") and
// journalctl --output=short-iso. Unparseable stamps yield the zero time, which
// keeps those events visible in an unbounded "all time" query.
var eventTimeLayouts = []string{
	"02/Jan/2006:15:04:05 -0700",
	time.RFC3339,
	"2006-01-02T15:04:05-0700",
	"2006-01-02T15:04:05.000000-0700",
	"2006-01-02 15:04:05",
}

func parseEventTime(ts string) time.Time {
	ts = strings.TrimSpace(ts)
	for _, layout := range eventTimeLayouts {
		if t, err := time.Parse(layout, ts); err == nil {
			return t
		}
	}
	return time.Time{}
}

// EventService keeps normalized events in a bounded in-memory ring. A background
// collector refills it on an interval, so the dashboard and log downloads read
// memory instead of re-scrape nginx and journalctl per request.
type EventService struct {
	systemSvc *SystemService
	nginxSvc  *NginxService
	logger    *zap.Logger

	ring     *ringBuffer
	interval time.Duration

	collectMux sync.Mutex
}

func NewEventService(systemSvc *SystemService, nginxSvc *NginxService, maxEvents int, interval time.Duration, logger *zap.Logger) *EventService {
	if maxEvents <= 0 {
		maxEvents = 5000
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &EventService{
		systemSvc: systemSvc,
		nginxSvc:  nginxSvc,
		logger:    logger,
		ring:      newRingBuffer(maxEvents),
		interval:  interval,
	}
}

// Start backfills once, then keeps the ring fresh until ctx is cancelled.
func (s *EventService) Start(ctx context.Context) {
	s.collect(ctx)

	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.collect(ctx)
			}
		}
	}()
}

// collect scrapes the log sources once and merges them into the ring.
func (s *EventService) collect(ctx context.Context) {
	s.collectMux.Lock()
	defer s.collectMux.Unlock()

	for _, e := range s.scrape() {
		s.ring.add(bufferedEvent{ev: e, at: parseEventTime(e.TS)})
	}
}

// GetNormalizedEvents returns the most recent events, newest first.
func (s *EventService) GetNormalizedEvents(lines int) (*EventsResponse, error) {
	return s.GetEventsFiltered(lines, time.Time{}, time.Time{}), nil
}

// GetEventsFiltered returns events newer than since and older than until, newest
// first. A zero bound is unbounded.
func (s *EventService) GetEventsFiltered(lines int, since, until time.Time) *EventsResponse {
	held := s.ring.snapshot()

	out := make([]NormalizedEvent, 0, len(held))
	for _, b := range held {
		if !since.IsZero() && b.at.Before(since) {
			continue
		}
		if !until.IsZero() && b.at.After(until) {
			continue
		}
		out = append(out, b.ev)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].TS == out[j].TS {
			return out[i].ID > out[j].ID
		}
		return out[i].TS > out[j].TS
	})

	if lines > 0 && len(out) > lines {
		out = out[:lines]
	}

	stats := LogStats{Total: len(out)}
	for _, e := range out {
		switch e.Level {
		case "error":
			stats.Errors++
		case "warning":
			stats.Warnings++
		case "success":
			stats.Success++
		case "info":
			stats.Info++
		}
	}

	return &EventsResponse{Events: out, Stats: stats}
}

// FormatEventsPlain renders events as a downloadable plain-text log.
func (s *EventService) FormatEventsPlain(resp *EventsResponse) string {
	var b strings.Builder
	for _, e := range resp.Events {
		fmt.Fprintf(&b, "%s [%s] [%s] [%s] %s\n",
			e.TS, strings.ToUpper(e.Level), e.Source, e.Event, e.Message)
	}
	return b.String()
}

// scrape reads the current log sources and returns freshly normalized events.
// ponytail: reads a fixed window of recent lines per source on every tick and
// relies on the ring's ID dedup to discard overlap. If scrape cost ever matters
// on a Pi, swap journalctl for a "--since <last-poll>" cursor and tail
// access.log by byte offset instead.
// scrapeLines is how many recent lines are read per source on each tick. It is
// wider than any single page request so the buffer stays complete between polls.
const scrapeLines = 200

func (s *EventService) scrape() []NormalizedEvent {
	var events []NormalizedEvent

	// Fetch Nginx Access Logs
	if accessLogs, err := s.nginxSvc.GetAccessLog(scrapeLines); err == nil {
		for _, al := range accessLogs {
			level := "info"
			if al.Status >= 400 {
				level = "error"
			}
			msg := fmt.Sprintf("%s %s - HTTP %d - %d bytes", al.Method, al.Path, al.Status, al.ResponseSize)
			if isNoise("NGINX", msg) {
				continue
			}

			// Try to parse ts
			ts := al.Timestamp // assuming string

			events = append(events, NormalizedEvent{
				ID:      generateID("nginx_acc", ts, msg),
				TS:      ts,
				Level:   level,
				Source:  "NGINX",
				Event:   "http_request",
				Message: msg,
			})
		}
	}

	// Fetch Journalctl Logs (App, Nginx, Cloudflared, System)
	fetchAndNormalize := func(serviceName, source string) {
		logs, err := s.systemSvc.GetJournalLogs(serviceName, scrapeLines)
		if err != nil {
			return
		}
		for _, l := range logs {
			msg := l.Message
			msgLower := strings.ToLower(msg)
			if isNoise(source, msg) {
				continue
			}

			// Clean up syslog timestamp prefix if present
			parts := strings.SplitN(msg, "]: ", 2)
			if len(parts) == 2 {
				msg = parts[1]
			}

			// Check for JSON
			if strings.HasPrefix(msg, "{") && strings.HasSuffix(msg, "}") {
				var j map[string]interface{}
				if err := json.Unmarshal([]byte(msg), &j); err == nil {
					// Extract fields
					extractedMsg := ""
					if m, ok := j["msg"].(string); ok {
						extractedMsg = m
					} else if m, ok := j["message"].(string); ok {
						extractedMsg = m
					}

					// Skip verbose runner logs
					if strings.Contains(extractedMsg, "job started") || strings.Contains(extractedMsg, "job completed") {
						continue
					}

					if extractedMsg != "" {
						msg = extractedMsg
						if errStr, ok := j["error"].(string); ok && errStr != "" {
							msg += " - " + errStr
						}
					}
				}
			}

			level := "info"
			lLevel := strings.ToLower(l.Level)
			if strings.Contains(lLevel, "err") || strings.Contains(msgLower, "error") || strings.Contains(msgLower, "fail") {
				level = "error"
			} else if strings.Contains(lLevel, "warn") || strings.Contains(msgLower, "warn") {
				level = "warning"
			} else if strings.Contains(lLevel, "ok") || strings.Contains(msgLower, "success") || strings.Contains(msgLower, "complete") {
				level = "success"
			}

			events = append(events, NormalizedEvent{
				ID:      generateID(source, l.Timestamp, msg),
				TS:      l.Timestamp,
				Level:   level,
				Source:  strings.ToUpper(source),
				Event:   "system_event",
				Message: msg,
			})
		}
	}

	fetchAndNormalize("opendeploy", "APP")
	fetchAndNormalize("nginx", "NGINX")
	fetchAndNormalize("cloudflared", "CLOUDFLARED")

	return events
}

func generateID(source, ts, msg string) string {
	h := sha1.New()
	h.Write([]byte(source + ts + msg))
	return "evt_" + hex.EncodeToString(h.Sum(nil))[:8]
}

// noiseSubstrings are messages the dashboard should not show. Each entry is a
// recurring, self-inflicted line that says nothing about the host's health:
// the dashboard's own polling, the log collector's journalctl invocations, and
// sudo session bookkeeping. Logging these made the page look machine-generated
// and buried the events a human actually needs.
var noiseSubstrings = []string{
	// The dashboard polls these continuously; each poll is otherwise an event.
	"get /api/v1/logs/events",
	"get /api/v1/system",
	"get /api/v1/wifi",
	"get /api/v1/services",
	"get /api/v1/logs",
	"get /api/v1/nginx",
	"get /api/v1/auth",
	"post /api/v1/auth",
	"get /api/v1/internet",
	"get /api/v1/tunnel",
	// Do NOT filter /api/v1/deploy(s) here: a failed deploy is exactly the
	// kind of real error this page exists to show.
	// The collector's own commands — never log how we fetched the logs.
	"job started",
	"job completed",
	"journal_logs",
	"/usr/bin/journalctl",
	"failed to create job record",
	"failed to create log file",
	"failed to update job record",
	// sudo session bookkeeping.
	"pam_unix(sudo:session)",
	"sudo session opened",
	"sudo session closed",
	"consumed",
	"deactivated successfully",
	"stopped opendeploy",
	"started opendeploy",
}

// noisePrefixes are path prefixes whose requests are infrastructure, not user
// activity: the SPA's own assets and Next.js internals.
var noisePrefixes = []string{
	"/_next/",
	"/favicon",
	"/__nextjs",
	"/favicon.ico",
}

// isNoise reports whether an event is dashboard chatter rather than a real
// occurrence worth showing.
func isNoise(source, msg string) bool {
	lower := strings.ToLower(msg)

	for _, p := range noisePrefixes {
		if strings.Contains(lower, p) {
			return true
		}
	}
	for _, n := range noiseSubstrings {
		if strings.Contains(lower, n) {
			return true
		}
	}

	// Nginx access lines are "GET /path - HTTP 200 - n bytes"; treat any polled
	// or asset path as noise rather than an event.
	if source == "NGINX" {
		for _, p := range noisePrefixes {
			if strings.Contains(lower, " "+p) {
				return true
			}
		}
	}
	return false
}
