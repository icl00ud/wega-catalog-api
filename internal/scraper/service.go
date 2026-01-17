package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"wega-catalog-api/internal/model"
)

// VehicleRepository defines methods needed from aplicacao repository
type VehicleRepository interface {
	GetAllVehicles(ctx context.Context) ([]model.Aplicacao, error)
	GetVehicleByID(ctx context.Context, id int) (*model.Aplicacao, error)
}

// EspecificacaoRepository defines methods for saving specifications
type EspecificacaoRepository interface {
	Insert(ctx context.Context, spec *model.EspecificacaoTecnica) error
	ExistsForVehicle(ctx context.Context, codigoAplicacao int) (bool, error)
}

// FalhaRepository defines methods for tracking failures
type FalhaRepository interface {
	Upsert(ctx context.Context, codigoAplicacao int, tipoErro, mensagemErro string) error
	MarkResolved(ctx context.Context, codigoAplicacao int) error
	GetPendingRetries(ctx context.Context, limit int) ([]model.ScraperFalha, error)
	CountPending(ctx context.Context) (int, error)
}

// MotulClient defines methods needed from Motul API client
type MotulClient interface {
	SearchVehicle(ctx context.Context, brand, modelName string, year int) (*MotulVehicle, error)
	GetSpecifications(ctx context.Context, vehicleTypeID string) ([]OilSpecification, error)
}

// OilSpecification represents a single oil specification from Motul
type OilSpecification struct {
	TipoFluido   string
	Viscosidade  string
	Capacidade   string
	Norma        string
	Recomendacao string
}

// MotulVehicle represents a vehicle from Motul API
type MotulVehicle struct {
	ID          string
	Brand       string
	Model       string
	Year        int
	Description string
	MotorType   string
}

// ScraperConfig holds configuration for the scraper
type ScraperConfig struct {
	Workers          int
	RateLimit        time.Duration
	CheckpointEvery  int
	CheckpointFile   string
	ResumeFromID     int
	DryRun           bool
	HTTPMonitorPort  int
	EnableMonitoring bool
}

// DefaultScraperConfig returns default configuration
func DefaultScraperConfig() ScraperConfig {
	return ScraperConfig{
		Workers:          5,
		RateLimit:        200 * time.Millisecond,
		CheckpointEvery:  100,
		CheckpointFile:   "scraper_checkpoint.json",
		ResumeFromID:     0,
		DryRun:           false,
		HTTPMonitorPort:  9090,
		EnableMonitoring: true,
	}
}

// ScraperService orchestrates the scraping process
type ScraperService struct {
	config      ScraperConfig
	vehicleRepo VehicleRepository
	specRepo    EspecificacaoRepository
	falhaRepo   FalhaRepository
	motulClient MotulClient
	checkpoint  *CheckpointManager
	progress    *ProgressTracker
	monitor     *HTTPMonitor
	logger      *slog.Logger
}

// NewScraperService creates a new scraper service
func NewScraperService(
	config ScraperConfig,
	vehicleRepo VehicleRepository,
	specRepo EspecificacaoRepository,
	motulClient MotulClient,
	logger *slog.Logger,
) *ScraperService {
	return &ScraperService{
		config:      config,
		vehicleRepo: vehicleRepo,
		specRepo:    specRepo,
		falhaRepo:   nil, // Optional, set via SetFalhaRepo
		motulClient: motulClient,
		checkpoint:  NewCheckpointManager(config.CheckpointFile),
		logger:      logger,
	}
}

// SetFalhaRepo sets the failure repository for tracking failed attempts
func (s *ScraperService) SetFalhaRepo(repo FalhaRepository) {
	s.falhaRepo = repo
}

