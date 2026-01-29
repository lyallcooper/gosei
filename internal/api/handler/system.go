package handler

import (
	"net/http"
	"runtime"
)

// SystemHandler handles system-related API requests
type SystemHandler struct {
	version string
}

// NewSystemHandler creates a new system handler
func NewSystemHandler(version string) *SystemHandler {
	return &SystemHandler{version: version}
}

// Health returns health status
func (h *SystemHandler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

// Version returns version information
func (h *SystemHandler) Version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version":   h.version,
		"goVersion": runtime.Version(),
		"os":        runtime.GOOS,
		"arch":      runtime.GOARCH,
	})
}
