package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"wega-catalog-api/internal/client"
)

// MotulCatalog holds the complete Motul catalog data
type MotulCatalog struct {
	LoadedAt time.Time                       `json:"loaded_at"`
	Brands   []CatalogBrand                  `json:"brands"`
	BrandMap map[string]*CatalogBrand        `json:"-"` // brand name (normalized) -> brand
	ModelMap map[string][]CatalogVehicleType `json:"-"` // brandID:modelID -> types
}

// CatalogBrand represents a brand with its models
type CatalogBrand struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Models []CatalogModel `json:"models"`
}

// CatalogModel represents a model with its vehicle types
type CatalogModel struct {
	ID    string               `json:"id"`
	Name  string               `json:"name"`
	Types []CatalogVehicleType `json:"types"`
}

// CatalogVehicleType represents a specific vehicle type
type CatalogVehicleType struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BrandID  string `json:"brand_id"`
	ModelID  string `json:"model_id"`
	FullPath string `json:"full_path"` // "Brand > Model > Type"
}

// CatalogLoader loads and caches the Motul catalog
type CatalogLoader struct {
	motulClient *client.MotulClient
	logger      *slog.Logger
	catalog     *MotulCatalog
	mu          sync.RWMutex
}

// NewCatalogLoader creates a new catalog loader
func NewCatalogLoader(motulClient *client.MotulClient, logger *slog.Logger) *CatalogLoader {
	return &CatalogLoader{
		motulClient: motulClient,
		logger:      logger,
	}
}

// LoadOrFetch loads catalog from file or fetches from API
func (l *CatalogLoader) LoadOrFetch(ctx context.Context, cacheFile string) (*MotulCatalog, error) {
	// Try to load from cache file first
	if catalog, err := l.loadFromFile(cacheFile); err == nil {
		l.logger.Info("loaded Motul catalog from cache",
			"file", cacheFile,
			"brands", len(catalog.Brands),
			"loaded_at", catalog.LoadedAt,
		)
		l.catalog = catalog
		l.buildIndexes()
		return catalog, nil
	}

	// Fetch from API
	l.logger.Info("fetching Motul catalog from API (this may take a few minutes)...")
	catalog, err := l.fetchFromAPI(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch catalog: %w", err)
	}

	// Save to cache file
	if err := l.saveToFile(cacheFile, catalog); err != nil {
		l.logger.Warn("failed to save catalog to cache", "error", err)
	} else {
		l.logger.Info("saved Motul catalog to cache", "file", cacheFile)
	}

	l.catalog = catalog
	l.buildIndexes()
	return catalog, nil
}

// GetCatalog returns the loaded catalog
func (l *CatalogLoader) GetCatalog() *MotulCatalog {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.catalog
}

// loadFromFile loads catalog from JSON file
func (l *CatalogLoader) loadFromFile(filename string) (*MotulCatalog, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var catalog MotulCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, err
	}

	// Check if cache is too old (older than 7 days)
	if time.Since(catalog.LoadedAt) > 7*24*time.Hour {
		return nil, fmt.Errorf("cache is too old")
	}

	return &catalog, nil
}

