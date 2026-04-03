// Package wiki manages a collection of notes in a single directory.
// All state mutations are serialized through a goroutine event loop.
package wiki

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/dan4thewin/tibbitwiki/internal/editor"
	"github.com/dan4thewin/tibbitwiki/internal/loader"
	"github.com/dan4thewin/tibbitwiki/internal/rcs"
	"github.com/dan4thewin/tibbitwiki/internal/render"
	"github.com/dan4thewin/tibbitwiki/internal/search"
	"github.com/dan4thewin/tibbitwiki/internal/store"
	"github.com/dan4thewin/tibbitwiki/internal/tid"
)

// NowFunc returns the current time. Overridden in tests.
var NowFunc = time.Now

// searchFunc wraps search.Search for testability (gomonkey + -coverprofile SIGBUS).
var searchFunc = search.Search

// ParseFunc wraps tid.Parse for testability (gomonkey + -coverprofile SIGBUS).
var ParseFunc = tid.Parse

// Sentinel errors for wiki operations. Server uses errors.Is() to
// map these to HTTP status codes.
var (
	ErrNoFile     = errors.New("no file for note")
	ErrOutOfRange = errors.New("line out of range")
	ErrAtHead     = errors.New("note is already at head")
)

func isoNow() string {
	return NowFunc().UTC().Format(time.RFC3339)
}

// Wiki manages notes in a directory with a serialized event loop.
type Wiki struct {
	Name          string
	noteDir       string
	store         *store.Store
	editor        *editor.Editor
	body          *lru.Cache[string, string]
	ops           chan func()
	done          chan struct{}
	symlinks      map[string]string // title → owner wiki name
	hub           *Hub
	attachExclude []string
	pdfConvertCmd string // command to enqueue PDF conversion (empty = disabled)
	templates     map[string]string
}

// New creates a Wiki for the given directory.
// cacheSize sets the LRU body cache capacity (number of note bodies).
func New(name, noteDir string, ed *editor.Editor, cacheSize int) *Wiki {
	cache, _ := lru.New[string, string](cacheSize)
	return &Wiki{
		Name:     name,
		noteDir:  noteDir,
		store:    store.New(),
		editor:   ed,
		body:     cache,
		ops:      make(chan func(), 64),
		done:     make(chan struct{}),
		symlinks: map[string]string{},
	}
}

// Store returns the underlying store for loader and test access.
func (w *Wiki) Store() *store.Store { return w.store }

// NoteDir returns the notes directory path.
func (w *Wiki) NoteDir() string { return w.noteDir }

// SeedBody populates the body cache for a title (test/loader use).
func (w *Wiki) SeedBody(title, text string) { w.body.Add(title, text) }

// SetSymlinks records which titles are symlinks to other wikis.
// Called once after loading, before Start.
func (w *Wiki) SetSymlinks(m map[string]string) { w.symlinks = m }

// SetHub gives this wiki access to the hub for symlink delegation.
// Called once after all wikis are created.
func (w *Wiki) SetHub(h *Hub) { w.hub = h }

// SetAttachExclude sets the list of suffixes to hide from attachment listings.
func (w *Wiki) SetAttachExclude(exts []string) { w.attachExclude = exts }

// SetPdfConvertCmd sets the command used to enqueue unconverted PDFs.
func (w *Wiki) SetPdfConvertCmd(cmd string) { w.pdfConvertCmd = cmd }

// SetTemplates sets the note body template definitions.
func (w *Wiki) SetTemplates(t map[string]string) { w.templates = t }

// ScanRCS detects notes checked out at non-head revisions by
// looking for read-only files with a CHECKOUT symbolic tag.
// Called once after loading, before Start.
func (w *Wiki) ScanRCS() {
	for _, td := range w.store.All() {
		fp := w.store.FilePath(td.Title())
		if fp == "" || !isReadOnly(fp) {
			continue
		}
		rev, err := rcs.CheckoutTag(fp, "CHECKOUT")
		if err != nil || rev == "" {
			continue
		}
		head, err := rcs.HeadRev(fp)
		if err != nil {
			continue
		}
		w.store.SetRCS(td.Title(), rev, head)
		log.Printf("wiki %s: %q at revision %s (head %s)", w.Name, td.Title(), rev, head)
	}
}

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

