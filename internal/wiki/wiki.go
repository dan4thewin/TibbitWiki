// Package wiki manages a collection of notes in a single directory.
// All state mutations are serialized through a goroutine event loop.
package wiki

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dan4thewin/tibbitwiki/internal/editor"
	"github.com/dan4thewin/tibbitwiki/internal/rcs"
	"github.com/dan4thewin/tibbitwiki/internal/render"
	"github.com/dan4thewin/tibbitwiki/internal/search"
	"github.com/dan4thewin/tibbitwiki/internal/store"
	"github.com/dan4thewin/tibbitwiki/internal/tid"
)

// NowFunc returns the current time. Overridden in tests.
var NowFunc = time.Now

func isoNow() string {
	return NowFunc().UTC().Format(time.RFC3339)
}

// Wiki manages notes in a directory with a serialized event loop.
type Wiki struct {
	Name    string
	noteDir string
	store   *store.Store
	editor  *editor.Editor
	ops     chan func()
	done    chan struct{}
}

// New creates a Wiki for the given directory.
func New(name, noteDir string, ed *editor.Editor) *Wiki {
	return &Wiki{
		Name:    name,
		noteDir: noteDir,
		store:   store.New(),
		editor:  ed,
		ops:     make(chan func(), 64),
		done:    make(chan struct{}),
	}
}

// Store returns the underlying store for loader and test access.
func (w *Wiki) Store() *store.Store { return w.store }

// NoteDir returns the notes directory path.
func (w *Wiki) NoteDir() string { return w.noteDir }

// Start launches the event loop goroutine.
func (w *Wiki) Start() { go w.loop() }

// Stop halts the event loop.
func (w *Wiki) Stop() { close(w.done) }

func (w *Wiki) loop() {
	for {
		select {
		case <-w.done:
			return
		case op := <-w.ops:
			op()
		}
	}
}

// do sends an operation to the event loop and blocks until complete.
func (w *Wiki) do(fn func()) {
	ch := make(chan struct{})
	w.ops <- func() {
		fn()
		close(ch)
	}
	<-ch
}

// Get returns a note's fields with rendered HTML, or nil if not found.
func (w *Wiki) Get(title string, renderPlain bool) map[string]string {
	var result map[string]string
	w.do(func() {
		td := w.store.Get(title)
		if td == nil {
			return
		}
		if fp := w.store.FilePath(title); fp != "" {
			touchAtime(fp)
		}
		result = make(map[string]string, len(td.Fields)+2)
		for k, v := range td.Fields {
			result[k] = v
		}
		result["revision"] = fmt.Sprintf("%d", w.store.Revision(title))
		if text := result["text"]; text != "" {
			if renderPlain {
				result["html"] = render.ToPlainHTML(text)
			} else {
				result["html"] = render.ToHTML(text)
			}
		}
	})
	return result
}

// Put saves a note. clientRev is the client's last-known revision
// (empty if new). Returns the new revision and whether a conflict
// was detected.
func (w *Wiki) Put(title string, fields map[string]string, clientRev string) (int, bool) {
	var rev int
	var conflict bool
	w.do(func() {
		if clientRev != "" {
			serverRev := w.store.Revision(title)
			if serverRev > 0 && clientRev != fmt.Sprintf("%d", serverRev) {
				conflict = true
				return
			}
		}
		w.stampTimestamps(title, fields)
		w.applySkinny(title, fields)
		td := &tid.Note{Fields: fields}
		w.persistNote(td, title)
		rev = w.store.Revision(title)
	})
	return rev, conflict
}

// Delete removes a note and its file on disk.
func (w *Wiki) Delete(title string) {
	w.do(func() {
		if fp := w.store.FilePath(title); fp != "" {
			if err := os.Remove(fp); err != nil && !os.IsNotExist(err) {
				log.Printf("wiki %s: error deleting %q: %v", w.Name, title, err)
			}
		}
		w.store.Delete(title)
	})
}

// List returns skinny fields for all notes including access times.
func (w *Wiki) List() []map[string]string {
	var result []map[string]string
	w.do(func() {
		all := w.store.All()
		result = make([]map[string]string, 0, len(all))
		for _, td := range all {
			fields := td.SkinnyFields()
			fields["revision"] = fmt.Sprintf("%d", w.store.Revision(td.Title()))
			if fp := w.store.FilePath(td.Title()); fp != "" {
				if at := fileAtime(fp); at != "" {
					fields["accessed"] = at
				}
			}
			result = append(result, fields)
		}
	})
	return result
}

// SearchResult holds a search match with title and matching lines.
type SearchResult struct {
	Title   string   `json:"title"`
	Matches []string `json:"matches"`
}

// Search runs a full-text search over the wiki's notes.
func (w *Wiki) Search(query string) ([]SearchResult, error) {
	var results []SearchResult
	var err error
	w.do(func() {
		if w.noteDir == "" {
			err = fmt.Errorf("search not available")
			return
		}
		hits, e := search.Search(w.noteDir, query)
		if e != nil {
			err = e
			return
		}
		for _, h := range hits {
			name := search.FileToTitle(h.File)
			if td := w.store.Get(name); td != nil {
				matches := h.Lines
				if matches == nil {
					matches = []string{}
				}
				results = append(results, SearchResult{
					Title:   td.Fields["title"],
					Matches: matches,
				})
			}
		}
		if results == nil {
			results = []SearchResult{}
		}
	})
	return results, err
}

