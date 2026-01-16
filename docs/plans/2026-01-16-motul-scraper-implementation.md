# Motul Oil Specifications Scraper - Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a Go CLI scraper that extracts oil specifications from Motul API and stores them in the Wega database for 49,034 vehicles.

**Architecture:** Layered architecture (CLI → Service → Client/Matcher/Parser → Repository → Database) with rate limiting, fuzzy matching (80% confidence), HTTP monitoring dashboard, and checkpoint-based resume capability.

**Tech Stack:** Go 1.21+, PostgreSQL 17, pgx/v5, chi router (for HTTP monitoring)

**Design Document:** `docs/plans/2026-01-16-motul-scraper-design.md`

---

## Task 1: Database Model and Migration

**Files:**
- Create: `internal/model/especificacao.go`
- Create: `internal/database/migrations.go`

**Step 1: Create model struct**

Create `internal/model/especificacao.go`:

```go
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
```

**Step 2: Create migration function**

Create `internal/database/migrations.go`:

```go
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const createEspecificacaoTableSQL = `
CREATE TABLE IF NOT EXISTS "ESPECIFICACAO_TECNICA" (
    "ID" SERIAL PRIMARY KEY,
    "CodigoAplicacao" INTEGER NOT NULL,
    "TipoFluido" VARCHAR(50) NOT NULL,
    "Viscosidade" VARCHAR(50),
    "Capacidade" VARCHAR(50),
    "Norma" VARCHAR(100),
    "Recomendacao" VARCHAR(20),
    "Observacao" TEXT,
    "Fonte" VARCHAR(50) DEFAULT 'MotulAPI',
    "MotulVehicleTypeId" VARCHAR(100),
    "MatchConfidence" DECIMAL(5,2),
    "CriadoEm" TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    "AtualizadoEm" TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_aplicacao
        FOREIGN KEY ("CodigoAplicacao")
        REFERENCES "APLICACAO" ("CodigoAplicacao")
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_specs_aplicacao ON "ESPECIFICACAO_TECNICA"("CodigoAplicacao");
CREATE INDEX IF NOT EXISTS idx_specs_tipo_fluido ON "ESPECIFICACAO_TECNICA"("TipoFluido");
CREATE INDEX IF NOT EXISTS idx_specs_fonte ON "ESPECIFICACAO_TECNICA"("Fonte");
`

// RunMigrations creates the ESPECIFICACAO_TECNICA table if it doesn't exist
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT FROM pg_tables
			WHERE schemaname = 'public'
			AND tablename = 'ESPECIFICACAO_TECNICA'
		)
	`).Scan(&exists)

	if err != nil {
		return fmt.Errorf("failed to check table existence: %w", err)
	}

	if !exists {
		_, err = pool.Exec(ctx, createEspecificacaoTableSQL)
		if err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}
```

**Step 3: Commit**

```bash
git add internal/model/especificacao.go internal/database/migrations.go
git commit -m "feat: add EspecificacaoTecnica model and migration"
```

---

## Task 2: Repository Layer

**Files:**
- Create: `internal/repository/especificacao_repo.go`

**Step 1: Create repository struct and interface**

Create `internal/repository/especificacao_repo.go`:

```go
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

// Insert inserts a new oil specification
func (r *EspecificacaoRepository) Insert(ctx context.Context, spec *model.EspecificacaoTecnica) error {
	query := `
		INSERT INTO "ESPECIFICACAO_TECNICA" (
			"CodigoAplicacao", "TipoFluido", "Viscosidade", "Capacidade",
			"Norma", "Recomendacao", "Observacao", "Fonte",
			"MotulVehicleTypeId", "MatchConfidence"
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING "ID", "CriadoEm", "AtualizadoEm"
	`

	err := r.db.QueryRow(
		ctx, query,
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
		return fmt.Errorf("failed to insert specification: %w", err)
	}

	return nil
}

// InsertBatch inserts multiple specifications in a single transaction
func (r *EspecificacaoRepository) InsertBatch(ctx context.Context, specs []*model.EspecificacaoTecnica) error {
	if len(specs) == 0 {
		return nil
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO "ESPECIFICACAO_TECNICA" (
			"CodigoAplicacao", "TipoFluido", "Viscosidade", "Capacidade",
			"Norma", "Recomendacao", "Observacao", "Fonte",
			"MotulVehicleTypeId", "MatchConfidence"
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	for _, spec := range specs {
		_, err := tx.Exec(
			ctx, query,
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
		)
		if err != nil {
			return fmt.Errorf("failed to insert spec: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ExistsForVehicle checks if specifications already exist for a vehicle
func (r *EspecificacaoRepository) ExistsForVehicle(ctx context.Context, codigoAplicacao int) (bool, error) {
	var exists bool
	query := `
		SELECT EXISTS(
			SELECT 1 FROM "ESPECIFICACAO_TECNICA"
			WHERE "CodigoAplicacao" = $1
			LIMIT 1
		)
	`

	err := r.db.QueryRow(ctx, query, codigoAplicacao).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check existence: %w", err)
	}

	return exists, nil
}
```

**Step 2: Commit**

```bash
git add internal/repository/especificacao_repo.go
git commit -m "feat: add EspecificacaoRepository with Insert/Batch methods"
```

---

## Task 3: Rate Limiter

**Files:**
- Create: `internal/client/rate_limiter.go`

**Step 1: Create rate limiter**

Create `internal/client/rate_limiter.go`:

```go
package client

import (
	"context"
	"time"
)

// RateLimiter controls request rate
type RateLimiter struct {
	ticker   *time.Ticker
	requests chan struct{}
}

// NewRateLimiter creates a rate limiter with specified rate
func NewRateLimiter(requestsPerSecond float64) *RateLimiter {
	interval := time.Duration(float64(time.Second) / requestsPerSecond)

	rl := &RateLimiter{
		ticker:   time.NewTicker(interval),
		requests: make(chan struct{}),
	}

	go func() {
		for range rl.ticker.C {
			select {
			case rl.requests <- struct{}{}:
			default:
			}
		}
	}()

	return rl
}

// Wait blocks until rate limit allows next request
func (rl *RateLimiter) Wait(ctx context.Context) error {
	select {
	case <-rl.requests:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop stops the rate limiter
func (rl *RateLimiter) Stop() {
	rl.ticker.Stop()
	close(rl.requests)
}
```

**Step 2: Commit**

```bash
git add internal/client/rate_limiter.go
git commit -m "feat: add rate limiter for API requests"
```

---

## Task 4: Motul HTTP Client (Part 1: Basic Structure)

**Files:**
- Create: `internal/client/motul.go`

**Step 1: Create client struct and constructor**

Create `internal/client/motul.go`:

```go
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	motulAPIBase = "https://gateway-apim.motul.com/oil-advisor"
	motulWebBase = "https://www.motul.com"
	locale       = "pt-BR"
	businessUnit = "Brazil"
)

// MotulClient handles communication with Motul API
type MotulClient struct {
	httpClient  *http.Client
	rateLimiter *RateLimiter
	retryConfig RetryConfig
}

// RetryConfig defines retry behavior
type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
}

// NewMotulClient creates a new Motul API client
func NewMotulClient(rateLimit float64) *MotulClient {
	return &MotulClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		rateLimiter: NewRateLimiter(rateLimit),
		retryConfig: RetryConfig{
			MaxRetries:     5,
			InitialBackoff: 1 * time.Second,
			MaxBackoff:     30 * time.Second,
			Multiplier:     2.0,
		},
	}
}

// fetchWithRetry performs HTTP request with retry logic
func (c *MotulClient) fetchWithRetry(ctx context.Context, url string) ([]byte, error) {
	backoff := c.retryConfig.InitialBackoff

	for attempt := 0; attempt <= c.retryConfig.MaxRetries; attempt++ {
		// Wait for rate limiter
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt < c.retryConfig.MaxRetries {
				time.Sleep(backoff)
				backoff = min(time.Duration(float64(backoff)*c.retryConfig.Multiplier), c.retryConfig.MaxBackoff)
				continue
			}
			return nil, fmt.Errorf("request failed after %d attempts: %w", attempt+1, err)
		}

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		// Success
		if resp.StatusCode == 200 {
			return body, nil
		}

		// Retry on 429, 500, 502, 503
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			if attempt < c.retryConfig.MaxRetries {
				time.Sleep(backoff)
				backoff = min(time.Duration(float64(backoff)*c.retryConfig.Multiplier), c.retryConfig.MaxBackoff)
				continue
			}
		}

		// Non-retryable error
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil, fmt.Errorf("max retries exceeded")
}

// Close closes the client
func (c *MotulClient) Close() {
	c.rateLimiter.Stop()
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
```

**Step 2: Commit**

```bash
git add internal/client/motul.go
git commit -m "feat: add Motul HTTP client with retry logic"
```

---

## Task 5: Motul Client (Part 2: API Methods)

**Files:**
- Modify: `internal/client/motul.go`

**Step 1: Add response types**

Add to `internal/client/motul.go` (after imports):

```go
// Brand represents a vehicle brand
type Brand struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// BrandsResponse wraps brands array
type BrandsResponse struct {
	Brands []Brand `json:"brands"`
}

// Model represents a vehicle model
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ModelsResponse wraps models array
type ModelsResponse struct {
	Models []Model `json:"models"`
}

// VehicleType represents a specific vehicle type/version
type VehicleType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// VehicleTypesResponse wraps types array
type VehicleTypesResponse struct {
	Types []VehicleType `json:"types"`
}

// SpecificationsResponse is the complex Motul recommendations JSON
type SpecificationsResponse struct {
	PageProps struct {
		Vehicle struct {
			CategoryID string `json:"categoryId"`
			Brand      string `json:"brand"`
			Type       string `json:"type"`
			Model      string `json:"model"`
			StartYear  string `json:"startYear"`
			EndYear    string `json:"endYear"`
		} `json:"vehicle"`
		Components []interface{} `json:"components"`
	} `json:"pageProps"`
}
```

**Step 2: Add API methods**

Add to `internal/client/motul.go` (before Close method):

```go
// GetBrands fetches all car brands from Motul
func (c *MotulClient) GetBrands(ctx context.Context) ([]Brand, error) {
	url := fmt.Sprintf("%s/vehicle-brands?categoryId=CAR&locale=%s&BU=%s",
		motulAPIBase, locale, businessUnit)

	body, err := c.fetchWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}

	var resp BrandsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse brands response: %w", err)
	}

	return resp.Brands, nil
}

// GetModels fetches models for a brand and year
func (c *MotulClient) GetModels(ctx context.Context, brandID string, year int) ([]Model, error) {
	url := fmt.Sprintf("%s/vehicle-models?vehicleBrandId=%s&year=%d&locale=%s&BU=%s",
		motulAPIBase, brandID, year, locale, businessUnit)

	body, err := c.fetchWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}

	var resp ModelsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse models response: %w", err)
	}

	return resp.Models, nil
}

