package api

import (
	"encoding/json"
	"log"
	"net/http"
	"fmt"
	"bufio"
	"net"
	"time"

	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

// JSON response helpers

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

func respondOK(w http.ResponseWriter, data interface{}) {
	respondJSON(w, http.StatusOK, data)
}

// Logging middleware
func loggingMiddleware(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			logger.Debug("request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", wrapped.status),
				zap.Duration("duration", time.Since(start)),
			)
		})
	}
}

// LAN Access only middleware
func lanAccessMiddleware(db *state.DB, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lanOnly, _ := db.GetSetupState("lan_access_only")
			if lanOnly == "true" {
				if r.Header.Get("CF-Connecting-IP") != "" {
					logger.Warn("Blocked Cloudflare access due to LAN-only policy",
						zap.String("ip", r.Header.Get("CF-Connecting-IP")),
						zap.String("path", r.URL.Path),
					)
					http.Error(w, `{"error":"access restricted to local network only"}`, http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// parseBody decodes a JSON request body into the target.
func parseBody(r *http.Request, target interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    hijacker, ok := w.ResponseWriter.(http.Hijacker)
    if !ok {
        return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
    }
    return hijacker.Hijack()
}

// Flush implements http.Flusher for SSE support
func (w *statusWriter) Flush() {
    if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
        flusher.Flush()
    }
}