package rag

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	akin "github.com/dan4thewin/akin-vecsearch"
)

// fakeEmbedder returns a fixed vector for any input.
// setupIndex creates a temp dir with wiki-like structure and an
// index pre-populated with known entries.
func setupIndex(t *testing.T) (ix *Indexer, wikiPath string) {
	t.Helper()
	dir := t.TempDir()
	wikiPath = dir

	// Create wiki subdirs with .md files.
	holdDir := filepath.Join(dir, "hold")
	workDir := filepath.Join(dir, "work")
	os.MkdirAll(holdDir, 0755)
	os.MkdirAll(workDir, 0755)

	os.WriteFile(filepath.Join(holdDir, "AlphaNote.md"),
		[]byte("title: AlphaNote\ntags:\n\nalpha content about Go programming"), 0644)
	os.WriteFile(filepath.Join(holdDir, "BetaNote.md"),
		[]byte("title: BetaNote\ntags:\n\nbeta content about Rust systems"), 0644)
	os.WriteFile(filepath.Join(workDir, "GammaNote.md"),
		[]byte("title: GammaNote\ntags:\n\ngamma content about Go testing"), 0644)

	// Create index and add entries directly (no Ollama needed).
	idxPath := filepath.Join(dir, "test.gob")
	idx, err := akin.Open(idxPath)
	if err != nil {
		t.Fatal(err)
	}

	// Add entries with known vectors so we can predict query results.
	// AlphaNote: [1, 0, 0]
	// BetaNote:  [0, 1, 0]
	// GammaNote: [0.9, 0, 0.4] — similar to Alpha
	entries := []akin.Entry{
		{File: filepath.Join(holdDir, "AlphaNote.md"), Offset: 0, Length: 35, Vector: []float32{1, 0, 0}},
		{File: filepath.Join(holdDir, "BetaNote.md"), Offset: 0, Length: 36, Vector: []float32{0, 1, 0}},
		{File: filepath.Join(workDir, "GammaNote.md"), Offset: 0, Length: 36, Vector: []float32{0.9, 0, 0.4}},
	}
	if err := idx.AddEntries(entries, "test-model", 3); err != nil {
		t.Fatal(err)
	}
	if err := idx.Save(); err != nil {
		t.Fatal(err)
	}

	// Create embedder with test hook.
	emb := akin.NewEmbedder("test-model", "http://unused:9999")

	ix = &Indexer{
		idx:          idx,
		emb:          emb,
		ch:           make(chan string, 256),
		done:         make(chan struct{}),
		wikiPath:     dir,
		saveInterval: 100 * time.Millisecond,
	}
	return ix, dir
}

func TestNewCreatesEmptyIndex(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		IndexPath: filepath.Join(dir, "idx.gob"),
		WikiPath:  dir,
		Model:     "test-model",
		OllamaURL: "http://unused:9999",
	}
	ix, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ix.Entries() != 0 {
		t.Errorf("expected 0 entries, got %d", ix.Entries())
	}
}

func TestNewModelMismatchRebuilds(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "idx.gob")

	// Create an index with model "old-model".
	idx, _ := akin.Open(idxPath)
	idx.AddEntries([]akin.Entry{
		{File: "test.md", Offset: 0, Length: 5, Vector: []float32{1, 0}},
	}, "old-model", 2)
	idx.Save()

	// Open with a different model — should rebuild.
	cfg := Config{
		IndexPath: idxPath,
		WikiPath:  dir,
		Model:     "new-model",
		OllamaURL: "http://unused:9999",
	}
	ix, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ix.Entries() != 0 {
		t.Errorf("expected 0 entries after rebuild, got %d", ix.Entries())
	}
}

func TestNewBadIndexPath(t *testing.T) {
	cfg := Config{
		IndexPath: "/dev/null/impossible/idx.gob",
		WikiPath:  t.TempDir(),
		Model:     "test-model",
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for bad index path")
	}
}