// GetVehicleTypes fetches specific types/versions for a model
func (c *MotulClient) GetVehicleTypes(ctx context.Context, modelID string) ([]VehicleType, error) {
	url := fmt.Sprintf("%s/vehicle-types?vehicleModelId=%s&locale=%s&BU=%s",
		motulAPIBase, modelID, locale, businessUnit)

	body, err := c.fetchWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}

	var resp VehicleTypesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse types response: %w", err)
	}

	return resp.Types, nil
}

// GetSpecifications fetches oil specifications for a vehicle type
func (c *MotulClient) GetSpecifications(ctx context.Context, vehicleTypeID string) (*SpecificationsResponse, error) {
	// Build ID needs URL encoding
	url := fmt.Sprintf("%s/_next/data/ErAVpxULBQDBZ6fA5O0c4/%s/lubricants/recommendations/%s.json?vehicleTypeId=%s",
		motulWebBase, locale, vehicleTypeID, vehicleTypeID)

	body, err := c.fetchWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}

	var resp SpecificationsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse specifications response: %w", err)
	}

	return &resp, nil
}
```

**Step 3: Commit**

```bash
git add internal/client/motul.go
git commit -m "feat: add Motul API methods (GetBrands, GetModels, GetTypes, GetSpecs)"
```

---

## Task 6: String Normalizer

**Files:**
- Create: `internal/matching/normalizer.go`

**Step 1: Create normalizer functions**

Create `internal/matching/normalizer.go`:

```go
package matching

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var (
	// Regex to extract numeric values
	numberRegex = regexp.MustCompile(`\d+[,.]?\d*`)

	// Common generation suffixes to remove
	generationSuffixes = []string{"G1", "G2", "G3", "G4", "G5", "G6", "G7", "G8"}
)

// Normalize normalizes a string for comparison
func Normalize(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Remove accents
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	s, _, _ = transform.String(t, s)

	// Remove extra whitespace
	s = strings.Join(strings.Fields(s), " ")

	// Trim
	s = strings.TrimSpace(s)

	return s
}

// RemoveGenerationSuffix removes generation markers like "G7"
func RemoveGenerationSuffix(model string) string {
	normalized := Normalize(model)

	for _, suffix := range generationSuffixes {
		suffixLower := strings.ToLower(suffix)
		if strings.HasSuffix(normalized, " "+suffixLower) {
			normalized = strings.TrimSuffix(normalized, " "+suffixLower)
		}
		if strings.HasSuffix(normalized, suffixLower) {
			normalized = strings.TrimSuffix(normalized, suffixLower)
		}
	}

	return normalized
}

// ExtractNumbers extracts all numbers from a string
func ExtractNumbers(s string) []string {
	return numberRegex.FindAllString(s, -1)
}

// NormalizeNumber normalizes number format (3,5 → 3.5)
func NormalizeNumber(s string) string {
	return strings.ReplaceAll(s, ",", ".")
}
```

**Step 2: Commit**

```bash
git add internal/matching/normalizer.go
git commit -m "feat: add string normalization utilities for matching"
```

---

## Task 7: Feature Extractor

**Files:**
- Create: `internal/matching/extractor.go`

**Step 1: Create feature extraction**

Create `internal/matching/extractor.go`:

```go
package matching

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	// Regex patterns for feature extraction
	cilindradaRegex = regexp.MustCompile(`\b(\d+[,.]?\d*)\s*(?:L|l|litro|litros)?\b`)
	valvulasRegex   = regexp.MustCompile(`\b(\d+)V\b`)
	cilindrosRegex  = regexp.MustCompile(`\b(\d+)\s*[Cc]il\b`)
	potenciaRegex   = regexp.MustCompile(`\b(\d+)\s*(?:cv|CV|hp|HP)\b`)
	anoRegex        = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
)

// VehicleFeatures holds extracted vehicle characteristics
type VehicleFeatures struct {
	Cilindrada float64 // Engine displacement (1.0, 1.6, 2.0)
	Valvulas   int     // Number of valves (8, 12, 16)
	Cilindros  int     // Number of cylinders (3, 4, 6, 8)
	Potencia   int     // Power in CV
	Ano        int     // Year
}

