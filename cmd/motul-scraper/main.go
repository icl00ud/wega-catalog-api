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

	"wega-catalog-api/internal/database"
	"wega-catalog-api/internal/repository"
	"wega-catalog-api/internal/scraper"
)

func main() {
	// Parse command line flags
	var (
		dbHost          = flag.String("db-host", getEnv("DB_HOST", "localhost"), "Database host")
		dbPort          = flag.Int("db-port", getEnvInt("DB_PORT", 5432), "Database port")
		dbName          = flag.String("db-name", getEnv("DB_NAME", "wega"), "Database name")
		dbUser          = flag.String("db-user", getEnv("DB_USER", "wega"), "Database user")
		dbPassword      = flag.String("db-password", getEnv("DB_PASSWORD", ""), "Database password")
		dbSSLMode       = flag.String("db-sslmode", getEnv("DB_SSLMODE", "disable"), "Database SSL mode")
		workers         = flag.Int("workers", 5, "Number of concurrent workers")
		rateLimitMs     = flag.Int("rate-limit", 200, "Rate limit in milliseconds between requests")
		checkpointEvery = flag.Int("checkpoint-every", 100, "Save checkpoint every N vehicles")
		checkpointFile  = flag.String("checkpoint-file", "scraper_checkpoint.json", "Checkpoint file path")
		resumeFromID    = flag.Int("resume-from", 0, "Resume from specific vehicle ID")
		dryRun          = flag.Bool("dry-run", false, "Dry run mode (don't make API calls)")
		monitorPort     = flag.Int("monitor-port", 9090, "HTTP monitoring server port")
		noMonitor       = flag.Bool("no-monitor", false, "Disable HTTP monitoring")
		logLevel        = flag.String("log-level", getEnv("LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	)

	flag.Parse()

	// Validate required flags
	if *dbPassword == "" {
		fmt.Fprintln(os.Stderr, "Error: database password is required (use -db-password or DB_PASSWORD env)")
		os.Exit(1)
	}

	// Setup logger
	logger := setupLogger(*logLevel)

	logger.Info("starting Motul scraper",
		"db_host", *dbHost,
		"db_port", *dbPort,
		"db_name", *dbName,
		"workers", *workers,
		"rate_limit_ms", *rateLimitMs,
		"dry_run", *dryRun,
	)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logger.Info("received signal, shutting down gracefully", "signal", sig)
		cancel()
	}()

	// Connect to database
	dbConfig := database.ConnectionConfig{
		Host:     *dbHost,
		Port:     *dbPort,
		Database: *dbName,
		User:     *dbUser,
		Password: *dbPassword,
		SSLMode:  *dbSSLMode,
		MaxConns: 25,
		MinConns: 5,
	}

	dbPool, err := database.Connect(ctx, dbConfig)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	logger.Info("connected to database")

	// Initialize repository
	vehicleRepo := repository.NewAplicacaoRepo(dbPool)

	// Create mock Motul client (TODO: implement real client)
	motulClient := &MockMotulClient{logger: logger}

	// Setup scraper config
	scraperConfig := scraper.ScraperConfig{
		Workers:          *workers,
		RateLimit:        time.Duration(*rateLimitMs) * time.Millisecond,
		CheckpointEvery:  *checkpointEvery,
		CheckpointFile:   *checkpointFile,
		ResumeFromID:     *resumeFromID,
		DryRun:           *dryRun,
		HTTPMonitorPort:  *monitorPort,
		EnableMonitoring: !*noMonitor,
	}

	// Create scraper service
	scraperService := scraper.NewScraperService(
		scraperConfig,
		vehicleRepo,
		motulClient,
		logger,
	)

	// Run scraper
	if err := scraperService.Run(ctx); err != nil {
		if err == context.Canceled {
			logger.Info("scraper cancelled")
			os.Exit(0)
		}
		logger.Error("scraper failed", "error", err)
		os.Exit(1)
	}

	logger.Info("scraper completed successfully")
}

// setupLogger creates a structured logger with the specified level
func setupLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})

	return slog.New(handler)
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt gets an integer environment variable or returns a default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intValue int
		if _, err := fmt.Sscanf(value, "%d", &intValue); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// MockMotulClient is a temporary mock implementation
type MockMotulClient struct {
	logger *slog.Logger
}

// SearchVehicle mocks the Motul API search
func (m *MockMotulClient) SearchVehicle(ctx context.Context, brand, model string, year int) (*scraper.MotulVehicle, error) {
	// TODO: Implement real Motul API client
	m.logger.Debug("mock Motul API call",
		"brand", brand,
		"model", model,
		"year", year,
	)

	// Return mock success for demonstration
	return &scraper.MotulVehicle{
		ID:          "mock-id",
		Brand:       brand,
		Model:       model,
		Year:        year,
		Description: fmt.Sprintf("%s %s %d", brand, model, year),
	}, nil
}
