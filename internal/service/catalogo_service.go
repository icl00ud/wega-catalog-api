package service

import (
	"context"

	"wega-catalog-api/internal/model"
	"wega-catalog-api/internal/repository"
)

type CatalogoService struct {
	fabricanteRepo *repository.FabricanteRepo
	aplicacaoRepo  *repository.AplicacaoRepo
	produtoRepo    *repository.ProdutoRepo
	referenciaRepo *repository.ReferenciaRepo
}

func NewCatalogoService(
	fr *repository.FabricanteRepo,
	ar *repository.AplicacaoRepo,
	pr *repository.ProdutoRepo,
	rr *repository.ReferenciaRepo,
) *CatalogoService {
	return &CatalogoService{
		fabricanteRepo: fr,
		aplicacaoRepo:  ar,
		produtoRepo:    pr,
		referenciaRepo: rr,
	}
}

// BuscarFiltros busca filtros para um veiculo
func (s *CatalogoService) BuscarFiltros(ctx context.Context, req model.BuscaFiltrosRequest) (*model.BuscaFiltrosResponse, error) {
	// Validar campos obrigatorios
	if req.Marca == "" || req.Modelo == "" {
		return &model.BuscaFiltrosResponse{
			Status:          "incompleto",
			Mensagem:        "Preciso saber pelo menos a marca e o modelo do veiculo.",
			CamposFaltantes: []string{"marca", "modelo"},
		}, nil
	}

	// Buscar aplicacoes que combinam
	aplicacoes, err := s.aplicacaoRepo.BuscarPorVeiculo(ctx, req.Marca, req.Modelo, req.Ano, req.Motor)
	if err != nil {
		return nil, err
	}

	// Nenhum resultado
	if len(aplicacoes) == 0 {
		return &model.BuscaFiltrosResponse{
			Status:   "nao_encontrado",
			Mensagem: "Nao encontrei esse veiculo no catalogo Wega. Verifique a marca e modelo.",
		}, nil
	}

	// Verifica se precisa de mais info (muitas opcoes diferentes)
	if len(aplicacoes) > 10 && (req.Ano == "" || req.Motor == "") {
		opcoes, _ := s.aplicacaoRepo.ListarOpcoes(ctx, req.Marca, req.Modelo)
		faltantes := []string{}
		if req.Ano == "" {
			faltantes = append(faltantes, "ano")
		}
		if req.Motor == "" {
			faltantes = append(faltantes, "motor")
		}
		return &model.BuscaFiltrosResponse{
			Status:            "incompleto",
			Mensagem:          "Encontrei varios veiculos. Pode me informar o ano e motorizacao?",
			CamposFaltantes:   faltantes,
			OpcoesDisponiveis: opcoes,
		}, nil
	}

	// Se ainda temos multiplas opcoes distintas, perguntar
	if len(aplicacoes) > 1 && s.saoOpcoesDistintas(aplicacoes) {
		opcoes := make([]model.OpcaoVeiculo, 0, len(aplicacoes))
		for _, a := range aplicacoes {
			opcoes = append(opcoes, model.OpcaoVeiculo{
				ID:        a.CodigoAplicacao,
				Descricao: a.DescricaoAplicacao,
			})
		}
		return &model.BuscaFiltrosResponse{
			Status:   "multiplos",
			Mensagem: "Encontrei mais de uma opcao. Qual delas?",
			Opcoes:   opcoes,
		}, nil
	}

	// Buscar filtros para as aplicacoes encontradas
	codigosAplicacao := make([]int, len(aplicacoes))
	for i, a := range aplicacoes {
		codigosAplicacao[i] = a.CodigoAplicacao
	}

	filtros, err := s.produtoRepo.BuscarPorAplicacoes(ctx, codigosAplicacao)
	if err != nil {
		return nil, err
	}

	if len(filtros) == 0 {
		return &model.BuscaFiltrosResponse{
			Status:   "nao_encontrado",
			Mensagem: "Encontrei o veiculo, mas nao ha filtros cadastrados para ele.",
			Veiculo: &model.VeiculoInfo{
				Marca:             aplicacoes[0].Marca,
				Modelo:            req.Modelo,
				DescricaoCompleta: aplicacoes[0].DescricaoAplicacao,
			},
		}, nil
	}

	// Montar resposta de sucesso
	return &model.BuscaFiltrosResponse{
		Status: "completo",
		Veiculo: &model.VeiculoInfo{
			Marca:             aplicacoes[0].Marca,
			Modelo:            req.Modelo,
			Ano:               req.Ano,
			Motor:             aplicacoes[0].Motor,
			DescricaoCompleta: aplicacoes[0].DescricaoAplicacao,
		},
		Filtros:      filtros,
		TotalFiltros: len(filtros),
	}, nil
}

// BuscarPorAplicacao busca filtros para uma aplicacao especifica
func (s *CatalogoService) BuscarPorAplicacao(ctx context.Context, aplicacaoID int) (*model.FiltrosAplicacaoResponse, error) {
	aplicacao, err := s.aplicacaoRepo.BuscarPorID(ctx, aplicacaoID)
	if err != nil {
		return nil, err
	}

	filtros, err := s.produtoRepo.BuscarPorAplicacao(ctx, aplicacaoID)
	if err != nil {
		return nil, err
	}

	return &model.FiltrosAplicacaoResponse{
		Aplicacao: aplicacao,
		Filtros:   filtros,
	}, nil
}

// saoOpcoesDistintas verifica se as aplicacoes sao veiculos realmente diferentes
func (s *CatalogoService) saoOpcoesDistintas(apps []model.Aplicacao) bool {
	if len(apps) <= 1 {
		return false
	}

	// Comparar os motores para ver se sao diferentes
	motores := make(map[string]bool)
	for _, a := range apps {
		if a.Motor != "" {
			motores[a.Motor] = true
		}
	}

	// Se ha mais de um motor diferente, sao opcoes distintas
	return len(motores) > 1
}