func TestParseFilePath(t *testing.T) {
	ix := &Indexer{wikiPath: "/wiki"}

	tests := []struct {
		path      string
		wantWiki  string
		wantTitle string
	}{
		{"/wiki/hold/MyNote.md", "hold", "MyNote"},
		{"/wiki/work/Some Note.md", "work", "Some Note"},
		{"/wiki/TopLevel.md", "", "TopLevel"},
		{"/other/path/file.md", "", "file.md"}, // can't compute rel
	}
	for _, tt := range tests {
		wiki, title := ix.parseFilePath(tt.path)
		if wiki != tt.wantWiki || title != tt.wantTitle {
			t.Errorf("parseFilePath(%q) = (%q, %q), want (%q, %q)",
				tt.path, wiki, title, tt.wantWiki, tt.wantTitle)
		}
	}
}

func TestReadSnippet(t *testing.T) {
	ix := &Indexer{}

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	os.WriteFile(path, []byte("hello world test content"), 0644)

	// Read a slice from the middle.
	got := ix.readSnippet(path, 6, 5)
	if got != "world" {
		t.Errorf("readSnippet = %q, want %q", got, "world")
	}

	// Non-existent file.
	got = ix.readSnippet(filepath.Join(dir, "nope"), 0, 5)
	if got != "" {
		t.Errorf("readSnippet non-existent = %q, want empty", got)
	}
}

func TestReadSnippetOffsetBeyondEOF(t *testing.T) {
	ix := &Indexer{}
	dir := t.TempDir()
	path := filepath.Join(dir, "short.md")
	os.WriteFile(path, []byte("hi"), 0644)

	got := ix.readSnippet(path, 100, 5)
	if got != "" {
		t.Errorf("readSnippet past EOF = %q, want empty", got)
	}
}

