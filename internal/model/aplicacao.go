package model

type Aplicacao struct {
	CodigoAplicacao    int    `json:"codigo_aplicacao"`
	CodigoFabricante   int    `json:"codigo_fabricante,omitempty"`
	Marca              string `json:"marca"`
	DescricaoAplicacao string `json:"descricao_aplicacao"`
	DescricaoCompleta  string `json:"descricao_completa,omitempty"`
	Motor              string `json:"motor,omitempty"`
	Periodo            string `json:"periodo,omitempty"`
	Ano                string `json:"ano,omitempty"`
	Fabricante         string `json:"fabricante,omitempty"` // For scraper - brand name
	Modelo             string `json:"modelo,omitempty"`     // For scraper - model name
}

type OpcoesVeiculo struct {
	Anos    []string `json:"anos,omitempty"`
	Motores []string `json:"motores,omitempty"`
}

type OpcaoVeiculo struct {
	ID        int    `json:"id"`
	Descricao string `json:"descricao"`
}