// Do sends an operation to the event loop and blocks until complete.
// Exported for cross-package synchronization in tests.
func (w *Wiki) Do(fn func()) { w.do(fn) }

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
// Body is loaded from LRU cache or disk on miss.
// Symlinked notes are delegated to their owning wiki.
func (w *Wiki) Get(title string, renderPlain bool) map[string]string {
	var result map[string]string
	var owner string
	w.do(func() {
		td := w.store.Get(title)
		if td == nil {
			owner = w.symlinks[title]
			return
		}
		result = w.buildNote(title, td, renderPlain)
	})
	if result == nil && owner != "" {
		if ow := w.hub.Wiki(owner); ow != nil {
			result = ow.Get(title, renderPlain)
			if result != nil {
				result["owner"] = owner
			}
		}
	}
	return result
}

// buildNote assembles a full note response. Must be called inside w.do.
func (w *Wiki) buildNote(title string, td *tid.Note, renderPlain bool) map[string]string {
	if fp := w.store.FilePath(title); fp != "" {
		touchAtime(fp)
	}
	result := make(map[string]string, len(td.Fields)+3)
	for k, v := range td.Fields {
		result[k] = v
	}
	text := w.fetchBody(title)
	result["text"] = text
	result["revision"] = fmt.Sprintf("%d", w.store.Revision(title))
	if fp := w.store.FilePath(title); fp != "" && isReadOnly(fp) {
		result["readOnly"] = "true"
	}
	if rev, head := w.store.RCS(title); rev != "" {
		result["rcsRev"] = rev
		result["rcsHead"] = head
	}
	if text != "" {
		expanded := render.ExpandTemplates(text, w.templates)
		if renderPlain {
			result["html"] = render.ToPlainHTML(expanded)
		} else {
			result["html"] = render.ToHTML(expanded)
		}
	}
	if a := w.noteAttachments(title); a != "" {
		result["attachments"] = a
	}
	return result
}

// noteAttachments returns a JSON array of attachment file names for a
// note, or "" if the note has no subdirectory or no files in it.
// Files matching any suffix in w.attachExclude are hidden.
func (w *Wiki) noteAttachments(title string) string {
	if w.noteDir == "" {
		return ""
	}
	subdir := filepath.Join(w.noteDir, title)
	entries, err := os.ReadDir(subdir)
	if err != nil {
		return ""
	}

	// Collect visible names and detect unconverted PDFs.
	has := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			has[e.Name()] = true
		}
	}
	var names []string
	var enqueue []string
	for _, e := range entries {
		if e.IsDir() || w.excluded(e.Name()) {
			continue
		}
		names = append(names, e.Name())
		if w.needsConvert(e.Name(), has) {
			enqueue = append(enqueue, filepath.Join(subdir, e.Name()))
		}
	}
	if len(enqueue) > 0 {
		w.enqueuePdfs(enqueue)
	}
	if len(names) == 0 {
		return ""
	}
	data, _ := json.Marshal(names)
	return string(data)
}

// needsConvert returns true if the file is a PDF lacking a .pdf.md sibling
// and conversion is enabled.
func (w *Wiki) needsConvert(name string, siblings map[string]bool) bool {
	return w.pdfConvertCmd != "" &&
		strings.HasSuffix(strings.ToLower(name), ".pdf") &&
		!siblings[name+".md"]
}

