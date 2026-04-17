package rag

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bobbydeveaux/fortress/internal/config"
	"github.com/bobbydeveaux/fortress/internal/embedder"
	"github.com/bobbydeveaux/fortress/internal/store"
)

type Pipeline struct {
	embedder embedder.Embedder
	store    store.Store
	cfg      *config.Config
	client   *http.Client
}

func NewPipeline(emb embedder.Embedder, s store.Store, cfg *config.Config) *Pipeline {
	return &Pipeline{
		embedder: emb,
		store:    s,
		cfg:      cfg,
		client:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *Pipeline) Answer(ctx context.Context, question string) (<-chan string, error) {
	// Step 1: embed the question
	vecs, err := p.embedder.Embed(ctx, []string{question})
	if err != nil {
		return nil, fmt.Errorf("embedding question: %w", err)
	}

	// Step 2: retrieve top-k chunks
	results, err := p.store.Search(ctx, vecs[0], 8)
	if err != nil {
		return nil, fmt.Errorf("searching: %w", err)
	}

	// Step 3: build prompt
	var contextBuf strings.Builder
	for i, r := range results {
		fmt.Fprintf(&contextBuf, "[%d] File: %s (lines %d-%d)\n%s\n\n",
			i+1, r.Chunk.Metadata.Path, r.Chunk.StartLine, r.Chunk.EndLine, r.Chunk.Content)
	}
	prompt := buildPrompt(question, contextBuf.String())

	// Step 4: stream from LLM
	switch p.cfg.ChatLLM {
	case "claude":
		return p.streamClaude(ctx, prompt)
	default:
		return p.streamOllama(ctx, prompt)
	}
}

func buildPrompt(question, context string) string {
	return fmt.Sprintf(`You are Jor-El, an expert on this codebase.
Answer the developer's question using only the provided context.
Cite sources using [N] notation referring to the context blocks above.
If the answer is not in the context, say so.

Context:
%s

Question: %s

Answer:`, context, question)
}

func (p *Pipeline) streamOllama(ctx context.Context, prompt string) (<-chan string, error) {
	body := map[string]interface{}{
		"model":  p.cfg.Ollama.ChatModel,
		"prompt": prompt,
		"stream": true,
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", p.cfg.Ollama.URL+"/api/generate", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ollama error %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan string, 100)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			var chunk struct {
				Response string `json:"response"`
				Done     bool   `json:"done"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
				continue
			}
			if chunk.Response != "" {
				select {
				case ch <- chunk.Response:
				case <-ctx.Done():
					return
				}
			}
			if chunk.Done {
				return
			}
		}
	}()

	return ch, nil
}

func (p *Pipeline) streamClaude(ctx context.Context, prompt string) (<-chan string, error) {
	body := map[string]interface{}{
		"model":      p.cfg.Claude.Model,
		"max_tokens": 4096,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.cfg.Claude.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("claude request: %w", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("claude error %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan string, 100)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}

			var event struct {
				Type  string `json:"type"`
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			if event.Type == "content_block_delta" && event.Delta.Text != "" {
				select {
				case ch <- event.Delta.Text:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}