// saveToFile saves catalog to JSON file
func (l *CatalogLoader) saveToFile(filename string, catalog *MotulCatalog) error {
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

// fetchFromAPI fetches complete catalog from Motul API
func (l *CatalogLoader) fetchFromAPI(ctx context.Context) (*MotulCatalog, error) {
	catalog := &MotulCatalog{
		LoadedAt: time.Now(),
		Brands:   []CatalogBrand{},
	}

	// 1. Get all brands
	l.logger.Info("fetching brands...")
	brands, err := l.motulClient.GetBrands(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get brands: %w", err)
	}
	l.logger.Info("fetched brands", "count", len(brands))

	// 2. For each brand, get models
	for i, brand := range brands {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		catalogBrand := CatalogBrand{
			ID:     brand.ID,
			Name:   brand.Name,
			Models: []CatalogModel{},
		}

		l.logger.Debug("fetching models for brand",
			"brand", brand.Name,
			"progress", fmt.Sprintf("%d/%d", i+1, len(brands)),
		)

		// Try multiple years to get models (some models only appear in certain years)
		yearsToTry := []int{2024, 2023, 2022, 2020, 2018, 2015, 2010, 2005, 2000}
		seenModels := make(map[string]bool)

		for _, year := range yearsToTry {
			models, err := l.motulClient.GetModels(ctx, brand.ID, year)
			if err != nil {
				l.logger.Debug("failed to get models for year",
					"brand", brand.Name,
					"year", year,
					"error", err,
				)
				continue
			}

			for _, model := range models {
				if seenModels[model.ID] {
					continue
				}
				seenModels[model.ID] = true

				catalogModel := CatalogModel{
					ID:    model.ID,
					Name:  model.Name,
					Types: []CatalogVehicleType{},
				}

				// 3. Get vehicle types for this model
				types, err := l.motulClient.GetVehicleTypes(ctx, model.ID)
				if err != nil {
					l.logger.Debug("failed to get types for model",
						"brand", brand.Name,
						"model", model.Name,
						"error", err,
					)
				} else {
					for _, vt := range types {
						catalogModel.Types = append(catalogModel.Types, CatalogVehicleType{
							ID:       vt.ID,
							Name:     vt.Name,
							BrandID:  brand.ID,
							ModelID:  model.ID,
							FullPath: fmt.Sprintf("%s > %s > %s", brand.Name, model.Name, vt.Name),
						})
					}
				}

				catalogBrand.Models = append(catalogBrand.Models, catalogModel)
			}
		}

		catalog.Brands = append(catalog.Brands, catalogBrand)

		// Log progress every 10 brands
		if (i+1)%10 == 0 {
			l.logger.Info("catalog loading progress",
				"brands_processed", i+1,
				"total_brands", len(brands),
			)
		}
	}

	// Count total types
	totalModels := 0
	totalTypes := 0
	for _, brand := range catalog.Brands {
		totalModels += len(brand.Models)
		for _, model := range brand.Models {
			totalTypes += len(model.Types)
		}
	}

	l.logger.Info("catalog loading complete",
		"brands", len(catalog.Brands),
		"models", totalModels,
		"vehicle_types", totalTypes,
	)

	return catalog, nil
}

// buildIndexes builds lookup indexes for fast access
func (l *CatalogLoader) buildIndexes() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.catalog == nil {
		return
	}

	l.catalog.BrandMap = make(map[string]*CatalogBrand)
	l.catalog.ModelMap = make(map[string][]CatalogVehicleType)

	for i := range l.catalog.Brands {
		brand := &l.catalog.Brands[i]
		// Index by normalized name
		normalizedName := normalizeString(brand.Name)
		l.catalog.BrandMap[normalizedName] = brand

		for j := range brand.Models {
			model := &brand.Models[j]
			key := fmt.Sprintf("%s:%s", brand.ID, model.ID)
			l.catalog.ModelMap[key] = model.Types
		}
	}
}

// GetBrandNames returns all brand names
func (l *CatalogLoader) GetBrandNames() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.catalog == nil {
		return nil
	}

	names := make([]string, len(l.catalog.Brands))
	for i, brand := range l.catalog.Brands {
		names[i] = brand.Name
	}
	return names
}

// GetModelNames returns all model names for a brand
func (l *CatalogLoader) GetModelNames(brandName string) []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.catalog == nil {
		return nil
	}

	normalized := normalizeString(brandName)
	brand, ok := l.catalog.BrandMap[normalized]
	if !ok {
		return nil
	}

	names := make([]string, len(brand.Models))
	for i, model := range brand.Models {
		names[i] = model.Name
	}
	return names
}

// GetVehicleTypes returns all vehicle types for a brand and model
func (l *CatalogLoader) GetVehicleTypes(brandName, modelName string) []CatalogVehicleType {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.catalog == nil {
		return nil
	}

	normalized := normalizeString(brandName)
	brand, ok := l.catalog.BrandMap[normalized]
	if !ok {
		return nil
	}

	// Find model
	normalizedModel := normalizeString(modelName)
	for _, model := range brand.Models {
		if normalizeString(model.Name) == normalizedModel {
			return model.Types
		}
	}

	return nil
}

// FindBrand finds a brand by name (case-insensitive)
func (l *CatalogLoader) FindBrand(brandName string) *CatalogBrand {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.catalog == nil {
		return nil
	}

	normalized := normalizeString(brandName)
	return l.catalog.BrandMap[normalized]
}

// normalizeString normalizes a string for comparison
func normalizeString(s string) string {
	// Simple normalization for map keys
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c = c + 32 // toLower
		}
		if c != ' ' {
			result = append(result, c)
		}
	}
	return string(result)
}