// excluded returns true if name matches any suffix in attachExclude.
func (w *Wiki) excluded(name string) bool {
	for _, ext := range w.attachExclude {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// enqueuePdfs fires off the configured convert command for each PDF path.
// Template specifiers: %s = PDF path, %n = wiki name, %d = wiki note dir.
// Each invocation is fire-and-forget: errors are logged, not propagated.
var execCommand = exec.Command

func (w *Wiki) enqueuePdfs(paths []string) {
	for _, p := range paths {
		parts := strings.Fields(w.pdfConvertCmd)
		for i, tok := range parts {
			tok = strings.Replace(tok, "%n", w.Name, -1)
			tok = strings.Replace(tok, "%d", w.noteDir, -1)
			parts[i] = strings.Replace(tok, "%s", p, -1)
		}
		cmd := execCommand(parts[0], parts[1:]...)
		if err := cmd.Start(); err != nil {
			log.Printf("pdf enqueue %s: %v", filepath.Base(p), err)
			continue
		}
		// Child double-forks and exits immediately; no Wait needed.
		// The brief zombie is harmless (reaped on next child spawn or exit).
	}
}

// Put saves a note. clientRev is the client's last-known revision
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

// Delete removes a note file and preserves its RCS history in last+chance/.
func (w *Wiki) Delete(title string) {
	w.do(func() {
		fp := w.store.FilePath(title)
		if fp == "" {
			return
		}
		// Move RCS archive to last+chance/ before removing the note.
		base := filepath.Base(fp)
		rcsFile := filepath.Join(filepath.Dir(fp), "RCS", base+",v")
		if _, err := os.Stat(rcsFile); err == nil {
			lcDir := filepath.Join(w.noteDir, "last+chance")
			if err := os.MkdirAll(lcDir, 0o755); err != nil {
				log.Printf("wiki %s: cannot create last+chance: %v", w.Name, err)
			} else if err := os.Rename(rcsFile, filepath.Join(lcDir, base+",v")); err != nil {
				log.Printf("wiki %s: cannot preserve %s: %v", w.Name, base+",v", err)
			}
		}
		if err := os.Remove(fp); err != nil && !os.IsNotExist(err) {
			log.Printf("wiki %s: error deleting %q: %v", w.Name, title, err)
		}
		w.store.Delete(title)
		w.body.Remove(title)
	})
}

// List returns skinny fields for all notes including access times.
// Symlinked notes are fetched live from their owning wikis.
func (w *Wiki) List() []map[string]string {
	var result []map[string]string
	var syms map[string]string
	w.do(func() {
		all := w.store.All()
		result = make([]map[string]string, 0, len(all)+len(w.symlinks))
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
		// Snapshot symlinks under the event loop lock.
		if len(w.symlinks) > 0 {
			syms = make(map[string]string, len(w.symlinks))
			for k, v := range w.symlinks {
				syms[k] = v
			}
		}
	})
	// Append symlinked notes from owner wikis.
	for title, owner := range syms {
		ow := w.hub.Wiki(owner)
		if ow == nil {
			continue
		}
		ow.do(func() {
			td := ow.store.Get(title)
			if td == nil {
				return
			}
			fields := td.SkinnyFields()
			fields["revision"] = fmt.Sprintf("%d", ow.store.Revision(title))
			fields["owner"] = owner
			if fp := ow.store.FilePath(title); fp != "" {
				if at := fileAtime(fp); at != "" {
					fields["accessed"] = at
				}
			}
			result = append(result, fields)
		})
	}
	return result
}

// SearchResult holds a search match with title and matching lines.
type SearchResult struct {
	Title   string   `json:"title"`
	Matches []string `json:"matches"`
}

// RevisionsResult holds revision history for a note.
type RevisionsResult struct {
	Revisions []string `json:"revisions"`
	DiffHTML  string   `json:"diffHTML"`
	IsHead    bool     `json:"isHead"`
}

// Revisions returns the revision history for a note: list of
// revision strings, concatenated diff HTML for the most recent
// maxDiffs pairs, and whether the working file is at head.
func (w *Wiki) Revisions(title string, maxDiffs int) (RevisionsResult, error) {
	var result RevisionsResult
	var err error
	w.do(func() {
		fp := w.store.FilePath(title)
		if fp == "" {
			err = ErrNoFile
			return
		}
		head, e := rcs.HeadRev(fp)
		if e != nil {
			err = e
			return
		}
		result.IsHead = !isReadOnly(fp)
		result.Revisions = buildRevList(head)
		result.DiffHTML = buildDiffHTML(fp, result.Revisions, maxDiffs)
	})
	return result, err
}

// CheckoutRevision checks out a specific revision (read-only) or
// restores the head (writable) when rev is empty.
// On checkout, sets the CHECKOUT symbolic tag and records RCS state.
// On restore, deletes the tag and clears RCS state.
func (w *Wiki) CheckoutRevision(title, rev string) error {
	var err error
	w.do(func() {
		fp := w.store.FilePath(title)
		if fp == "" {
			err = ErrNoFile
			return
		}
		if rev == "" {
			err = rcs.CheckoutHead(fp)
			if err == nil {
				rcs.DeleteTag(fp, "CHECKOUT")
				w.store.SetRCS(title, "", "")
				// Seed the body cache and store so the watcher sees
				// restored content as unchanged and skips checkin.
				if data, e := os.ReadFile(fp); e == nil {
					if td, e := ParseFunc(data); e == nil {
						w.body.Add(title, td.Text())
						w.store.PutWithPath(td, fp)
					}
				}
			}
		} else {
			err = rcs.Checkout(fp, rev)
			if err == nil {
				head, _ := rcs.HeadRev(fp)
				rcs.SetTag(fp, "CHECKOUT", rev)
				w.store.SetRCS(title, rev, head)
				// Seed the body cache and store with the checked-out
				// content so the watcher (and later promote) see it
				// as current.
				if data, e := os.ReadFile(fp); e == nil {
					if td, e := ParseFunc(data); e == nil {
						w.body.Add(title, td.Text())
						w.store.PutWithPath(td, fp)
					}
				}
			}
		}
	})
	return err
}

// IsReadOnly returns true when a note's file is read-only (checked
// out at a non-head revision).
func (w *Wiki) IsReadOnly(title string) bool {
	var ro bool
	w.do(func() {
		fp := w.store.FilePath(title)
		if fp != "" {
			ro = isReadOnly(fp)
		}
	})
	return ro
}

// PromoteRevision checks in the current (old) working file as the
// new head revision, then clears the CHECKOUT tag and RCS state.
func (w *Wiki) PromoteRevision(title string) error {
	var err error
	w.do(func() {
		fp := w.store.FilePath(title)
		if fp == "" {
			err = ErrNoFile
			return
		}
		if !isReadOnly(fp) {
			err = ErrAtHead
			return
		}
		if err = rcs.ForceCheckin(fp); err != nil {
			return
		}
		rcs.DeleteTag(fp, "CHECKOUT")
		w.store.SetRCS(title, "", "")
	})
	return err
}

func isReadOnly(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.Mode().Perm()&0200 == 0
}

// buildRevList generates ["1.1", "1.2", ..., "1.N"] from head "1.N".
func buildRevList(head string) []string {
	// Parse "1.N" to get N.
	var major, minor int
	fmt.Sscanf(head, "%d.%d", &major, &minor)
	revs := make([]string, 0, minor)
	for i := 1; i <= minor; i++ {
		revs = append(revs, fmt.Sprintf("%d.%d", major, i))
	}
	return revs
}

// buildDiffHTML runs rcsdiff between consecutive revision pairs
// (most recent maxDiffs pairs) and returns syntax-highlighted HTML.
func buildDiffHTML(path string, revs []string, maxDiffs int) string {
	if len(revs) < 2 {
		return ""
	}
	start := 0
	if len(revs)-1 > maxDiffs {
		start = len(revs) - 1 - maxDiffs
	}
	var buf strings.Builder
	for i := start; i < len(revs)-1; i++ {
		diff, err := rcs.DiffRange(path, revs[i], revs[i+1])
		if err != nil || diff == "" {
			continue
		}
		fmt.Fprintf(&buf, "<h4>%s → %s</h4>\n", revs[i], revs[i+1])
		buf.WriteString(render.DiffToHTML(diff))
	}
	return buf.String()
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
		hits, e := searchFunc(w.noteDir, query)
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
			err = ErrNoFile
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
		td, e := ParseFunc(newData)
		if e != nil {
			err = fmt.Errorf("parse error: %w", e)
			return
		}
		td.Fields["modified"] = isoNow()
		bodyText := td.Text()
		w.body.Add(title, bodyText)
		w.store.PutWithPath(td, fp)
		html = render.ToHTML(render.ExpandTemplates(bodyText, w.templates))
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
			err = fmt.Errorf("no file path for note: %w", ErrNoFile)
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

// NewDraft creates a Draft.md file and opens it in the editor.
// If Draft.md already exists, it just opens the editor.
// The file has no title; the watcher ignores it until the user
// writes one, at which point rename-on-mismatch kicks in.
func (w *Wiki) NewDraft() (string, error) {
	var fp string
	var err error
	w.do(func() {
		if w.editor == nil {
			err = fmt.Errorf("editor not configured")
			return
		}
		fp = filepath.Join(w.noteDir, "Draft.md")
		if _, statErr := os.Stat(fp); statErr != nil {
			now := isoNow()
			td := &tid.Note{Fields: map[string]string{
				"title":    "",
				"tags":     "",
				"created":  now,
				"modified": now,
			}}
			if writeErr := os.WriteFile(fp, tid.Serialize(td), 0644); writeErr != nil {
				err = writeErr
				return
			}
		}
		err = w.editor.Open(fp)
	})
	return fp, err
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
	if text := w.fetchBody(title); text != "" {
		f["text"] = text
	}
}

// persistNote saves a note to disk and RCS, then updates the store.
// Deliberate design choice: disk/RCS failures are logged but do not
// fail the operation — the in-memory store is always updated. This
// favors availability (note is accessible until restart) over strict
// durability. Disk failures at this layer indicate a systemic problem
// (full disk, permissions) that logging is the appropriate response to.
func (w *Wiki) persistNote(td *tid.Note, title string) {
	w.body.Add(title, td.Text())
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
	// Known symlink with unchanged owner — nothing to do.
	// This avoids reading/parsing on every watcher fire caused by
	// the owning wiki's touchAtime modifying the shared real file.
	if owner := loader.SymlinkOwner(path); owner != "" {
		title := tid.FilenameToTitle(filepath.Base(path))
		if w.symlinks[title] == owner {
			return
		}
		// New or changed symlink — parse to get the real title.
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("wiki %s: error reading symlink %s: %v", w.Name, path, err)
			return
		}
		td, err := ParseFunc(data)
		if err != nil {
			log.Printf("wiki %s: error parsing symlink %s: %v", w.Name, path, err)
			return
		}
		if td.Title() != "" {
			w.symlinks[td.Title()] = owner
			log.Printf("wiki %s: symlink %q → %s", w.Name, td.Title(), owner)
		}
		return
	}

	// Handle file deletion: clean up store, body cache, and symlinks.
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			title := tid.FilenameToTitle(filepath.Base(path))
			w.store.Delete(title)
			w.body.Remove(title)
			delete(w.symlinks, title)
			log.Printf("wiki %s: removed %q", w.Name, title)
			return
		}
		log.Printf("wiki %s: error reading %s: %v", w.Name, path, err)
		return
	}

	td, err := ParseFunc(data)
	if err != nil {
		log.Printf("wiki %s: error parsing %s: %v", w.Name, path, err)
		return
	}
	if td.Title() == "" {
		return
	}
	fileTitle := tid.FilenameToTitle(filepath.Base(path))
	if td.Title() != fileTitle {
		w.renameAndRescan(path, fileTitle, td.Title())
		return
	}
	if w.isUnchanged(td) {
		return
	}
	w.body.Add(td.Title(), td.Text())
	td.Fields["modified"] = isoNow()
	if err := os.WriteFile(path, tid.Serialize(td), 0644); err != nil {
		log.Printf("wiki %s: error writing %q: %v", w.Name, td.Title(), err)
	}
	w.store.PutWithPath(td, path)
	log.Printf("wiki %s: reloaded %q", w.Name, td.Title())
	// Skip checkin for read-only files (checked out at non-head revision).
	if isReadOnly(path) {
		return
	}
	if err := rcs.Checkin(path); err != nil {
		log.Printf("wiki %s: rcs checkin %s: %v", w.Name, path, err)
	}
}

