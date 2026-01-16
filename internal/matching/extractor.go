package matching

import (
	"regexp"
	"strconv"
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
