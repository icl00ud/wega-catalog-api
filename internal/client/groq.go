package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	groqAPIBase = "https://api.groq.com/openai/v1/chat/completions"
	groqModel   = "llama-3.1-8b-instant" // Free tier model with 6K TPM
)

// ErrAllKeysExhaustedDaily is returned when all API keys have hit their daily limit
var ErrAllKeysExhaustedDaily = fmt.Errorf("all API keys exhausted for the day")

// GroqClient handles communication with Groq API for LLM normalization
// Supports multiple API keys with automatic failover on rate limit (429)
// and daily limit exhaustion with automatic reset at midnight UTC
type GroqClient struct {
	httpClient  *http.Client
	apiKeys     []string
	currentKey  atomic.Int32
	keyMutex    sync.RWMutex
	keyStatus   []keyStatus // Track status of each key
	rateLimiter *RateLimiter
	logger      *slog.Logger

	// Daily limit tracking
	allExhaustedUntil time.Time // When all keys are exhausted, wait until this time
}

// keyStatus tracks the health of an API key
type keyStatus struct {
	// Per-minute rate limiting (resets after 1 minute)
	rateLimited   bool
	rateLimitedAt time.Time

	// Daily limit exhaustion (resets at midnight UTC)
	dailyExhausted   bool
	dailyExhaustedAt time.Time

	errorCount int
}

// GroqRequest represents a chat completion request
type GroqRequest struct {
	Model       string        `json:"model"`
	Messages    []GroqMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

// GroqMessage represents a chat message
type GroqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GroqResponse represents a chat completion response
type GroqResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code,omitempty"`
	} `json:"error,omitempty"`
}

// BatchMatchRequest represents a single vehicle to match in a batch
type BatchMatchRequest struct {
	ID      int      // Internal ID for tracking
	Vehicle string   // Vehicle description
	Options []string // Available options to match against
}

// BatchMatchResult represents the result of a batch match
type BatchMatchResult struct {
	ID           int
	MatchedIndex int    // 0-based index of matched option, -1 if no match
	MatchedValue string // The matched option value
	Error        error
}

// NewGroqClient creates a new Groq API client with a single key
func NewGroqClient(apiKey string, requestsPerMinute float64, logger *slog.Logger) *GroqClient {
	return NewGroqClientMultiKey([]string{apiKey}, requestsPerMinute, logger)
}

// NewGroqClientMultiKey creates a new Groq API client with multiple keys for failover
func NewGroqClientMultiKey(apiKeys []string, requestsPerMinute float64, logger *slog.Logger) *GroqClient {
	if len(apiKeys) == 0 {
		panic("at least one API key is required")
	}

	client := &GroqClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiKeys:     apiKeys,
		keyStatus:   make([]keyStatus, len(apiKeys)),
		rateLimiter: NewRateLimiter(requestsPerMinute / 60.0), // Convert to per-second
		logger:      logger,
	}

	// Start background goroutine to reset keys at midnight UTC
	go client.midnightResetLoop()

	logger.Info("Groq client initialized",
		"keys_count", len(apiKeys),
		"rpm", requestsPerMinute,
	)

	return client
}

// midnightResetLoop resets all daily-exhausted keys at midnight UTC
func (c *GroqClient) midnightResetLoop() {
	for {
		now := time.Now().UTC()
		// Calculate time until next midnight UTC
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		sleepDuration := nextMidnight.Sub(now)

		c.logger.Debug("midnight reset scheduled",
			"next_reset", nextMidnight,
			"sleep_duration", sleepDuration,
		)

		time.Sleep(sleepDuration)

		// Reset all keys
		c.resetAllDailyLimits()
	}
}

