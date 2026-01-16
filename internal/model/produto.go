package model

type Produto struct {
	CodigoProduto int      `json:"codigo_produto"`
	CodigoWega    string   `json:"codigo_wega"`
	Descricao     string   `json:"descricao,omitempty"`
	Tipo          string   `json:"tipo"`
	FotoURL       *string  `json:"foto_url"`
	Preco         *float64 `json:"preco,omitempty"`
}

type TipoFiltro struct {
	Codigo    int    `json:"codigo"`
	Descricao string `json:"descricao"`
}

type TiposFiltroResponse struct {
	Tipos []TipoFiltro `json:"tipos"`
}