// ExtractFeatures extracts technical features from vehicle description
func ExtractFeatures(description string, year int) VehicleFeatures {
	normalized := Normalize(description)

	features := VehicleFeatures{
		Ano: year,
	}

	// Extract cilindrada (1.0, 1.6, 2.0, etc)
	if matches := cilindradaRegex.FindStringSubmatch(normalized); len(matches) > 1 {
		if val, err := strconv.ParseFloat(NormalizeNumber(matches[1]), 64); err == nil {
			features.Cilindrada = val
		}
	}

	// Extract valvulas (8V, 12V, 16V)
	if matches := valvulasRegex.FindStringSubmatch(description); len(matches) > 1 {
		if val, err := strconv.Atoi(matches[1]); err == nil {
			features.Valvulas = val
		}
	}

	// Extract cilindros (3 cil, 4 cil)
	if matches := cilindrosRegex.FindStringSubmatch(normalized); len(matches) > 1 {
		if val, err := strconv.Atoi(matches[1]); err == nil {
			features.Cilindros = val
		}
	}

	// Extract potencia (84 cv, 120 hp)
	if matches := potenciaRegex.FindStringSubmatch(normalized); len(matches) > 1 {
		if val, err := strconv.Atoi(matches[1]); err == nil {
			features.Potencia = val
		}
	}

	// Extract year from description if not provided
	if features.Ano == 0 {
		if matches := anoRegex.FindStringSubmatch(description); len(matches) > 1 {
			if val, err := strconv.Atoi(matches[1]); err == nil {
				features.Ano = val
			}
		}
	}

	return features
}

// HasFeature checks if a specific feature is present
func (f VehicleFeatures) HasCilindrada() bool {
	return f.Cilindrada > 0
}

func (f VehicleFeatures) HasValvulas() bool {
	return f.Valvulas > 0
}

func (f VehicleFeatures) HasCilindros() bool {
	return f.Cilindros > 0
}

func (f VehicleFeatures) HasPotencia() bool {
	return f.Potencia > 0
}

func (f VehicleFeatures) HasAno() bool {
	return f.Ano > 0
}
```

**Step 2: Commit**

```bash
git add internal/matching/extractor.go
git commit -m "feat: add feature extraction for vehicle matching"
```

---

## Task 8: Fuzzy Matcher

**Files:**
- Create: `internal/matching/matcher.go`

**Step 1: Create matcher types and scoring**

Create `internal/matching/matcher.go`:

```go
package matching

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"wega-catalog-api/internal/client"
	"wega-catalog-api/internal/model"
)

const (
	scoreCilindrada = 40
	scoreValvulas   = 20
	scoreCilindros  = 15
	scorePotencia   = 15
	scoreAno        = 10
)

// MatchScore represents the matching score breakdown
type MatchScore struct {
	Cilindrada int
	Valvulas   int
	Cilindros  int
	Potencia   int
	Ano        int
	Total      int
	Confidence float64
}

// MatchResult contains the best match and its score
type MatchResult struct {
	VehicleType     *client.VehicleType
	Score           MatchScore
	MotulFeatures   VehicleFeatures
	WegaFeatures    VehicleFeatures
}

// VehicleMatcher performs fuzzy matching between Wega and Motul vehicles
type VehicleMatcher struct {
	minConfidence float64
}

// NewVehicleMatcher creates a new matcher
func NewVehicleMatcher(minConfidence float64) *VehicleMatcher {
	return &VehicleMatcher{
		minConfidence: minConfidence,
	}
}

// FindBestMatch finds the best matching Motul vehicle type
func (m *VehicleMatcher) FindBestMatch(
	wegaVehicle *model.Aplicacao,
	motulTypes []client.VehicleType,
) (*MatchResult, error) {
	if len(motulTypes) == 0 {
		return nil, fmt.Errorf("no motul types to match")
	}

	// Extract features from Wega vehicle
	wegaYear := parseYear(wegaVehicle.Ano)
	wegaFeatures := ExtractFeatures(wegaVehicle.DescricaoCompleta, wegaYear)

	var bestMatch *MatchResult

	for i := range motulTypes {
		motulType := &motulTypes[i]

		// Extract features from Motul vehicle
		motulFeatures := ExtractFeatures(motulType.Name, wegaYear)

		// Calculate score
		score := m.calculateScore(wegaFeatures, motulFeatures)

		// Update best match
		if bestMatch == nil || score.Total > bestMatch.Score.Total {
			bestMatch = &MatchResult{
				VehicleType:   motulType,
				Score:         score,
				MotulFeatures: motulFeatures,
				WegaFeatures:  wegaFeatures,
			}
		}
	}

	// Check if best match meets minimum confidence
	if bestMatch.Score.Confidence < m.minConfidence {
		return nil, fmt.Errorf(
			"best match confidence %.2f below threshold %.2f",
			bestMatch.Score.Confidence,
			m.minConfidence,
		)
	}

	return bestMatch, nil
}

// calculateScore calculates matching score between two vehicles
func (m *VehicleMatcher) calculateScore(wega, motul VehicleFeatures) MatchScore {
	score := MatchScore{}

	// Cilindrada (40 points) - CRITICAL
	if wega.HasCilindrada() && motul.HasCilindrada() {
		if math.Abs(wega.Cilindrada-motul.Cilindrada) < 0.1 {
			score.Cilindrada = scoreCilindrada
		}
	}

	// Valvulas (20 points)
	if wega.HasValvulas() && motul.HasValvulas() {
		if wega.Valvulas == motul.Valvulas {
			score.Valvulas = scoreValvulas
		}
	}

	// Cilindros (15 points)
	if wega.HasCilindros() && motul.HasCilindros() {
		if wega.Cilindros == motul.Cilindros {
			score.Cilindros = scoreCilindros
		}
	}

	// Potencia (15 points) - tolerance ±5cv
	if wega.HasPotencia() && motul.HasPotencia() {
		if math.Abs(float64(wega.Potencia-motul.Potencia)) <= 5 {
			score.Potencia = scorePotencia
		}
	}

	// Ano (10 points)
	if wega.HasAno() && motul.HasAno() {
		if wega.Ano == motul.Ano {
			score.Ano = scoreAno
		}
	}

	score.Total = score.Cilindrada + score.Valvulas + score.Cilindros + score.Potencia + score.Ano
	score.Confidence = float64(score.Total) / 100.0

	return score
}

// parseYear extracts year from string (handles "2020", "2019 -->", etc)
func parseYear(anoStr string) int {
	// Extract first 4-digit number
	normalized := strings.TrimSpace(anoStr)
	if len(normalized) >= 4 {
		if year, err := strconv.Atoi(normalized[:4]); err == nil {
			if year >= 1900 && year <= 2100 {
				return year
			}
		}
	}
	return 0
}
```

**Step 2: Add Aplicacao model reference**

Note: The code above references `model.Aplicacao`. We need to check if it exists or create it.

Check if `internal/model/aplicacao.go` exists:

```bash
ls internal/model/aplicacao.go
```

If it doesn't exist, create it with the structure from APLICACAO table:

```go
package model

type Aplicacao struct {
	CodigoAplicacao   int    `json:"codigo_aplicacao"`
	CodigoFabricante  int    `json:"codigo_fabricante"`
	DescricaoCompleta string `json:"descricao_completa"`
	Ano               string `json:"ano"`
	Motor             string `json:"motor"`
	// Add other fields as needed
}
```

**Step 3: Commit**

```bash
git add internal/matching/matcher.go
# If you created aplicacao.go:
# git add internal/model/aplicacao.go
git commit -m "feat: add fuzzy matching algorithm with scoring"
```

---

## Task 9: Motul JSON Parser

**Files:**
- Create: `internal/parser/motul_parser.go`

**Step 1: Create parser for oil specifications**

Create `internal/parser/motul_parser.go`:

```go
package parser

import (
	"fmt"
	"regexp"
	"strings"

	"wega-catalog-api/internal/client"
)

var (
	viscosityRegex = regexp.MustCompile(`\b\d+W-?\d+\b`)
	capacityRegex  = regexp.MustCompile(`\b\d+[,\.]\d*\s*(?:L|l|litro|litros)?\b`)
)

