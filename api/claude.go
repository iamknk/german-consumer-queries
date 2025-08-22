package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type ClaudeClient struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

func NewClaudeClient() (*ClaudeClient, error) {
	key := os.Getenv("CLAUDE_API_KEY")
	if key == "" {
		return nil, errors.New("CLAUDE_API_KEY missing")
	}
	base := os.Getenv("CLAUDE_BASE_URL")
	if base == "" {
		base = "https://api.anthropic.com/v1/messages"
	}
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return &ClaudeClient{
		BaseURL: base,
		APIKey:  key,
		Model:   model,
		Client:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// Request/response types
type claudeReq struct {
	Model     string      `json:"model"`
	MaxTokens int         `json:"max_tokens"`
	System    string      `json:"system,omitempty"`
	Messages  []claudeMsg `json:"messages"`
}
type claudeMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type claudeResp struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// Implements LLMClient
func (c *ClaudeClient) CompleteJSON(ctx context.Context, systemPrompt, user string) (string, error) {
	payload := claudeReq{
		Model:     c.Model,
		MaxTokens: 1000,
		System:    systemPrompt, // ✅ Anthropic expects system prompt here
		Messages: []claudeMsg{
			{Role: "user", Content: user},
		},
	}

	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", c.BaseURL, bytes.NewReader(b))
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	res, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("claude: %s", body)
	}

	var out claudeResp
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if len(out.Content) == 0 {
		return "", errors.New("no content")
	}
	return out.Content[0].Text, nil
}
