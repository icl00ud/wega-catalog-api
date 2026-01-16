package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"wega-catalog-api/internal/model"
	"wega-catalog-api/internal/repository"
	"wega-catalog-api/internal/service"
)

type FiltroHandler struct {
	catalogoSvc *service.CatalogoService
	produtoRepo *repository.ProdutoRepo
}

func NewFiltroHandler(catalogoSvc *service.CatalogoService, produtoRepo *repository.ProdutoRepo) *FiltroHandler {
	return &FiltroHandler{
		catalogoSvc: catalogoSvc,
		produtoRepo: produtoRepo,
	}
}

// BuscarFiltros busca filtros por veiculo (marca, modelo, ano, motor)
func (h *FiltroHandler) BuscarFiltros(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req model.BuscaFiltrosRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(model.ErrorResponse{
			Error:   "invalid_request",
			Message: "JSON invalido no corpo da requisicao",
		})
		return
	}

	response, err := h.catalogoSvc.BuscarFiltros(ctx, req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(model.ErrorResponse{
			Error:   "database_error",
			Message: "Erro ao buscar filtros",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// PorAplicacao busca filtros para uma aplicacao especifica pelo ID
func (h *FiltroHandler) PorAplicacao(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idParam := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(model.ErrorResponse{
			Error:   "invalid_id",
			Message: "ID da aplicacao deve ser um numero",
		})
		return
	}

	response, err := h.catalogoSvc.BuscarPorAplicacao(ctx, id)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(model.ErrorResponse{
			Error:   "not_found",
			Message: "Aplicacao nao encontrada",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ListTipos lista todos os tipos de filtro
func (h *FiltroHandler) ListTipos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tipos, err := h.produtoRepo.ListarTiposFiltro(ctx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(model.ErrorResponse{
			Error:   "database_error",
			Message: "Erro ao buscar tipos de filtro",
		})
		return
	}

	if tipos == nil {
		tipos = []model.TipoFiltro{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(model.TiposFiltroResponse{
		Tipos: tipos,
	})
}
