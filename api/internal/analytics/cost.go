package analytics

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// pricing stores per-model token costs in USD per 1,000 tokens.
// input = prompt tokens, output = completion tokens.
type pricing struct {
	inputPer1K  float64
	outputPer1K float64
}

var (
	pricingMutex       sync.RWMutex
	activePricingTable = make(map[string]pricing)
)

func init() {
	// Initialize with compile-time defaults.
	for k, v := range pricingTable {
		activePricingTable[k] = v
	}
}

// pricingTable maps lowercase model name prefixes to pricing.
// Updated June 2026. Prices sourced from public provider pricing pages.
//
// When no exact match is found we fall back to prefix matching
// (e.g. "gpt-4o-mini-2024-07-18" matches "gpt-4o-mini").
var pricingTable = map[string]pricing{
	// ── OpenAI ──────────────────────────────────────────────
	"gpt-4o":            {inputPer1K: 0.0025, outputPer1K: 0.01},
	"gpt-4o-mini":       {inputPer1K: 0.00015, outputPer1K: 0.0006},
	"gpt-4-turbo":       {inputPer1K: 0.01, outputPer1K: 0.03},
	"gpt-4":             {inputPer1K: 0.03, outputPer1K: 0.06},
	"gpt-3.5-turbo":     {inputPer1K: 0.0005, outputPer1K: 0.0015},
	"o1":                {inputPer1K: 0.015, outputPer1K: 0.06},
	"o1-mini":           {inputPer1K: 0.003, outputPer1K: 0.012},
	"o3":                {inputPer1K: 0.01, outputPer1K: 0.04},
	"o3-mini":           {inputPer1K: 0.0011, outputPer1K: 0.0044},
	"o4-mini":           {inputPer1K: 0.0011, outputPer1K: 0.0044},

	// ── Anthropic ──────────────────────────────────────────
	"claude-sonnet-4":   {inputPer1K: 0.003, outputPer1K: 0.015},
	"claude-3.5-sonnet": {inputPer1K: 0.003, outputPer1K: 0.015},
	"claude-3-sonnet":   {inputPer1K: 0.003, outputPer1K: 0.015},
	"claude-3-opus":     {inputPer1K: 0.015, outputPer1K: 0.075},
	"claude-3-haiku":    {inputPer1K: 0.00025, outputPer1K: 0.00125},
	"claude-3.5-haiku":  {inputPer1K: 0.0008, outputPer1K: 0.004},

	// ── Google ─────────────────────────────────────────────
	"gemini-2.5-pro":    {inputPer1K: 0.00125, outputPer1K: 0.01},
	"gemini-2.5-flash":  {inputPer1K: 0.000075, outputPer1K: 0.0003},
	"gemini-2.0-flash":  {inputPer1K: 0.0001, outputPer1K: 0.0004},
	"gemini-1.5-pro":    {inputPer1K: 0.00125, outputPer1K: 0.005},
	"gemini-1.5-flash":  {inputPer1K: 0.000075, outputPer1K: 0.0003},

	// ── Mistral ────────────────────────────────────────────
	"mistral-large":     {inputPer1K: 0.002, outputPer1K: 0.006},
	"mistral-medium":    {inputPer1K: 0.0027, outputPer1K: 0.0081},
	"mistral-small":     {inputPer1K: 0.001, outputPer1K: 0.003},
	"codestral":         {inputPer1K: 0.001, outputPer1K: 0.003},

	// ── Cohere ─────────────────────────────────────────────
	"command-r-plus":    {inputPer1K: 0.003, outputPer1K: 0.015},
	"command-r":         {inputPer1K: 0.0005, outputPer1K: 0.0015},

	// ── Meta (via providers) ───────────────────────────────
	"llama-3.1-405b":    {inputPer1K: 0.003, outputPer1K: 0.003},
	"llama-3.1-70b":     {inputPer1K: 0.00079, outputPer1K: 0.00079},
	"llama-3.1-8b":      {inputPer1K: 0.00018, outputPer1K: 0.00018},
	"llama-3-70b":       {inputPer1K: 0.00079, outputPer1K: 0.00079},
	"llama-3-8b":        {inputPer1K: 0.00018, outputPer1K: 0.00018},

	// ── DeepSeek ───────────────────────────────────────────
	"deepseek-v3":       {inputPer1K: 0.00014, outputPer1K: 0.00028},
	"deepseek-r1":       {inputPer1K: 0.00055, outputPer1K: 0.00219},
	"deepseek-chat":     {inputPer1K: 0.00014, outputPer1K: 0.00028},
	"deepseek-coder":    {inputPer1K: 0.00014, outputPer1K: 0.00028},

	// ── xAI ────────────────────────────────────────────────
	"grok-3":            {inputPer1K: 0.003, outputPer1K: 0.015},
	"grok-3-mini":       {inputPer1K: 0.0003, outputPer1K: 0.0005},
}

