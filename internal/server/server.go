// Package server implements the TibbitWiki HTTP API.
package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/dan4thewin/tibbitwiki/internal/editor"
	"github.com/dan4thewin/tibbitwiki/internal/rcs"
	"github.com/dan4thewin/tibbitwiki/internal/render"
	"github.com/dan4thewin/tibbitwiki/internal/tid"
	"github.com/dan4thewin/tibbitwiki/internal/wiki"
)

//go:embed index.html
var defaultIndexHTML []byte

type contextKey string

const wikiContextKey contextKey = "wiki"

// wikiFrom extracts the Wiki from the request context.
func wikiFrom(r *http.Request) *wiki.Wiki {
	return r.Context().Value(wikiContextKey).(*wiki.Wiki)
}

// Server is the HTTP handler for the TibbitWiki API.
type Server struct {
	hub        *wiki.Hub
	editor     *editor.Editor
	themeRev   *atomic.Int64
	pollMS     int
	maxDiffs   int
	indexHTML  string
	wikiPath   string
	configPath string
	restartCh  chan struct{}
	mux        *http.ServeMux
}

// New creates a Server backed by the given Hub.
// themeRev is bumped externally when per-wiki CSS is regenerated.
func New(hub *wiki.Hub, ed *editor.Editor, themeRev *atomic.Int64, pollMS, maxDiffs int, indexHTML, wikiPath, configPath string) *Server {
	srv := &Server{
		hub:        hub,
		editor:     ed,
		themeRev:   themeRev,
		pollMS:     pollMS,
		maxDiffs:   maxDiffs,
		indexHTML:  indexHTML,
		wikiPath:   wikiPath,
		configPath: configPath,
		restartCh:  make(chan struct{}, 1),
		mux:        http.NewServeMux(),
	}
	srv.routes()
	return srv
}

// RestartCh returns a channel that receives when a restart is requested.
func (s *Server) RestartCh() <-chan struct{} {
	return s.restartCh
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/status", s.handleStatus)
	s.mux.HandleFunc("/notes", s.handleListNotes)
	s.mux.HandleFunc("/notes/", s.handleNote)
	s.mux.HandleFunc("/line-edit/", s.handleLineEdit)
	s.mux.HandleFunc("/edit-external/", s.handleEditExternal)
	s.mux.HandleFunc("/new-draft", s.handleNewDraft)
	s.mux.HandleFunc("/edit-config", s.handleEditConfig)
	s.mux.HandleFunc("/restart", s.handleRestart)
	s.mux.HandleFunc("/theme.css", s.handleThemeCSS)
	s.mux.HandleFunc("/search", s.handleSearch)
	s.mux.HandleFunc("/revisions/", s.handleRevisions)
}

// ServeHTTP extracts the wiki name from the first path segment,
// injects the Wiki into the request context, and dispatches to
// the internal mux with the prefix stripped.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	if r.URL.Path == "/" {
		names := s.hub.Names()
		if len(names) == 1 {
			http.Redirect(w, r, "/"+names[0]+"/", http.StatusFound)
			return
		}
		s.handleWikiSelector(w, r)
		return
	}

	name, rest := splitPrefix(r.URL.Path)
	wk := s.hub.Wiki(name)
	if wk == nil {
		http.NotFound(w, r)
		return
	}

	r2 := r.Clone(context.WithValue(r.Context(), wikiContextKey, wk))
	r2.URL.Path = rest
	s.mux.ServeHTTP(w, r2)
}

// splitPrefix splits "/{name}/rest/path" into name and "/rest/path".
// For "/{name}" with no trailing content, rest is "/".
func splitPrefix(path string) (name, rest string) {
	s := path[1:] // strip leading /
	i := strings.IndexByte(s, '/')
	if i == -1 {
		return s, "/"
	}
	return s[:i], s[i:]
}

