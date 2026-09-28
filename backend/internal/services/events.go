package services

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"crypto/sha1"
	"encoding/hex"
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

type EventService struct {
	systemSvc *SystemService
	nginxSvc  *NginxService
}

func NewEventService(systemSvc *SystemService, nginxSvc *NginxService) *EventService {
	return &EventService{systemSvc: systemSvc, nginxSvc: nginxSvc}
}

func (s *EventService) GetNormalizedEvents(lines int) (*EventsResponse, error) {
	var events []NormalizedEvent

	// Fetch Nginx Access Logs
	if accessLogs, err := s.nginxSvc.GetAccessLog(lines); err == nil {
		for _, al := range accessLogs {
			level := "info"
			if al.Status >= 400 {
				level = "error"
			}
			msg := fmt.Sprintf("%s %s - HTTP %d - %d bytes", al.Method, al.Path, al.Status, al.ResponseSize)
			
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
		logs, err := s.systemSvc.GetJournalLogs(serviceName, lines)
		if err != nil {
			return
		}
		for _, l := range logs {
			// Skip internal polling and repetitive logs
			msg := l.Message
			msgLower := strings.ToLower(msg)
if strings.Contains(msgLower, "pam_unix(sudo:session)") ||
				strings.Contains(msgLower, "sudo session opened") ||
				strings.Contains(msgLower, "sudo session closed") ||
			strings.Contains(msgLower, "get /api/v1/system") ||
				strings.Contains(msgLower, "get /api/v1/wifi") ||
				strings.Contains(msgLower, "get /api/v1/services") ||
				strings.Contains(msgLower, "get /api/v1/logs/events") ||
				strings.Contains(msgLower, "get /api/v1/nginx/logs") {
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
	
	// Deduplicate events that are too similar in close proximity
	events = deduplicateEvents(events)

	// Sort by timestamp desc
	sort.Slice(events, func(i, j int) bool {
		return events[i].TS > events[j].TS
	})
	
	// Cap at 'lines' count
	if len(events) > lines {
		events = events[:lines]
	}

	stats := LogStats{}
	stats.Total = len(events)
	for _, e := range events {
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

	return &EventsResponse{
		Events: events,
		Stats:  stats,
	}, nil
}

func generateID(source, ts, msg string) string {
	h := sha1.New()
	h.Write([]byte(source + ts + msg))
	return "evt_" + hex.EncodeToString(h.Sum(nil))[:8]
}

func deduplicateEvents(events []NormalizedEvent) []NormalizedEvent {
	// Simple deduplication based on exact message match within a time window
	// For now, let's just do exact message deduplication globally, keeping the latest one, and adding a count if we wanted to (but we'll just keep the latest for simplicity).
	
	// To implement "· X occurrences", let's augment the message.
	type key struct {
		source string
		msg    string
	}
	
	counts := make(map[key]int)
	latest := make(map[key]NormalizedEvent)
	
	for _, e := range events {
		k := key{e.Source, e.Message}
		counts[k]++
		// If ts is newer (lexicographically higher) or first seen
		if existing, ok := latest[k]; !ok || e.TS > existing.TS {
			latest[k] = e
		}
	}
	
	var deduped []NormalizedEvent
	for k, e := range latest {
		count := counts[k]
		if count > 1 {
			e.Message = fmt.Sprintf("%s · %d occurrences", e.Message, count)
		}
		deduped = append(deduped, e)
	}
	
	return deduped
}
