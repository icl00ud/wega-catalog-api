package handler

import (
	"encoding/json"
	"net/http"

	"wega-catalog-api/internal/model"
	"wega-catalog-api/internal/repository"
)

type ReferenciaHandler struct {
	repo *repository.ReferenciaRepo
}

func NewReferenciaHandler(repo *repository.ReferenciaRepo) *ReferenciaHandler {
	return &ReferenciaHandler{repo: repo}
}

// Buscar busca equivalencias Wega para um codigo de concorrente
func (h *ReferenciaHandler) Buscar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	codigo := r.URL.Query().Get("codigo")
	if codigo == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(model.ErrorResponse{
			Error:   "missing_param",
			Message: "Parametro 'codigo' e obrigatorio",
		})
		return
	}

	response, err := h.repo.BuscarPorCodigo(ctx, codigo)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(model.ErrorResponse{
			Error:   "database_error",
			Message: "Erro ao buscar referencia cruzada",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