// resetAllDailyLimits resets daily exhaustion status for all keys
func (c *GroqClient) resetAllDailyLimits() {
	c.keyMutex.Lock()
	defer c.keyMutex.Unlock()

	resetCount := 0
	for i := range c.keyStatus {
		if c.keyStatus[i].dailyExhausted {
			c.keyStatus[i].dailyExhausted = false
			c.keyStatus[i].dailyExhaustedAt = time.Time{}
			resetCount++
		}
		// Also reset per-minute rate limits
		c.keyStatus[i].rateLimited = false
		c.keyStatus[i].rateLimitedAt = time.Time{}
		c.keyStatus[i].errorCount = 0
	}

	// Reset the global exhaustion flag
	c.allExhaustedUntil = time.Time{}

	if resetCount > 0 {
		c.logger.Info("midnight reset: all API keys restored",
			"keys_reset", resetCount,
			"total_keys", len(c.apiKeys),
		)
	}
}

// GetKeyCount returns the number of API keys configured
func (c *GroqClient) GetKeyCount() int {
	return len(c.apiKeys)
}

// GetKeyStatus returns status information about all keys
func (c *GroqClient) GetKeyStatus() map[string]interface{} {
	c.keyMutex.RLock()
	defer c.keyMutex.RUnlock()

	activeKeys := 0
	rateLimitedKeys := 0
	dailyExhaustedKeys := 0

	for _, status := range c.keyStatus {
		if status.dailyExhausted {
			dailyExhaustedKeys++
		} else if status.rateLimited {
			rateLimitedKeys++
		} else {
			activeKeys++
		}
	}

	result := map[string]interface{}{
		"total_keys":           len(c.apiKeys),
		"active_keys":          activeKeys,
		"rate_limited_keys":    rateLimitedKeys,
		"daily_exhausted_keys": dailyExhaustedKeys,
	}

	if !c.allExhaustedUntil.IsZero() {
		result["all_exhausted_until"] = c.allExhaustedUntil
		result["wait_duration"] = time.Until(c.allExhaustedUntil).String()
	}

	return result
}

// getCurrentKey returns the current API key to use
func (c *GroqClient) getCurrentKey() (string, int) {
	idx := int(c.currentKey.Load()) % len(c.apiKeys)
	return c.apiKeys[idx], idx
}

// isDailyLimitError checks if the error response indicates daily limit exhaustion
// Groq returns specific error messages for daily vs per-minute limits
func (c *GroqClient) isDailyLimitError(statusCode int, body []byte) bool {
	if statusCode != http.StatusTooManyRequests {
		return false
	}

	// Check for daily limit indicators in the response
	bodyStr := strings.ToLower(string(body))

	// Daily limit error patterns from Groq API
	dailyPatterns := []string{
		"tokens per day",
		"requests per day",
		"daily",
		"quota",
	}

	for _, pattern := range dailyPatterns {
		if strings.Contains(bodyStr, pattern) {
			return true
		}
	}

	return false
}

