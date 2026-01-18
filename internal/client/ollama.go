package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	defaultOllamaModel = "llama3.1:8b"
)

// OllamaClient handles communication with local Ollama API for LLM normalization
type OllamaClient struct {
	httpClient *http.Client
	baseURL    string
	model      string
	logger     *slog.Logger
}

// OllamaChatRequest represents an Ollama chat API request
type OllamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []OllamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  OllamaOptions   `json:"options,omitempty"`
}

// OllamaMessage represents a chat message
type OllamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OllamaOptions represents generation options
type OllamaOptions struct {
	Temperature float64 `json:"temperature"`
	NumPredict  int     `json:"num_predict"`
}

// OllamaChatResponse represents an Ollama chat API response
type OllamaChatResponse struct {
	Model     string        `json:"model"`
	CreatedAt string        `json:"created_at"`
	Message   OllamaMessage `json:"message"`
	Done      bool          `json:"done"`
	Error     string        `json:"error,omitempty"`

	// Timing info
	TotalDuration      int64 `json:"total_duration"`
	LoadDuration       int64 `json:"load_duration"`
	PromptEvalCount    int   `json:"prompt_eval_count"`
	PromptEvalDuration int64 `json:"prompt_eval_duration"`
	EvalCount          int   `json:"eval_count"`
	EvalDuration       int64 `json:"eval_duration"`
}

// NewOllamaClient creates a new Ollama API client
func NewOllamaClient(baseURL string, model string, logger *slog.Logger) *OllamaClient {
	if model == "" {
		model = defaultOllamaModel
	}

	// Ensure baseURL doesn't have trailing slash
	baseURL = strings.TrimRight(baseURL, "/")

	client := &OllamaClient{
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Longer timeout for local inference
		},
		baseURL: baseURL,
		model:   model,
		logger:  logger,
	}

	logger.Info("Ollama client initialized",
		"base_url", baseURL,
		"model", model,
	)

	return client
}

// systemPrompt is the robust system prompt for vehicle matching
const systemPrompt = `Reply with ONLY a number (1-9). Match vehicle to best option based on:
- Engine type: TURBO/TSI/T200/THP must match turbo options, naturally aspirated must match non-turbo
- Engine size: 1.0, 1.4, 2.0 etc should match closely
- Power (cv/hp): match as closely as possible
- Fuel: Flex/Diesel/Gasoline should match when possible
If no good match, reply 0.`

// NormalizeVehicle uses LLM to find the best match from Motul options
func (c *OllamaClient) NormalizeVehicle(ctx context.Context, wegaVehicle string, motulOptions []string) (string, error) {
	if len(motulOptions) == 0 {
		return "", fmt.Errorf("no Motul options provided")
	}

	// If only one option, return it directly (no LLM needed)
	if len(motulOptions) == 1 {
		return motulOptions[0], nil
	}

	// Build numbered options list
	var optionsList strings.Builder
	for i, opt := range motulOptions {
		optionsList.WriteString(fmt.Sprintf("%d. %s\n", i+1, opt))
	}

	// Build user prompt
	userPrompt := fmt.Sprintf("Vehicle: %s\n%s", wegaVehicle, optionsList.String())

	// Make request
	response, err := c.doRequest(ctx, systemPrompt, userPrompt)
	if err != nil {
		return "", err
	}

	// Parse the response number
	response = strings.TrimSpace(response)

	// Try to extract first digit from response
	var optionNum int
	for _, char := range response {
		if char >= '0' && char <= '9' {
			optionNum = int(char - '0')
			break
		}
	}

	if optionNum == 0 {
		// LLM indicated no match or failed to parse - use smart fallback
		c.logger.Warn("LLM response not a valid number, using smart fallback",
			"response", response,
			"wega_vehicle", wegaVehicle,
		)
		return c.smartFallback(wegaVehicle, motulOptions), nil
	}

	// Validate option number
	if optionNum > len(motulOptions) {
		c.logger.Warn("invalid option number from LLM, using smart fallback",
			"option_num", optionNum,
			"total_options", len(motulOptions),
		)
		return c.smartFallback(wegaVehicle, motulOptions), nil
	}

	return motulOptions[optionNum-1], nil
}

