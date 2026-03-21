// Package server implements the TibbitWiki HTTP API.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dan4thewin/tibbitwiki/internal/editor"
	"github.com/dan4thewin/tibbitwiki/internal/rcs"
	"github.com/dan4thewin/tibbitwiki/internal/render"
	"github.com/dan4thewin/tibbitwiki/internal/search"
	"github.com/dan4thewin/tibbitwiki/internal/store"
	"github.com/dan4thewin/tibbitwiki/internal/tid"
)

// Server is the HTTP handler for the TibbitWiki API.
type Server struct {
	store     *store.Store
	indexHTML string // path to pre-rendered index.html
	editor    *editor.Editor
	noteDir   string // directory for writing new .tid files
	mux       *http.ServeMux
}

// New creates a Server with the given store, index HTML path, and
// optional editor for external editing.
func New(s *store.Store, indexHTML string, ed *editor.Editor) *Server {
	srv := &Server{
		store:     s,
		indexHTML: indexHTML,
		editor:    ed,
		mux:       http.NewServeMux(),
	}
	srv.routes()
	return srv
}

// SetNoteDir sets the directory for writing new note files.
func (s *Server) SetNoteDir(dir string) {
	s.noteDir = dir
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/status", s.handleStatus)
	s.mux.HandleFunc("/favicon.ico", s.handleFavicon)
	s.mux.HandleFunc("/notes", s.handleListNotes)
	s.mux.HandleFunc("/notes/", s.handleNote)
	s.mux.HandleFunc("/line-edit/", s.handleLineEdit)
	s.mux.HandleFunc("/edit-external/", s.handleEditExternal)
	s.mux.HandleFunc("/theme.css", s.handleThemeCSS)
	s.mux.HandleFunc("/search", s.handleSearch)
}

// ServeHTTP delegates to the internal mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleThemeCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
	w.Write([]byte(render.ThemeCSS()))
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}
	if s.noteDir == "" {
		http.Error(w, "search not available", http.StatusInternalServerError)
		return
	}
	hits, err := search.Search(s.noteDir, q)
	if err != nil {
		http.Error(w, fmt.Sprintf("search error: %v", err), http.StatusInternalServerError)
		return
	}
	type result struct {
		Title   string   `json:"title"`
		Matches []string `json:"matches"`
	}
	var results []result
	for _, h := range hits {
		name := search.FileToTitle(h.File)
		td := s.store.Get(name)
		if td != nil {
			matches := h.Lines
			if matches == nil {
				matches = []string{}
			}
			results = append(results, result{
				Title:   td.Fields["title"],
				Matches: matches,
			})
		}
	}
	if results == nil {
		results = []result{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// handleIndex serves the index HTML page.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if s.indexHTML == "" {
		http.Error(w, "index.html not configured", http.StatusInternalServerError)
		return
	}
	http.ServeFile(w, r, s.indexHTML)
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

// handleFavicon serves the favicon from the $:/favicon.ico note.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	td := s.store.Get("$:/favicon.ico")
	if td == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	w.Write([]byte(td.Text()))
}

