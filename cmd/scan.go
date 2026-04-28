package cmd

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"time"

	"github.com/bobbydeveaux/fortress/internal/chunker"
	"github.com/bobbydeveaux/fortress/internal/datasource/confluence"
	"github.com/bobbydeveaux/fortress/internal/docs"
	"github.com/bobbydeveaux/fortress/internal/embedder"
	"github.com/bobbydeveaux/fortress/internal/scanner"
	"github.com/bobbydeveaux/fortress/internal/store"
	"github.com/fatih/color"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "Scan, chunk, embed, and store codebase knowledge",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runScan,
}

var (
	scanDryRun     bool
	scanFull       bool
	scanUpload     string
	scanConfluence bool
)

func init() {
	scanCmd.Flags().BoolVar(&scanDryRun, "dry-run", false, "Preview what would be scanned")
	scanCmd.Flags().BoolVar(&scanFull, "full", false, "Force full re-scan")
	scanCmd.Flags().StringVar(&scanUpload, "upload", "", "Upload DB to cloud storage URI after scan")
	scanCmd.Flags().BoolVar(&scanConfluence, "confluence", false, "Also scan Confluence spaces")
	scanCmd.Flags().Bool("ci", false, "CI mode")
	rootCmd.AddCommand(scanCmd)
}

func createEmbedder() (embedder.Embedder, error) {
	switch cfg.Embedder {
	case "openai":
		if cfg.OpenAI.APIKey == "" {
			return nil, fmt.Errorf("OpenAI API key required (set OPENAI_API_KEY or configure in fortress.yaml)")
		}
		return embedder.NewOpenAI(cfg.OpenAI.APIKey, cfg.OpenAI.EmbedModel), nil
	default:
		return embedder.NewOllama(cfg.Ollama.URL, cfg.Ollama.EmbedModel), nil
	}
}

func collectDocuments(ctx context.Context, sc *scanner.Scanner, db store.Store, path string) ([]scanner.Document, []error) {
	docCh, errCh := sc.Scan(ctx, path)

	var documents []scanner.Document
	var scanErrors []error

	go func() {
		for err := range errCh {
			scanErrors = append(scanErrors, err)
		}
	}()

	for doc := range docCh {
		if scanDryRun {
			fmt.Printf("  %s [%s/%s]\n", doc.RelPath, doc.FileType, doc.Category)
			continue
		}
		if !scanFull {
			existingHash, _ := db.GetContentHash(ctx, doc.ID)
			if existingHash == doc.ContentHash {
				continue
			}
		}
		documents = append(documents, doc)
	}

	return documents, scanErrors
}

func extractGitHistory(documents []scanner.Document) []scanner.Document {
	gitRepos := make(map[string]string)
	for _, doc := range documents {
		if doc.Repo != "" && doc.RepoRoot != "" {
			gitRepos[doc.Repo] = doc.RepoRoot
		}
	}

	var gitDocs []scanner.Document
	for repoName, repoRoot := range gitRepos {
		info, err := scanner.ExtractGitInfo(repoRoot, 200)
		if err != nil || info.Log == "" {
			continue
		}
		gitDocs = append(gitDocs, scanner.Document{
			ID:          hashPath("git-history:" + repoName),
			Path:        repoRoot + "/.git/log",
			RelPath:     ".git-history/" + repoName,
			Repo:        repoName,
			RepoRoot:    repoRoot,
			Category:    scanner.CategoryDocs,
			FileType:    scanner.FileTypeGitHistory,
			SourceType:  scanner.SourceTypeFilesystem,
			Content:     fmt.Sprintf("[REPO: %s] Git History\n\n%s", repoName, info.Log),
			ContentHash: hashPath(info.Log),
			Metadata: map[string]string{
				"remote_url": info.RemoteURL,
				"head_sha":   info.LastCommitSHA,
			},
		})
	}
	return gitDocs
}

func scanConfluencePages(ctx context.Context, db store.Store) ([]scanner.Document, []error) {
	if !scanConfluence || cfg.Confluence.BaseURL == "" {
		return nil, nil
	}

	cyan := color.New(color.FgCyan)
	cyan.Println("Scanning Confluence...")

	cp := confluence.New(
		cfg.Confluence.BaseURL,
		cfg.Confluence.Email,
		cfg.Confluence.APIToken,
		cfg.Confluence.SpaceKeys,
	)
	cDocCh, cErrCh := cp.Scan(ctx)

	var documents []scanner.Document
	var scanErrors []error

	go func() {
		for err := range cErrCh {
			fmt.Fprintf(os.Stderr, "Confluence error: %v\n", err)
			scanErrors = append(scanErrors, err)
		}
	}()

	for doc := range cDocCh {
		if !scanFull {
			existingHash, _ := db.GetContentHash(ctx, doc.ID)
			if existingHash == doc.ContentHash {
				continue
			}
		}
		documents = append(documents, doc)
	}
	fmt.Printf("Found %s Confluence pages to index\n", color.YellowString("%d", len(documents)))

	return documents, scanErrors
}

