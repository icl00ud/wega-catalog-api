package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"wega-catalog-api/internal/model"
)

type EspecificacaoRepository struct {
	db *pgxpool.Pool
}

func NewEspecificacaoRepository(db *pgxpool.Pool) *EspecificacaoRepository {
	return &EspecificacaoRepository{db: db}
}

// Insert insere uma especificacao tecnica e retorna o registro com ID e timestamps gerados
func (r *EspecificacaoRepository) Insert(ctx context.Context, spec *model.EspecificacaoTecnica) error {
	query := `
		INSERT INTO "ESPECIFICACAO_TECNICA" (
			"CodigoAplicacao",
			"TipoFluido",
			"Viscosidade",
			"Capacidade",
			"Norma",
			"Recomendacao",
			"Observacao",
			"Fonte",
			"MotulVehicleTypeId",
			"MatchConfidence"
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING "ID", "CriadoEm", "AtualizadoEm"
	`

	err := r.db.QueryRow(
		ctx,
		query,
		spec.CodigoAplicacao,
		spec.TipoFluido,
		spec.Viscosidade,
		spec.Capacidade,
		spec.Norma,
		spec.Recomendacao,
		spec.Observacao,
		spec.Fonte,
		spec.MotulVehicleTypeID,
		spec.MatchConfidence,
	).Scan(&spec.ID, &spec.CriadoEm, &spec.AtualizadoEm)

	if err != nil {
		return fmt.Errorf("failed to insert especificacao: %w", err)
	}

	return nil
}

// InsertBatch insere multiplas especificacoes em uma transacao
func (r *EspecificacaoRepository) InsertBatch(ctx context.Context, specs []model.EspecificacaoTecnica) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO "ESPECIFICACAO_TECNICA" (
			"CodigoAplicacao",
			"TipoFluido",
			"Viscosidade",
			"Capacidade",
			"Norma",
			"Recomendacao",
			"Observacao",
			"Fonte",
			"MotulVehicleTypeId",
			"MatchConfidence"
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING "ID", "CriadoEm", "AtualizadoEm"
	`

	for i := range specs {
		err := tx.QueryRow(
			ctx,
			query,
			specs[i].CodigoAplicacao,
			specs[i].TipoFluido,
			specs[i].Viscosidade,
			specs[i].Capacidade,
			specs[i].Norma,
			specs[i].Recomendacao,
			specs[i].Observacao,
			specs[i].Fonte,
			specs[i].MotulVehicleTypeID,
			specs[i].MatchConfidence,
		).Scan(&specs[i].ID, &specs[i].CriadoEm, &specs[i].AtualizadoEm)

		if err != nil {
			return fmt.Errorf("failed to insert spec at index %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ExistsForVehicle verifica se existem especificacoes para um determinado veiculo
func (r *EspecificacaoRepository) ExistsForVehicle(ctx context.Context, codigoAplicacao int) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM "ESPECIFICACAO_TECNICA"
			WHERE "CodigoAplicacao" = $1
		)
	`

	var exists bool
	err := r.db.QueryRow(ctx, query, codigoAplicacao).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check existence: %w", err)
	}

	return exists, nil
}
