package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"wega-catalog-api/internal/model"
)

type ProdutoRepo struct {
	db *pgxpool.Pool
}

func NewProdutoRepo(db *pgxpool.Pool) *ProdutoRepo {
	return &ProdutoRepo{db: db}
}

// BuscarPorAplicacoes busca produtos para uma lista de aplicacoes
func (r *ProdutoRepo) BuscarPorAplicacoes(ctx context.Context, codigosAplicacao []int) ([]model.Produto, error) {
	if len(codigosAplicacao) == 0 {
		return []model.Produto{}, nil
	}

	query := `
		SELECT DISTINCT
			p."CodigoProduto",
			p."NumeroProduto" as codigo_wega,
			COALESCE(p."DescricaoProduto", '') as descricao,
			sg."DescricaoSubGrupoProduto" as tipo,
			p."ArquivoFotoProduto" as foto,
			p."PrecoProduto" as preco
		FROM "PRODUTO_APLICACAO" pa
		JOIN "PRODUTO" p ON pa."CodigoProduto" = p."CodigoProduto"
		JOIN "SUBGRUPOPRODUTO" sg ON p."CodigoSubGrupoProduto" = sg."CodigoSubGrupoProduto"
		WHERE pa."CodigoAplicacao" = ANY($1)
		ORDER BY sg."DescricaoSubGrupoProduto", p."NumeroProduto"
	`

	rows, err := r.db.Query(ctx, query, codigosAplicacao)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var produtos []model.Produto
	for rows.Next() {
		var p model.Produto
		if err := rows.Scan(&p.CodigoProduto, &p.CodigoWega, &p.Descricao, &p.Tipo, &p.FotoURL, &p.Preco); err != nil {
			return nil, err
		}
		produtos = append(produtos, p)
	}

	return produtos, rows.Err()
}

// BuscarPorAplicacao busca produtos para uma aplicacao especifica
func (r *ProdutoRepo) BuscarPorAplicacao(ctx context.Context, codigoAplicacao int) ([]model.Produto, error) {
	return r.BuscarPorAplicacoes(ctx, []int{codigoAplicacao})
}

// ListarTiposFiltro retorna todos os tipos de filtro (SubGrupos)
func (r *ProdutoRepo) ListarTiposFiltro(ctx context.Context) ([]model.TipoFiltro, error) {
	query := `
		SELECT "CodigoSubGrupoProduto", "DescricaoSubGrupoProduto"
		FROM "SUBGRUPOPRODUTO"
		ORDER BY "DescricaoSubGrupoProduto"
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tipos []model.TipoFiltro
	for rows.Next() {
		var t model.TipoFiltro
		if err := rows.Scan(&t.Codigo, &t.Descricao); err != nil {
			return nil, err
		}
		tipos = append(tipos, t)
	}

	return tipos, rows.Err()
}
