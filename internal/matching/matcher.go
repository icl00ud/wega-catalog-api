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
	wegaYear := ParseYear(wegaVehicle.Ano)
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
