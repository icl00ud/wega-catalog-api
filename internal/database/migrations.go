package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunMigrations executes all database migrations
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Create ESPECIFICACAO_TECNICA table if not exists
	if err := createEspecificacaoTecnicaTable(ctx, pool); err != nil {
		return err
	}

	// Create SCRAPER_FALHAS table for retry tracking
	if err := createScraperFalhasTable(ctx, pool); err != nil {
		return err
	}

	return nil
}

// createEspecificacaoTecnicaTable creates the specifications table
func createEspecificacaoTecnicaTable(ctx context.Context, pool *pgxpool.Pool) error {
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
			"Fonte" VARCHAR(50) NOT NULL DEFAULT 'MotulAPI',
			"MotulVehicleTypeId" VARCHAR(100),
			"MatchConfidence" DECIMAL(5,2),
			"CriadoEm" TIMESTAMP NOT NULL DEFAULT NOW(),
			"AtualizadoEm" TIMESTAMP NOT NULL DEFAULT NOW(),
			CONSTRAINT "fk_especificacao_aplicacao"
				FOREIGN KEY ("CodigoAplicacao")
				REFERENCES "APLICACAO"("CodigoAplicacao")
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

// createScraperFalhasTable creates the table for tracking failed scraper attempts
func createScraperFalhasTable(ctx context.Context, pool *pgxpool.Pool) error {
	// Check if table exists
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public'
			AND table_name = 'SCRAPER_FALHAS'
		)
	`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if SCRAPER_FALHAS table exists: %w", err)
	}

	if exists {
		return nil
	}

	// Create table
	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS "SCRAPER_FALHAS" (
			"ID" SERIAL PRIMARY KEY,
			"CodigoAplicacao" INTEGER NOT NULL,
			"TipoErro" VARCHAR(100) NOT NULL,
			"MensagemErro" TEXT,
			"Tentativas" INTEGER NOT NULL DEFAULT 1,
			"UltimaTentativa" TIMESTAMP NOT NULL DEFAULT NOW(),
			"ProximaTentativa" TIMESTAMP,
			"Resolvido" BOOLEAN NOT NULL DEFAULT FALSE,
			"ResolvidoEm" TIMESTAMP,
			"CriadoEm" TIMESTAMP NOT NULL DEFAULT NOW(),
			CONSTRAINT "fk_falha_aplicacao"
				FOREIGN KEY ("CodigoAplicacao")
				REFERENCES "APLICACAO"("CodigoAplicacao")
				ON DELETE CASCADE,
			CONSTRAINT "uq_falha_aplicacao"
				UNIQUE ("CodigoAplicacao")
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create SCRAPER_FALHAS table: %w", err)
	}

	// Create indexes
	_, err = pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS "idx_falhas_resolvido"
		ON "SCRAPER_FALHAS"("Resolvido") WHERE "Resolvido" = FALSE
	`)
	if err != nil {
		return fmt.Errorf("failed to create idx_falhas_resolvido: %w", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS "idx_falhas_proxima_tentativa"
		ON "SCRAPER_FALHAS"("ProximaTentativa") WHERE "Resolvido" = FALSE
	`)
	if err != nil {
		return fmt.Errorf("failed to create idx_falhas_proxima_tentativa: %w", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS "idx_falhas_tipo"
		ON "SCRAPER_FALHAS"("TipoErro")
	`)
	if err != nil {
		return fmt.Errorf("failed to create idx_falhas_tipo: %w", err)
	}

	return nil
}