func chunkDocuments(documents []scanner.Document) ([]chunker.Chunk, map[string][]chunker.Chunk) {
	dispatcher := chunker.NewDispatcher(cfg.ChunkSize)
	var allChunks []chunker.Chunk
	docChunks := make(map[string][]chunker.Chunk)

	for _, doc := range documents {
		chunks, err := dispatcher.Chunk(doc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: chunking %s: %v\n", doc.RelPath, err)
			continue
		}
		allChunks = append(allChunks, chunks...)
		docChunks[doc.ID] = chunks
	}
	return allChunks, docChunks
}

func embedChunks(ctx context.Context, emb embedder.Embedder, allChunks []chunker.Chunk) ([]chunker.Chunk, error) {
	pool := embedder.NewPool(emb, cfg.EmbedWorkers, cfg.EmbedBatchSize)

	bar := progressbar.NewOptions(len(allChunks),
		progressbar.OptionSetDescription("Embedding"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(40),
	)

	embedded, err := pool.EmbedChunks(ctx, allChunks, func(n int) {
		bar.Add(n)
	})
	bar.Finish()
	fmt.Println()

	return embedded, err
}

func storeDocuments(ctx context.Context, db store.Store, documents []scanner.Document, docChunks map[string][]chunker.Chunk) {
	fmt.Println("Storing documents...")
	repoChunks := make(map[string]int)
	repoFiles := make(map[string]int)

	for _, doc := range documents {
		chunks := docChunks[doc.ID]
		if err := db.UpsertDocument(ctx, doc, chunks); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: storing %s: %v\n", doc.RelPath, err)
			continue
		}
		if doc.Repo != "" {
			repoFiles[doc.Repo]++
			repoChunks[doc.Repo] += len(chunks)
		}
	}

	updateScanState(ctx, db, documents, repoFiles, repoChunks)
}

func updateScanState(ctx context.Context, db store.Store, documents []scanner.Document, repoFiles, repoChunks map[string]int) {
	for repo, fileCount := range repoFiles {
		state := store.ScanState{
			Repo:         repo,
			LastScanTime: time.Now(),
			FileCount:    fileCount,
			ChunkCount:   repoChunks[repo],
		}
		for _, doc := range documents {
			if doc.Repo == repo && doc.RepoRoot != "" {
				info, err := scanner.ExtractGitInfo(doc.RepoRoot, 0)
				if err == nil {
					state.LastCommitSHA = info.LastCommitSHA
				}
				break
			}
		}
		db.UpdateScanState(ctx, state)
	}
}

func printStats(ctx context.Context, db store.Store) {
	stats, _ := db.GetStats(ctx)

	cyan := color.New(color.FgCyan)
	green := color.New(color.FgGreen, color.Bold)
	yellow := color.New(color.FgYellow)
	dim := color.New(color.FgHiBlack)

	fmt.Println()
	green.Println("Done! Jor-El now knows:")
	fmt.Println()
	dim.Print("  Repos:      ")
	yellow.Printf("%d\n", stats.Repos)
	dim.Print("  Files:      ")
	yellow.Printf("%d\n", stats.Files)
	dim.Print("  Chunks:     ")
	yellow.Printf("%d\n", stats.Chunks)
	dim.Print("  Categories: ")
	yellow.Printf("%d\n", stats.Categories)
	dim.Print("  DB size:    ")
	cyan.Printf("%.2f MB\n", stats.DBSizeMB)
}

func runScan(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	ctx := context.Background()

	emb, err := createEmbedder()
	if err != nil {
		return err
	}

	db, err := store.New(cfg.DBPath, emb.Dimensions())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	sc := scanner.New(cfg)

	cyan := color.New(color.FgCyan)
	cyan.Printf("Scanning %s...\n", path)

	documents, scanErrors := collectDocuments(ctx, sc, db, path)

	if scanDryRun {
		fmt.Printf("\nWould scan %d files\n", len(documents))
		return nil
	}

	documents = append(documents, extractGitHistory(documents)...)

	cDocs, cErrs := scanConfluencePages(ctx, db)
	documents = append(documents, cDocs...)
	scanErrors = append(scanErrors, cErrs...)

	if len(documents) == 0 {
		fmt.Println("No changes detected.")
		return nil
	}

	fmt.Printf("Processing %s files...\n", color.YellowString("%d", len(documents)))

	allChunks, docChunks := chunkDocuments(documents)

	fmt.Printf("Generated %s chunks, embedding...\n", color.YellowString("%d", len(allChunks)))

	embeddedChunks, err := embedChunks(ctx, emb, allChunks)
	if err != nil {
		return fmt.Errorf("embedding: %w", err)
	}

	// Rebuild docChunks map with embeddings
	idx := 0
	for _, doc := range documents {
		n := len(docChunks[doc.ID])
		docChunks[doc.ID] = embeddedChunks[idx : idx+n]
		idx += n
	}

	storeDocuments(ctx, db, documents, docChunks)

	fmt.Println("Generating documentation...")
	gen := docs.NewGenerator(db, cfg.DocsPath)
	if err := gen.Generate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: generating docs: %v\n", err)
	}

	if scanUpload != "" {
		fmt.Printf("Uploading to %s...\n", scanUpload)
	}

	printStats(ctx, db)

	if len(scanErrors) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d scan errors (non-fatal)\n", len(scanErrors))
	}

	return nil
}

func hashPath(path string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(path)))
}
