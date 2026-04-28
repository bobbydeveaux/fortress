package cmd

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/bobbydeveaux/fortress/internal/chunker"
	"github.com/bobbydeveaux/fortress/internal/embedder"
	"github.com/bobbydeveaux/fortress/internal/scanner"
	"github.com/bobbydeveaux/fortress/internal/store"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch [path]",
	Short: "Watch filesystem and re-index on change",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWatch,
}

func init() {
	rootCmd.AddCommand(watchCmd)
}

type watchDeps struct {
	ctx     context.Context
	sc      *scanner.Scanner
	disp    *chunker.Dispatcher
	pool    *embedder.Pool
	db      store.Store
	ignorer *scanner.Ignorer
	absPath string
	mu      sync.Mutex
	pending map[string]*time.Timer
}

func runWatch(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	emb := newEmbedder()

	db, err := store.New(cfg.DBPath, emb.Dimensions())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(absPath); err != nil {
		return fmt.Errorf("watching %s: %w", absPath, err)
	}

	fmt.Printf("Watching %s for changes...\n", absPath)

	deps := &watchDeps{
		ctx:     context.Background(),
		sc:      scanner.New(cfg),
		disp:    chunker.NewDispatcher(cfg.ChunkSize),
		pool:    embedder.NewPool(emb, cfg.EmbedWorkers, cfg.EmbedBatchSize),
		db:      db,
		ignorer: scanner.NewIgnorer(cfg.Ignore),
		absPath: absPath,
		pending: make(map[string]*time.Timer),
	}

	return watchLoop(deps, watcher)
}

func newEmbedder() embedder.Embedder {
	if cfg.Embedder == "openai" {
		return embedder.NewOpenAI(cfg.OpenAI.APIKey, cfg.OpenAI.EmbedModel)
	}
	return embedder.NewOllama(cfg.Ollama.URL, cfg.Ollama.EmbedModel)
}

func watchLoop(deps *watchDeps, watcher *fsnotify.Watcher) error {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			deps.handleEvent(event)
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

func (d *watchDeps) handleEvent(event fsnotify.Event) {
	relPath, _ := filepath.Rel(d.absPath, event.Name)
	if d.ignorer.ShouldIgnore(relPath, false) {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if t, exists := d.pending[event.Name]; exists {
		t.Stop()
	}

	switch {
	case event.Has(fsnotify.Write) || event.Has(fsnotify.Create):
		d.scheduleUpdate(event.Name, relPath)
	case event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename):
		d.scheduleRemove(event.Name, relPath)
	}
}

func (d *watchDeps) scheduleUpdate(name, relPath string) {
	d.pending[name] = time.AfterFunc(500*time.Millisecond, func() {
		d.mu.Lock()
		delete(d.pending, name)
		d.mu.Unlock()

		d.processFileUpdate(name, relPath)
	})
}

func (d *watchDeps) processFileUpdate(name, relPath string) {
	doc, err := d.sc.ScanFile(name)
	if err != nil {
		log.Printf("Error scanning %s: %v", name, err)
		return
	}

	chunks, err := d.disp.Chunk(*doc)
	if err != nil {
		log.Printf("Error chunking %s: %v", name, err)
		return
	}

	chunks, err = d.pool.EmbedChunks(d.ctx, chunks, nil)
	if err != nil {
		log.Printf("Error embedding %s: %v", name, err)
		return
	}

	if err := d.db.UpsertDocument(d.ctx, *doc, chunks); err != nil {
		log.Printf("Error storing %s: %v", name, err)
		return
	}

	fmt.Printf("  Updated: %s (%d chunks)\n", relPath, len(chunks))
}

func (d *watchDeps) scheduleRemove(name, relPath string) {
	d.pending[name] = time.AfterFunc(500*time.Millisecond, func() {
		d.mu.Lock()
		delete(d.pending, name)
		d.mu.Unlock()

		docID := hashPath(relPath)
		if err := d.db.DeleteDocument(d.ctx, docID); err != nil {
			log.Printf("Error deleting %s: %v", name, err)
			return
		}
		fmt.Printf("  Removed: %s\n", relPath)
	})
}
