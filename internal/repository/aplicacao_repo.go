package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"wega-catalog-api/internal/model"
)

type AplicacaoRepo struct {
	db *pgxpool.Pool
}

func NewAplicacaoRepo(db *pgxpool.Pool) *AplicacaoRepo {
	return &AplicacaoRepo{db: db}
}

// BuscarPorVeiculo busca aplicacoes por marca, modelo, ano e motor
func (r *AplicacaoRepo) BuscarPorVeiculo(ctx context.Context, marca, modelo, ano, motor string) ([]model.Aplicacao, error) {
	query := `
		SELECT DISTINCT
			a."CodigoAplicacao",
			f."DescricaoFabricante" as marca,
			a."DescricaoAplicacao",
			COALESCE(a."ComplementoAplicacao3", '') as motor,
			COALESCE(a."ComplementoAplicacao2", '') as periodo
		FROM "APLICACAO" a
		JOIN "FABRICANTE" f ON a."CodigoFabricante" = f."CodigoFabricante"
		WHERE f."FlagAplicacao" = 1
	`

	args := []interface{}{}
	argIndex := 1

	// Filtro por marca
	if marca != "" {
		query += fmt.Sprintf(` AND LOWER(f."DescricaoFabricante") ILIKE $%d`, argIndex)
		args = append(args, "%"+strings.ToLower(marca)+"%")
		argIndex++
	}

	// Filtro por modelo
	if modelo != "" {
		query += fmt.Sprintf(` AND LOWER(a."DescricaoAplicacao") ILIKE $%d`, argIndex)
		args = append(args, "%"+strings.ToLower(modelo)+"%")
		argIndex++
	}

	// Filtro por ano
	if ano != "" {
		query += fmt.Sprintf(` AND a."DescricaoAplicacao" ILIKE $%d`, argIndex)
		args = append(args, "%"+ano+"%")
		argIndex++
	}

	// Filtro por motor
	if motor != "" {
		query += fmt.Sprintf(` AND a."DescricaoAplicacao" ILIKE $%d`, argIndex)
		args = append(args, "%"+motor+"%")
		argIndex++
	}

	query += ` ORDER BY a."DescricaoAplicacao" LIMIT 50`

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aplicacoes []model.Aplicacao
	for rows.Next() {
		var a model.Aplicacao
		if err := rows.Scan(&a.CodigoAplicacao, &a.Marca, &a.DescricaoAplicacao, &a.Motor, &a.Periodo); err != nil {
			return nil, err
		}
		aplicacoes = append(aplicacoes, a)
	}

	return aplicacoes, rows.Err()
}

// ListarOpcoes retorna opcoes de anos e motores disponiveis para marca/modelo
func (r *AplicacaoRepo) ListarOpcoes(ctx context.Context, marca, modelo string) (*model.OpcoesVeiculo, error) {
	query := `
		SELECT DISTINCT
			COALESCE(a."ComplementoAplicacao2", '') as periodo,
			COALESCE(a."ComplementoAplicacao3", '') as motor
		FROM "APLICACAO" a
		JOIN "FABRICANTE" f ON a."CodigoFabricante" = f."CodigoFabricante"
		WHERE f."FlagAplicacao" = 1
			AND LOWER(f."DescricaoFabricante") ILIKE $1
			AND LOWER(a."DescricaoAplicacao") ILIKE $2
		ORDER BY periodo, motor
	`

	rows, err := r.db.Query(ctx, query, "%"+strings.ToLower(marca)+"%", "%"+strings.ToLower(modelo)+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	anosMap := make(map[string]bool)
	motoresMap := make(map[string]bool)

	for rows.Next() {
		var periodo, motor string
		if err := rows.Scan(&periodo, &motor); err != nil {
			return nil, err
		}
		if periodo != "" {
			anosMap[periodo] = true
		}
		if motor != "" {
			motoresMap[motor] = true
		}
	}

	opcoes := &model.OpcoesVeiculo{}
	for ano := range anosMap {
		opcoes.Anos = append(opcoes.Anos, ano)
	}
	for motor := range motoresMap {
		opcoes.Motores = append(opcoes.Motores, motor)
	}

	return opcoes, rows.Err()
}

// BuscarPorID busca uma aplicacao pelo ID
func (r *AplicacaoRepo) BuscarPorID(ctx context.Context, id int) (*model.Aplicacao, error) {
	query := `
		SELECT
			a."CodigoAplicacao",
			f."DescricaoFabricante" as marca,
			a."DescricaoAplicacao",
			COALESCE(a."ComplementoAplicacao3", '') as motor,
			COALESCE(a."ComplementoAplicacao2", '') as periodo
		FROM "APLICACAO" a
		JOIN "FABRICANTE" f ON a."CodigoFabricante" = f."CodigoFabricante"
		WHERE a."CodigoAplicacao" = $1
	`

	var a model.Aplicacao
	err := r.db.QueryRow(ctx, query, id).Scan(
		&a.CodigoAplicacao, &a.Marca, &a.DescricaoAplicacao, &a.Motor, &a.Periodo,
	)
	if err != nil {
		return nil, err
	}

	return &a, nil
}

// GetAllVehicles returns all vehicles from the database for scraping
func (r *AplicacaoRepo) GetAllVehicles(ctx context.Context) ([]model.Aplicacao, error) {
	query := `
		SELECT
			a."CodigoAplicacao",
			a."CodigoFabricante",
			f."DescricaoFabricante" as fabricante,
			a."DescricaoAplicacao" as modelo,
			COALESCE(a."ComplementoAplicacao2", '') as periodo,
			COALESCE(a."ComplementoAplicacao3", '') as motor
		FROM "APLICACAO" a
		JOIN "FABRICANTE" f ON a."CodigoFabricante" = f."CodigoFabricante"
		WHERE f."FlagAplicacao" = 1
		ORDER BY a."CodigoAplicacao"
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query vehicles: %w", err)
	}
	defer rows.Close()

	var vehicles []model.Aplicacao
	for rows.Next() {
		var v model.Aplicacao
		if err := rows.Scan(
			&v.CodigoAplicacao,
			&v.CodigoFabricante,
			&v.Fabricante,
			&v.Modelo,
			&v.Periodo,
			&v.Motor,
		); err != nil {
			return nil, fmt.Errorf("failed to scan vehicle: %w", err)
		}
		vehicles = append(vehicles, v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating vehicles: %w", err)
	}

	return vehicles, nil
}
