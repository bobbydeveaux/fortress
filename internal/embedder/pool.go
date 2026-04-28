package embedder

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/bobbydeveaux/fortress/internal/chunker"
)

type Pool struct {
	embedder  Embedder
	workers   int
	batchSize int
}

func NewPool(emb Embedder, workers, batchSize int) *Pool {
	if workers <= 0 {
		workers = runtime.NumCPU()
		if workers > 4 {
			workers = 4
		}
	}
	if batchSize <= 0 {
		batchSize = 32
	}
	return &Pool{
		embedder:  emb,
		workers:   workers,
		batchSize: batchSize,
	}
}

type embedJob struct {
	indices []int
	texts   []string
}

func (p *Pool) EmbedChunks(ctx context.Context, chunks []chunker.Chunk, progress func(n int)) ([]chunker.Chunk, error) {
	if len(chunks) == 0 {
		return chunks, nil
	}

	jobs := make(chan embedJob, p.workers*2)
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup

	result := make([]chunker.Chunk, len(chunks))
	copy(result, chunks)

	p.startWorkers(ctx, &wg, jobs, result, &mu, &firstErr, progress)

	if err := p.sendBatches(ctx, chunks, jobs); err != nil {
		wg.Wait()
		return nil, err
	}

	close(jobs)
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return result, nil
}

func (p *Pool) startWorkers(ctx context.Context, wg *sync.WaitGroup, jobs <-chan embedJob, result []chunker.Chunk, mu *sync.Mutex, firstErr *error, progress func(n int)) {
	for w := 0; w < p.workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.runWorker(ctx, jobs, result, mu, firstErr, progress)
		}()
	}
}

func (p *Pool) runWorker(ctx context.Context, jobs <-chan embedJob, result []chunker.Chunk, mu *sync.Mutex, firstErr *error, progress func(n int)) {
	for j := range jobs {
		mu.Lock()
		failed := *firstErr != nil
		mu.Unlock()
		if failed {
			return
		}

		vecs, err := p.embedder.Embed(ctx, j.texts)
		if err != nil {
			mu.Lock()
			if *firstErr == nil {
				*firstErr = fmt.Errorf("embedding batch: %w", err)
			}
			mu.Unlock()
			return
		}

		mu.Lock()
		for k, idx := range j.indices {
			result[idx].Embedding = vecs[k]
		}
		if progress != nil {
			progress(len(j.indices))
		}
		mu.Unlock()
	}
}

func (p *Pool) sendBatches(ctx context.Context, chunks []chunker.Chunk, jobs chan<- embedJob) error {
	for i := 0; i < len(chunks); i += p.batchSize {
		end := i + p.batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		j := buildBatch(chunks, i, end)
		select {
		case jobs <- j:
		case <-ctx.Done():
			close(jobs)
			return ctx.Err()
		}
	}
	return nil
}

func buildBatch(chunks []chunker.Chunk, start, end int) embedJob {
	indices := make([]int, 0, end-start)
	texts := make([]string, 0, end-start)
	for j := start; j < end; j++ {
		indices = append(indices, j)
		text := chunks[j].Content
		if len(text) > 2000 {
			text = text[:2000]
		}
		texts = append(texts, text)
	}
	return embedJob{indices: indices, texts: texts}
}