// smartFallback selects the best option based on turbo/aspirated engine detection
func (c *OllamaClient) smartFallback(wegaVehicle string, motulOptions []string) string {
	wegaLower := strings.ToLower(wegaVehicle)

	// Check if Wega vehicle is turbo
	turboKeywords := []string{"turbo", "tsi", "tfsi", "t200", "thp", "130cv", "130 cv", "125cv", "125 cv", "116cv", "116 cv"}
	wegaIsTurbo := false
	for _, kw := range turboKeywords {
		if strings.Contains(wegaLower, kw) {
			wegaIsTurbo = true
			break
		}
	}

	// Check for diesel
	dieselKeywords := []string{"diesel", "tdi", "cdti", "hdi", "dci", "jtd", "d4d"}
	wegaIsDiesel := false
	for _, kw := range dieselKeywords {
		if strings.Contains(wegaLower, kw) {
			wegaIsDiesel = true
			break
		}
	}

	// Find matching option based on engine characteristics
	for _, opt := range motulOptions {
		optLower := strings.ToLower(opt)

		// Check diesel match
		optIsDiesel := false
		for _, kw := range dieselKeywords {
			if strings.Contains(optLower, kw) {
				optIsDiesel = true
				break
			}
		}
		if wegaIsDiesel != optIsDiesel {
			continue // Skip if diesel status doesn't match
		}

		// Check turbo match
		optIsTurbo := false
		for _, kw := range turboKeywords {
			if strings.Contains(optLower, kw) {
				optIsTurbo = true
				break
			}
		}

		// Match turbo with turbo, non-turbo with non-turbo
		if wegaIsTurbo == optIsTurbo {
			c.logger.Info("smart fallback matched by engine type",
				"wega", wegaVehicle,
				"matched", opt,
				"is_turbo", wegaIsTurbo,
				"is_diesel", wegaIsDiesel,
			)
			return opt
		}
	}

	// If no match by engine type, return first option
	c.logger.Warn("smart fallback: no engine type match, using first option",
		"wega", wegaVehicle,
	)
	return motulOptions[0]
}

// doRequest makes a chat request to Ollama
func (c *OllamaClient) doRequest(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	req := OllamaChatRequest{
		Model: c.model,
		Messages: []OllamaMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream: false,
		Options: OllamaOptions{
			Temperature: 0.0, // Deterministic output
			NumPredict:  3,   // Very short response (just a number)
		},
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.baseURL + "/api/chat"

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama API error (status %d): %s", resp.StatusCode, string(body))
	}

	var ollamaResp OllamaChatResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if ollamaResp.Error != "" {
		return "", fmt.Errorf("Ollama API error: %s", ollamaResp.Error)
	}

	latency := time.Since(startTime)
	c.logger.Debug("Ollama request completed",
		"latency_ms", latency.Milliseconds(),
		"prompt_tokens", ollamaResp.PromptEvalCount,
		"eval_tokens", ollamaResp.EvalCount,
	)

	return ollamaResp.Message.Content, nil
}

// FindBestBrand finds the best matching brand from available options
func (c *OllamaClient) FindBestBrand(ctx context.Context, wegaBrand string, motulBrands []string) (string, error) {
	if len(motulBrands) == 0 {
		return "", fmt.Errorf("no Motul brands provided")
	}

	// Try exact match first (case-insensitive)
	for _, brand := range motulBrands {
		if strings.EqualFold(brand, wegaBrand) {
			return brand, nil
		}
	}

	// Use LLM for fuzzy matching
	return c.NormalizeVehicle(ctx, wegaBrand, motulBrands)
}

// FindBestModel finds the best matching model from available options
func (c *OllamaClient) FindBestModel(ctx context.Context, wegaModel string, motulModels []string) (string, error) {
	if len(motulModels) == 0 {
		return "", fmt.Errorf("no Motul models provided")
	}

	// Try exact match first
	for _, model := range motulModels {
		if strings.EqualFold(model, wegaModel) {
			return model, nil
		}
	}

	// Use LLM for fuzzy matching
	return c.NormalizeVehicle(ctx, wegaModel, motulModels)
}

// Ping checks if the Ollama server is reachable and the model is loaded
func (c *OllamaClient) Ping(ctx context.Context) error {
	url := c.baseURL + "/api/tags"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Ollama returned status %d", resp.StatusCode)
	}

	// Check if model is available
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), c.model) {
		c.logger.Warn("model may not be loaded",
			"model", c.model,
			"available_models", string(body),
		)
	}

	return nil
}