// handleListNotes returns all notes without their text
// field, used by the browser's polling sync.
func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	all := s.store.All()
	skinny := make([]map[string]string, 0, len(all))
	for _, td := range all {
		fields := td.SkinnyFields()
		fields["revision"] = fmt.Sprintf("%d", s.store.Revision(td.Title()))
		skinny = append(skinny, fields)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(skinny)
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
		s.getNote(w, title)
	case http.MethodPut:
		s.putNote(w, r, title)
	case http.MethodDelete:
		s.deleteNote(w, title)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// getNote returns a single note as JSON with all fields.
func (s *Server) getNote(w http.ResponseWriter, title string) {
	td := s.store.Get(title)
	if td == nil {
		http.NotFound(w, nil)
		return
	}
	fields := make(map[string]string, len(td.Fields))
	for k, v := range td.Fields {
		fields[k] = v
	}
	fields["revision"] = fmt.Sprintf("%d", s.store.Revision(title))
	if text := fields["text"]; text != "" {
		fields["html"] = render.ToHTML(text)
	}
	w.Header().Set("Content-Type", "application/json")
	etag := fmt.Sprintf("\"default/%s/%d:\"", title, s.store.Revision(title))
	w.Header().Set("ETag", etag)
	json.NewEncoder(w).Encode(fields)
}

// putNote saves a note from the request body.
func (s *Server) putNote(w http.ResponseWriter, r *http.Request, title string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var fields map[string]interface{}
	if err := json.Unmarshal(body, &fields); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Flatten any nested "fields" object into top-level fields.
	if nested, ok := fields["fields"].(map[string]interface{}); ok {
		for k, v := range nested {
			fields[k] = v
		}
		delete(fields, "fields")
	}

	// Reject stale PUTs: if the browser's revision is behind the
	// store (meaning an external edit occurred), return 409 so the
	// syncer refetches instead of overwriting.
	if revVal, ok := fields["revision"]; ok {
		if clientRev, ok := revVal.(string); ok {
			serverRev := s.store.Revision(title)
			if serverRev > 0 && clientRev != fmt.Sprintf("%d", serverRev) {
				w.WriteHeader(http.StatusConflict)
				return
			}
		}
	}

	// Build note fields as strings.
	tFields := make(map[string]string)
	for k, v := range fields {
		if k == "revision" {
			continue
		}
		tFields[k] = fmt.Sprintf("%v", v)
	}
	tFields["title"] = title

	// Handle skinny notes — preserve existing text if the
	// incoming note is marked as skinny.
	if tFields["_is_skinny"] != "" {
		delete(tFields, "_is_skinny")
		if existing := s.store.Get(title); existing != nil {
			if existingText := existing.Text(); existingText != "" {
				tFields["text"] = existingText
			}
		}
	}

	td := &tid.Note{Fields: tFields}

	// Write to disk.
	filePath := s.store.FilePath(title)
	if filePath == "" && s.noteDir != "" {
		// New note — derive filename from title.
		filePath = filepath.Join(s.noteDir, tid.TitleToFilename(title))
	}
	if filePath != "" {
		if err := os.WriteFile(filePath, tid.Serialize(td), 0644); err != nil {
			log.Printf("error writing note %q: %v", title, err)
		} else {
			if err := rcs.Checkin(filePath); err != nil {
				log.Printf("rcs checkin %q: %v", title, err)
			}
		}
		s.store.PutWithPath(td, filePath)
	} else {
		s.store.Put(td)
	}

	rev := s.store.Revision(title)
	etag := fmt.Sprintf("\"default/%s/%d:\"", title, rev)
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusNoContent)
}

// deleteNote removes a note and its file on disk.
func (s *Server) deleteNote(w http.ResponseWriter, title string) {
	if filePath := s.store.FilePath(title); filePath != "" {
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			log.Printf("error deleting file for %q: %v", title, err)
		}
	}

	s.store.Delete(title)
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

	filePath := s.store.FilePath(title)
	if filePath == "" {
		http.Error(w, "no file path for note", http.StatusNotFound)
		return
	}

	if s.editor == nil {
		http.Error(w, "editor not configured", http.StatusInternalServerError)
		return
	}

	if err := s.editor.Open(filePath); err != nil {
		http.Error(w, fmt.Sprintf("editor launch failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleLineEdit replaces or inserts a line in a note's source text.
//
//	POST /line-edit/:title with JSON body:
//	  {"line": N, "text": "new content"}           — replace line N
//	  {"line": N, "text": "new content", "insert": true} — insert after line N
//
// Returns the re-rendered HTML of the full note body.
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

	filePath := s.store.FilePath(title)
	if filePath == "" {
		http.Error(w, "no file for note", http.StatusNotFound)
		return
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "reading file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	lines := strings.Split(string(data), "\n")

	// data-line is relative to the body text (after headers + blank
	// line). Find where the body starts in the file.
	bodyStart := 0
	for i, line := range lines {
		if line == "" {
			bodyStart = i + 1
			break
		}
	}
	absLine := bodyStart + req.Line // 1-based body line → 0-based file index
	if absLine > len(lines) {
		http.Error(w, "line "+strconv.Itoa(req.Line)+" out of range", http.StatusBadRequest)
		return
	}

	if req.Insert {
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:absLine]...)
		newLines = append(newLines, req.Text)
		newLines = append(newLines, lines[absLine:]...)
		lines = newLines
	} else {
		lines[absLine-1] = req.Text
	}

	newData := []byte(strings.Join(lines, "\n"))
	if err := os.WriteFile(filePath, newData, 0644); err != nil {
		http.Error(w, "writing file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := rcs.Checkin(filePath); err != nil {
		log.Printf("rcs checkin %q: %v", title, err)
	}

	// Re-parse the file to update the store.
	td, err := tid.Parse(newData)
	if err != nil {
		http.Error(w, "parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.store.PutWithPath(td, filePath)

	// Return re-rendered HTML for the note body.
	text := td.Text()
	resp := map[string]string{
		"html":     render.ToHTML(text),
		"revision": fmt.Sprintf("%d", s.store.Revision(title)),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// extractTitle pulls the note title from a URL path by removing
// the given prefix and URL-decoding the remainder.
func extractTitle(path, prefix string) string {
	raw := strings.TrimPrefix(path, prefix)
	if raw == "" {
		return ""
	}
	// The title may be URL-encoded in the request path.
	return raw
}