// UpdatePricingFromRemote fetches the latest pricing database from LiteLLM's public repository.
// It parses input_cost_per_token and output_cost_per_token, and updates the active pricing map.
func UpdatePricingFromRemote(ctx context.Context) error {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json", nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var remoteData map[string]struct {
		InputCostPerToken  float64 `json:"input_cost_per_token"`
		OutputCostPerToken float64 `json:"output_cost_per_token"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&remoteData); err != nil {
		return err
	}

	pricingMutex.Lock()
	defer pricingMutex.Unlock()

	for model, info := range remoteData {
		if info.InputCostPerToken > 0 || info.OutputCostPerToken > 0 {
			activePricingTable[strings.ToLower(model)] = pricing{
				inputPer1K:  info.InputCostPerToken * 1000.0,
				outputPer1K: info.OutputCostPerToken * 1000.0,
			}
		}
	}
	return nil
}

// StartPricingSync starts a background worker that fetches pricing updates from the remote registry.
func StartPricingSync(ctx context.Context, interval time.Duration) {
	// Attempt initial fetch on start
	_ = UpdatePricingFromRemote(ctx)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = UpdatePricingFromRemote(ctx)
			}
		}
	}()
}

// EstimateCost estimates the cost in USD for a model invocation.
// It uses the active pricing table with prefix matching.
// Returns 0 if the model is not found.
func EstimateCost(model, provider string, promptTokens, completionTokens int32) float64 {
	p, ok := lookupPricing(model)
	if !ok {
		return 0
	}
	return (float64(promptTokens) / 1000.0 * p.inputPer1K) +
		(float64(completionTokens) / 1000.0 * p.outputPer1K)
}

// lookupPricing finds pricing by exact match first, then by longest prefix match.
func lookupPricing(model string) (pricing, bool) {
	lower := strings.ToLower(strings.TrimSpace(model))
	if lower == "" {
		return pricing{}, false
	}

	pricingMutex.RLock()
	defer pricingMutex.RUnlock()

	// Exact match first.
	if p, ok := activePricingTable[lower]; ok {
		return p, true
	}

	// Prefix match - find the longest matching prefix.
	var bestKey string
	var bestPrice pricing
	for key, p := range activePricingTable {
		if strings.HasPrefix(lower, key) && len(key) > len(bestKey) {
			bestKey = key
			bestPrice = p
		}
	}
	if bestKey != "" {
		return bestPrice, true
	}

	return pricing{}, false
}

// EstimateCostBatch estimates cost for a batch of model invocations.
// Each element is (model, provider, promptTokens, completionTokens).
func EstimateCostBatch(calls []struct {
	Model            string
	Provider         string
	PromptTokens     int32
	CompletionTokens int32
}) float64 {
	var total float64
	for _, c := range calls {
		total += EstimateCost(c.Model, c.Provider, c.PromptTokens, c.CompletionTokens)
	}
	return total
}

// SupportedModels returns the list of model name prefixes that have
// pricing data available.
func SupportedModels() []string {
	pricingMutex.RLock()
	defer pricingMutex.RUnlock()

	models := make([]string, 0, len(activePricingTable))
	for k := range activePricingTable {
		models = append(models, k)
	}
	return models
}