// LineEdit replaces or inserts a line in a note's source text.
// Returns the re-rendered HTML and new revision.
func (w *Wiki) LineEdit(title string, line int, text string, insert bool) (string, int, error) {
	var html string
	var rev int
	var err error
	w.do(func() {
		fp := w.store.FilePath(title)
		if fp == "" {
			err = fmt.Errorf("no file for note")
			return
		}
		newData, e := spliceFileLine(fp, line, text, insert)
		if e != nil {
			err = e
			return
		}
		if e := os.WriteFile(fp, newData, 0644); e != nil {
			err = fmt.Errorf("writing file: %w", e)
			return
		}
		if e := rcs.Checkin(fp); e != nil {
			log.Printf("wiki %s: rcs checkin %q: %v", w.Name, title, e)
		}
		td, e := tid.Parse(newData)
		if e != nil {
			err = fmt.Errorf("parse error: %w", e)
			return
		}
		td.Fields["modified"] = isoNow()
		w.store.PutWithPath(td, fp)
		html = render.ToHTML(td.Text())
		rev = w.store.Revision(title)
	})
	return html, rev, err
}

// EditExternal launches the configured editor for a note.
func (w *Wiki) EditExternal(title string) error {
	var err error
	w.do(func() {
		fp := w.store.FilePath(title)
		if fp == "" {
			err = fmt.Errorf("no file path for note")
			return
		}
		if w.editor == nil {
			err = fmt.Errorf("editor not configured")
			return
		}
		err = w.editor.Open(fp)
	})
	return err
}

// Reload handles a file change notification from the watcher.
// Non-blocking: the operation is queued for the event loop.
func (w *Wiki) Reload(path string) {
	w.ops <- func() {
		w.reloadFile(path)
	}
}

// --- internal helpers ---

func (w *Wiki) stampTimestamps(title string, f map[string]string) {
	now := isoNow()
	f["modified"] = now
	if existing := w.store.Get(title); existing != nil {
		if c := existing.Fields["created"]; c != "" && f["created"] == "" {
			f["created"] = c
		}
	} else {
		f["created"] = now
	}
}

func (w *Wiki) applySkinny(title string, f map[string]string) {
	if f["_is_skinny"] == "" {
		return
	}
	delete(f, "_is_skinny")
	if existing := w.store.Get(title); existing != nil {
		if t := existing.Text(); t != "" {
			f["text"] = t
		}
	}
}

func (w *Wiki) persistNote(td *tid.Note, title string) {
	filePath := w.store.FilePath(title)
	if filePath == "" && w.noteDir != "" {
		filePath = filepath.Join(w.noteDir, tid.TitleToFilename(title))
	}
	if filePath == "" {
		w.store.Put(td)
		return
	}
	if err := os.WriteFile(filePath, tid.Serialize(td), 0644); err != nil {
		log.Printf("wiki %s: error writing %q: %v", w.Name, title, err)
	} else if err := rcs.Checkin(filePath); err != nil {
		log.Printf("wiki %s: rcs checkin %q: %v", w.Name, title, err)
	}
	w.store.PutWithPath(td, filePath)
}

func (w *Wiki) reloadFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			title := tid.FilenameToTitle(filepath.Base(path))
			w.store.Delete(title)
			log.Printf("wiki %s: removed %q", w.Name, title)
			return
		}
		log.Printf("wiki %s: error reading %s: %v", w.Name, path, err)
		return
	}
	td, err := tid.Parse(data)
	if err != nil {
		log.Printf("wiki %s: error parsing %s: %v", w.Name, path, err)
		return
	}
	if existing := w.store.Get(td.Title()); existing != nil {
		if fieldsEqual(existing.Fields, td.Fields) {
			return
		}
	}
	w.store.PutWithPath(td, path)
	log.Printf("wiki %s: reloaded %q", w.Name, td.Title())
	if err := rcs.Checkin(path); err != nil {
		log.Printf("wiki %s: rcs checkin %s: %v", w.Name, path, err)
	}
}

func touchAtime(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	_ = os.Chtimes(path, NowFunc(), info.ModTime())
}

func fileAtime(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return atime(info).UTC().Format(time.RFC3339)
}

func fieldsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func spliceFileLine(filePath string, line int, text string, insert bool) ([]byte, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	bodyStart := 0
	for i, l := range lines {
		if l == "" {
			bodyStart = i + 1
			break
		}
	}
	absLine := bodyStart + line
	if absLine > len(lines) {
		return nil, fmt.Errorf("line %d out of range", line)
	}
	if insert {
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:absLine]...)
		newLines = append(newLines, text)
		newLines = append(newLines, lines[absLine:]...)
		lines = newLines
	} else {
		lines[absLine-1] = text
	}
	return []byte(strings.Join(lines, "\n")), nil
}
