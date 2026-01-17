package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"wega-catalog-api/internal/client"
)

// SmartMatcher uses pre-loaded catalog and Groq LLM for intelligent matching
type SmartMatcher struct {
	catalog   *CatalogLoader
	groq      *client.GroqClient
	motul     *client.MotulClient
	logger    *slog.Logger

	// Caches to avoid repeated LLM calls
	brandCache sync.Map // wegaBrand -> motulBrandName
	modelCache sync.Map // wegaBrand:wegaModel -> motulModelName
	typeCache  sync.Map // wegaBrand:wegaModel:wegaType -> CatalogVehicleType
}

// MatchResult represents a successful match
type SmartMatchResult struct {
	VehicleType    CatalogVehicleType
	Confidence     float64
	MatchMethod    string // "exact", "fuzzy", "llm"
	MotulBrand     string
	MotulModel     string
}

// NewSmartMatcher creates a new smart matcher
func NewSmartMatcher(
	catalog *CatalogLoader,
	groq *client.GroqClient,
	motul *client.MotulClient,
	logger *slog.Logger,
) *SmartMatcher {
	return &SmartMatcher{
		catalog: catalog,
		groq:    groq,
		motul:   motul,
		logger:  logger,
	}
}

// FindMatch finds the best matching vehicle type for a Wega vehicle
func (m *SmartMatcher) FindMatch(ctx context.Context, wegaBrand, wegaModel, wegaDescription string, year int) (*SmartMatchResult, error) {
	// 1. Find or match brand
	motulBrand, err := m.matchBrand(ctx, wegaBrand)
	if err != nil {
		return nil, fmt.Errorf("brand not found: %w", err)
	}

	// 2. Find or match model
	motulModel, err := m.matchModel(ctx, motulBrand, wegaModel)
	if err != nil {
		return nil, fmt.Errorf("model not found: %w", err)
	}

	// 3. Get vehicle types for this brand/model
	types := m.catalog.GetVehicleTypes(motulBrand, motulModel)
	if len(types) == 0 {
		return nil, fmt.Errorf("no vehicle types found for %s %s", motulBrand, motulModel)
	}

	// 4. If only one type, return it
	if len(types) == 1 {
		return &SmartMatchResult{
			VehicleType: types[0],
			Confidence:  1.0,
			MatchMethod: "single",
			MotulBrand:  motulBrand,
			MotulModel:  motulModel,
		}, nil
	}

	// 5. Try exact match on type name
	for _, vt := range types {
		if containsAllParts(vt.Name, wegaDescription) {
			return &SmartMatchResult{
				VehicleType: types[0],
				Confidence:  0.95,
				MatchMethod: "exact",
				MotulBrand:  motulBrand,
				MotulModel:  motulModel,
			}, nil
		}
	}

	// 6. Use LLM to find best match
	typeNames := make([]string, len(types))
	for i, vt := range types {
		typeNames[i] = vt.Name
	}

	fullDescription := fmt.Sprintf("%s %s %s", wegaBrand, wegaModel, wegaDescription)
	if year > 0 {
		fullDescription = fmt.Sprintf("%s (%d)", fullDescription, year)
	}

	matchedName, err := m.groq.NormalizeVehicle(ctx, fullDescription, typeNames)
	if err != nil {
		m.logger.Warn("LLM matching failed, using first option",
			"wega", fullDescription,
			"error", err,
		)
		return &SmartMatchResult{
			VehicleType: types[0],
			Confidence:  0.5,
			MatchMethod: "fallback",
			MotulBrand:  motulBrand,
			MotulModel:  motulModel,
		}, nil
	}

	// Find the matched type
	for _, vt := range types {
		if vt.Name == matchedName {
			return &SmartMatchResult{
				VehicleType: vt,
				Confidence:  0.85,
				MatchMethod: "llm",
				MotulBrand:  motulBrand,
				MotulModel:  motulModel,
			}, nil
		}
	}

	// Shouldn't happen, but fallback
	return &SmartMatchResult{
		VehicleType: types[0],
		Confidence:  0.5,
		MatchMethod: "fallback",
		MotulBrand:  motulBrand,
		MotulModel:  motulModel,
	}, nil
}