func TestQueryByNoteExcludesSelf(t *testing.T) {
	ix, dir := setupIndex(t)

	alphaPath := filepath.Join(dir, "hold", "AlphaNote.md")
	results := ix.QueryByNote(alphaPath, 5)

	// Should get GammaNote (similar vector) but NOT AlphaNote.
	for _, r := range results {
		if r.Title == "AlphaNote" {
			t.Error("QueryByNote should exclude the queried note itself")
		}
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Title != "GammaNote" {
		t.Errorf("top result = %q, want GammaNote (most similar to Alpha)", results[0].Title)
	}
}

func TestQueryByNoteNotInIndex(t *testing.T) {
	ix, _ := setupIndex(t)

	results := ix.QueryByNote("/no/such/file.md", 5)
	if results != nil {
		t.Errorf("expected nil for unknown file, got %v", results)
	}
}

func TestQueryByNoteRespectN(t *testing.T) {
	ix, dir := setupIndex(t)

	alphaPath := filepath.Join(dir, "hold", "AlphaNote.md")
	results := ix.QueryByNote(alphaPath, 1)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestEntryVector(t *testing.T) {
	ix, dir := setupIndex(t)

	alphaPath := filepath.Join(dir, "hold", "AlphaNote.md")
	vec := ix.entryVector(alphaPath)
	if vec == nil {
		t.Fatal("expected vector for AlphaNote")
	}
	if vec[0] != 1 || vec[1] != 0 || vec[2] != 0 {
		t.Errorf("vector = %v, want [1 0 0]", vec)
	}

	// Unknown file.
	vec = ix.entryVector("/no/such/file.md")
	if vec != nil {
		t.Errorf("expected nil for unknown file, got %v", vec)
	}
}

func TestToResult(t *testing.T) {
	ix, dir := setupIndex(t)

	r := akin.Result{
		File:   filepath.Join(dir, "hold", "AlphaNote.md"),
		Offset: 0,
		Length: 10,
		Score:  0.95,
	}
	result := ix.toResult(r)

	if result.Wiki != "hold" {
		t.Errorf("wiki = %q, want %q", result.Wiki, "hold")
	}
	if result.Title != "AlphaNote" {
		t.Errorf("title = %q, want %q", result.Title, "AlphaNote")
	}
	if result.Score != 0.95 {
		t.Errorf("score = %f, want 0.95", result.Score)
	}
	if result.Snippet == "" {
		t.Error("expected non-empty snippet")
	}
}

func TestToResults(t *testing.T) {
	ix, dir := setupIndex(t)
	input := []akin.Result{
		{File: filepath.Join(dir, "hold", "AlphaNote.md"), Offset: 0, Length: 5, Score: 0.9},
		{File: filepath.Join(dir, "work", "GammaNote.md"), Offset: 0, Length: 5, Score: 0.8},
	}
	results := ix.toResults(input)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Wiki != "hold" || results[1].Wiki != "work" {
		t.Errorf("wiki names = (%q, %q), want (hold, work)", results[0].Wiki, results[1].Wiki)
	}
}

func TestIndexAndRemoveViaChannel(t *testing.T) {
	dir := t.TempDir()
	holdDir := filepath.Join(dir, "hold")
	os.MkdirAll(holdDir, 0755)

	notePath := filepath.Join(holdDir, "TestNote.md")
	os.WriteFile(notePath, []byte("title: TestNote\ntags:\n\ntest body"), 0644)

	idxPath := filepath.Join(dir, "idx.gob")
	idx, _ := akin.Open(idxPath)

	// Pre-add an entry so we can test removal.
	idx.AddEntries([]akin.Entry{
		{File: notePath, Offset: 0, Length: 10, Vector: []float32{1, 0, 0}},
	}, "test-model", 3)

	emb := akin.NewEmbedder("test-model", "http://unused:9999")
	ix := &Indexer{
		idx:          idx,
		emb:          emb,
		ch:           make(chan string, 256),
		done:         make(chan struct{}),
		wikiPath:     dir,
		saveInterval: 50 * time.Millisecond,
	}

	if ix.Entries() != 1 {
		t.Fatalf("expected 1 entry, got %d", ix.Entries())
	}

	// Remove via NUL-prefixed path (direct processPath call).
	ix.processPath("\x00" + notePath)
	if ix.Entries() != 0 {
		t.Errorf("expected 0 entries after remove, got %d", ix.Entries())
	}
	if !ix.dirty {
		t.Error("expected dirty after remove")
	}
}

func TestProcessPathDeletedFile(t *testing.T) {
	dir := t.TempDir()
	holdDir := filepath.Join(dir, "hold")
	os.MkdirAll(holdDir, 0755)

	notePath := filepath.Join(holdDir, "Gone.md")
	// Don't create the file — simulate deletion.

	idxPath := filepath.Join(dir, "idx.gob")
	idx, _ := akin.Open(idxPath)
	idx.AddEntries([]akin.Entry{
		{File: notePath, Offset: 0, Length: 5, Vector: []float32{1, 0, 0}},
	}, "test-model", 3)

	ix := &Indexer{
		idx:      idx,
		wikiPath: dir,
	}

	ix.processPath(notePath) // file doesn't exist → remove from index
	if ix.Entries() != 0 {
		t.Errorf("expected 0 entries after deleted-file processing, got %d", ix.Entries())
	}
	if !ix.dirty {
		t.Error("expected dirty after processing deleted file")
	}
}

func TestSaveFlushes(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "idx.gob")
	idx, _ := akin.Open(idxPath)

	ix := &Indexer{
		idx:   idx,
		dirty: true,
	}

	ix.save()
	if ix.dirty {
		t.Error("expected dirty=false after save")
	}

	// File should exist.
	if _, err := os.Stat(idxPath); os.IsNotExist(err) {
		t.Error("expected index file to exist after save")
	}
}

func TestSaveSkipsClean(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "idx.gob")
	idx, _ := akin.Open(idxPath)

	ix := &Indexer{
		idx:   idx,
		dirty: false,
	}

	ix.save()
	// File should NOT exist (never saved).
	if _, err := os.Stat(idxPath); !os.IsNotExist(err) {
		t.Error("expected no file when clean")
	}
}

