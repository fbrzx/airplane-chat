package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message represents a single turn in a chat conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client provides a minimal chat interface compatible with Ollama's REST API.
type Client interface {
	Generate(ctx context.Context, messages []Message) (string, error)
}

type client struct {
	host   string
	model  string
	client *http.Client
}

// NewClient constructs a Client backed by Ollama's /api/chat endpoint.
func NewClient(host, model string) Client {
	return &client{
		host:  strings.TrimRight(host, "/"),
		model: model,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type chatResponse struct {
	Message Message `json:"message"`
	Error   string  `json:"error"`
	Done    bool    `json:"done"`
}

func (c *client) Generate(ctx context.Context, messages []Message) (string, error) {
	if c.host == "" {
		return "", fmt.Errorf("ollama host must be configured")
	}
	if c.model == "" {
		return "", fmt.Errorf("ollama model must be configured")
	}

	payload := chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		if len(data) > 0 {
			return "", fmt.Errorf("ollama chat API error: %s", string(data))
		}
		return "", fmt.Errorf("ollama chat API returned status %s", resp.Status)
	}

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if parsed.Error != "" {
		return "", fmt.Errorf("ollama error: %s", parsed.Error)
	}

	return parsed.Message.Content, nil
}