// Run executes the scraping process
func (s *ScraperService) Run(ctx context.Context) error {
	s.logger.Info("starting scraper service",
		"workers", s.config.Workers,
		"rate_limit", s.config.RateLimit,
		"dry_run", s.config.DryRun,
	)

	// Load vehicles from database
	vehicles, err := s.vehicleRepo.GetAllVehicles(ctx)
	if err != nil {
		return fmt.Errorf("failed to load vehicles: %w", err)
	}

	s.logger.Info("loaded vehicles", "count", len(vehicles))

	// Handle resume from checkpoint
	startIndex := 0
	if s.checkpoint.Exists() {
		checkpoint, err := s.checkpoint.Load()
		if err != nil {
			s.logger.Warn("failed to load checkpoint, starting fresh", "error", err)
		} else {
			s.logger.Info("resuming from checkpoint",
				"last_id", checkpoint.LastProcessedID,
				"saved_at", checkpoint.SavedAt,
			)
			// Find index of last processed vehicle
			for i, v := range vehicles {
				if v.CodigoAplicacao == checkpoint.LastProcessedID {
					startIndex = i + 1
					break
				}
			}
		}
	}

	if s.config.ResumeFromID > 0 {
		s.logger.Info("resuming from specific ID", "id", s.config.ResumeFromID)
		for i, v := range vehicles {
			if v.CodigoAplicacao >= s.config.ResumeFromID {
				startIndex = i
				break
			}
		}
	}

	vehiclesToProcess := vehicles[startIndex:]
	s.logger.Info("processing vehicles",
		"total", len(vehicles),
		"to_process", len(vehiclesToProcess),
		"skipped", startIndex,
	)

	// Initialize progress tracker
	s.progress = NewProgressTracker(len(vehiclesToProcess))

	// Start HTTP monitoring server if enabled
	if s.config.EnableMonitoring {
		s.monitor = NewHTTPMonitor(s.config.HTTPMonitorPort, s.progress)
		if err := s.monitor.Start(); err != nil {
			s.logger.Warn("failed to start HTTP monitor", "error", err)
		} else {
			s.logger.Info("HTTP monitoring started", "port", s.config.HTTPMonitorPort)
			defer func() {
				s.monitor.Stop(context.Background())
			}()
		}
	}

	// Create work queue
	workQueue := make(chan model.Aplicacao, s.config.Workers*2)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < s.config.Workers; i++ {
		wg.Add(1)
		go s.worker(ctx, i, workQueue, &wg)
	}

	// Feed work queue
	checkpointCounter := 0
	lastProcessedID := 0

	s.logger.Info("starting to feed work queue",
		"vehicles_to_process", len(vehiclesToProcess),
		"workers", s.config.Workers,
	)

	for i, vehicle := range vehiclesToProcess {
		select {
		case <-ctx.Done():
			s.logger.Info("context cancelled, stopping...")
			close(workQueue)
			wg.Wait()
			return ctx.Err()
		case workQueue <- vehicle:
			lastProcessedID = vehicle.CodigoAplicacao
			checkpointCounter++

			// Log first few vehicles being queued
			if i < 5 {
				s.logger.Info("queued vehicle",
					"index", i,
					"id", vehicle.CodigoAplicacao,
					"description", vehicle.DescricaoAplicacao,
				)
			}

			// Save checkpoint periodically
			if checkpointCounter%s.config.CheckpointEvery == 0 {
				if err := s.checkpoint.Save(lastProcessedID, s.progress); err != nil {
					s.logger.Warn("failed to save checkpoint", "error", err)
				} else {
					s.logger.Info("checkpoint saved", "last_id", lastProcessedID)
				}
			}
		}
	}

	// Close queue and wait for workers
	close(workQueue)
	wg.Wait()

	// Final checkpoint save
	if err := s.checkpoint.Save(lastProcessedID, s.progress); err != nil {
		s.logger.Warn("failed to save final checkpoint", "error", err)
	}

	// Print final statistics
	s.printFinalStats()

	return nil
}

// worker processes vehicles from the work queue
func (s *ScraperService) worker(ctx context.Context, id int, queue <-chan model.Aplicacao, wg *sync.WaitGroup) {
	defer wg.Done()

	s.logger.Info("worker started", "worker_id", id)

	rateLimiter := time.NewTicker(s.config.RateLimit)
	defer rateLimiter.Stop()

	processedCount := 0
	for vehicle := range queue {
		// Rate limiting
		<-rateLimiter.C

		// Process vehicle
		s.processVehicle(ctx, vehicle)
		processedCount++

		// Log progress every 100 vehicles per worker
		if processedCount%100 == 0 {
			s.logger.Info("worker progress",
				"worker_id", id,
				"processed", processedCount,
			)
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			s.logger.Info("worker stopping due to context cancellation", "worker_id", id)
			return
		default:
		}
	}

	s.logger.Info("worker finished", "worker_id", id, "total_processed", processedCount)
}