func TestStartStopDrains(t *testing.T) {
	dir := t.TempDir()
	holdDir := filepath.Join(dir, "hold")
	os.MkdirAll(holdDir, 0755)

	idxPath := filepath.Join(dir, "idx.gob")
	idx, _ := akin.Open(idxPath)
	idx.AddEntries([]akin.Entry{
		{File: filepath.Join(holdDir, "A.md"), Offset: 0, Length: 1, Vector: []float32{1, 0, 0}},
	}, "test-model", 3)

	ix := &Indexer{
		idx:          idx,
		emb:          akin.NewEmbedder("test-model", "http://unused:9999"),
		ch:           make(chan string, 256),
		done:         make(chan struct{}),
		wikiPath:     dir,
		saveInterval: 100 * time.Millisecond,
	}

	ix.Start()

	// Queue a removal — should be drained on Stop.
	ix.Remove(filepath.Join(holdDir, "A.md"))
	time.Sleep(20 * time.Millisecond) // let loop pick it up

	ix.Stop()

	if ix.Entries() != 0 {
		t.Errorf("expected 0 entries after Stop drain, got %d", ix.Entries())
	}
}

func TestIndexChannelFull(t *testing.T) {
	ix := &Indexer{
		ch: make(chan string, 1),
	}
	// Fill the channel.
	ix.ch <- "first"

	// This should drop without blocking.
	ix.Index("second")

	// Drain and verify only first is there.
	got := <-ix.ch
	if got != "first" {
		t.Errorf("expected 'first', got %q", got)
	}
}

func TestIndexChannelSuccess(t *testing.T) {
	ix := &Indexer{
		ch: make(chan string, 1),
	}
	ix.Index("note.md")
	got := <-ix.ch
	if got != "note.md" {
		t.Errorf("expected 'note.md', got %q", got)
	}
}

func TestRemoveChannelFull(t *testing.T) {
	ix := &Indexer{
		ch: make(chan string, 1),
	}
	ix.ch <- "first"

	// Should drop without blocking.
	ix.Remove("second")

	got := <-ix.ch
	if got != "first" {
		t.Errorf("expected 'first', got %q", got)
	}
}

func TestNewDefaultSaveInterval(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		IndexPath: filepath.Join(dir, "idx.gob"),
		WikiPath:  dir,
		Model:     "test-model",
		OllamaURL: "http://unused:9999",
	}
	ix, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ix.saveInterval != 30*time.Second {
		t.Errorf("default saveInterval = %v, want 30s", ix.saveInterval)
	}
}

