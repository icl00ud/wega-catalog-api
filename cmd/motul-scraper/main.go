package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"wega-catalog-api/internal/client"
	"wega-catalog-api/internal/database"
	"wega-catalog-api/internal/repository"
	"wega-catalog-api/internal/scraper"
)

func main() {
	// Parse command line flags
	var (
		// Database flags
		dbHost     = flag.String("db-host", getEnv("DB_HOST", "localhost"), "Database host")
		dbPort     = flag.Int("db-port", getEnvInt("DB_PORT", 5432), "Database port")
		dbName     = flag.String("db-name", getEnv("DB_NAME", "wega"), "Database name")
		dbUser     = flag.String("db-user", getEnv("DB_USER", "wega"), "Database user")
		dbPassword = flag.String("db-password", getEnv("DB_PASSWORD", ""), "Database password")
		dbSSLMode  = flag.String("db-sslmode", getEnv("DB_SSLMODE", "disable"), "Database SSL mode")

		// Groq API flags - supports multiple keys separated by comma for failover
		groqAPIKeys = flag.String("groq-api-keys", getEnv("GROQ_API_KEYS", getEnv("GROQ_API_KEY", "")), "Groq API keys (comma-separated for failover)")
		groqRPM     = flag.Int("groq-rpm", 30, "Groq requests per minute per key (free tier: 30)")

		// Catalog cache flags
		catalogCache = flag.String("catalog-cache", "motul_catalog.json", "Motul catalog cache file")

		// Scraper flags
		workers         = flag.Int("workers", 1, "Number of concurrent workers (keep low for LLM rate limits)")
		rateLimitMs     = flag.Int("rate-limit", 2000, "Rate limit in milliseconds between requests")
		checkpointEvery = flag.Int("checkpoint-every", 50, "Save checkpoint every N vehicles")
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

	if *groqAPIKeys == "" {
		fmt.Fprintln(os.Stderr, "Error: Groq API key(s) required (use -groq-api-keys or GROQ_API_KEYS env)")
		fmt.Fprintln(os.Stderr, "Multiple keys can be separated by comma for automatic failover")
		fmt.Fprintln(os.Stderr, "Get your free API key at: https://console.groq.com/keys")
		os.Exit(1)
	}

	// Parse API keys (comma-separated)
	apiKeys := parseAPIKeys(*groqAPIKeys)
	if len(apiKeys) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no valid API keys provided")
		os.Exit(1)
	}

	// Setup logger
	logger := setupLogger(*logLevel)

	logger.Info("starting Motul scraper with smart matching",
		"db_host", *dbHost,
		"db_port", *dbPort,
		"db_name", *dbName,
		"workers", *workers,
		"rate_limit_ms", *rateLimitMs,
		"groq_rpm", *groqRPM,
		"groq_keys", len(apiKeys),
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

	// Run database migrations
	if err := database.RunMigrations(ctx, dbPool); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("database migrations completed")

	// Initialize repository
	vehicleRepo := repository.NewAplicacaoRepo(dbPool)
	specRepo := repository.NewEspecificacaoRepository(dbPool)
	falhaRepo := repository.NewScraperFalhaRepo(dbPool)

	// Create Motul API client (1 request per second for catalog loading)
	motulClient := client.NewMotulClient(1.0)

	// Create catalog loader and load catalog
	catalogLoader := scraper.NewCatalogLoader(motulClient, logger)
	_, err = catalogLoader.LoadOrFetch(ctx, *catalogCache)
	if err != nil {
		logger.Error("failed to load Motul catalog", "error", err)
		os.Exit(1)
	}

	// Create Groq client for LLM normalization (with multi-key failover support)
	groqClient := client.NewGroqClientMultiKey(apiKeys, float64(*groqRPM), logger)

	// Create smart matcher
	smartMatcher := scraper.NewSmartMatcher(catalogLoader, groqClient, motulClient, logger)

	// Create adapter that implements scraper.MotulClient interface
	motulAdapter := scraper.NewMotulAdapter(smartMatcher, motulClient, logger)

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
		specRepo,
		motulAdapter,
		logger,
	)

	// Set failure repository for tracking failed attempts
	scraperService.SetFalhaRepo(falhaRepo)

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

// parseAPIKeys splits comma-separated API keys and filters empty ones
func parseAPIKeys(keysStr string) []string {
	parts := strings.Split(keysStr, ",")
	var keys []string
	for _, k := range parts {
		k = strings.TrimSpace(k)
		if k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}
