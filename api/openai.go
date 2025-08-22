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

type OpenAIClient struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

func NewOpenAIClient() (*OpenAIClient, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, errors.New("OPENAI_API_KEY missing")
	}
	base := os.Getenv("OPENAI_BASE_URL")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-5-mini"
	}
	return &OpenAIClient{
		BaseURL: base,
		APIKey:  key,
		Model:   model,
		Client:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReq struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// LLMClient interface
type LLMClient interface {
	CompleteJSON(ctx context.Context, systemPrompt, user string) (string, error)
}

func (c *OpenAIClient) CompleteJSON(ctx context.Context, systemPrompt, user string) (string, error) {
	if systemPrompt != "" && !containsJSONWord(systemPrompt) {
		systemPrompt += "\n\n(Hinweis: Antworte ausschließlich mit einem einzigen JSON-Objekt passend zum Schema.)"
	}

	payload := chatReq{
		Model:       c.Model,
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: user},
		},
	}

	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("openai: %s", body)
	}

	var out chatResp
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", errors.New("no choices")
	}
	return out.Choices[0].Message.Content, nil
}

func containsJSONWord(s string) bool {
	for i := 0; i+3 < len(s); i++ {
		if (s[i] == 'J' || s[i] == 'j') &&
			(s[i+1] == 'S' || s[i+1] == 's') &&
			(s[i+2] == 'O' || s[i+2] == 'o') &&
			(s[i+3] == 'N' || s[i+3] == 'n') {
			return true
		}
	}
	return false
}