// commercialVehiclePatterns contains patterns to skip (trucks, buses, tractors, etc.)
// These vehicles typically don't exist in Motul's car catalog
var commercialVehiclePatterns = []string{
	// Truck model patterns (more generic)
	"cargo", "constellation", "worker", "delivery",
	"fh ", "fh-", "fm ", "fm-", "fmx", "vm ", "vm-", "nh12", "nh ", "edc",
	"axor", "atego", "actros", "arocs",
	"stralis", "trakker", "eurocargo",
	"serie p", "serie g", "serie r", "serie s",
	// Bus models
	"of-", "o-", "volare", "busscar", "mascarello",
	"marcopolo", "neobus", "caio", "comil",
	// Tractors/Agricultural
	"trator", "colheitadeira", "retroescavadeira",
	"mf ", "massey", "new holland", "case ih", "john deere",
	"valtra", "ls tractor",
	// Heavy equipment
	"escavadeira", "pa carregadeira", "motoniveladora",
	"rolo compactador", "guindaste", "empilhadeira",
	"compressor", "gerador",
	// Specific commercial brands/series
	"9200", "9800", "4700", "8600", // International trucks
	"series ", "hr ", "hd ",
	// Ford trucks (various formats)
	"f-350", "f-4000", "f-14000", "f350", "f4000", "f14000",
	"fb4000", "fb-4000", "f 4000", "fb 4000",
	// Chevrolet/GM trucks
	"d-20", "d20", "d-40", "d40", "d-60", "d60",
	"c-10", "c10", "c-60", "c60", "c-15", "c15",
	// VW trucks (numeric models)
	"5.140", "6.80", "6.90", "7.90", "7.100", "7.110", "7.120",
	"8.120", "8.140", "8.150", "8.160",
	"9.150", "9.170", "10.160", "11.130", "11.180", "12.140", "13.150", "13.180",
	"15.170", "15.180", "15.190", "16.200", "17.180", "17.190", "17.210", "17.220", "17.230", "17.250", "17.280", "17.310",
	"18.310", "19.320", "19.330", "19.360", "19.390", "19.420",
	"23.210", "23.220", "23.230", "23.250", "23.310", "24.250", "24.280", "24.310",
	"25.320", "25.360", "25.370", "25.390", "25.420", "26.260", "26.280", "26.310",
	"31.260", "31.280", "31.310", "31.320", "31.330", "31.370", "31.390", "31.420",
	"furgovan", "kombi furgao",
	// Agrale specific
	"6000", "7000", "8000", "8500", "9200", "10000", "13000", "14000",
}

// commercialBrands are brands that are primarily commercial/industrial vehicles
var commercialBrands = []string{
	// Truck manufacturers
	"scania", "daf", "man", "iveco",
	"international", "navistar", "freightliner", "kenworth", "peterbilt",
	"hino", "isuzu trucks", "ud trucks", "fuso",
	// Industrial/Equipment
	"atlas copco", "caterpillar", "komatsu", "jcb", "bobcat",
	"case", "new holland", "massey ferguson", "john deere", "valtra",
	"agrale",                      // Mostly trucks/buses
	"cummins", "perkins", "deutz", // Engines
	// Motorcycle brands (also not in Motul car catalog)
	"yamaha", "honda motos", "suzuki motos", "kawasaki", "harley",
	"bmw motorrad", "ducati", "triumph", "ktm",
}

// isCommercialVehicle checks if a vehicle is a commercial vehicle that should be skipped
func (s *ScraperService) isCommercialVehicle(brand, model, description string) bool {
	// Normalize all to lowercase for comparison
	brandLower := strings.ToLower(brand)
	modelLower := strings.ToLower(model)
	descLower := strings.ToLower(description)

	// Check brand
	for _, cb := range commercialBrands {
		if strings.Contains(brandLower, cb) {
			return true
		}
	}

	// Check model patterns
	combined := modelLower + " " + descLower
	for _, pattern := range commercialVehiclePatterns {
		if strings.Contains(combined, pattern) {
			return true
		}
	}

	return false
}