// rotateKey switches to the next available API key
// Returns true if a non-exhausted key was found
func (c *GroqClient) rotateKey(failedIdx int, isDailyLimit bool) bool {
	c.keyMutex.Lock()
	defer c.keyMutex.Unlock()

	now := time.Now()

	if isDailyLimit {
		// Mark as daily exhausted (won't reset until midnight)
		c.keyStatus[failedIdx].dailyExhausted = true
		c.keyStatus[failedIdx].dailyExhaustedAt = now
		c.logger.Warn("API key daily limit exhausted",
			"key_idx", failedIdx,
		)
	} else {
		// Mark as temporarily rate limited (resets after 1 minute)
		c.keyStatus[failedIdx].rateLimited = true
		c.keyStatus[failedIdx].rateLimitedAt = now
	}

	// Find next available key
	startIdx := (failedIdx + 1) % len(c.apiKeys)
	for i := 0; i < len(c.apiKeys); i++ {
		idx := (startIdx + i) % len(c.apiKeys)
		status := &c.keyStatus[idx]

		// Skip daily-exhausted keys (they won't recover until midnight)
		if status.dailyExhausted {
			continue
		}

		// Check if per-minute rate limit has expired (1 minute cooldown)
		if status.rateLimited && time.Since(status.rateLimitedAt) > time.Minute {
			status.rateLimited = false
			status.errorCount = 0
		}

		if !status.rateLimited {
			c.currentKey.Store(int32(idx))
			c.logger.Info("rotated to new API key",
				"from_idx", failedIdx,
				"to_idx", idx,
				"total_keys", len(c.apiKeys),
				"daily_limit", isDailyLimit,
			)
			return true
		}
	}

	// Check if all keys are daily-exhausted
	allDailyExhausted := true
	for _, status := range c.keyStatus {
		if !status.dailyExhausted {
			allDailyExhausted = false
			break
		}
	}

	if allDailyExhausted {
		// Calculate next midnight UTC
		nowUTC := time.Now().UTC()
		nextMidnight := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day()+1, 0, 0, 0, 0, time.UTC)
		c.allExhaustedUntil = nextMidnight

		c.logger.Warn("all API keys daily limit exhausted, waiting until midnight UTC",
			"total_keys", len(c.apiKeys),
			"resume_at", nextMidnight,
			"wait_duration", time.Until(nextMidnight),
		)
	} else {
		c.logger.Warn("all API keys temporarily rate limited",
			"total_keys", len(c.apiKeys),
		)
	}

	return false
}

// markKeySuccess marks a key as successful (resets error count)
func (c *GroqClient) markKeySuccess(idx int) {
	c.keyMutex.Lock()
	defer c.keyMutex.Unlock()
	c.keyStatus[idx].errorCount = 0
	c.keyStatus[idx].rateLimited = false
	// Note: don't reset dailyExhausted here, it only resets at midnight
}

// waitUntilMidnight blocks until midnight UTC when all keys are exhausted
// Returns nil when ready to resume, or context error if cancelled
func (c *GroqClient) waitUntilMidnight(ctx context.Context) error {
	c.keyMutex.RLock()
	exhaustedUntil := c.allExhaustedUntil
	c.keyMutex.RUnlock()

	if exhaustedUntil.IsZero() || time.Now().After(exhaustedUntil) {
		return nil
	}

	waitDuration := time.Until(exhaustedUntil)
	c.logger.Info("waiting until midnight for API key reset",
		"resume_at", exhaustedUntil,
		"wait_duration", waitDuration,
	)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(waitDuration):
		c.logger.Info("midnight reached, resuming with fresh API keys")
		return nil
	}
}

// NormalizeVehicle uses LLM to find the best match from Motul options
// Uses optimized minimal prompt to save tokens (~60% reduction)
func (c *GroqClient) NormalizeVehicle(ctx context.Context, wegaVehicle string, motulOptions []string) (string, error) {
	if len(motulOptions) == 0 {
		return "", fmt.Errorf("no Motul options provided")
	}

	// If only one option, return it directly (no LLM needed)
	if len(motulOptions) == 1 {
		return motulOptions[0], nil
	}

	// Build compact options list
	optionsList := ""
	for i, opt := range motulOptions {
		optionsList += fmt.Sprintf("%d.%s ", i+1, opt)
	}

	// CRITICAL: Prompt must force LLM to output ONLY a number
	// The previous prompt was too complex and LLM responded with explanations
	// This version uses a simple Q&A format that works better with Llama 3.1
	prompt := fmt.Sprintf(`Q: Which option best matches "%s"?
IMPORTANT: If vehicle has NO turbo keywords (Turbo/TSI/T200/THP/130cv), choose NON-turbo option.
%s
A:`, wegaVehicle, strings.TrimSpace(optionsList))

	// Rate limit
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limit wait failed: %w", err)
	}

	// Make request with automatic failover
	response, err := c.doRequestWithFailover(ctx, prompt)
	if err != nil {
		return "", err
	}

	// Parse the response number
	response = strings.TrimSpace(response)

	// Try to extract first digit from response
	var optionNum int
	for _, char := range response {
		if char >= '1' && char <= '9' {
			optionNum = int(char - '0')
			break
		}
	}

	if optionNum == 0 {
		// LLM didn't return a valid number - use smart fallback based on engine type
		c.logger.Warn("LLM response not a number, using smart fallback",
			"response", response,
			"wega_vehicle", wegaVehicle,
		)
		return c.smartFallback(wegaVehicle, motulOptions), nil
	}

	// Validate option number
	if optionNum <= 0 || optionNum > len(motulOptions) {
		if optionNum == 0 {
			return "", fmt.Errorf("LLM indicated no match")
		}
		c.logger.Warn("invalid option number from LLM, using smart fallback",
			"option_num", optionNum,
			"total_options", len(motulOptions),
		)
		return c.smartFallback(wegaVehicle, motulOptions), nil
	}

	return motulOptions[optionNum-1], nil
}