// OilSpec represents a parsed oil specification
type OilSpec struct {
	TipoFluido   string
	Viscosidade  string
	Capacidade   string
	Norma        string
	Recomendacao string
	Observacao   string
}

// MotulParser parses Motul API responses
type MotulParser struct{}

// NewMotulParser creates a new parser
func NewMotulParser() *MotulParser {
	return &MotulParser{}
}

// ParseSpecifications extracts oil specifications from Motul response
func (p *MotulParser) ParseSpecifications(resp *client.SpecificationsResponse) ([]OilSpec, error) {
	if resp == nil {
		return nil, fmt.Errorf("nil response")
	}

	components := resp.PageProps.Components
	if len(components) == 0 {
		return nil, fmt.Errorf("no components in response")
	}

	specs := []OilSpec{}

	// Find motor (engine oil) specifications
	motorSpecs := p.findMotorSpecs(components)
	specs = append(specs, motorSpecs...)

	// Find transmission oil specifications
	transmissionSpecs := p.findTransmissionSpecs(components)
	specs = append(specs, transmissionSpecs...)

	return specs, nil
}

// findMotorSpecs finds engine oil specifications in components array
func (p *MotulParser) findMotorSpecs(components []interface{}) []OilSpec {
	specs := []OilSpec{}

	// Search for "motor" keyword in components
	for i, comp := range components {
		if str, ok := comp.(string); ok {
			if strings.ToLower(str) == "motor" {
				// Look around this index for viscosity and capacity
				viscosity := p.findNearbyViscosity(components, i, 20)
				capacity := p.findNearbyCapacity(components, i, 20)

				if viscosity != "" {
					specs = append(specs, OilSpec{
						TipoFluido:   "Motor",
						Viscosidade:  viscosity,
						Capacidade:   capacity,
						Recomendacao: "Primaria",
					})
				}
			}
		}
	}

	return specs
}

// findTransmissionSpecs finds transmission oil specifications
func (p *MotulParser) findTransmissionSpecs(components []interface{}) []OilSpec {
	specs := []OilSpec{}

	// Search for transmission keywords
	transmissionKeywords := []string{"transmissão", "transmissao", "cambio", "câmbio"}

	for i, comp := range components {
		if str, ok := comp.(string); ok {
			strLower := strings.ToLower(str)
			for _, keyword := range transmissionKeywords {
				if strings.Contains(strLower, keyword) {
					viscosity := p.findNearbyViscosity(components, i, 20)
					capacity := p.findNearbyCapacity(components, i, 20)

					if viscosity != "" {
						specs = append(specs, OilSpec{
							TipoFluido:   "Transmissao",
							Viscosidade:  viscosity,
							Capacidade:   capacity,
							Recomendacao: "Primaria",
						})
						break
					}
				}
			}
		}
	}

	return specs
}

// findNearbyViscosity searches for viscosity pattern near an index
func (p *MotulParser) findNearbyViscosity(components []interface{}, startIdx, radius int) string {
	start := max(0, startIdx-radius)
	end := min(len(components), startIdx+radius)

	for i := start; i < end; i++ {
		if str, ok := components[i].(string); ok {
			if matches := viscosityRegex.FindString(str); matches != "" {
				return matches
			}
		}
	}

	return ""
}