// handleWikiSelector lists available wikis as links.
func (s *Server) handleWikiSelector(w http.ResponseWriter, r *http.Request) {
	names := s.hub.Names()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>TibbitWiki</title>
<style>
:root {
  --tw-bg: #1a1a2e; --tw-fg: #e0e0e0; --tw-note_bg: #16213e;
  --tw-accent: #6cacee; --tw-accent_hover: #8ecaff;
  --tw-border: #333; --tw-note_radius: 6px;
  --tw-font_family: system-ui, -apple-system, BlinkMacSystemFont, sans-serif;
  --tw-title_fg: #e0e0e0;
}
body { margin: 0; padding: 40px; background: var(--tw-bg); color: var(--tw-fg); font-family: var(--tw-font_family); }
h1 { color: var(--tw-title_fg, var(--tw-fg)); }
ul { list-style: none; padding: 0; }
li { margin-bottom: 8px; }
li a { display: inline-block; padding: 10px 20px; background: var(--tw-note_bg); border: 1px solid var(--tw-border); border-radius: var(--tw-note_radius); color: var(--tw-accent); text-decoration: none; font-size: 18px; }
li a:hover { color: var(--tw-accent_hover); border-color: var(--tw-accent); }
</style></head><body>`)
	fmt.Fprint(w, "<h1>TibbitWiki</h1><ul>")
	for _, name := range names {
		esc := html.EscapeString(name)
		fmt.Fprintf(w, `<li><a href="/%s/">%s</a></li>`, esc, esc)
	}
	fmt.Fprint(w, "</ul></body></html>")
}

func (s *Server) handleThemeCSS(w http.ResponseWriter, r *http.Request) {
	wk := wikiFrom(r)
	cssPath := filepath.Join(s.wikiPath, wk.Name+".css")
	http.ServeFile(w, r, cssPath)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}
	results, err := wikiFrom(r).Search(q)
	if err != nil {
		http.Error(w, fmt.Sprintf("search error: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// handleIndex serves the index HTML page, or static files from
// note subdirectories (e.g. /Meeting/report.pdf).
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		if s.indexHTML != "" {
			http.ServeFile(w, r, s.indexHTML)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(defaultIndexHTML)
		return
	}
	s.serveAttachment(w, r)
}

// serveAttachment serves static files from note subdirectories.
// A path like /Meeting/report.pdf maps to {noteDir}/Meeting/report.pdf.
// Directories serve their index.html if present.
func (s *Server) serveAttachment(w http.ResponseWriter, r *http.Request) {
	wk := wikiFrom(r)
	clean := filepath.Clean(r.URL.Path)
	fp := filepath.Join(wk.NoteDir(), clean)
	// Ensure the resolved path stays within the note directory.
	if !strings.HasPrefix(fp, wk.NoteDir()+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	fi, err := os.Stat(fp)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Directories: redirect to add trailing slash so relative URLs
	// resolve correctly, then serve index.html if present.
	if fi.IsDir() {
		if !strings.HasSuffix(r.URL.Path, "/") {
			// Relative redirect preserving the wiki prefix.
			w.Header().Set("Location", filepath.Base(clean)+"/")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		fp = filepath.Join(fp, "index.html")
		fi, err = os.Stat(fp)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	} else {
		// Non-directory files require a subdir (e.g. /Meeting/file.pdf).
		// Top-level files are notes, not attachments.
		if !strings.Contains(clean[1:], string(filepath.Separator)) {
			http.NotFound(w, r)
			return
		}
	}
	// ServeContent avoids http.ServeFile's index.html redirect.
	// Markdown files are rendered through goldmark and served as HTML.
	if strings.HasSuffix(fp, ".md") {
		data, err := os.ReadFile(fp)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		body := render.AttachmentToHTML(string(data))
		page := render.AttachmentPage(body)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(page))
		return
	}
	f, err := os.Open(fp)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	http.ServeContent(w, r, fp, fi.ModTime(), f)
}

// handleStatus returns server status as JSON.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"username":  "DG",
		"anonymous": false,
		"read_only": false,
		"space":     map[string]string{"recipe": "default"},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleListNotes returns all notes without their text field.
func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Theme-Rev", strconv.FormatInt(s.themeRev.Load(), 10))
	w.Header().Set("X-Poll-Interval", strconv.Itoa(s.pollMS))
	json.NewEncoder(w).Encode(wikiFrom(r).List())
}

// handleNote dispatches GET, PUT and DELETE for a single note.
func (s *Server) handleNote(w http.ResponseWriter, r *http.Request) {
	title := extractTitle(r.URL.Path, "/notes/")
	if title == "" {
		http.Error(w, "missing note title", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getNote(w, r, title)
	case http.MethodPut:
		s.putNote(w, r, title)
	case http.MethodDelete:
		s.deleteNote(w, r, title)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// getNote returns a single note as JSON with all fields.
func (s *Server) getNote(w http.ResponseWriter, r *http.Request, title string) {
	wk := wikiFrom(r)
	renderPlain := r.URL.Query().Get("render") == "plain"
	fields := wk.Get(title, renderPlain)
	if fields == nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", fmt.Sprintf("\"%s/%s/%s:\"", wk.Name, title, fields["revision"]))
	json.NewEncoder(w).Encode(fields)
}

// putNote saves a note from the request body.
func (s *Server) putNote(w http.ResponseWriter, r *http.Request, title string) {
	wk := wikiFrom(r)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	// Flatten any nested "fields" object.
	if nested, ok := raw["fields"].(map[string]interface{}); ok {
		for k, v := range nested {
			raw[k] = v
		}
		delete(raw, "fields")
	}
	clientRev, _ := raw["revision"].(string)
	// Build string fields, skipping revision.
	fields := make(map[string]string)
	for k, v := range raw {
		if k == "revision" {
			continue
		}
		fields[k] = fmt.Sprintf("%v", v)
	}
	fields["title"] = title

	rev, conflict := wk.Put(title, fields, clientRev)
	if conflict {
		w.WriteHeader(http.StatusConflict)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%s/%s/%d:\"", wk.Name, title, rev))
	w.WriteHeader(http.StatusNoContent)
}

// deleteNote removes a note.
func (s *Server) deleteNote(w http.ResponseWriter, r *http.Request, title string) {
	wikiFrom(r).Delete(title)
	w.WriteHeader(http.StatusNoContent)
}

// handleEditExternal launches the configured editor for a note.
func (s *Server) handleEditExternal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	title := extractTitle(r.URL.Path, "/edit-external/")
	if title == "" {
		http.Error(w, "missing note title", http.StatusBadRequest)
		return
	}
	if err := wikiFrom(r).EditExternal(title); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, wiki.ErrNoFile) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleNewDraft creates a Draft.md and opens the editor.
func (s *Server) handleNewDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := wikiFrom(r).NewDraft(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleEditConfig launches the editor on the config file.
func (s *Server) handleEditConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.editor == nil {
		http.Error(w, "editor not configured", http.StatusInternalServerError)
		return
	}
	if s.configPath == "" {
		http.Error(w, "no config file", http.StatusNotFound)
		return
	}
	if err := s.editor.Open(s.configPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRestart signals main to gracefully shut down and re-exec.
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	select {
	case s.restartCh <- struct{}{}:
		w.WriteHeader(http.StatusNoContent)
	default:
		// Already restarting.
		w.WriteHeader(http.StatusNoContent)
	}
}
func (s *Server) handleLineEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	title := extractTitle(r.URL.Path, "/line-edit/")
	if title == "" {
		http.Error(w, "missing note title", http.StatusBadRequest)
		return
	}
	var req struct {
		Line   int    `json:"line"`
		Text   string `json:"text"`
		Insert bool   `json:"insert"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Line < 1 {
		http.Error(w, "line must be >= 1", http.StatusBadRequest)
		return
	}
	html, rev, err := wikiFrom(r).LineEdit(title, req.Line, req.Text, req.Insert)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, wiki.ErrOutOfRange) {
			status = http.StatusBadRequest
		} else if errors.Is(err, wiki.ErrNoFile) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"html":     html,
		"revision": fmt.Sprintf("%d", rev),
	})
}