func (w *Wiki) renameAndRescan(path, oldTitle, newTitle string) {
	newPath := filepath.Join(filepath.Dir(path), tid.TitleToFilename(newTitle))
	if err := os.Rename(path, newPath); err != nil {
		log.Printf("wiki %s: rename %s → %s: %v", w.Name, path, newPath, err)
		return
	}
	log.Printf("wiki %s: renamed %q → %q", w.Name, oldTitle, newTitle)
	// Rename attachment directory if it exists.
	oldDir := filepath.Join(w.noteDir, oldTitle)
	newDir := filepath.Join(w.noteDir, newTitle)
	if info, err := os.Stat(oldDir); err == nil && info.IsDir() {
		if err := os.Rename(oldDir, newDir); err != nil {
			log.Printf("wiki %s: rename attachments %s → %s: %v", w.Name, oldDir, newDir, err)
		}
	}
	// Rename RCS archive to preserve history under the new name.
	rcsDir := filepath.Join(w.noteDir, "RCS")
	oldArchive := filepath.Join(rcsDir, tid.TitleToFilename(oldTitle)+",v")
	newArchive := filepath.Join(rcsDir, tid.TitleToFilename(newTitle)+",v")
	if _, err := os.Stat(oldArchive); err == nil {
		if err := os.Rename(oldArchive, newArchive); err != nil {
			log.Printf("wiki %s: rename RCS archive: %v", w.Name, err)
		}
	}
	if err := rcs.Checkin(newPath); err != nil {
		log.Printf("wiki %s: rcs checkin %s: %v", w.Name, newPath, err)
	}
	w.rescan()
}