// findNearbyCapacity searches for capacity pattern near an index
func (p *MotulParser) findNearbyCapacity(components []interface{}, startIdx, radius int) string {
	start := max(0, startIdx-radius)
	end := min(len(components), startIdx+radius)

	for i := start; i < end; i++ {
		if str, ok := components[i].(string); ok {
			if matches := capacityRegex.FindString(str); matches != "" {
				// Normalize format
				normalized := strings.ReplaceAll(matches, ",", ".")
				if !strings.Contains(normalized, "L") && !strings.Contains(normalized, "l") {
					normalized += " L"
				}
				return normalized
			}
		}
	}

	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

**Step 2: Commit**

```bash
git add internal/parser/motul_parser.go
git commit -m "feat: add Motul JSON parser for oil specifications"
```

---

## Task 10: Progress Tracker

**Files:**
- Create: `internal/scraper/progress.go`

**Step 1: Create progress tracking**

Create `internal/scraper/progress.go`:

```go
package scraper

import (
	"sync"
	"time"
)

// ProgressTracker tracks scraping progress
type ProgressTracker struct {
	mu sync.RWMutex

	StartedAt        time.Time
	TotalVehicles    int
	Processed        int
	Success          int
	Failed           int
	Skipped          int
	CurrentVehicle   string
	LastError        string

	// Matching stats
	ExactMatch       int
	FuzzyMatch       int
	NoMatch          int

	// Performance
	TotalRequests    int
	NetworkErrors    int
	RateLimitHits    int
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(totalVehicles int) *ProgressTracker {
	return &ProgressTracker{
		StartedAt:     time.Now(),
		TotalVehicles: totalVehicles,
	}
}

// IncrementProcessed increments processed counter
func (p *ProgressTracker) IncrementProcessed() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Processed++
}

// IncrementSuccess increments success counter
func (p *ProgressTracker) IncrementSuccess() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Success++
}

// IncrementFailed increments failed counter and sets error
func (p *ProgressTracker) IncrementFailed(err string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Failed++
	p.LastError = err
}

// IncrementSkipped increments skipped counter
func (p *ProgressTracker) IncrementSkipped() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Skipped++
}

// IncrementExactMatch increments exact match counter
func (p *ProgressTracker) IncrementExactMatch() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ExactMatch++
}

// IncrementFuzzyMatch increments fuzzy match counter
func (p *ProgressTracker) IncrementFuzzyMatch() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.FuzzyMatch++
}

// IncrementNoMatch increments no match counter
func (p *ProgressTracker) IncrementNoMatch() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.NoMatch++
}

// SetCurrentVehicle sets the current vehicle being processed
func (p *ProgressTracker) SetCurrentVehicle(vehicle string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.CurrentVehicle = vehicle
}

// IncrementRequests increments total requests counter
func (p *ProgressTracker) IncrementRequests() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.TotalRequests++
}

// GetSnapshot returns a snapshot of current progress
func (p *ProgressTracker) GetSnapshot() ProgressSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	elapsed := time.Since(p.StartedAt)
	percentage := 0.0
	if p.TotalVehicles > 0 {
		percentage = (float64(p.Processed) / float64(p.TotalVehicles)) * 100
	}

	// Calculate ETA
	var eta time.Time
	var remaining time.Duration
	if p.Processed > 0 {
		avgTimePerVehicle := elapsed / time.Duration(p.Processed)
		remainingVehicles := p.TotalVehicles - p.Processed
		remaining = avgTimePerVehicle * time.Duration(remainingVehicles)
		eta = time.Now().Add(remaining)
	}

	// Calculate rate
	reqPerSecond := 0.0
	if elapsed.Seconds() > 0 {
		reqPerSecond = float64(p.TotalRequests) / elapsed.Seconds()
	}

	avgTimePerVehicle := 0.0
	if p.Processed > 0 {
		avgTimePerVehicle = elapsed.Seconds() / float64(p.Processed)
	}

	return ProgressSnapshot{
		Status:         "running",
		StartedAt:      p.StartedAt,
		Elapsed:        elapsed,
		TotalVehicles:  p.TotalVehicles,
		Processed:      p.Processed,
		Success:        p.Success,
		Failed:         p.Failed,
		Skipped:        p.Skipped,
		Percentage:     percentage,
		CurrentVehicle: p.CurrentVehicle,
		LastError:      p.LastError,
		ExactMatch:     p.ExactMatch,
		FuzzyMatch:     p.FuzzyMatch,
		NoMatch:        p.NoMatch,
		TotalRequests:  p.TotalRequests,
		RequestsPerSec: reqPerSecond,
		AvgTimePerVehicle: avgTimePerVehicle,
		ETA:            eta,
		Remaining:      remaining,
	}
}

// ProgressSnapshot is a point-in-time snapshot of progress
type ProgressSnapshot struct {
	Status            string
	StartedAt         time.Time
	Elapsed           time.Duration
	TotalVehicles     int
	Processed         int
	Success           int
	Failed            int
	Skipped           int
	Percentage        float64
	CurrentVehicle    string
	LastError         string
	ExactMatch        int
	FuzzyMatch        int
	NoMatch           int
	TotalRequests     int
	RequestsPerSec    float64
	AvgTimePerVehicle float64
	ETA               time.Time
	Remaining         time.Duration
}
```

**Step 2: Commit**

```bash
git add internal/scraper/progress.go
git commit -m "feat: add progress tracking for scraper"
```

---

## Task 11: Checkpoint Manager

**Files:**
- Create: `internal/scraper/checkpoint.go`

**Step 1: Create checkpoint management**

Create `internal/scraper/checkpoint.go`:

```go
package scraper

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Checkpoint represents saved scraper state
type Checkpoint struct {
	LastProcessedID int       `json:"last_processed_id"`
	StartedAt       time.Time `json:"started_at"`
	SavedAt         time.Time `json:"saved_at"`
	Stats           struct {
		Success int `json:"success"`
		Failed  int `json:"failed"`
		Skipped int `json:"skipped"`
	} `json:"stats"`
}

// CheckpointManager handles saving and loading scraper state
type CheckpointManager struct {
	filePath string
}

// NewCheckpointManager creates a new checkpoint manager
func NewCheckpointManager(filePath string) *CheckpointManager {
	return &CheckpointManager{
		filePath: filePath,
	}
}

// Save saves the current checkpoint
func (c *CheckpointManager) Save(lastID int, progress *ProgressTracker) error {
	snapshot := progress.GetSnapshot()

	checkpoint := Checkpoint{
		LastProcessedID: lastID,
		StartedAt:       snapshot.StartedAt,
		SavedAt:         time.Now(),
	}
	checkpoint.Stats.Success = snapshot.Success
	checkpoint.Stats.Failed = snapshot.Failed
	checkpoint.Stats.Skipped = snapshot.Skipped

	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	if err := os.WriteFile(c.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write checkpoint file: %w", err)
	}

	return nil
}

// Load loads the checkpoint if it exists
func (c *CheckpointManager) Load() (*Checkpoint, error) {
	data, err := os.ReadFile(c.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No checkpoint exists
		}
		return nil, fmt.Errorf("failed to read checkpoint file: %w", err)
	}

	var checkpoint Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, fmt.Errorf("failed to unmarshal checkpoint: %w", err)
	}

	return &checkpoint, nil
}

// Delete removes the checkpoint file
func (c *CheckpointManager) Delete() error {
	if err := os.Remove(c.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete checkpoint file: %w", err)
	}
	return nil
}

// Exists checks if checkpoint file exists
func (c *CheckpointManager) Exists() bool {
	_, err := os.Stat(c.filePath)
	return err == nil
}
```

**Step 2: Commit**

```bash
git add internal/scraper/checkpoint.go
git commit -m "feat: add checkpoint manager for resume capability"
```

---

## Task 12: HTTP Monitoring Server

**Files:**
- Create: `internal/scraper/http_monitor.go`

**Step 1: Create HTTP status server**

Create `internal/scraper/http_monitor.go`:

```go
package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// HTTPMonitor provides HTTP endpoints for monitoring scraper progress
type HTTPMonitor struct {
	server   *http.Server
	progress *ProgressTracker
}

// NewHTTPMonitor creates a new HTTP monitoring server
func NewHTTPMonitor(port int, progress *ProgressTracker) *HTTPMonitor {
	mux := http.NewServeMux()

	monitor := &HTTPMonitor{
		server: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
		progress: progress,
	}

	mux.HandleFunc("/status", monitor.handleStatus)
	mux.HandleFunc("/health", monitor.handleHealth)

	return monitor
}

// Start starts the HTTP server in a goroutine
func (m *HTTPMonitor) Start() error {
	go func() {
		slog.Info("Starting HTTP monitor", "addr", m.server.Addr)
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP monitor error", "error", err)
		}
	}()
	return nil
}

// Stop gracefully stops the HTTP server
func (m *HTTPMonitor) Stop(ctx context.Context) error {
	slog.Info("Stopping HTTP monitor")
	return m.server.Shutdown(ctx)
}

// handleStatus returns current scraper status as JSON
func (m *HTTPMonitor) handleStatus(w http.ResponseWriter, r *http.Request) {
	snapshot := m.progress.GetSnapshot()

	response := map[string]interface{}{
		"status":     snapshot.Status,
		"started_at": snapshot.StartedAt.Format(time.RFC3339),
		"elapsed":    snapshot.Elapsed.String(),
		"progress": map[string]interface{}{
			"total_vehicles": snapshot.TotalVehicles,
			"processed":      snapshot.Processed,
			"success":        snapshot.Success,
			"failed":         snapshot.Failed,
			"skipped":        snapshot.Skipped,
			"percentage":     fmt.Sprintf("%.2f", snapshot.Percentage),
		},
		"matching_stats": map[string]interface{}{
			"exact_match": snapshot.ExactMatch,
			"fuzzy_match": snapshot.FuzzyMatch,
			"no_match":    snapshot.NoMatch,
		},
		"rate": map[string]interface{}{
			"current_rps":           fmt.Sprintf("%.2f", snapshot.RequestsPerSec),
			"avg_time_per_vehicle":  fmt.Sprintf("%.2fs", snapshot.AvgTimePerVehicle),
		},
		"eta": map[string]interface{}{
			"remaining_vehicles":      snapshot.TotalVehicles - snapshot.Processed,
			"estimated_completion":    snapshot.ETA.Format(time.RFC3339),
			"time_remaining":          snapshot.Remaining.String(),
		},
		"last_error":      snapshot.LastError,
		"current_vehicle": snapshot.CurrentVehicle,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleHealth returns simple health check
func (m *HTTPMonitor) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}
```

**Step 2: Commit**

```bash
git add internal/scraper/http_monitor.go
git commit -m "feat: add HTTP monitoring server for real-time status"
```

---

## Task 13: Scraper Service (Part 1: Structure)

**Files:**
- Create: `internal/scraper/service.go`

**Step 1: Create service structure and configuration**

Create `internal/scraper/service.go`:

```go
package scraper

import (
	"context"
	"fmt"
	"log/slog"

	"wega-catalog-api/internal/client"
	"wega-catalog-api/internal/matching"
	"wega-catalog-api/internal/parser"
	"wega-catalog-api/internal/repository"
)

// Config holds scraper configuration
type Config struct {
	DBConnection   string
	RateLimit      float64
	BatchSize      int
	MinConfidence  float64
	Resume         bool
	DryRun         bool
	Limit          int
	FilterBrand    string
	SkipExisting   bool
	StateFile      string
	AuditFile      string
}

// Service orchestrates the scraping process
type Service struct {
	motulClient   *client.MotulClient
	matcher       *matching.VehicleMatcher
	parser        *parser.MotulParser
	aplicacaoRepo *repository.AplicacaoRepository
	especRepo     *repository.EspecificacaoRepository
	progress      *ProgressTracker
	checkpoint    *CheckpointManager
	httpMonitor   *HTTPMonitor
	config        Config
}

// NewService creates a new scraper service
func NewService(
	motulClient *client.MotulClient,
	aplicacaoRepo *repository.AplicacaoRepository,
	especRepo *repository.EspecificacaoRepository,
	config Config,
) *Service {
	progress := NewProgressTracker(0) // Will set total later

	return &Service{
		motulClient:   motulClient,
		matcher:       matching.NewVehicleMatcher(config.MinConfidence),
		parser:        parser.NewMotulParser(),
		aplicacaoRepo: aplicacaoRepo,
		especRepo:     especRepo,
		progress:      progress,
		checkpoint:    NewCheckpointManager(config.StateFile),
		config:        config,
	}
}

// SetHTTPMonitor sets the HTTP monitoring server
func (s *Service) SetHTTPMonitor(monitor *HTTPMonitor) {
	s.httpMonitor = monitor
}
```

**Step 2: Commit**

```bash
git add internal/scraper/service.go
git commit -m "feat: add scraper service structure and config"
```

---

## Task 14: Scraper Service (Part 2: Main Logic)

**Files:**
- Modify: `internal/scraper/service.go`

**Step 1: Add main Run method**

Add to `internal/scraper/service.go` (after SetHTTPMonitor method):

```go
// Run executes the scraping process
func (s *Service) Run(ctx context.Context) error {
	slog.Info("Starting Motul scraper",
		"rate_limit", s.config.RateLimit,
		"batch_size", s.config.BatchSize,
		"min_confidence", s.config.MinConfidence,
		"dry_run", s.config.DryRun,
	)

	// Load checkpoint if resume flag is set
	startFromID := 0
	if s.config.Resume {
		checkpoint, err := s.checkpoint.Load()
		if err != nil {
			return fmt.Errorf("failed to load checkpoint: %w", err)
		}
		if checkpoint != nil {
			startFromID = checkpoint.LastProcessedID
			slog.Info("Resuming from checkpoint", "last_processed_id", startFromID)
		}
	}

	// Fetch vehicles from database
	vehicles, err := s.aplicacaoRepo.GetAllVehicles(ctx, startFromID, s.config.Limit)
	if err != nil {
		return fmt.Errorf("failed to fetch vehicles: %w", err)
	}

	slog.Info("Fetched vehicles from database", "count", len(vehicles))
	s.progress.TotalVehicles = len(vehicles)

	// Pre-fetch and cache all brands
	slog.Info("Fetching Motul brands...")
	brands, err := s.motulClient.GetBrands(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch brands: %w", err)
	}
	slog.Info("Cached Motul brands", "count", len(brands))

	// Build brand lookup map
	brandMap := make(map[string]string) // name -> id
	for _, brand := range brands {
		normalized := matching.Normalize(brand.Name)
		brandMap[normalized] = brand.ID
	}

	// Process vehicles
	batch := make([]*model.EspecificacaoTecnica, 0, s.config.BatchSize)

	for i, vehicle := range vehicles {
		select {
		case <-ctx.Done():
			slog.Info("Context cancelled, stopping scraper")
			return ctx.Err()
		default:
		}

		s.progress.SetCurrentVehicle(vehicle.DescricaoCompleta)

		// Skip if already has specifications
		if s.config.SkipExisting {
			exists, err := s.especRepo.ExistsForVehicle(ctx, vehicle.CodigoAplicacao)
			if err != nil {
				slog.Warn("Failed to check existence", "error", err)
			} else if exists {
				s.progress.IncrementSkipped()
				s.progress.IncrementProcessed()
				continue
			}
		}

		// Process vehicle
		specs, matchResult, err := s.processVehicle(ctx, vehicle, brandMap)
		if err != nil {
			slog.Warn("Failed to process vehicle",
				"vehicle", vehicle.DescricaoCompleta,
				"error", err,
			)
			s.progress.IncrementFailed(err.Error())
			s.progress.IncrementProcessed()
			continue
		}

		// Track matching type
		if matchResult != nil {
			if matchResult.Score.Confidence >= 0.99 {
				s.progress.IncrementExactMatch()
			} else {
				s.progress.IncrementFuzzyMatch()
			}
		}

		// Add to batch
		batch = append(batch, specs...)
		s.progress.IncrementSuccess()
		s.progress.IncrementProcessed()

		// Commit batch
		if len(batch) >= s.config.BatchSize {
			if err := s.commitBatch(ctx, batch, vehicle.CodigoAplicacao); err != nil {
				return fmt.Errorf("failed to commit batch: %w", err)
			}
			batch = batch[:0] // Clear batch
		}

		// Log progress periodically
		if (i+1)%100 == 0 {
			snapshot := s.progress.GetSnapshot()
			slog.Info("Progress",
				"processed", snapshot.Processed,
				"percentage", fmt.Sprintf("%.2f%%", snapshot.Percentage),
				"eta", snapshot.Remaining.String(),
			)
		}
	}

	// Commit remaining batch
	if len(batch) > 0 {
		if err := s.commitBatch(ctx, batch, vehicles[len(vehicles)-1].CodigoAplicacao); err != nil {
			return fmt.Errorf("failed to commit final batch: %w", err)
		}
	}

	slog.Info("Scraping completed successfully")
	s.generateReport()

	return nil
}
```

**Step 2: Commit**

```bash
git add internal/scraper/service.go
git commit -m "feat: add scraper main Run logic with batch processing"
```

---

## Task 15: Scraper Service (Part 3: Helper Methods)

**Files:**
- Modify: `internal/scraper/service.go`

**Step 1: Add processVehicle method**

Add to `internal/scraper/service.go`:

```go
// processVehicle processes a single vehicle and returns specifications
func (s *Service) processVehicle(
	ctx context.Context,
	vehicle *model.Aplicacao,
	brandMap map[string]string,
) ([]*model.EspecificacaoTecnica, *matching.MatchResult, error) {
	// Find brand in Motul
	normalizedBrand := matching.Normalize(vehicle.Fabricante)
	brandID, ok := brandMap[normalizedBrand]
	if !ok {
		s.progress.IncrementNoMatch()
		return nil, nil, fmt.Errorf("brand not found in Motul: %s", vehicle.Fabricante)
	}

	// Get models for this brand and year
	year := matching.ParseYear(vehicle.Ano)
	models, err := s.motulClient.GetModels(ctx, brandID, year)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get models: %w", err)
	}
	s.progress.IncrementRequests()

	// Find matching model
	normalizedModel := matching.RemoveGenerationSuffix(vehicle.Modelo)
	var modelID string
	for _, m := range models {
		if matching.Normalize(m.Name) == normalizedModel {
			modelID = m.ID
			break
		}
	}

	if modelID == "" {
		s.progress.IncrementNoMatch()
		return nil, nil, fmt.Errorf("model not found in Motul: %s", vehicle.Modelo)
	}

	// Get vehicle types
	types, err := s.motulClient.GetVehicleTypes(ctx, modelID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get types: %w", err)
	}
	s.progress.IncrementRequests()

	if len(types) == 0 {
		s.progress.IncrementNoMatch()
		return nil, nil, fmt.Errorf("no types found for model")
	}

	// Find best match
	matchResult, err := s.matcher.FindBestMatch(vehicle, types)
	if err != nil {
		s.progress.IncrementNoMatch()
		return nil, nil, fmt.Errorf("no match found: %w", err)
	}

	// Get specifications
	specsResp, err := s.motulClient.GetSpecifications(ctx, matchResult.VehicleType.ID)
	if err != nil {
		return nil, matchResult, fmt.Errorf("failed to get specifications: %w", err)
	}
	s.progress.IncrementRequests()

	// Parse specifications
	oilSpecs, err := s.parser.ParseSpecifications(specsResp)
	if err != nil {
		return nil, matchResult, fmt.Errorf("failed to parse specifications: %w", err)
	}

	// Convert to database models
	dbSpecs := make([]*model.EspecificacaoTecnica, 0, len(oilSpecs))
	for _, spec := range oilSpecs {
		confidence := matchResult.Score.Confidence
		dbSpec := &model.EspecificacaoTecnica{
			CodigoAplicacao:    vehicle.CodigoAplicacao,
			TipoFluido:         spec.TipoFluido,
			Viscosidade:        stringPtr(spec.Viscosidade),
			Capacidade:         stringPtr(spec.Capacidade),
			Norma:              stringPtr(spec.Norma),
			Recomendacao:       stringPtr(spec.Recomendacao),
			Observacao:         stringPtr(spec.Observacao),
			Fonte:              "MotulAPI",
			MotulVehicleTypeID: stringPtr(matchResult.VehicleType.ID),
			MatchConfidence:    &confidence,
		}
		dbSpecs = append(dbSpecs, dbSpec)
	}

	return dbSpecs, matchResult, nil
}

// commitBatch commits a batch of specifications to the database
func (s *Service) commitBatch(ctx context.Context, batch []*model.EspecificacaoTecnica, lastID int) error {
	if s.config.DryRun {
		slog.Info("Dry run: would commit batch", "size", len(batch))
		return nil
	}

	if err := s.especRepo.InsertBatch(ctx, batch); err != nil {
		return err
	}

	// Save checkpoint
	if err := s.checkpoint.Save(lastID, s.progress); err != nil {
		slog.Warn("Failed to save checkpoint", "error", err)
	}

	return nil
}

// generateReport generates final report
func (s *Service) generateReport() {
	snapshot := s.progress.GetSnapshot()

	slog.Info("=== SCRAPING REPORT ===")
	slog.Info("Duration", "elapsed", snapshot.Elapsed.String())
	slog.Info("Vehicles",
		"total", snapshot.TotalVehicles,
		"success", snapshot.Success,
		"failed", snapshot.Failed,
		"skipped", snapshot.Skipped,
	)
	slog.Info("Matching",
		"exact", snapshot.ExactMatch,
		"fuzzy", snapshot.FuzzyMatch,
		"no_match", snapshot.NoMatch,
	)
	slog.Info("Performance",
		"total_requests", snapshot.TotalRequests,
		"avg_rps", fmt.Sprintf("%.2f", snapshot.RequestsPerSec),
		"avg_time_per_vehicle", fmt.Sprintf("%.2fs", snapshot.AvgTimePerVehicle),
	)
}

// Helper function to create string pointer
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
```

**Step 2: Commit**

```bash
git add internal/scraper/service.go
git commit -m "feat: add processVehicle and helper methods to scraper"
```

---

## Task 16: Add missing repository method (GetAllVehicles)

**Files:**
- Modify: `internal/repository/aplicacao_repo.go`

**Step 1: Add GetAllVehicles method**

Open `internal/repository/aplicacao_repo.go` and add:

```go
// GetAllVehicles fetches vehicles for scraping
func (r *AplicacaoRepo) GetAllVehicles(ctx context.Context, startFromID int, limit int) ([]*model.Aplicacao, error) {
	query := `
		SELECT
			"CodigoAplicacao",
			"CodigoFabricante",
			"DescricaoCompleta",
			COALESCE("Ano", '') as "Ano",
			COALESCE("Motor", '') as "Motor"
		FROM "APLICACAO"
		WHERE "CodigoAplicacao" > $1
		ORDER BY "CodigoAplicacao" ASC
	`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := r.db.Query(ctx, query, startFromID)
	if err != nil {
		return nil, fmt.Errorf("failed to query vehicles: %w", err)
	}
	defer rows.Close()

	vehicles := []*model.Aplicacao{}
	for rows.Next() {
		var v model.Aplicacao
		if err := rows.Scan(
			&v.CodigoAplicacao,
			&v.CodigoFabricante,
			&v.DescricaoCompleta,
			&v.Ano,
			&v.Motor,
		); err != nil {
			return nil, fmt.Errorf("failed to scan vehicle: %w", err)
		}
		vehicles = append(vehicles, &v)
	}

	return vehicles, nil
}
```

**Step 2: Also add fields to Aplicacao model if missing**

Ensure `internal/model/aplicacao.go` has all needed fields:

```go
package model

type Aplicacao struct {
	CodigoAplicacao   int    `json:"codigo_aplicacao"`
	CodigoFabricante  int    `json:"codigo_fabricante"`
	DescricaoCompleta string `json:"descricao_completa"`
	Ano               string `json:"ano"`
	Motor             string `json:"motor"`
	Fabricante        string `json:"fabricante"` // Will be joined or set separately
	Modelo            string `json:"modelo"`     // Will be extracted or set separately
}
```

**Step 3: Commit**

```bash
git add internal/repository/aplicacao_repo.go internal/model/aplicacao.go
git commit -m "feat: add GetAllVehicles method to AplicacaoRepository"
```

---

## Task 17: CLI Main (Part 1: Flags and Config)

**Files:**
- Create: `cmd/motul-scraper/main.go`

**Step 1: Create CLI with flags**

Create `cmd/motul-scraper/main.go`:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wega-catalog-api/internal/client"
	"wega-catalog-api/internal/database"
	"wega-catalog-api/internal/repository"
	"wega-catalog-api/internal/scraper"
)

