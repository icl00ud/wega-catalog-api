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
}

// MotulClient defines methods needed from Motul API client
type MotulClient interface {
	SearchVehicle(ctx context.Context, brand, model string, year int) (*MotulVehicle, error)
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
	Workers           int
	RateLimit         time.Duration
	CheckpointEvery   int
	CheckpointFile    string
	ResumeFromID      int
	DryRun            bool
	HTTPMonitorPort   int
	EnableMonitoring  bool
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
	motulClient MotulClient,
	logger *slog.Logger,
) *ScraperService {
	return &ScraperService{
		config:      config,
		vehicleRepo: vehicleRepo,
		motulClient: motulClient,
		checkpoint:  NewCheckpointManager(config.CheckpointFile),
		logger:      logger,
	}
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
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				s.monitor.Stop(shutdownCtx)
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

	for _, vehicle := range vehiclesToProcess {
		select {
		case <-ctx.Done():
			s.logger.Info("context cancelled, stopping...")
			close(workQueue)
			wg.Wait()
			return ctx.Err()
		case workQueue <- vehicle:
			lastProcessedID = vehicle.CodigoAplicacao
			checkpointCounter++

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

	rateLimiter := time.NewTicker(s.config.RateLimit)
	defer rateLimiter.Stop()

	for vehicle := range queue {
		// Rate limiting
		<-rateLimiter.C

		// Process vehicle
		s.processVehicle(ctx, vehicle)

		// Check context cancellation
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// processVehicle handles a single vehicle scraping
func (s *ScraperService) processVehicle(ctx context.Context, vehicle model.Aplicacao) {
	s.progress.SetCurrentVehicle(vehicle.DescricaoAplicacao)
	s.progress.IncrementProcessed()

	// Parse vehicle data
	brand, model, year, err := s.parseVehicleDescription(vehicle)
	if err != nil {
		s.logger.Debug("failed to parse vehicle",
			"id", vehicle.CodigoAplicacao,
			"description", vehicle.DescricaoAplicacao,
			"error", err,
		)
		s.progress.IncrementSkipped()
		return
	}

	// Skip if dry run
	if s.config.DryRun {
		s.logger.Info("dry run - would search Motul",
			"brand", brand,
			"model", model,
			"year", year,
		)
		s.progress.IncrementSuccess()
		return
	}

	// Search Motul API
	s.progress.IncrementRequests()
	motulVehicle, err := s.motulClient.SearchVehicle(ctx, brand, model, year)
	if err != nil {
		s.logger.Warn("Motul API search failed",
			"id", vehicle.CodigoAplicacao,
			"brand", brand,
			"model", model,
			"year", year,
			"error", err,
		)
		s.progress.IncrementFailed(err.Error())
		return
	}

	if motulVehicle == nil {
		s.logger.Debug("no match found in Motul",
			"id", vehicle.CodigoAplicacao,
			"brand", brand,
			"model", model,
			"year", year,
		)
		s.progress.IncrementNoMatch()
		return
	}

	// Determine match type
	if s.isExactMatch(vehicle, motulVehicle) {
		s.progress.IncrementExactMatch()
		s.logger.Info("exact match",
			"id", vehicle.CodigoAplicacao,
			"wega", vehicle.DescricaoAplicacao,
			"motul", motulVehicle.Description,
		)
	} else {
		s.progress.IncrementFuzzyMatch()
		s.logger.Info("fuzzy match",
			"id", vehicle.CodigoAplicacao,
			"wega", vehicle.DescricaoAplicacao,
			"motul", motulVehicle.Description,
		)
	}

	s.progress.IncrementSuccess()
}

// parseVehicleDescription extracts brand, model, and year from vehicle description
func (s *ScraperService) parseVehicleDescription(vehicle model.Aplicacao) (brand, model string, year int, err error) {
	// Use brand from Fabricante field if available
	brand = vehicle.Fabricante
	if brand == "" {
		brand = vehicle.Marca
	}

	// Use model from Modelo field if available
	model = vehicle.Modelo
	if model == "" {
		model = vehicle.DescricaoAplicacao
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

	if brand == "" || model == "" {
		return "", "", 0, fmt.Errorf("missing brand or model")
	}

	// Normalize strings
	brand = s.normalizeString(brand)
	model = s.normalizeString(model)

	return brand, model, year, nil
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
