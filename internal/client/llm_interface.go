package client

import "context"

// LLMClient defines the interface for LLM-based vehicle matching
// Both GroqClient and OllamaClient implement this interface
type LLMClient interface {
	// NormalizeVehicle finds the best match from options for a vehicle
	NormalizeVehicle(ctx context.Context, vehicle string, options []string) (string, error)

	// FindBestBrand finds the best matching brand from available options
	FindBestBrand(ctx context.Context, brand string, options []string) (string, error)

	// FindBestModel finds the best matching model from available options
	FindBestModel(ctx context.Context, model string, options []string) (string, error)
}

// Ensure both clients implement LLMClient
var _ LLMClient = (*GroqClient)(nil)
var _ LLMClient = (*OllamaClient)(nil)
