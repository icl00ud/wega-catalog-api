package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"wega-catalog-api/internal/model"
)

type ReferenciaRepo struct {
	db *pgxpool.Pool
}

func NewReferenciaRepo(db *pgxpool.Pool) *ReferenciaRepo {
	return &ReferenciaRepo{db: db}
}

// BuscarPorCodigo busca equivalencias Wega para um codigo de concorrente
func (r *ReferenciaRepo) BuscarPorCodigo(ctx context.Context, codigo string) (*model.ReferenciaResponse, error) {
	query := `
		SELECT DISTINCT
			f."DescricaoFabricante" as marca_concorrente,
			p."CodigoProduto",
			p."NumeroProduto" as codigo_wega,
			COALESCE(p."DescricaoProduto", '') as descricao,
			sg."DescricaoSubGrupoProduto" as tipo,
			p."ArquivoFotoProduto" as foto
		FROM "REFERENCIACRUZADA" rc
		JOIN "PRODUTO" p ON rc."CodigoProduto" = p."CodigoProduto"
		JOIN "FABRICANTE" f ON rc."CodigoFabricante" = f."CodigoFabricante"
		JOIN "SUBGRUPOPRODUTO" sg ON p."CodigoSubGrupoProduto" = sg."CodigoSubGrupoProduto"
		WHERE UPPER(rc."NumeroProdutoPesq") = UPPER($1)
		ORDER BY p."NumeroProduto"
	`

	rows, err := r.db.Query(ctx, query, strings.TrimSpace(codigo))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	response := &model.ReferenciaResponse{
		CodigoPesquisado: codigo,
		EquivalentesWega: []model.Produto{},
	}

	for rows.Next() {
		var marcaConcorrente string
		var p model.Produto
		if err := rows.Scan(&marcaConcorrente, &p.CodigoProduto, &p.CodigoWega, &p.Descricao, &p.Tipo, &p.FotoURL); err != nil {
			return nil, err
		}
		if response.MarcaConcorrente == "" {
			response.MarcaConcorrente = marcaConcorrente
		}
		response.EquivalentesWega = append(response.EquivalentesWega, p)
	}

	return response, rows.Err()
}
