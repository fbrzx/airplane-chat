package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Embedder generates vector representations for text.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type ollamaEmbedder struct {
	host      string
	model     string
	dimension int
	client    *http.Client
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaResponse struct {
	Embedding []float64 `json:"embedding"`
}

// NewOllamaEmbedder constructs an embedder backed by Ollama's embedding API.
func NewOllamaEmbedder(host, model string, dimension int, timeout time.Duration) Embedder {
	return &ollamaEmbedder{
		host:      strings.TrimRight(host, "/"),
		model:     model,
		dimension: dimension,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (e *ollamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, 0, len(texts))
	url := fmt.Sprintf("%s/api/embeddings", e.host)

	for _, text := range texts {
		reqBody, err := json.Marshal(ollamaRequest{Model: e.model, Prompt: text})
		if err != nil {
			return nil, fmt.Errorf("marshal ollama request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("create ollama request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := e.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("call ollama embeddings API: %w", err)
		}

		var payload ollamaResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode ollama response: %w", err)
		}
		resp.Body.Close()

		vec := make([]float32, len(payload.Embedding))
		for i, value := range payload.Embedding {
			vec[i] = float32(value)
		}

		if e.dimension > 0 && len(vec) != e.dimension {
			return nil, fmt.Errorf("ollama embedding dimension mismatch: expected %d, got %d", e.dimension, len(vec))
		}

		results = append(results, vec)
	}

	return results, nil
}
