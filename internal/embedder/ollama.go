package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type OllamaEmbedder struct {
	BaseURL string
	Model   string
	client  *http.Client
}

func NewOllama(baseURL, model string) *OllamaEmbedder {
	return &OllamaEmbedder{
		BaseURL: baseURL,
		Model:   model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (o *OllamaEmbedder) Dimensions() int {
	return 768
}

func (o *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body := map[string]interface{}{
		"model": o.Model,
		"input": texts,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	return withRetries(ctx, 3, func(ctx context.Context) ([][]float32, bool, error) {
		return o.doEmbedRequest(ctx, jsonBody, len(texts))
	})
}

func (o *OllamaEmbedder) doEmbedRequest(ctx context.Context, jsonBody []byte, expectedCount int) ([][]float32, bool, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", o.BaseURL+"/api/embed", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, true, err
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode == 429 || resp.StatusCode == 503 {
		return nil, true, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		return nil, false, fmt.Errorf("ollama API error %d: %s", resp.StatusCode, string(respBody))
	}
	if err != nil {
		return nil, false, err
	}

	return o.parseOllamaResponse(respBody, expectedCount)
}

func (o *OllamaEmbedder) parseOllamaResponse(respBody []byte, expectedCount int) ([][]float32, bool, error) {
	var parsed struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, false, fmt.Errorf("parsing response: %w", err)
	}
	if len(parsed.Embeddings) != expectedCount {
		return nil, false, fmt.Errorf("expected %d embeddings, got %d", expectedCount, len(parsed.Embeddings))
	}
	return parsed.Embeddings, false, nil
}