func main() {
	// Parse flags
	var (
		dbConnection  = flag.String("db-connection", os.Getenv("DATABASE_URL"), "PostgreSQL connection string")
		rateLimit     = flag.Float64("rate-limit", 1.0, "Requests per second (e.g., 0.5, 1.0, 2.0)")
		batchSize     = flag.Int("batch-size", 100, "Commit to database every N vehicles")
		httpPort      = flag.Int("http-port", 8081, "HTTP monitoring server port")
		minConfidence = flag.Float64("min-confidence", 0.80, "Minimum matching confidence (0.0-1.0)")
		resume        = flag.Bool("resume", false, "Resume from checkpoint")
		dryRun        = flag.Bool("dry-run", false, "Run without saving to database")
		limit         = flag.Int("limit", 0, "Process only N vehicles (0 = all)")
		filterBrand   = flag.String("filter-brand", "", "Process only vehicles from specific brand")
		skipExisting  = flag.Bool("skip-existing", false, "Skip vehicles that already have specs")
		stateFile     = flag.String("state-file", "scraper.state.json", "Checkpoint state file")
		auditFile     = flag.String("audit-file", "scraper.audit.log", "Audit log file")
	)

	flag.Parse()

	// Validate flags
	if *dbConnection == "" {
		slog.Error("Database connection string required (--db-connection or DATABASE_URL)")
		os.Exit(1)
	}

	if *rateLimit < 0.1 {
		slog.Error("Rate limit too low (minimum: 0.1 req/s)")
		os.Exit(1)
	}

	if *minConfidence < 0.5 || *minConfidence > 1.0 {
		slog.Error("Min confidence must be between 0.5 and 1.0")
		os.Exit(1)
	}

	// Setup logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Create config
	config := scraper.Config{
		DBConnection:  *dbConnection,
		RateLimit:     *rateLimit,
		BatchSize:     *batchSize,
		MinConfidence: *minConfidence,
		Resume:        *resume,
		DryRun:        *dryRun,
		Limit:         *limit,
		FilterBrand:   *filterBrand,
		SkipExisting:  *skipExisting,
		StateFile:     *stateFile,
		AuditFile:     *auditFile,
	}

	// Run scraper
	if err := run(config, *httpPort); err != nil {
		slog.Error("Scraper failed", "error", err)
		os.Exit(1)
	}
}
```

**Step 2: Commit**

```bash
git add cmd/motul-scraper/main.go
git commit -m "feat: add CLI flags and configuration parsing"
```

---

## Task 18: CLI Main (Part 2: Wiring and Execution)

**Files:**
- Modify: `cmd/motul-scraper/main.go`

**Step 1: Add run function**

Add to `cmd/motul-scraper/main.go` (after main function):

```go
func run(config scraper.Config, httpPort int) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Received shutdown signal, gracefully stopping...")
		cancel()
	}()

	// Connect to database
	slog.Info("Connecting to database...")
	dbPool, err := database.Connect(ctx, config.DBConnection)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer dbPool.Close()

	slog.Info("Connected to database")

	// Run migrations
	slog.Info("Running migrations...")
	if err := database.RunMigrations(ctx, dbPool); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	slog.Info("Migrations completed")

	// Create repositories
	aplicacaoRepo := repository.NewAplicacaoRepository(dbPool)
	especRepo := repository.NewEspecificacaoRepository(dbPool)

	// Create Motul client
	motulClient := client.NewMotulClient(config.RateLimit)
	defer motulClient.Close()

	// Create scraper service
	scraperService := scraper.NewService(motulClient, aplicacaoRepo, especRepo, config)

	// Start HTTP monitoring server
	progress := scraper.NewProgressTracker(0)
	httpMonitor := scraper.NewHTTPMonitor(httpPort, progress)
	scraperService.SetHTTPMonitor(httpMonitor)

	if err := httpMonitor.Start(); err != nil {
		return fmt.Errorf("failed to start HTTP monitor: %w", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		httpMonitor.Stop(shutdownCtx)
	}()

	slog.Info("HTTP monitoring available", "url", fmt.Sprintf("http://localhost:%d/status", httpPort))

	// Run scraper
	if err := scraperService.Run(ctx); err != nil {
		if err == context.Canceled {
			slog.Info("Scraper stopped by user")
			return nil
		}
		return err
	}

	return nil
}
```

**Step 2: Fix missing methods in database package**

Check if `internal/database/connection.go` has a `Connect` function. If not, add it:

```go
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect creates a new database connection pool
func Connect(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}
```

**Step 3: Commit**

```bash
git add cmd/motul-scraper/main.go internal/database/connection.go
git commit -m "feat: add CLI run function with dependency wiring"
```

---

## Task 19: Add go.mod dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add missing dependency (golang.org/x/text)**

Run:

```bash
go get golang.org/x/text
go mod tidy
```

**Step 2: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add golang.org/x/text for string normalization"
```

