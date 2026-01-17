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

	components := resp.Vehicle.Components
	if len(components) == 0 {
		return nil, fmt.Errorf("no components in response")
	}

	specs := []OilSpec{}

	// Convert to interface{} slice for legacy parsing logic
	var componentsIface []interface{}
	for _, c := range components {
		componentsIface = append(componentsIface, c)
	}

	// Find motor (engine oil) specifications
	motorSpecs := p.findMotorSpecs(componentsIface)
	specs = append(specs, motorSpecs...)

	// Find transmission oil specifications
	transmissionSpecs := p.findTransmissionSpecs(componentsIface)
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
