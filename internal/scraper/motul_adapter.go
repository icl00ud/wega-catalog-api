package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"wega-catalog-api/internal/client"
)

// MotulAdapter adapts the smart matcher to work with the scraper service
type MotulAdapter struct {
	smartMatcher *SmartMatcher
	motulClient  *client.MotulClient
	logger       *slog.Logger
}

// NewMotulAdapter creates a new Motul adapter with smart matching
func NewMotulAdapter(
	smartMatcher *SmartMatcher,
	motulClient *client.MotulClient,
	logger *slog.Logger,
) *MotulAdapter {
	return &MotulAdapter{
		smartMatcher: smartMatcher,
		motulClient:  motulClient,
		logger:       logger,
	}
}

// SearchVehicle implements the scraper.MotulClient interface
func (a *MotulAdapter) SearchVehicle(ctx context.Context, brand, model string, year int) (*MotulVehicle, error) {
	// Use smart matcher to find the best match
	result, err := a.smartMatcher.FindMatch(ctx, brand, model, model, year)
	if err != nil {
		return nil, err
	}

	return &MotulVehicle{
		ID:          result.VehicleType.ID,
		Brand:       result.MotulBrand,
		Model:       result.MotulModel,
		Year:        year,
		Description: result.VehicleType.Name,
		MotorType:   result.MatchMethod,
	}, nil
}

// GetSpecifications fetches oil specifications from Motul API
func (a *MotulAdapter) GetSpecifications(ctx context.Context, vehicleTypeID string) ([]OilSpecification, error) {
	a.logger.Debug("fetching specifications", "vehicleTypeID", vehicleTypeID)

	resp, err := a.motulClient.GetSpecifications(ctx, vehicleTypeID)
	if err != nil {
		a.logger.Error("GetSpecifications API call failed", "vehicleTypeID", vehicleTypeID, "error", err)
		return nil, fmt.Errorf("failed to get specifications: %w", err)
	}

	a.logger.Debug("received specifications response",
		"vehicleTypeID", vehicleTypeID,
		"components_count", len(resp.Vehicle.Components),
		"vehicle_model", resp.Vehicle.Model,
		"vehicle_type", resp.Vehicle.Type,
	)

	var result []OilSpecification

	// Parse components from the response (components are nested inside vehicle)
	for _, comp := range resp.Vehicle.Components {
		spec := OilSpecification{
			TipoFluido: a.parseFluidType(comp.Category.Name),
		}

		// Extract capacity
		if len(comp.Capacities) > 0 {
			var capacities []string
			for _, cap := range comp.Capacities {
				if cap.Label != "" {
					capacities = append(capacities, cap.Label+" L")
				}
			}
			spec.Capacidade = strings.Join(capacities, ", ")
		}

		// Extract product recommendations and viscosities
		if len(comp.Recommendations) > 0 {
			var productNames []string
			var viscosities []string

			for _, rec := range comp.Recommendations {
				for _, prod := range rec.Products {
					if prod.Name != "" {
						productNames = append(productNames, prod.Name)
						// Extract viscosity from product name (e.g., "MOTUL 8100 ECO-NERGY 5W-30")
						if visc := extractViscosity(prod.Name); visc != "" {
							viscosities = append(viscosities, visc)
						}
					}
				}
			}

			// Remove duplicates
			spec.Recomendacao = strings.Join(unique(productNames), ", ")
			spec.Viscosidade = strings.Join(unique(viscosities), ", ")
		}

		// Only add if we have useful data
		if spec.TipoFluido != "" && (spec.Viscosidade != "" || spec.Capacidade != "" || spec.Recomendacao != "") {
			result = append(result, spec)
		}
	}

	return result, nil
}

// extractViscosity extracts viscosity pattern from product name
func extractViscosity(name string) string {
	// Common viscosity patterns: 5W-30, 10W-40, 0W-20, etc.
	parts := strings.Fields(name)
	for _, part := range parts {
		if len(part) >= 4 && strings.Contains(part, "W-") {
			return part
		}
		if len(part) >= 3 && strings.Contains(part, "W") && !strings.HasPrefix(strings.ToLower(part), "w") {
			return part
		}
	}
	return ""
}

// unique returns unique strings from a slice
func unique(strs []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range strs {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// parseFluidType converts Motul component type to Portuguese fluid type
func (a *MotulAdapter) parseFluidType(componentType string) string {
	typeMap := map[string]string{
		"ENGINE_OIL":       "Óleo do Motor",
		"TRANSMISSION_OIL": "Óleo de Transmissão",
		"BRAKE_FLUID":      "Fluido de Freio",
		"COOLANT":          "Líquido de Arrefecimento",
		"POWER_STEERING":   "Direção Hidráulica",
		"DIFFERENTIAL":     "Diferencial",
	}

	if pt, ok := typeMap[componentType]; ok {
		return pt
	}
	return componentType
}
