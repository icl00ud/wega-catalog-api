package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunMigrations executes all database migrations
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Check if table exists
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public'
			AND table_name = 'ESPECIFICACAO_TECNICA'
		)
	`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if ESPECIFICACAO_TECNICA table exists: %w", err)
	}

	if exists {
		return nil
	}

	// Create table
	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS "ESPECIFICACAO_TECNICA" (
			"ID" SERIAL PRIMARY KEY,
			"CodigoAplicacao" INTEGER NOT NULL,
			"TipoFluido" VARCHAR(50) NOT NULL,
			"Viscosidade" VARCHAR(50),
			"Capacidade" VARCHAR(50),
			"Norma" VARCHAR(100),
			"Recomendacao" TEXT,
			"Observacao" TEXT,
			"Fonte" VARCHAR(50) NOT NULL,
			"MotulVehicleTypeID" VARCHAR(50),
			"MatchConfidence" DECIMAL(5,2),
			"CriadoEm" TIMESTAMP NOT NULL DEFAULT NOW(),
			"AtualizadoEm" TIMESTAMP NOT NULL DEFAULT NOW(),
			CONSTRAINT "fk_especificacao_aplicacao"
				FOREIGN KEY ("CodigoAplicacao")
				REFERENCES "APLICACAO"("Codigo")
				ON DELETE CASCADE
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create ESPECIFICACAO_TECNICA table: %w", err)
	}

	// Create indexes
	_, err = pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS "idx_especificacao_aplicacao"
		ON "ESPECIFICACAO_TECNICA"("CodigoAplicacao")
	`)
	if err != nil {
		return fmt.Errorf("failed to create idx_especificacao_aplicacao: %w", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS "idx_especificacao_tipo"
		ON "ESPECIFICACAO_TECNICA"("TipoFluido")
	`)
	if err != nil {
		return fmt.Errorf("failed to create idx_especificacao_tipo: %w", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS "idx_especificacao_fonte"
		ON "ESPECIFICACAO_TECNICA"("Fonte")
	`)
	if err != nil {
		return fmt.Errorf("failed to create idx_especificacao_fonte: %w", err)
	}

	return nil
}