func (w *Wiki) isUnchanged(td *tid.Note) bool {
	existing := w.store.Get(td.Title())
	if existing == nil {
		return false
	}
	if !metaFieldsEqual(existing.Fields, td.Fields) {
		return false
	}
	// Check body against cache. If the body was evicted, metadata
	// match alone is sufficient — a watcher echo of our own write
	// should not create a phantom RCS revision.
	cached, ok := w.body.Get(td.Title())
	if !ok {
		return true
	}
	return cached == td.Text()
}

// rescan clears all state and re-reads every .md file from noteDir.
func (w *Wiki) rescan() {
	w.store.Clear()
	w.body.Purge()
	entries, err := os.ReadDir(w.noteDir)
	if err != nil {
		log.Printf("wiki %s: rescan readdir: %v", w.Name, err)
		return
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(w.noteDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("wiki %s: rescan skip %s: %v", w.Name, path, err)
			continue
		}
		td, err := ParseFunc(data)
		if err != nil {
			log.Printf("wiki %s: rescan skip %s: %v", w.Name, path, err)
			continue
		}
		w.store.PutWithPath(td, path)
		w.body.Add(td.Title(), td.Text())
		count++
	}
	log.Printf("wiki %s: rescan loaded %d notes", w.Name, count)
}

// fetchBody returns the body text for a note, checking the LRU
// cache first and falling back to a disk read on miss.
func (w *Wiki) fetchBody(title string) string {
	if text, ok := w.body.Get(title); ok {
		return text
	}
	fp := w.store.FilePath(title)
	if fp == "" {
		return ""
	}
	data, err := os.ReadFile(fp)
	if err != nil {
		log.Printf("wiki %s: reading body %q: %v", w.Name, title, err)
		return ""
	}
	td, err := ParseFunc(data)
	if err != nil {
		log.Printf("wiki %s: parsing body %q: %v", w.Name, title, err)
		return ""
	}
	text := td.Text()
	w.body.Add(title, text)
	return text
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

// metaFieldsEqual compares metadata fields (everything except text).
// stored has no text (stripped by store); parsed may have text.
// managedField returns true for server-managed timestamps that may
// differ between store (set by isoNow) and disk (may be absent).
func managedField(k string) bool {
	return k == "modified" || k == "created"
}

func metaFieldsEqual(stored, parsed map[string]string) bool {
	parsedMeta := 0
	for k := range parsed {
		if k != "text" && !managedField(k) {
			parsedMeta++
		}
	}
	storedMeta := 0
	for k := range stored {
		if !managedField(k) {
			storedMeta++
		}
	}
	if storedMeta != parsedMeta {
		return false
	}
	for k, v := range stored {
		if managedField(k) {
			continue
		}
		if parsed[k] != v {
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
		return nil, fmt.Errorf("line %d: %w", line, ErrOutOfRange)
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
