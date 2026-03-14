package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

type sseHandlers struct {
	db     *state.DB
	logger *zap.Logger
}

// streamDeployLogs streams deployment logs via Server-Sent Events (SSE).
// The client connects once and receives logs in real-time.
// First, all existing logs are sent (catch-up), then new logs are polled.
func (h *sseHandlers) streamDeployLogs(w http.ResponseWriter, r *http.Request) {
	deployID := chi.URLParam(r, "deployId")
	if deployID == "" {
		http.Error(w, "missing deployId", http.StatusBadRequest)
		return
	}

	// Verify deploy exists
	deploy, err := h.db.GetDeploy(deployID)
	if err != nil {
		h.logger.Error("failed to get deploy", zap.String("deployId", deployID), zap.Error(err))
		http.Error(w, "failed to get deploy", http.StatusInternalServerError)
		return
	}
	if deploy == nil {
		http.Error(w, "deploy not found", http.StatusNotFound)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send initial deploy status
	statusData, _ := json.Marshal(map[string]interface{}{
		"type":   "status",
		"status": deploy.Status,
	})
	fmt.Fprintf(w, "event: status\ndata: %s\n\n", statusData)
	flusher.Flush()

	// Send all existing logs first (catch-up)
	existingLogs, err := h.db.ListDeployLogs(deployID, 10000, 0)
	if err != nil {
		h.logger.Error("failed to list existing logs", zap.String("deployId", deployID), zap.Error(err))
		// Don't return error here, just log it and continue
		existingLogs = []state.DeployLog{}
	}

	var lastTimestamp time.Time
	for _, log := range existingLogs {
		logData, _ := json.Marshal(map[string]interface{}{
			"type":      "log",
			"stream":    log.Stream,
			"message":   log.Message,
			"timestamp": log.LogTimestamp,
		})
		fmt.Fprintf(w, "event: log\ndata: %s\n\n", logData)
		lastTimestamp = log.LogTimestamp
	}
	flusher.Flush()

	// If deploy is already finished, send done and close
	if deploy.Status == "success" || deploy.Status == "failed" {
		doneData, _ := json.Marshal(map[string]interface{}{
			"type":          "done",
			"status":        deploy.Status,
			"outputPath":    deploy.OutputPath,
			"framework":     deploy.Framework,
			"isBackend":     deploy.IsBackend,
			"buildDuration": deploy.BuildDuration,
		})
		fmt.Fprintf(w, "event: done\ndata: %s\n\n", doneData)
		flusher.Flush()
		return
	}

	// Poll for new logs until deploy is complete
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			// Send heartbeat to keep connection alive
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-ticker.C:
			// Poll for new logs
			var newLogs []state.DeployLog
			if !lastTimestamp.IsZero() {
				newLogs, err = h.db.GetDeployLogsAfter(deployID, lastTimestamp)
			} else {
				newLogs, err = h.db.ListDeployLogs(deployID, 10000, 0)
			}

			if err != nil {
				h.logger.Warn("failed to poll logs", zap.String("deployId", deployID), zap.Error(err))
				continue
			}

			for _, log := range newLogs {
				logData, _ := json.Marshal(map[string]interface{}{
					"type":      "log",
					"stream":    log.Stream,
					"message":   log.Message,
					"timestamp": log.LogTimestamp,
				})
				fmt.Fprintf(w, "event: log\ndata: %s\n\n", logData)
				lastTimestamp = log.LogTimestamp
			}

			if len(newLogs) > 0 {
				flusher.Flush()
			}

			// Check if deploy is complete
			deploy, err = h.db.GetDeploy(deployID)
			if err != nil {
				h.logger.Warn("failed to get deploy status", zap.String("deployId", deployID), zap.Error(err))
				continue
			}

			if deploy != nil && (deploy.Status == "success" || deploy.Status == "failed") {
				doneData, _ := json.Marshal(map[string]interface{}{
					"type":          "done",
					"status":        deploy.Status,
					"outputPath":    deploy.OutputPath,
					"framework":     deploy.Framework,
					"isBackend":     deploy.IsBackend,
					"buildDuration": deploy.BuildDuration,
				})
				fmt.Fprintf(w, "event: done\ndata: %s\n\n", doneData)
				flusher.Flush()
				return
			}
		}
	}
}

// longPollDeployLogs returns logs after a given timestamp, blocking up to 10s for new logs
func (h *sseHandlers) longPollDeployLogs(w http.ResponseWriter, r *http.Request) {
	deployID := chi.URLParam(r, "deployId")
	if deployID == "" {
		respondError(w, http.StatusBadRequest, "missing deployId")
		return
	}

	afterStr := r.URL.Query().Get("after")
	var after time.Time
	if afterStr != "" {
		after, _ = time.Parse(time.RFC3339Nano, afterStr)
	}

	// Try to get logs immediately
	var logs []state.DeployLog
	var err error

	if !after.IsZero() {
		logs, err = h.db.GetDeployLogsAfter(deployID, after)
	} else {
		logs, err = h.db.ListDeployLogs(deployID, 10000, 0)
	}

	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get logs")
		return
	}

	// If we have logs, return immediately
	if len(logs) > 0 {
		respondOK(w, logs)
		return
	}

	// Otherwise, wait up to 10 seconds for new logs
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			respondOK(w, []state.DeployLog{})
			return
		case <-deadline:
			// Timeout — return empty
			respondOK(w, []state.DeployLog{})
			return
		case <-ticker.C:
			if !after.IsZero() {
				logs, err = h.db.GetDeployLogsAfter(deployID, after)
			} else {
				logs, err = h.db.ListDeployLogs(deployID, 10000, 0)
			}

			if err != nil {
				continue
			}

			if len(logs) > 0 {
				respondOK(w, logs)
				return
			}
		}
	}
}