---

## Task 20: Fix compilation issues and missing ParseYear export

**Files:**
- Modify: `internal/matching/matcher.go`

**Step 1: Export ParseYear function**

In `internal/matching/matcher.go`, change `parseYear` to `ParseYear` (capital P):

```go
// ParseYear extracts year from string (handles "2020", "2019 -->", etc)
func ParseYear(anoStr string) int {
	// Extract first 4-digit number
	normalized := strings.TrimSpace(anoStr)
	if len(normalized) >= 4 {
		if year, err := strconv.Atoi(normalized[:4]); err == nil {
			if year >= 1900 && year <= 2100 {
				return year
			}
		}
	}
	return 0
}
```

And update the reference in the same file (change lowercase `parseYear` to `ParseYear`):

```go
func (m *VehicleMatcher) FindBestMatch(...) {
	// ...
	wegaYear := ParseYear(wegaVehicle.Ano)
	// ...
}
```

**Step 2: Commit**

```bash
git add internal/matching/matcher.go
git commit -m "fix: export ParseYear function for use in scraper"
```

---

## Task 21: Test Build

**Files:**
- None (testing)

**Step 1: Build the scraper**

Run:

```bash
go build -o motul-scraper ./cmd/motul-scraper
```

Expected: Should compile successfully with no errors.