func TestQueryContextCancelled(t *testing.T) {
	ix, _ := setupIndex(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	_, err := ix.Query(ctx, "test", 5)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestQuerySuccess(t *testing.T) {
	ix, _ := setupIndex(t)

	// Set embedFunc so Query works without Ollama.
	ix.emb.SetEmbedFunc(func(_ context.Context, texts []string) ([][]float32, error) {
		vecs := make([][]float32, len(texts))
		for i := range texts {
			vecs[i] = []float32{1, 0, 0} // matches AlphaNote best
		}
		return vecs, nil
	})

	results, err := ix.Query(context.Background(), "test", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].Title != "AlphaNote" {
		t.Errorf("top result = %q, want AlphaNote", results[0].Title)
	}
	if results[0].Wiki != "hold" {
		t.Errorf("wiki = %q, want hold", results[0].Wiki)
	}
}

func TestProcessPathIndexFile(t *testing.T) {
	dir := t.TempDir()
	holdDir := filepath.Join(dir, "hold")
	os.MkdirAll(holdDir, 0755)

	notePath := filepath.Join(holdDir, "Live.md")
	os.WriteFile(notePath, []byte("title: Live\ntags:\n\nlive content here"), 0644)

	idxPath := filepath.Join(dir, "idx.gob")
	idx, _ := akin.Open(idxPath)

	emb := akin.NewEmbedder("test-model", "http://unused:9999")
	emb.SetEmbedFunc(func(_ context.Context, texts []string) ([][]float32, error) {
		vecs := make([][]float32, len(texts))
		for i := range texts {
			vecs[i] = []float32{0.5, 0.5, 0.5}
		}
		return vecs, nil
	})

	ix := &Indexer{
		idx:      idx,
		emb:      emb,
		wikiPath: dir,
	}

	ix.processPath(notePath)
	if ix.Entries() == 0 {
		t.Error("expected entries after indexing live file")
	}
	if !ix.dirty {
		t.Error("expected dirty after indexing")
	}
}

func TestProcessPathIndexFileError(t *testing.T) {
	dir := t.TempDir()
	holdDir := filepath.Join(dir, "hold")
	os.MkdirAll(holdDir, 0755)

	notePath := filepath.Join(holdDir, "Bad.md")
	os.WriteFile(notePath, []byte("title: Bad\ntags:\n\nbad content"), 0644)

	idxPath := filepath.Join(dir, "idx.gob")
	idx, _ := akin.Open(idxPath)

	emb := akin.NewEmbedder("test-model", "http://unused:9999")
	emb.SetEmbedFunc(func(_ context.Context, _ []string) ([][]float32, error) {
		return nil, context.Canceled
	})

	ix := &Indexer{
		idx:      idx,
		emb:      emb,
		wikiPath: dir,
	}

	// Should log error but not panic or mark dirty.
	ix.processPath(notePath)
	if ix.Entries() != 0 {
		t.Error("expected no entries after embed error")
	}
	if ix.dirty {
		t.Error("expected not dirty after embed error")
	}
}

func TestProcessPathContextLengthRetry(t *testing.T) {
	dir := t.TempDir()
	holdDir := filepath.Join(dir, "hold")
	os.MkdirAll(holdDir, 0755)

	notePath := filepath.Join(holdDir, "Big.md")
	os.WriteFile(notePath, []byte("title: Big\ntags:\n\nbig content"), 0644)

	idxPath := filepath.Join(dir, "idx.gob")
	idx, _ := akin.Open(idxPath)

	calls := 0
	emb := akin.NewEmbedder("test-model", "http://unused:9999")
	emb.SetEmbedFunc(func(_ context.Context, texts []string) ([][]float32, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("context length exceeded")
		}
		vecs := make([][]float32, len(texts))
		for i := range texts {
			vecs[i] = []float32{0.1, 0.2, 0.3}
		}
		return vecs, nil
	})

	ix := &Indexer{
		idx:       idx,
		emb:       emb,
		wikiPath:  dir,
		chunkSize: 2000,
	}

	ix.processPath(notePath)
	if calls < 2 {
		t.Errorf("expected retry, got %d calls", calls)
	}
	if ix.Entries() == 0 {
		t.Error("expected entries after retry succeeded")
	}
	if !ix.dirty {
		t.Error("expected dirty after successful retry")
	}
}

func TestProcessPathContextLengthGiveUp(t *testing.T) {
	dir := t.TempDir()
	holdDir := filepath.Join(dir, "hold")
	os.MkdirAll(holdDir, 0755)

	notePath := filepath.Join(holdDir, "Huge.md")
	os.WriteFile(notePath, []byte("title: Huge\ntags:\n\nhuge content"), 0644)

	idxPath := filepath.Join(dir, "idx.gob")
	idx, _ := akin.Open(idxPath)

	emb := akin.NewEmbedder("test-model", "http://unused:9999")
	emb.SetEmbedFunc(func(_ context.Context, _ []string) ([][]float32, error) {
		return nil, fmt.Errorf("context length exceeded")
	})

	ix := &Indexer{
		idx:       idx,
		emb:       emb,
		wikiPath:  dir,
		chunkSize: 100, // will halve to 50, then 25 (<50) → give up
	}

	ix.processPath(notePath)
	if ix.Entries() != 0 {
		t.Error("expected no entries after giving up")
	}
	if ix.dirty {
		t.Error("expected not dirty after giving up")
	}
}

func TestLoopTickerSaves(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "idx.gob")
	idx, _ := akin.Open(idxPath)
	idx.AddEntries([]akin.Entry{
		{File: "f.md", Offset: 0, Length: 1, Vector: []float32{1}},
	}, "m", 1)

	ix := &Indexer{
		idx:          idx,
		emb:          akin.NewEmbedder("m", "http://unused:9999"),
		ch:           make(chan string, 256),
		done:         make(chan struct{}),
		wikiPath:     dir,
		saveInterval: 50 * time.Millisecond,
		dirty:        true,
	}

	ix.Start()
	// Wait for at least one ticker fire.
	time.Sleep(120 * time.Millisecond)
	ix.Stop()

	// Index file should exist (ticker triggered save).
	if _, err := os.Stat(idxPath); os.IsNotExist(err) {
		t.Error("expected index file after ticker save")
	}
}