// matchBrand finds or matches the brand using cache and LLM
func (m *SmartMatcher) matchBrand(ctx context.Context, wegaBrand string) (string, error) {
	// Check cache
	if cached, ok := m.brandCache.Load(wegaBrand); ok {
		return cached.(string), nil
	}

	// Try exact match first
	brand := m.catalog.FindBrand(wegaBrand)
	if brand != nil {
		m.brandCache.Store(wegaBrand, brand.Name)
		return brand.Name, nil
	}

	// Try common aliases
	aliases := map[string]string{
		"vw":         "volkswagen",
		"volkswagen": "volkswagen",
		"bmw":        "bmw",
		"mercedes":   "mercedes-benz",
		"merc":       "mercedes-benz",
		"gm":         "chevrolet",
		"chevy":      "chevrolet",
		"fiat":       "fiat",
	}

	normalized := strings.ToLower(strings.TrimSpace(wegaBrand))
	if alias, ok := aliases[normalized]; ok {
		brand = m.catalog.FindBrand(alias)
		if brand != nil {
			m.brandCache.Store(wegaBrand, brand.Name)
			return brand.Name, nil
		}
	}

	// Use LLM to find best match
	brandNames := m.catalog.GetBrandNames()
	if len(brandNames) == 0 {
		return "", fmt.Errorf("no brands in catalog")
	}

	matchedBrand, err := m.groq.FindBestBrand(ctx, wegaBrand, brandNames)
	if err != nil {
		return "", err
	}

	m.brandCache.Store(wegaBrand, matchedBrand)
	return matchedBrand, nil
}

// matchModel finds or matches the model using cache and LLM
func (m *SmartMatcher) matchModel(ctx context.Context, motulBrand, wegaModel string) (string, error) {
	cacheKey := fmt.Sprintf("%s:%s", motulBrand, wegaModel)

	// Check cache
	if cached, ok := m.modelCache.Load(cacheKey); ok {
		return cached.(string), nil
	}

	// Get available models for this brand
	modelNames := m.catalog.GetModelNames(motulBrand)
	if len(modelNames) == 0 {
		return "", fmt.Errorf("no models found for brand %s", motulBrand)
	}

	// Try exact match first
	normalizedWega := strings.ToLower(strings.TrimSpace(wegaModel))
	for _, modelName := range modelNames {
		if strings.ToLower(modelName) == normalizedWega {
			m.modelCache.Store(cacheKey, modelName)
			return modelName, nil
		}
	}

	// Try partial match (model name contained in Wega model)
	for _, modelName := range modelNames {
		if strings.Contains(normalizedWega, strings.ToLower(modelName)) {
			m.modelCache.Store(cacheKey, modelName)
			return modelName, nil
		}
	}

	// Use LLM to find best match
	matchedModel, err := m.groq.FindBestModel(ctx, wegaModel, modelNames)
	if err != nil {
		return "", err
	}

	m.modelCache.Store(cacheKey, matchedModel)
	return matchedModel, nil
}

// containsAllParts checks if target contains all significant parts of source
func containsAllParts(target, source string) bool {
	sourceLower := strings.ToLower(source)
	targetLower := strings.ToLower(target)

	// Extract significant parts (numbers and significant words)
	parts := strings.Fields(sourceLower)
	matches := 0

	for _, part := range parts {
		// Skip common words
		if len(part) < 2 || isCommonWord(part) {
			continue
		}
		if strings.Contains(targetLower, part) {
			matches++
		}
	}

	// At least 2 significant parts should match
	return matches >= 2
}

// isCommonWord returns true for common filler words
func isCommonWord(word string) bool {
	common := map[string]bool{
		"de": true, "do": true, "da": true, "o": true, "a": true,
		"e": true, "em": true, "com": true, "para": true,
		"cv": true, "hp": true, "v": true,
	}
	return common[word]
}
