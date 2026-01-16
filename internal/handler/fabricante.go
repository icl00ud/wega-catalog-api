package handler

import (
	"encoding/json"
	"net/http"

	"wega-catalog-api/internal/model"
	"wega-catalog-api/internal/repository"
)

type FabricanteHandler struct {
	repo *repository.FabricanteRepo
}

func NewFabricanteHandler(repo *repository.FabricanteRepo) *FabricanteHandler {
	return &FabricanteHandler{repo: repo}
}

func (h *FabricanteHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tipo := r.URL.Query().Get("tipo")

	var fabricantes []model.Fabricante
	var err error

	switch tipo {
	case "concorrente":
		fabricantes, err = h.repo.ListarConcorrentes(ctx)
	default:
		fabricantes, err = h.repo.ListarVeiculos(ctx)
	}

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(model.ErrorResponse{
			Error:   "database_error",
			Message: "Erro ao buscar fabricantes",
		})
		return
	}

	if fabricantes == nil {
		fabricantes = []model.Fabricante{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(model.FabricantesResponse{
		Fabricantes: fabricantes,
	})
}