func TestSaveError(t *testing.T) {
	// Create an index whose path is unwritable.
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "sub", "idx.gob")
	// Don't create "sub" dir — save will fail.

	idx, _ := akin.Open(idxPath) // opens fine (empty, new)
	idx.AddEntries([]akin.Entry{
		{File: "f.md", Offset: 0, Length: 1, Vector: []float32{1}},
	}, "m", 1)

	ix := &Indexer{
		idx:   idx,
		dirty: true,
	}

	// save should log error but not panic. dirty remains true.
	ix.save()
	if !ix.dirty {
		t.Error("expected dirty to remain true after save error")
	}
}

func TestNewRemoveStaleIndexError(t *testing.T) {
	// Create a directory where the index "file" is — os.Remove will fail.
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "idx.gob")
	os.MkdirAll(idxPath, 0755) // make it a directory

	// First, we need a valid gob index to trigger model mismatch.
	// Since idxPath is a directory, Open will fail reading it.
	// We need a different setup: write a real index, then make the
	// path un-removable.

	// Actually: create a real index file with "old-model", then
	// replace it with a directory — but Open reads the file first.
	// Let's just test the error path by making a non-empty dir.
	tmpIdx := filepath.Join(dir, "real.gob")
	idx, _ := akin.Open(tmpIdx)
	idx.AddEntries([]akin.Entry{
		{File: "f.md", Offset: 0, Length: 1, Vector: []float32{1}},
	}, "old-model", 1)
	idx.Save()

	// Copy the real index to a path that has a non-removable parent.
	badDir := filepath.Join(dir, "locked")
	os.MkdirAll(badDir, 0755)
	badPath := filepath.Join(badDir, "idx.gob")
	data, _ := os.ReadFile(tmpIdx)
	os.WriteFile(badPath, data, 0644)

	// Make the directory read-only so Remove fails.
	os.Chmod(badDir, 0555)
	t.Cleanup(func() { os.Chmod(badDir, 0755) })

	cfg := Config{
		IndexPath: badPath,
		WikiPath:  dir,
		Model:     "new-model",
		OllamaURL: "http://unused:9999",
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error when index file can't be removed")
	}
}

