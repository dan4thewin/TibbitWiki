// Package rag provides semantic similarity search over wiki notes
// using akin vector embeddings. A dedicated goroutine owns the index
// and receives file paths to (re-)index via a channel.
package rag

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	akin "github.com/dan4thewin/akin-vecsearch"
)

// Package-level function variables for testing. These are not safe
// for t.Parallel() — tests that swap them must run sequentially.
var openIndex = akin.Open

// Indexer owns the akin index and processes indexing requests
// on a dedicated goroutine.
type Indexer struct {
	idx  *akin.Index
	emb  *akin.Embedder
	ch   chan string // file paths to index
	done chan struct{}
	wg   sync.WaitGroup

	// wikiPath is the root directory containing all wiki subdirs.
	// Used to derive wiki names from file paths.
	wikiPath string

	// saveInterval controls how often the index is persisted.
	saveInterval time.Duration
	dirty        bool
}

// Config holds RAG indexer configuration.
type Config struct {
	IndexPath    string        // path to the .gob index file
	WikiPath     string        // root wiki directory
	OllamaURL    string        // Ollama base URL
	Model        string        // embedding model name
	SaveInterval time.Duration // how often to flush to disk (0 = 30s)
}

// Result is a semantic search result.
type Result struct {
	Title   string  `json:"title"`
	Wiki    string  `json:"wiki"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
	Body    string  `json:"body,omitempty"`
}

// New creates an Indexer. Call Start() to begin processing.
func New(cfg Config) (*Indexer, error) {
	if cfg.SaveInterval == 0 {
		cfg.SaveInterval = 30 * time.Second
	}
	idx, err := openIndex(cfg.IndexPath)
	if err != nil {
		return nil, fmt.Errorf("rag: open index: %w", err)
	}

	// Check for model mismatch — requires full re-index.
	if idx.Model() != "" && idx.Model() != cfg.Model {
		log.Printf("rag: model changed from %q to %q — rebuilding index", idx.Model(), cfg.Model)
		if err := os.Remove(cfg.IndexPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("rag: remove stale index: %w", err)
		}
		idx, err = openIndex(cfg.IndexPath)
		if err != nil {
			return nil, fmt.Errorf("rag: open fresh index: %w", err)
		}
	}

	emb := akin.NewEmbedder(cfg.Model, cfg.OllamaURL)

	return &Indexer{
		idx:          idx,
		emb:          emb,
		ch:           make(chan string, 256),
		done:         make(chan struct{}),
		wikiPath:     cfg.WikiPath,
		saveInterval: cfg.SaveInterval,
	}, nil
}

// Start launches the indexing goroutine.
func (ix *Indexer) Start() {
	ix.wg.Add(1)
	go ix.loop()
}

// Stop signals the indexer to drain remaining work, save, and exit.
func (ix *Indexer) Stop() {
	close(ix.done)
	ix.wg.Wait()
}

// Index queues a file path for (re-)indexing. Non-blocking; drops
// the request if the channel is full (the next watcher event will
// catch it).
func (ix *Indexer) Index(path string) {
	select {
	case ix.ch <- path:
	default:
		log.Printf("rag: index channel full, dropping %s", path)
	}
}

// Remove queues removal of a file from the index.
// Sends a path prefixed with "\x00" to signal deletion.
func (ix *Indexer) Remove(path string) {
	select {
	case ix.ch <- "\x00" + path:
	default:
		log.Printf("rag: index channel full, dropping remove %s", path)
	}
}

// Query performs a semantic search using query text.
func (ix *Indexer) Query(ctx context.Context, query string, n int) ([]Result, error) {
	results, err := ix.idx.QueryText(ctx, ix.emb, query, n)
	if err != nil {
		return nil, fmt.Errorf("rag: query: %w", err)
	}
	return ix.toResults(results), nil
}

// QueryByNote queries for notes similar to the given note file path.
// Uses the note's existing vectors — no embedding call needed.
func (ix *Indexer) QueryByNote(notePath string, n int) []Result {
	// Find the note's first entry vector in the index.
	vec := ix.entryVector(notePath)
	if vec == nil {
		return nil
	}
	results := ix.idx.Query(vec, n+1) // +1 to exclude self
	// Filter out the queried note itself.
	filtered := make([]Result, 0, len(results))
	for _, r := range results {
		if r.File != notePath {
			filtered = append(filtered, ix.toResult(r))
		}
		if len(filtered) >= n {
			break
		}
	}
	return filtered
}

// Entries returns the number of entries in the index.
func (ix *Indexer) Entries() int {
	return ix.idx.Entries()
}

// loop is the indexing goroutine.
func (ix *Indexer) loop() {
	defer ix.wg.Done()

	ticker := time.NewTicker(ix.saveInterval)
	defer ticker.Stop()

	for {
		select {
		case path, ok := <-ix.ch:
			if !ok {
				return
			}
			ix.processPath(path)
		case <-ticker.C:
			ix.save()
		case <-ix.done:
			// Drain remaining items.
			for {
				select {
				case path := <-ix.ch:
					ix.processPath(path)
				default:
					ix.save()
					return
				}
			}
		}
	}
}

// processPath handles a single index or remove request.
func (ix *Indexer) processPath(path string) {
	if strings.HasPrefix(path, "\x00") {
		realPath := path[1:]
		ix.idx.Remove(realPath)
		ix.dirty = true
		log.Printf("rag: removed %s", realPath)
		return
	}

	// Check if file still exists (might have been deleted).
	if _, err := os.Stat(path); os.IsNotExist(err) {
		ix.idx.Remove(path)
		ix.dirty = true
		log.Printf("rag: removed (deleted) %s", path)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := ix.idx.IndexFile(ctx, ix.emb, path, akin.IndexOpts{}); err != nil {
		log.Printf("rag: indexing %s: %v", path, err)
		return
	}
	ix.dirty = true
}

// save persists the index if dirty.
func (ix *Indexer) save() {
	if !ix.dirty {
		return
	}
	if err := ix.idx.Save(); err != nil {
		log.Printf("rag: save: %v", err)
		return
	}
	ix.dirty = false
}

// entryVector returns the first vector for the given file, or nil.
func (ix *Indexer) entryVector(path string) []float32 {
	vecs := ix.idx.EntryVectors(path)
	if len(vecs) == 0 {
		return nil
	}
	return vecs[0]
}

// toResults converts akin results to RAG results.
func (ix *Indexer) toResults(results []akin.Result) []Result {
	out := make([]Result, len(results))
	for i, r := range results {
		out[i] = ix.toResult(r)
	}
	return out
}

// toResult converts a single akin result to a RAG result.
func (ix *Indexer) toResult(r akin.Result) Result {
	wiki, title := ix.parseFilePath(r.File)
	snippet := ix.readSnippet(r.File, r.Offset, r.Length)
	return Result{
		Title:   title,
		Wiki:    wiki,
		Score:   r.Score,
		Snippet: snippet,
	}
}

// parseFilePath extracts wiki name and note title from a file path.
// e.g. /Users/dg/TibbitWiki/hold/SomeNote.md → ("hold", "SomeNote")
func (ix *Indexer) parseFilePath(path string) (wiki, title string) {
	rel, err := filepath.Rel(ix.wikiPath, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", filepath.Base(path)
	}
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	if len(parts) < 2 {
		return "", strings.TrimSuffix(parts[0], ".md")
	}
	wiki = parts[0]
	title = strings.TrimSuffix(parts[1], ".md")
	return wiki, title
}

// readSnippet reads the chunk text from a file.
func (ix *Indexer) readSnippet(path string, offset, length int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, length)
	n, err := f.ReadAt(buf, int64(offset))
	if n == 0 && err != nil {
		return ""
	}
	return string(buf[:n])
}

// SetEmbedFunc overrides the embedder's embedding function for testing.
func (ix *Indexer) SetEmbedFunc(fn func(ctx context.Context, texts []string) ([][]float32, error)) {
	ix.emb.SetEmbedFunc(fn)
}

// AddTestEntries adds a single entry to the index for testing.
func (ix *Indexer) AddTestEntries(file string, vec []float32) {
	ix.idx.AddEntries([]akin.Entry{
		{File: file, Offset: 0, Length: 12, Vector: vec},
	}, ix.emb.Model(), len(vec)) //nolint:errcheck
}
