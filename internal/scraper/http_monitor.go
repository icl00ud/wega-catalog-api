package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// HTTPMonitor provides HTTP endpoints for monitoring scraper progress
type HTTPMonitor struct {
	server   *http.Server
	progress *ProgressTracker
}

// NewHTTPMonitor creates a new HTTP monitoring server
func NewHTTPMonitor(port int, progress *ProgressTracker) *HTTPMonitor {
	mux := http.NewServeMux()

	monitor := &HTTPMonitor{
		server: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
		progress: progress,
	}

	mux.HandleFunc("/status", monitor.handleStatus)
	mux.HandleFunc("/health", monitor.handleHealth)

	return monitor
}

// Start starts the HTTP server in a goroutine
func (m *HTTPMonitor) Start() error {
	go func() {
		slog.Info("Starting HTTP monitor", "addr", m.server.Addr)
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP monitor error", "error", err)
		}
	}()
	return nil
}

// Stop gracefully stops the HTTP server
func (m *HTTPMonitor) Stop(ctx context.Context) error {
	slog.Info("Stopping HTTP monitor")
	return m.server.Shutdown(ctx)
}

// handleStatus returns current scraper status as JSON
func (m *HTTPMonitor) handleStatus(w http.ResponseWriter, r *http.Request) {
	snapshot := m.progress.GetSnapshot()

	response := map[string]interface{}{
		"status":     snapshot.Status,
		"started_at": snapshot.StartedAt.Format(time.RFC3339),
		"elapsed":    snapshot.Elapsed.String(),
		"progress": map[string]interface{}{
			"total_vehicles": snapshot.TotalVehicles,
			"processed":      snapshot.Processed,
			"success":        snapshot.Success,
			"failed":         snapshot.Failed,
			"skipped":        snapshot.Skipped,
			"percentage":     fmt.Sprintf("%.2f", snapshot.Percentage),
		},
		"matching_stats": map[string]interface{}{
			"exact_match": snapshot.ExactMatch,
			"fuzzy_match": snapshot.FuzzyMatch,
			"no_match":    snapshot.NoMatch,
		},
		"rate": map[string]interface{}{
			"current_rps":           fmt.Sprintf("%.2f", snapshot.RequestsPerSec),
			"avg_time_per_vehicle":  fmt.Sprintf("%.2fs", snapshot.AvgTimePerVehicle),
		},
		"eta": map[string]interface{}{
			"remaining_vehicles":      snapshot.TotalVehicles - snapshot.Processed,
			"estimated_completion":    snapshot.ETA.Format(time.RFC3339),
			"time_remaining":          snapshot.Remaining.String(),
		},
		"last_error":      snapshot.LastError,
		"current_vehicle": snapshot.CurrentVehicle,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleHealth returns simple health check
func (m *HTTPMonitor) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}
