package model

import "time"

type EspecificacaoTecnica struct {
	ID                  int       `json:"id"`
	CodigoAplicacao     int       `json:"codigo_aplicacao"`
	TipoFluido          string    `json:"tipo_fluido"`
	Viscosidade         *string   `json:"viscosidade,omitempty"`
	Capacidade          *string   `json:"capacidade,omitempty"`
	Norma               *string   `json:"norma,omitempty"`
	Recomendacao        *string   `json:"recomendacao,omitempty"`
	Observacao          *string   `json:"observacao,omitempty"`
	Fonte               string    `json:"fonte"`
	MotulVehicleTypeID  *string   `json:"motul_vehicle_type_id,omitempty"`
	MatchConfidence     *float64  `json:"match_confidence,omitempty"`
	CriadoEm            time.Time `json:"criado_em"`
	AtualizadoEm        time.Time `json:"atualizado_em"`
}
