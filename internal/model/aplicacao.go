package model

type Aplicacao struct {
	CodigoAplicacao    int    `json:"codigo_aplicacao"`
	Marca              string `json:"marca"`
	DescricaoAplicacao string `json:"descricao_aplicacao"`
	Motor              string `json:"motor,omitempty"`
	Periodo            string `json:"periodo,omitempty"`
}

type OpcoesVeiculo struct {
	Anos    []string `json:"anos,omitempty"`
	Motores []string `json:"motores,omitempty"`
}

type OpcaoVeiculo struct {
	ID        int    `json:"id"`
	Descricao string `json:"descricao"`
}
