package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"wega-catalog-api/internal/model"
)

type HealthHandler struct {
	db *pgxpool.Pool
}

func NewHealthHandler(db *pgxpool.Pool) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	dbStatus := "connected"
	if err := h.db.Ping(ctx); err != nil {
		dbStatus = "disconnected"
	}

	response := model.HealthResponse{
		Status:    "ok",
		Database:  dbStatus,
		Timestamp: time.Now(),
	}

	if dbStatus == "disconnected" {
		response.Status = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