// processVehicle handles a single vehicle scraping
func (s *ScraperService) processVehicle(ctx context.Context, vehicle model.Aplicacao) {
	s.logger.Info("processing vehicle",
		"id", vehicle.CodigoAplicacao,
		"description", vehicle.DescricaoAplicacao[:min(50, len(vehicle.DescricaoAplicacao))],
	)

	s.progress.SetCurrentVehicle(vehicle.DescricaoAplicacao)
	s.progress.IncrementProcessed()

	// Parse vehicle data early to check if it's commercial
	brand, modelName, year, parseErr := s.parseVehicleDescription(vehicle)

	// Skip commercial vehicles (trucks, buses, tractors) - they're not in Motul car catalog
	if parseErr == nil && s.isCommercialVehicle(brand, modelName, vehicle.DescricaoAplicacao) {
		s.logger.Info("skipping commercial vehicle",
			"id", vehicle.CodigoAplicacao,
			"brand", brand,
			"model", modelName,
		)
		s.progress.IncrementSkipped()
		return
	}

	// Check if specs already exist for this vehicle
	if s.specRepo != nil {
		exists, err := s.specRepo.ExistsForVehicle(ctx, vehicle.CodigoAplicacao)
		if err != nil {
			s.logger.Warn("failed to check existing specs", "id", vehicle.CodigoAplicacao, "error", err)
		} else if exists {
			s.logger.Debug("specs already exist, skipping", "id", vehicle.CodigoAplicacao)
			s.progress.IncrementSkipped()
			return
		}
	}

	// Check parse error (we already parsed above for commercial check)
	if parseErr != nil {
		s.logger.Debug("failed to parse vehicle",
			"id", vehicle.CodigoAplicacao,
			"description", vehicle.DescricaoAplicacao,
			"error", parseErr,
		)
		s.progress.IncrementSkipped()
		return
	}

	// Skip if dry run
	if s.config.DryRun {
		s.logger.Info("dry run - would search Motul",
			"brand", brand,
			"model", modelName,
			"year", year,
		)
		s.progress.IncrementSuccess()
		return
	}

	// Search Motul API
	s.progress.IncrementRequests()
	motulVehicle, err := s.motulClient.SearchVehicle(ctx, brand, modelName, year)
	if err != nil {
		s.logger.Warn("Motul API search failed",
			"id", vehicle.CodigoAplicacao,
			"brand", brand,
			"model", modelName,
			"year", year,
			"error", err,
		)
		s.progress.IncrementFailed(err.Error())
		s.saveFailure(ctx, vehicle.CodigoAplicacao, err.Error())
		return
	}

	if motulVehicle == nil {
		s.logger.Debug("no match found in Motul",
			"id", vehicle.CodigoAplicacao,
			"brand", brand,
			"model", modelName,
			"year", year,
		)
		s.progress.IncrementNoMatch()
		return
	}

	// Determine match type and log
	matchMethod := "fuzzy"
	if s.isExactMatch(vehicle, motulVehicle) {
		matchMethod = "exact"
		s.progress.IncrementExactMatch()
	} else {
		s.progress.IncrementFuzzyMatch()
	}

	s.logger.Info(matchMethod+" match",
		"id", vehicle.CodigoAplicacao,
		"wega", vehicle.DescricaoAplicacao,
		"motul", motulVehicle.Description,
	)

	// Fetch specifications from Motul
	specs, err := s.motulClient.GetSpecifications(ctx, motulVehicle.ID)
	if err != nil {
		s.logger.Warn("failed to get specifications",
			"id", vehicle.CodigoAplicacao,
			"motul_id", motulVehicle.ID,
			"error", err,
		)
		s.progress.IncrementFailed("specs_fetch_error")
		s.saveFailure(ctx, vehicle.CodigoAplicacao, "specs_fetch_error: "+err.Error())
		return
	}

	if len(specs) == 0 {
		s.logger.Debug("no specifications found",
			"id", vehicle.CodigoAplicacao,
			"motul_id", motulVehicle.ID,
		)
		s.progress.IncrementNoMatch()
		return
	}

	// Save specifications to database
	if s.specRepo != nil {
		confidence := 0.85
		if matchMethod == "exact" {
			confidence = 0.95
		}

		savedCount := 0
		for _, spec := range specs {
			especificacao := &model.EspecificacaoTecnica{
				CodigoAplicacao:    vehicle.CodigoAplicacao,
				TipoFluido:         spec.TipoFluido,
				Viscosidade:        strPtr(spec.Viscosidade),
				Capacidade:         strPtr(spec.Capacidade),
				Norma:              strPtr(spec.Norma),
				Recomendacao:       strPtr(spec.Recomendacao),
				Fonte:              "motul",
				MotulVehicleTypeID: strPtr(motulVehicle.ID),
				MatchConfidence:    &confidence,
			}

			if err := s.specRepo.Insert(ctx, especificacao); err != nil {
				s.logger.Warn("failed to save specification",
					"id", vehicle.CodigoAplicacao,
					"tipo", spec.TipoFluido,
					"error", err,
				)
				continue
			}
			savedCount++
		}

		s.logger.Info("saved specifications",
			"id", vehicle.CodigoAplicacao,
			"count", savedCount,
			"total", len(specs),
		)

		// Mark any previous failure as resolved
		if savedCount > 0 {
			s.markFailureResolved(ctx, vehicle.CodigoAplicacao)
		}
	}

	s.progress.IncrementSuccess()
}

