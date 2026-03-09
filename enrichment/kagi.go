package enrichment

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// KagiSummarizer calls the Kagi Universal Summarizer API.
type KagiSummarizer struct {
	APIKey     string
	Client     *http.Client
	Delay      time.Duration // delay between requests
	Balance    *float64      // last known API balance (nil = unknown)
	MinBalance float64       // stop if balance drops below this
}

type kagiResponse struct {
	Meta struct {
		ID         string   `json:"id"`
		Ms         int      `json:"ms"`
		APIBalance *float64 `json:"api_balance"`
	} `json:"meta"`
	Data struct {
		Output string `json:"output"`
		Tokens int    `json:"tokens"`
	} `json:"data"`
	Error []struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	} `json:"error"`
}

// NewKagiSummarizer creates a summarizer with sensible defaults.
func NewKagiSummarizer(apiKey string) *KagiSummarizer {
	return &KagiSummarizer{
		APIKey:     apiKey,
		Client:     &http.Client{Timeout: 45 * time.Second},
		Delay:      500 * time.Millisecond, // 2 req/sec max
		MinBalance: 1.0,                    // stop at $1 remaining
	}
}

// Summarize fetches a summary for the given URL.
// Returns empty string (not error) for URLs that can't be summarized.
// Returns ErrBalanceLow if API balance drops below MinBalance.
func (k *KagiSummarizer) Summarize(targetURL string) (string, error) {
	// Pace requests
	time.Sleep(k.Delay)

	endpoint := fmt.Sprintf("https://kagi.com/api/v0/summarize?url=%s&summary_type=takeaway",
		url.QueryEscape(targetURL))

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bot "+k.APIKey)

	resp, err := k.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle HTTP-level rate limiting
	if resp.StatusCode == 429 {
		retryAfter := resp.Header.Get("Retry-After")
		return "", fmt.Errorf("rate limited (429), retry-after: %s", retryAfter)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	var result kagiResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	// Track API balance
	if result.Meta.APIBalance != nil {
		k.Balance = result.Meta.APIBalance
		if *k.Balance < k.MinBalance {
			return "", fmt.Errorf("%w: $%.2f remaining", ErrBalanceLow, *k.Balance)
		}
	}

	if len(result.Error) > 0 {
		// Not fatal — some URLs just can't be summarized
		return "", nil
	}

	return result.Data.Output, nil
}

// ErrBalanceLow signals that the API balance dropped below the minimum.
var ErrBalanceLow = fmt.Errorf("kagi API balance too low")
