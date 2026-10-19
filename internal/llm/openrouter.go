// Package llm is a minimal client for OpenRouter's OpenAI-compatible chat
// completions API.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const DefaultBaseURL = "https://openrouter.ai/api/v1"

// ErrRateLimited is returned when OpenRouter answers with HTTP 429, which is
// common on :free models.
var ErrRateLimited = errors.New("openrouter: rate limited")

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

func New(apiKey, model string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		APIKey:     apiKey,
		Model:      model,
		HTTPClient: http.DefaultClient,
	}
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends the conversation and returns the assistant's reply text.
func (c *Client) Complete(ctx context.Context, msgs []Message) (string, error) {
	body, err := json.Marshal(chatRequest{Model: c.Model, Messages: msgs})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "corg")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openrouter: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("openrouter: reading response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", ErrRateLimited
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("openrouter: status %d, undecodable body: %w", resp.StatusCode, err)
	}
	// OpenRouter can report errors in the body even with a 200 status.
	if cr.Error != nil {
		return "", fmt.Errorf("openrouter: status %d: %s", resp.StatusCode, cr.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openrouter: status %d", resp.StatusCode)
	}
	if len(cr.Choices) == 0 {
		return "", errors.New("openrouter: no choices in response")
	}
	text := strings.TrimSpace(cr.Choices[0].Message.Content)
	if text == "" {
		return "", errors.New("openrouter: empty reply")
	}
	return text, nil
}