// handleRevisions dispatches GET and POST for revision operations.
// GET /revisions/{title} returns revision history with diffs.
// POST /revisions/{title}/checkout?rev=1.3 checks out a revision.
// POST /revisions/{title}/promote promotes the checked-out revision.
func (s *Server) handleRevisions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/revisions/")
	if path == "" {
		http.Error(w, "missing note title", http.StatusBadRequest)
		return
	}
	// Check for /checkout suffix.
	if strings.HasSuffix(path, "/checkout") {
		title := strings.TrimSuffix(path, "/checkout")
		if !tid.ValidTitle(title) {
			http.Error(w, "invalid note title", http.StatusBadRequest)
			return
		}
		s.checkoutRevision(w, r, title)
		return
	}
	// Check for /promote suffix.
	if strings.HasSuffix(path, "/promote") {
		title := strings.TrimSuffix(path, "/promote")
		if !tid.ValidTitle(title) {
			http.Error(w, "invalid note title", http.StatusBadRequest)
			return
		}
		s.promoteRevision(w, r, title)
		return
	}
	if !tid.ValidTitle(path) {
		http.Error(w, "invalid note title", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.getRevisions(w, r, path)
}

func (s *Server) getRevisions(w http.ResponseWriter, r *http.Request, title string) {
	result, err := wikiFrom(r).Revisions(title, s.maxDiffs)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, wiki.ErrNoFile) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) checkoutRevision(w http.ResponseWriter, r *http.Request, title string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rev := r.URL.Query().Get("rev")
	if rev != "" && !rcs.ValidRevision(rev) {
		http.Error(w, "invalid revision", http.StatusBadRequest)
		return
	}
	if err := wikiFrom(r).CheckoutRevision(title, rev); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, wiki.ErrNoFile) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) promoteRevision(w http.ResponseWriter, r *http.Request, title string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := wikiFrom(r).PromoteRevision(title); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, wiki.ErrNoFile) {
			status = http.StatusNotFound
		} else if errors.Is(err, wiki.ErrAtHead) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// extractTitle pulls the note title from a URL path by removing
// the given prefix. Returns "" for empty or invalid titles.
func extractTitle(path, prefix string) string {
	raw := strings.TrimPrefix(path, prefix)
	if raw == "" {
		return ""
	}
	if !tid.ValidTitle(raw) {
		return ""
	}
	return raw
}
