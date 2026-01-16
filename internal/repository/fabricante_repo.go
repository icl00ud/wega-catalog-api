package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"wega-catalog-api/internal/model"
)

type FabricanteRepo struct {
	db *pgxpool.Pool
}

func NewFabricanteRepo(db *pgxpool.Pool) *FabricanteRepo {
	return &FabricanteRepo{db: db}
}

// ListarVeiculos retorna fabricantes de veiculos (FlagAplicacao = 1)
func (r *FabricanteRepo) ListarVeiculos(ctx context.Context) ([]model.Fabricante, error) {
	query := `
		SELECT "CodigoFabricante", "DescricaoFabricante"
		FROM "FABRICANTE"
		WHERE "FlagAplicacao" = 1
		ORDER BY "DescricaoFabricante"
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fabricantes []model.Fabricante
	for rows.Next() {
		var f model.Fabricante
		if err := rows.Scan(&f.Codigo, &f.Descricao); err != nil {
			return nil, err
		}
		fabricantes = append(fabricantes, f)
	}

	return fabricantes, rows.Err()
}

// ListarConcorrentes retorna fabricantes concorrentes (FlagProduto = 1)
func (r *FabricanteRepo) ListarConcorrentes(ctx context.Context) ([]model.Fabricante, error) {
	query := `
		SELECT "CodigoFabricante", "DescricaoFabricante"
		FROM "FABRICANTE"
		WHERE "FlagProduto" = 1
		ORDER BY "DescricaoFabricante"
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fabricantes []model.Fabricante
	for rows.Next() {
		var f model.Fabricante
		if err := rows.Scan(&f.Codigo, &f.Descricao); err != nil {
			return nil, err
		}
		fabricantes = append(fabricantes, f)
	}

	return fabricantes, rows.Err()
}
