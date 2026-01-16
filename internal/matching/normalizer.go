package matching

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var (
	// Regex to extract numeric values
	numberRegex = regexp.MustCompile(`\d+[,.]?\d*`)

	// Common generation suffixes to remove
	generationSuffixes = []string{"G1", "G2", "G3", "G4", "G5", "G6", "G7", "G8"}
)

// Normalize normalizes a string for comparison
func Normalize(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Remove accents
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	s, _, _ = transform.String(t, s)

	// Remove extra whitespace
	s = strings.Join(strings.Fields(s), " ")

	// Trim
	s = strings.TrimSpace(s)

	return s
}

// RemoveGenerationSuffix removes generation markers like "G7"
func RemoveGenerationSuffix(model string) string {
	normalized := Normalize(model)

	for _, suffix := range generationSuffixes {
		suffixLower := strings.ToLower(suffix)
		if strings.HasSuffix(normalized, " "+suffixLower) {
			normalized = strings.TrimSuffix(normalized, " "+suffixLower)
		}
		if strings.HasSuffix(normalized, suffixLower) {
			normalized = strings.TrimSuffix(normalized, suffixLower)
		}
	}

	return normalized
}

// ExtractNumbers extracts all numbers from a string
func ExtractNumbers(s string) []string {
	return numberRegex.FindAllString(s, -1)
}

// NormalizeNumber normalizes number format (3,5 → 3.5)
func NormalizeNumber(s string) string {
	return strings.ReplaceAll(s, ",", ".")
}
