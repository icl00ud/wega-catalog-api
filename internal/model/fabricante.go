package model

type Fabricante struct {
	Codigo    int    `json:"codigo"`
	Descricao string `json:"descricao"`
}

type FabricantesResponse struct {
	Fabricantes []Fabricante `json:"fabricantes"`
}