**Step 2: Test help flag**

Run:

```bash
./motul-scraper --help
```

Expected: Should show all available flags.

**Step 3: If build succeeds, commit**

```bash
git add .
git commit -m "build: verify motul-scraper compiles successfully"
```

---

## Task 22: Create README for scraper

**Files:**
- Create: `cmd/motul-scraper/README.md`

**Step 1: Create usage documentation**

Create `cmd/motul-scraper/README.md`:

```markdown
# Motul Oil Specifications Scraper

CLI tool to extract oil specifications from Motul API and populate the Wega catalog database.

## Usage

### Basic Usage

```bash
./motul-scraper --db-connection="postgres://..."
```

### VM Deployment

**Build for Linux:**
```bash
GOOS=linux GOARCH=amd64 go build -o motul-scraper ./cmd/motul-scraper
```

**Upload to VM:**
```bash
scp -i ~/Downloads/ssh-key-2025-12-23.key \
    motul-scraper \
    ubuntu@140.238.178.70:/home/ubuntu/
```

**Run in tmux:**
```bash
ssh -i ~/Downloads/ssh-key-2025-12-23.key ubuntu@140.238.178.70

tmux new -s scraper
./motul-scraper \
  --db-connection="postgres://postgres:...@o8cok8s4cg408cos4k0sowos:5432/postgres?sslmode=require" \
  --http-port=8081

# Detach: Ctrl+B, D
# Reattach: tmux attach -t scraper
```

**Monitor from Mac:**
```bash
watch -n 5 'curl -s http://140.238.178.70:8081/status | jq ".progress"'
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--db-connection` | $DATABASE_URL | PostgreSQL connection string |
| `--rate-limit` | 1.0 | Requests per second |
| `--batch-size` | 100 | Commit every N vehicles |
| `--http-port` | 8081 | Monitoring server port |
| `--min-confidence` | 0.80 | Minimum match confidence |
| `--resume` | false | Resume from checkpoint |
| `--dry-run` | false | Test without saving |
| `--limit` | 0 | Process only N vehicles |
| `--filter-brand` | "" | Process specific brand only |
| `--skip-existing` | false | Skip vehicles with existing specs |

## Monitoring

- **Status endpoint:** `http://IP:8081/status`
- **Health check:** `http://IP:8081/health`

## Files Generated

- `scraper.state.json` - Checkpoint for resume
- `scraper.audit.log` - Audit trail
- `scraper.log` - Main log (stdout)

## Estimated Runtime

~14 hours for 49,034 vehicles at 1 req/second.
```

**Step 2: Commit**

```bash
git add cmd/motul-scraper/README.md
git commit -m "docs: add scraper usage documentation"
```

---

## Task 23: Final verification and deployment instructions

**Files:**
- None (documentation)

**Step 1: Verify all components**

Run a dry-run test:

```bash
./motul-scraper \
  --db-connection="postgres://..." \
  --dry-run \
  --limit=5
```

Expected: Should process 5 vehicles without errors, without saving to DB.

**Step 2: Create deployment checklist**

The implementation is complete. Final deployment steps:

1. **Build for Linux:**
   ```bash
   GOOS=linux GOARCH=amd64 go build -o motul-scraper ./cmd/motul-scraper
   ```

2. **Upload to VM:**
   ```bash
   scp -i ~/Downloads/ssh-key-2025-12-23.key motul-scraper ubuntu@140.238.178.70:/home/ubuntu/
   ```

3. **Run on VM:**
   ```bash
   ssh -i ~/Downloads/ssh-key-2025-12-23.key ubuntu@140.238.178.70
   tmux new -s scraper
   ./motul-scraper --db-connection="postgres://postgres:Erqn72G9MD3xKdrULr12LIcwBF5sn8C6g52GJuOCw4pO6vLUwAdCr3EPKMil5lQ2@o8cok8s4cg408cos4k0sowos:5432/postgres?sslmode=require"
   ```

4. **Monitor from Mac:**
   ```bash
   watch -n 5 'curl -s http://140.238.178.70:8081/status | jq'
   ```

---

## Summary

This plan implements a complete Motul scraper with:

✅ Database schema and migrations
✅ Repository layer for data access
✅ Rate-limited HTTP client with retry
✅ Fuzzy matching algorithm (80% confidence)
✅ JSON parser for Motul's complex format
✅ Progress tracking and HTTP monitoring
✅ Checkpoint/resume capability
✅ CLI with comprehensive flags
✅ VM deployment support

**Total Tasks:** 23
**Estimated Implementation Time:** 2-3 hours (bite-sized tasks)
**Runtime on VM:** ~14 hours for full catalog