// strPtr returns a pointer to a string, or nil if empty
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// parseVehicleDescription extracts brand, model, and year from vehicle description
func (s *ScraperService) parseVehicleDescription(vehicle model.Aplicacao) (brand, modelName string, year int, err error) {
	// Use brand from Fabricante field if available
	brand = vehicle.Fabricante
	if brand == "" {
		brand = vehicle.Marca
	}

	// Use model from Modelo field if available
	modelName = vehicle.Modelo
	if modelName == "" {
		modelName = vehicle.DescricaoAplicacao
	}

	// Extract only the base model name (before first " - " or " /")
	if idx := strings.Index(modelName, " - "); idx > 0 {
		modelName = modelName[:idx]
	}
	if idx := strings.Index(modelName, " /"); idx > 0 {
		modelName = modelName[:idx]
	}

	// Try to extract year from Periodo or Ano field
	yearStr := vehicle.Periodo
	if yearStr == "" {
		yearStr = vehicle.Ano
	}

	// Parse year from string (format might be "2020", "2019 -->", etc.)
	if yearStr != "" {
		// Extract first 4-digit number
		for i := 0; i < len(yearStr)-3; i++ {
			if yearStr[i] >= '0' && yearStr[i] <= '9' {
				potentialYear := yearStr[i : i+4]
				var parsedYear int
				if _, err := fmt.Sscanf(potentialYear, "%d", &parsedYear); err == nil {
					if parsedYear >= 1990 && parsedYear <= 2030 {
						year = parsedYear
						break
					}
				}
			}
		}
	}

	if brand == "" || modelName == "" {
		return "", "", 0, fmt.Errorf("missing brand or model")
	}

	// Normalize strings
	brand = s.normalizeString(brand)
	modelName = s.normalizeString(modelName)

	return brand, modelName, year, nil
}

// normalizeString removes accents and normalizes text
func (s *ScraperService) normalizeString(text string) string {
	// Remove accents
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	normalized, _, _ := transform.String(t, text)

	// Trim and convert to title case
	normalized = strings.TrimSpace(normalized)
	return strings.Title(strings.ToLower(normalized))
}

// isExactMatch determines if Wega and Motul vehicles are an exact match
func (s *ScraperService) isExactMatch(wega model.Aplicacao, motul *MotulVehicle) bool {
	// Normalize both descriptions
	wegaDesc := s.normalizeString(wega.DescricaoAplicacao)
	motulDesc := s.normalizeString(motul.Description)

	// Check if descriptions are similar (fuzzy matching could be enhanced)
	return strings.Contains(wegaDesc, motulDesc) || strings.Contains(motulDesc, wegaDesc)
}

// printFinalStats prints final scraping statistics
func (s *ScraperService) printFinalStats() {
	snapshot := s.progress.GetSnapshot()

	s.logger.Info("scraping completed",
		"elapsed", snapshot.Elapsed.String(),
		"total", snapshot.TotalVehicles,
		"processed", snapshot.Processed,
		"success", snapshot.Success,
		"failed", snapshot.Failed,
		"skipped", snapshot.Skipped,
		"exact_match", snapshot.ExactMatch,
		"fuzzy_match", snapshot.FuzzyMatch,
		"no_match", snapshot.NoMatch,
		"total_requests", snapshot.TotalRequests,
		"req_per_sec", fmt.Sprintf("%.2f", snapshot.RequestsPerSec),
	)
}

// saveFailure records a failed scraping attempt to the database
func (s *ScraperService) saveFailure(ctx context.Context, codigoAplicacao int, errMsg string) {
	if s.falhaRepo == nil {
		return // No failure repository configured
	}

	tipoErro := model.ClassifyError(errMsg)
	if err := s.falhaRepo.Upsert(ctx, codigoAplicacao, tipoErro, errMsg); err != nil {
		s.logger.Warn("failed to save failure record",
			"id", codigoAplicacao,
			"error", err,
		)
	}
}

// markFailureResolved marks a previously failed vehicle as resolved
func (s *ScraperService) markFailureResolved(ctx context.Context, codigoAplicacao int) {
	if s.falhaRepo == nil {
		return // No failure repository configured
	}

	if err := s.falhaRepo.MarkResolved(ctx, codigoAplicacao); err != nil {
		s.logger.Debug("failed to mark failure as resolved",
			"id", codigoAplicacao,
			"error", err,
		)
	}
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
