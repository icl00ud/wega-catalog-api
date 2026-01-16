package model

import "time"

// BuscaFiltrosRequest representa a requisicao de busca de filtros
type BuscaFiltrosRequest struct {
	Marca       string `json:"marca"`
	Modelo      string `json:"modelo"`
	Ano         string `json:"ano,omitempty"`
	Motor       string `json:"motor,omitempty"`
	Combustivel string `json:"combustivel,omitempty"`
}

// BuscaFiltrosResponse representa a resposta da busca de filtros
type BuscaFiltrosResponse struct {
	Status       string         `json:"status"` // "completo", "incompleto", "multiplos", "nao_encontrado"
	Mensagem     string         `json:"mensagem,omitempty"`
	Veiculo      *VeiculoInfo   `json:"veiculo,omitempty"`
	Filtros      []Produto      `json:"filtros,omitempty"`
	TotalFiltros int            `json:"total_filtros,omitempty"`
	// Quando incompleto
	CamposFaltantes   []string       `json:"campos_faltantes,omitempty"`
	OpcoesDisponiveis *OpcoesVeiculo `json:"opcoes_disponiveis,omitempty"`
	// Quando multiplos
	Opcoes []OpcaoVeiculo `json:"opcoes,omitempty"`
}

// VeiculoInfo representa informacoes do veiculo encontrado
type VeiculoInfo struct {
	Marca             string `json:"marca"`
	Modelo            string `json:"modelo"`
	Ano               string `json:"ano,omitempty"`
	Motor             string `json:"motor,omitempty"`
	DescricaoCompleta string `json:"descricao_completa"`
}

// FiltrosAplicacaoResponse representa a resposta de filtros por aplicacao
type FiltrosAplicacaoResponse struct {
	Aplicacao *Aplicacao `json:"aplicacao"`
	Filtros   []Produto  `json:"filtros"`
}

// ReferenciaResponse representa a resposta de referencia cruzada
type ReferenciaResponse struct {
	CodigoPesquisado  string    `json:"codigo_pesquisado"`
	MarcaConcorrente  string    `json:"marca_concorrente,omitempty"`
	EquivalentesWega  []Produto `json:"equivalentes_wega"`
}

// HealthResponse representa a resposta do health check
type HealthResponse struct {
	Status    string    `json:"status"`
	Database  string    `json:"database"`
	Timestamp time.Time `json:"timestamp"`
}

// ErrorResponse representa uma resposta de erro
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}