// smartFallback selects the best option based on turbo/aspirated engine detection
// This is used when the LLM fails to return a valid number
func (c *GroqClient) smartFallback(wegaVehicle string, motulOptions []string) string {
	wegaLower := strings.ToLower(wegaVehicle)

	// Check if Wega vehicle is turbo
	turboKeywords := []string{"turbo", "tsi", "tfsi", "t200", "thp", "130cv", "130 cv", "125cv", "125 cv"}
	wegaIsTurbo := false
	for _, kw := range turboKeywords {
		if strings.Contains(wegaLower, kw) {
			wegaIsTurbo = true
			break
		}
	}

	// Find matching option based on turbo status
	for _, opt := range motulOptions {
		optLower := strings.ToLower(opt)
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

// NormalizeVehicleBatch processes multiple vehicles in a single LLM call
// Returns a map of vehicle index to matched option index (-1 if no match)
// This saves ~40% tokens by reducing prompt overhead
func (c *GroqClient) NormalizeVehicleBatch(ctx context.Context, requests []BatchMatchRequest) ([]BatchMatchResult, error) {
	if len(requests) == 0 {
		return nil, fmt.Errorf("no requests provided")
	}

	// For single request, use regular method
	if len(requests) == 1 {
		req := requests[0]
		result, err := c.NormalizeVehicle(ctx, req.Vehicle, req.Options)
		if err != nil {
			return []BatchMatchResult{{ID: req.ID, MatchedIndex: -1, Error: err}}, nil
		}
		// Find index of matched result
		for i, opt := range req.Options {
			if opt == result {
				return []BatchMatchResult{{ID: req.ID, MatchedIndex: i, MatchedValue: result}}, nil
			}
		}
		return []BatchMatchResult{{ID: req.ID, MatchedIndex: 0, MatchedValue: req.Options[0]}}, nil
	}

	// Build batch prompt
	var sb strings.Builder
	sb.WriteString("Match each vehicle to its best option. Reply with comma-separated numbers.\n")

	for i, req := range requests {
		optsList := ""
		for j, opt := range req.Options {
			optsList += fmt.Sprintf("%d.%s ", j+1, opt)
		}
		sb.WriteString(fmt.Sprintf("V%d:%s|Opts:%s\n", i+1, req.Vehicle, strings.TrimSpace(optsList)))
	}

	sb.WriteString(fmt.Sprintf("Reply format: n1,n2,n3... (numbers 1-%d for each, 0=no match)",
		maxOptions(requests)))

	// Rate limit
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait failed: %w", err)
	}

	// Make request
	response, err := c.doRequestWithFailover(ctx, sb.String())
	if err != nil {
		// Return errors for all requests
		results := make([]BatchMatchResult, len(requests))
		for i, req := range requests {
			results[i] = BatchMatchResult{ID: req.ID, MatchedIndex: -1, Error: err}
		}
		return results, nil
	}

	// Parse response (format: "1,2,0,3,1")
	return c.parseBatchResponse(response, requests), nil
}

// parseBatchResponse parses the comma-separated batch response
func (c *GroqClient) parseBatchResponse(response string, requests []BatchMatchRequest) []BatchMatchResult {
	results := make([]BatchMatchResult, len(requests))

	// Initialize with defaults
	for i, req := range requests {
		results[i] = BatchMatchResult{
			ID:           req.ID,
			MatchedIndex: 0, // Default to first option
			MatchedValue: req.Options[0],
		}
	}

	// Clean response and extract numbers
	response = strings.TrimSpace(response)
	// Extract all numbers from response
	re := regexp.MustCompile(`\d+`)
	numbers := re.FindAllString(response, -1)

	for i, numStr := range numbers {
		if i >= len(requests) {
			break
		}

		num, err := strconv.Atoi(numStr)
		if err != nil {
			continue
		}

		req := requests[i]
		if num == 0 {
			results[i].MatchedIndex = -1
			results[i].MatchedValue = ""
			results[i].Error = fmt.Errorf("LLM indicated no match")
		} else if num > 0 && num <= len(req.Options) {
			results[i].MatchedIndex = num - 1
			results[i].MatchedValue = req.Options[num-1]
		}
		// Invalid numbers keep the default (first option)
	}

	return results
}

// maxOptions returns the maximum number of options across all requests
func maxOptions(requests []BatchMatchRequest) int {
	max := 0
	for _, req := range requests {
		if len(req.Options) > max {
			max = len(req.Options)
		}
	}
	return max
}

// doRequestWithFailover makes a request with automatic key rotation on 429
// If all keys are daily-exhausted, waits until midnight UTC and retries
func (c *GroqClient) doRequestWithFailover(ctx context.Context, prompt string) (string, error) {
	req := GroqRequest{
		Model: groqModel,
		Messages: []GroqMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.0, // Zero temperature for deterministic output
		MaxTokens:   5,   // Force short response (just a number)
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	c.logger.Info("starting Groq API request")

	// Outer loop: handles midnight wait and retry
	for {
		// Check if we need to wait for midnight
		if err := c.waitUntilMidnight(ctx); err != nil {
			return "", err
		}

		// Inner loop: try each key
		triedKeys := 0
		for triedKeys < len(c.apiKeys) {
			// Check context before each request
			if ctx.Err() != nil {
				return "", ctx.Err()
			}

			apiKey, keyIdx := c.getCurrentKey()

			// Skip if this key is daily-exhausted
			c.keyMutex.RLock()
			isDailyExhausted := c.keyStatus[keyIdx].dailyExhausted
			c.keyMutex.RUnlock()

			if isDailyExhausted {
				c.logger.Info("skipping daily-exhausted key",
					"key_idx", keyIdx,
					"tried_keys", triedKeys,
				)
				triedKeys++
				c.currentKey.Store(int32((keyIdx + 1) % len(c.apiKeys)))
				continue
			}

			c.logger.Info("attempting Groq API call",
				"key_idx", keyIdx,
				"tried_keys", triedKeys,
			)

			httpReq, err := http.NewRequestWithContext(ctx, "POST", groqAPIBase, bytes.NewReader(reqBody))
			if err != nil {
				return "", fmt.Errorf("failed to create request: %w", err)
			}

			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)

			resp, err := c.httpClient.Do(httpReq)
			if err != nil {
				c.logger.Error("HTTP request failed", "error", err)
				return "", fmt.Errorf("failed to send request: %w", err)
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return "", fmt.Errorf("failed to read response: %w", err)
			}

			// Check for rate limit (429)
			if resp.StatusCode == http.StatusTooManyRequests {
				isDailyLimit := c.isDailyLimitError(resp.StatusCode, body)

				c.logger.Warn("rate limit hit, rotating key",
					"key_idx", keyIdx,
					"status", resp.StatusCode,
					"is_daily_limit", isDailyLimit,
					"response_body", string(body),
				)

				if c.rotateKey(keyIdx, isDailyLimit) {
					triedKeys++
					continue // Try with next key
				}

				// All keys exhausted - check if daily or temporary
				c.keyMutex.RLock()
				allExhaustedUntil := c.allExhaustedUntil
				c.keyMutex.RUnlock()

				if !allExhaustedUntil.IsZero() {
					c.logger.Info("all keys daily-exhausted, will wait for midnight",
						"resume_at", allExhaustedUntil,
					)
					// All keys daily-exhausted, break inner loop to wait for midnight
					break
				}

				// All keys temporarily rate-limited, return error
				return "", fmt.Errorf("all API keys rate limited: %s", string(body))
			}

			if resp.StatusCode != http.StatusOK {
				c.logger.Error("Groq API returned non-200 status",
					"status", resp.StatusCode,
					"body", string(body),
				)
				return "", fmt.Errorf("Groq API error (status %d): %s", resp.StatusCode, string(body))
			}

			var groqResp GroqResponse
			if err := json.Unmarshal(body, &groqResp); err != nil {
				return "", fmt.Errorf("failed to parse response: %w", err)
			}

			if groqResp.Error != nil {
				// Check if error indicates daily limit
				if strings.Contains(strings.ToLower(groqResp.Error.Message), "daily") ||
					strings.Contains(strings.ToLower(groqResp.Error.Message), "quota") {
					c.rotateKey(keyIdx, true)
					triedKeys++
					continue
				}
				return "", fmt.Errorf("Groq API error: %s", groqResp.Error.Message)
			}

			if len(groqResp.Choices) == 0 {
				return "", fmt.Errorf("no choices in response")
			}

			// Success! Mark key as healthy
			c.markKeySuccess(keyIdx)

			c.logger.Info("Groq API request successful",
				"key_idx", keyIdx,
				"tokens_used", groqResp.Usage.TotalTokens,
			)

			return groqResp.Choices[0].Message.Content, nil
		}

		// All keys tried in inner loop
		c.keyMutex.RLock()
		allExhaustedUntil := c.allExhaustedUntil
		c.keyMutex.RUnlock()

		if allExhaustedUntil.IsZero() {
			// Not waiting for midnight, all keys just temporarily exhausted
			c.logger.Error("all API keys exhausted (temporary)")
			return "", fmt.Errorf("all API keys exhausted")
		}

		// Will wait for midnight in next iteration of outer loop
		c.logger.Info("all keys exhausted, will wait for midnight reset",
			"resume_at", allExhaustedUntil,
		)
	}
}

// FindBestBrand finds the best matching brand from available options
func (c *GroqClient) FindBestBrand(ctx context.Context, wegaBrand string, motulBrands []string) (string, error) {
	if len(motulBrands) == 0 {
		return "", fmt.Errorf("no Motul brands provided")
	}

	// Try exact match first (case-insensitive)
	for _, brand := range motulBrands {
		if normalizeForComparison(brand) == normalizeForComparison(wegaBrand) {
			return brand, nil
		}
	}

	// Use LLM for fuzzy matching
	return c.NormalizeVehicle(ctx, wegaBrand, motulBrands)
}

// FindBestModel finds the best matching model from available options
func (c *GroqClient) FindBestModel(ctx context.Context, wegaModel string, motulModels []string) (string, error) {
	if len(motulModels) == 0 {
		return "", fmt.Errorf("no Motul models provided")
	}

	// Try exact match first
	for _, model := range motulModels {
		if normalizeForComparison(model) == normalizeForComparison(wegaModel) {
			return model, nil
		}
	}

	// Use LLM for fuzzy matching
	return c.NormalizeVehicle(ctx, wegaModel, motulModels)
}

// normalizeForComparison normalizes strings for comparison
func normalizeForComparison(s string) string {
	// Simple normalization - lowercase and remove extra spaces
	return strings.ToLower(strings.TrimSpace(s))
}
