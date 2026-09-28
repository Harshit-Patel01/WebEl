package api

import (
	"net/http"
	"strconv"

	"github.com/opendeploy/opendeploy/internal/services"
)

type eventsHandlers struct {
	eventSvc *services.EventService
}

func (h *eventsHandlers) getEvents(w http.ResponseWriter, r *http.Request) {
	lines := 100
	if q := r.URL.Query().Get("lines"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			lines = n
		}
	}

	resp, err := h.eventSvc.GetNormalizedEvents(lines)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, resp)
}
