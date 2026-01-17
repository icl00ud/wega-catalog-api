package repository

import (
	"context"
	"fmt"
	"time"

	"wega-catalog-api/internal/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ScraperFalhaRepo handles database operations for scraper failures
type ScraperFalhaRepo struct {
	pool *pgxpool.Pool
}

// NewScraperFalhaRepo creates a new scraper failure repository
func NewScraperFalhaRepo(pool *pgxpool.Pool) *ScraperFalhaRepo {
	return &ScraperFalhaRepo{pool: pool}
}

// Upsert inserts or updates a failure record
// If the vehicle already has a failure record, it increments the attempt counter
func (r *ScraperFalhaRepo) Upsert(ctx context.Context, codigoAplicacao int, tipoErro, mensagemErro string) error {
	// Calculate next retry time based on error type
	var proximaTentativa *time.Time
	switch tipoErro {
	case model.ErroTipoRateLimit:
		// Rate limit: retry in 1 minute
		t := time.Now().Add(1 * time.Minute)
		proximaTentativa = &t
	case model.ErroTipoRede:
		// Network error: retry in 5 minutes
		t := time.Now().Add(5 * time.Minute)
		proximaTentativa = &t
	case model.ErroTipoModeloNaoEncontrado:
		// Model not found: don't auto-retry (likely permanent)
		proximaTentativa = nil
	default:
		// Other errors: retry in 30 minutes
		t := time.Now().Add(30 * time.Minute)
		proximaTentativa = &t
	}

	query := `
		INSERT INTO "SCRAPER_FALHAS" (
			"CodigoAplicacao", "TipoErro", "MensagemErro", "Tentativas", 
			"UltimaTentativa", "ProximaTentativa"
		) VALUES ($1, $2, $3, 1, NOW(), $4)
		ON CONFLICT ("CodigoAplicacao") DO UPDATE SET
			"TipoErro" = EXCLUDED."TipoErro",
			"MensagemErro" = EXCLUDED."MensagemErro",
			"Tentativas" = "SCRAPER_FALHAS"."Tentativas" + 1,
			"UltimaTentativa" = NOW(),
			"ProximaTentativa" = EXCLUDED."ProximaTentativa",
			"Resolvido" = FALSE,
			"ResolvidoEm" = NULL
	`

	_, err := r.pool.Exec(ctx, query, codigoAplicacao, tipoErro, mensagemErro, proximaTentativa)
	if err != nil {
		return fmt.Errorf("failed to upsert scraper failure: %w", err)
	}

	return nil
}

// MarkResolved marks a failure as resolved (specs were successfully saved)
func (r *ScraperFalhaRepo) MarkResolved(ctx context.Context, codigoAplicacao int) error {
	query := `
		UPDATE "SCRAPER_FALHAS"
		SET "Resolvido" = TRUE, "ResolvidoEm" = NOW()
		WHERE "CodigoAplicacao" = $1
	`

	_, err := r.pool.Exec(ctx, query, codigoAplicacao)
	if err != nil {
		return fmt.Errorf("failed to mark failure as resolved: %w", err)
	}

	return nil
}

// GetPendingRetries returns failures that are ready for retry
func (r *ScraperFalhaRepo) GetPendingRetries(ctx context.Context, limit int) ([]model.ScraperFalha, error) {
	query := `
		SELECT 
			"ID", "CodigoAplicacao", "TipoErro", "MensagemErro", 
			"Tentativas", "UltimaTentativa", "ProximaTentativa",
			"Resolvido", "ResolvidoEm", "CriadoEm"
		FROM "SCRAPER_FALHAS"
		WHERE "Resolvido" = FALSE
		AND ("ProximaTentativa" IS NULL OR "ProximaTentativa" <= NOW())
		ORDER BY "ProximaTentativa" ASC NULLS LAST, "Tentativas" ASC
		LIMIT $1
	`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending retries: %w", err)
	}
	defer rows.Close()

	var falhas []model.ScraperFalha
	for rows.Next() {
		var f model.ScraperFalha
		err := rows.Scan(
			&f.ID, &f.CodigoAplicacao, &f.TipoErro, &f.MensagemErro,
			&f.Tentativas, &f.UltimaTentativa, &f.ProximaTentativa,
			&f.Resolvido, &f.ResolvidoEm, &f.CriadoEm,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan failure row: %w", err)
		}
		falhas = append(falhas, f)
	}

	return falhas, nil
}

// GetRetryableByType returns failures of a specific type ready for retry
func (r *ScraperFalhaRepo) GetRetryableByType(ctx context.Context, tipoErro string, limit int) ([]model.ScraperFalha, error) {
	query := `
		SELECT 
			"ID", "CodigoAplicacao", "TipoErro", "MensagemErro", 
			"Tentativas", "UltimaTentativa", "ProximaTentativa",
			"Resolvido", "ResolvidoEm", "CriadoEm"
		FROM "SCRAPER_FALHAS"
		WHERE "Resolvido" = FALSE
		AND "TipoErro" = $1
		AND ("ProximaTentativa" IS NULL OR "ProximaTentativa" <= NOW())
		ORDER BY "Tentativas" ASC, "UltimaTentativa" ASC
		LIMIT $2
	`

	rows, err := r.pool.Query(ctx, query, tipoErro, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query retryable by type: %w", err)
	}
	defer rows.Close()

	var falhas []model.ScraperFalha
	for rows.Next() {
		var f model.ScraperFalha
		err := rows.Scan(
			&f.ID, &f.CodigoAplicacao, &f.TipoErro, &f.MensagemErro,
			&f.Tentativas, &f.UltimaTentativa, &f.ProximaTentativa,
			&f.Resolvido, &f.ResolvidoEm, &f.CriadoEm,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan failure row: %w", err)
		}
		falhas = append(falhas, f)
	}

	return falhas, nil
}

// GetStats returns statistics about failures
func (r *ScraperFalhaRepo) GetStats(ctx context.Context) (map[string]int, error) {
	query := `
		SELECT "TipoErro", COUNT(*) as count
		FROM "SCRAPER_FALHAS"
		WHERE "Resolvido" = FALSE
		GROUP BY "TipoErro"
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query failure stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int)
	for rows.Next() {
		var tipoErro string
		var count int
		if err := rows.Scan(&tipoErro, &count); err != nil {
			return nil, fmt.Errorf("failed to scan stats row: %w", err)
		}
		stats[tipoErro] = count
	}

	return stats, nil
}

// CountPending returns total count of unresolved failures
func (r *ScraperFalhaRepo) CountPending(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM "SCRAPER_FALHAS" WHERE "Resolvido" = FALSE
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count pending failures: %w", err)
	}
	return count, nil
}

// DeleteResolved removes resolved failure records older than specified duration
func (r *ScraperFalhaRepo) DeleteResolved(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)

	result, err := r.pool.Exec(ctx, `
		DELETE FROM "SCRAPER_FALHAS"
		WHERE "Resolvido" = TRUE AND "ResolvidoEm" < $1
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to delete resolved failures: %w", err)
	}

	return result.RowsAffected(), nil
}
