package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	motulAPIBase = "https://gateway-apim.motul.com/oil-advisor"
	motulWebBase = "https://www.motul.com"
	locale       = "pt-BR"
	businessUnit = "Brazil"
)

// Brand represents a vehicle brand
type Brand struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// BrandsResponse wraps brands array
type BrandsResponse struct {
	Brands []Brand `json:"brands"`
}

// Model represents a vehicle model
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ModelsResponse wraps models array
type ModelsResponse struct {
	Models []Model `json:"models"`
}

// VehicleType represents a specific vehicle type/version
type VehicleType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// VehicleTypesResponse wraps types array
type VehicleTypesResponse struct {
	Types []VehicleType `json:"types"`
}

// SpecificationsResponse is the Motul recommendations API response
type SpecificationsResponse struct {
	Vehicle struct {
		CategoryID string      `json:"categoryId"`
		Brand      string      `json:"brand"`
		Type       string      `json:"type"`
		Model      string      `json:"model"`
		StartYear  string      `json:"startYear"`
		EndYear    string      `json:"endYear"`
		Components []Component `json:"components"` // Components are nested inside vehicle
	} `json:"vehicle"`
}

// Component represents a vehicle component with oil recommendations
type Component struct {
	Category struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"category"`
	Capacities []struct {
		Label string `json:"label"`
	} `json:"capacities"`
	Recommendations []struct {
		Conditions struct {
			Usage   string `json:"usage"`
			Mileage string `json:"mileage"`
		} `json:"conditions"`
		Products []struct {
			Name string `json:"name"`
		} `json:"products"`
	} `json:"recommendations"`
}

// MotulClient handles communication with Motul API
type MotulClient struct {
	httpClient  *http.Client
	rateLimiter *RateLimiter
	retryConfig RetryConfig
}

// RetryConfig defines retry behavior
type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
}

// NewMotulClient creates a new Motul API client
func NewMotulClient(rateLimit float64) *MotulClient {
	return &MotulClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		rateLimiter: NewRateLimiter(rateLimit),
		retryConfig: RetryConfig{
			MaxRetries:     5,
			InitialBackoff: 1 * time.Second,
			MaxBackoff:     30 * time.Second,
			Multiplier:     2.0,
		},
	}
}

// fetchWithRetry performs HTTP request with retry logic
func (c *MotulClient) fetchWithRetry(ctx context.Context, url string) ([]byte, error) {
	backoff := c.retryConfig.InitialBackoff

	for attempt := 0; attempt <= c.retryConfig.MaxRetries; attempt++ {
		// Wait for rate limiter
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt < c.retryConfig.MaxRetries {
				time.Sleep(backoff)
				backoff = min(time.Duration(float64(backoff)*c.retryConfig.Multiplier), c.retryConfig.MaxBackoff)
				continue
			}
			return nil, fmt.Errorf("request failed after %d attempts: %w", attempt+1, err)
		}

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		// Success
		if resp.StatusCode == 200 {
			return body, nil
		}

		// Retry on 429, 500, 502, 503
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			if attempt < c.retryConfig.MaxRetries {
				time.Sleep(backoff)
				backoff = min(time.Duration(float64(backoff)*c.retryConfig.Multiplier), c.retryConfig.MaxBackoff)
				continue
			}
		}

		// Non-retryable error
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil, fmt.Errorf("max retries exceeded")
}

// GetBrands fetches all car brands from Motul
func (c *MotulClient) GetBrands(ctx context.Context) ([]Brand, error) {
	url := fmt.Sprintf("%s/vehicle-brands?categoryId=CAR&locale=%s&BU=%s",
		motulAPIBase, locale, businessUnit)

	body, err := c.fetchWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}

	var resp BrandsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse brands response: %w", err)
	}

	return resp.Brands, nil
}

// GetModels fetches models for a brand and year
func (c *MotulClient) GetModels(ctx context.Context, brandID string, year int) ([]Model, error) {
	url := fmt.Sprintf("%s/vehicle-models?vehicleBrandId=%s&year=%d&locale=%s&BU=%s",
		motulAPIBase, brandID, year, locale, businessUnit)

	body, err := c.fetchWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}

	var resp ModelsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse models response: %w", err)
	}

	return resp.Models, nil
}

// GetVehicleTypes fetches specific types/versions for a model
func (c *MotulClient) GetVehicleTypes(ctx context.Context, modelID string) ([]VehicleType, error) {
	url := fmt.Sprintf("%s/vehicle-types?vehicleModelId=%s&locale=%s&BU=%s",
		motulAPIBase, modelID, locale, businessUnit)

	body, err := c.fetchWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}

	var resp VehicleTypesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse types response: %w", err)
	}

	return resp.Types, nil
}

// GetSpecifications fetches oil specifications for a vehicle type
func (c *MotulClient) GetSpecifications(ctx context.Context, vehicleTypeID string) (*SpecificationsResponse, error) {
	url := fmt.Sprintf("%s/recommendations?vehicleTypeId=%s&locale=%s&BU=%s",
		motulAPIBase, vehicleTypeID, locale, businessUnit)

	body, err := c.fetchWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}

	var resp SpecificationsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse specifications response: %w", err)
	}

	return &resp, nil
}

// Close closes the client
func (c *MotulClient) Close() {
	c.rateLimiter.Stop()
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