func TestNewReopenFailsAfterRemove(t *testing.T) {
	// Create a real index with "old-model" so model mismatch triggers.
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "idx.gob")
	idx, _ := akin.Open(idxPath)
	idx.AddEntries([]akin.Entry{
		{File: "f.md", Offset: 0, Length: 1, Vector: []float32{1}},
	}, "old-model", 1)
	idx.Save()

	// Inject a failing openIndex so the re-open after Remove fails.
	orig := openIndex
	callCount := 0
	openIndex = func(path string) (*akin.Index, error) {
		callCount++
		if callCount == 1 {
			// First call: return the real index (triggers model mismatch).
			return orig(path)
		}
		// Second call: simulate failure after successful Remove.
		return nil, fmt.Errorf("injected open error")
	}
	defer func() { openIndex = orig }()

	cfg := Config{
		IndexPath: idxPath,
		WikiPath:  dir,
		Model:     "new-model",
		OllamaURL: "http://unused:9999",
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error when re-open fails after remove")
	}
	if !strings.Contains(err.Error(), "open fresh index") {
		t.Errorf("error = %q, want 'open fresh index'", err)
	}
}

func TestLoopChannelClose(t *testing.T) {
	dir := t.TempDir()
	idx, _ := akin.Open(filepath.Join(dir, "idx.gob"))
	ix := &Indexer{
		idx:          idx,
		emb:          akin.NewEmbedder("m", "http://unused:9999"),
		ch:           make(chan string, 256),
		done:         make(chan struct{}),
		wikiPath:     dir,
		saveInterval: time.Hour, // won't fire
	}

	ix.wg.Add(1)
	go ix.loop()

	// Close the channel — loop should exit via !ok path.
	close(ix.ch)
	ix.wg.Wait() // would hang if loop doesn't exit
}

func TestSetEmbedFuncCoverage(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		IndexPath: filepath.Join(dir, "idx.gob"),
		WikiPath:  dir,
		Model:     "m",
		OllamaURL: "http://unused:9999",
	}
	ix, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	ix.SetEmbedFunc(func(_ context.Context, _ []string) ([][]float32, error) {
		called = true
		return [][]float32{{1}}, nil
	})
	vecs, err := ix.emb.Embed(context.Background(), []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("expected SetEmbedFunc to install the override")
	}
	if len(vecs) != 1 {
		t.Errorf("expected 1 vector, got %d", len(vecs))
	}
}

func TestAddTestEntriesCoverage(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		IndexPath: filepath.Join(dir, "idx.gob"),
		WikiPath:  dir,
		Model:     "m",
		OllamaURL: "http://unused:9999",
	}
	ix, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ix.AddTestEntries("test.md", []float32{1, 0, 0})
	if ix.Entries() != 1 {
		t.Errorf("expected 1 entry, got %d", ix.Entries())
	}
}

func TestLoopDrainOnStop(t *testing.T) {
	dir := t.TempDir()
	holdDir := filepath.Join(dir, "hold")
	os.MkdirAll(holdDir, 0755)

	// Create two files with entries in the index.
	pathA := filepath.Join(holdDir, "A.md")
	pathB := filepath.Join(holdDir, "B.md")
	os.WriteFile(pathA, []byte("a"), 0644)
	os.WriteFile(pathB, []byte("b"), 0644)

	idx, _ := akin.Open(filepath.Join(dir, "idx.gob"))
	idx.AddEntries([]akin.Entry{
		{File: pathA, Offset: 0, Length: 1, Vector: []float32{1, 0, 0}},
		{File: pathB, Offset: 0, Length: 1, Vector: []float32{0, 1, 0}},
	}, "m", 3)

	ix := &Indexer{
		idx:          idx,
		emb:          akin.NewEmbedder("m", "http://unused:9999"),
		ch:           make(chan string, 256),
		done:         make(chan struct{}),
		wikiPath:     dir,
		saveInterval: time.Hour,
	}

	// Queue removals before starting the loop.
	ix.ch <- "\x00" + pathA
	ix.ch <- "\x00" + pathB

	ix.Start()
	// Stop immediately — forces the drain path in loop().
	ix.Stop()

	if ix.Entries() != 0 {
		t.Errorf("expected 0 entries after drain, got %d", ix.Entries())
	}
}
