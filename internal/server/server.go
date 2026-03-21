// Package server implements the TibbitWiki HTTP API.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dan4thewin/tibbitwiki/internal/render"
	"github.com/dan4thewin/tibbitwiki/internal/wiki"
)

// Server is the HTTP handler for the TibbitWiki API.
type Server struct {
	hub       *wiki.Hub
	indexHTML string
	mux       *http.ServeMux
}

// New creates a Server backed by the given Hub.
func New(hub *wiki.Hub, indexHTML string) *Server {
	srv := &Server{
		hub:       hub,
		indexHTML: indexHTML,
		mux:       http.NewServeMux(),
	}
	srv.routes()
	return srv
}

func (s *Server) wiki() *wiki.Wiki {
	return s.hub.Default()
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
	results, err := s.wiki().Search(q)
	if err != nil {
		http.Error(w, fmt.Sprintf("search error: %v", err), http.StatusInternalServerError)
		return
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
	fields := s.wiki().Get("$:/favicon.ico", false)
	if fields == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	w.Write([]byte(fields["text"]))
}

// handleListNotes returns all notes without their text field.
func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.wiki().List())
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
		s.deleteNote(w, title)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// getNote returns a single note as JSON with all fields.
func (s *Server) getNote(w http.ResponseWriter, r *http.Request, title string) {
	renderPlain := r.URL.Query().Get("render") == "plain"
	fields := s.wiki().Get(title, renderPlain)
	if fields == nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", fmt.Sprintf("\"default/%s/%s:\"", title, fields["revision"]))
	json.NewEncoder(w).Encode(fields)
}

// putNote saves a note from the request body.
func (s *Server) putNote(w http.ResponseWriter, r *http.Request, title string) {
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

	rev, conflict := s.wiki().Put(title, fields, clientRev)
	if conflict {
		w.WriteHeader(http.StatusConflict)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"default/%s/%d:\"", title, rev))
	w.WriteHeader(http.StatusNoContent)
}

// deleteNote removes a note.
func (s *Server) deleteNote(w http.ResponseWriter, title string) {
	s.wiki().Delete(title)
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
	if err := s.wiki().EditExternal(title); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "no file") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleLineEdit replaces or inserts a line in a note's source text.
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
	html, rev, err := s.wiki().LineEdit(title, req.Line, req.Text, req.Insert)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "out of range") {
			status = http.StatusBadRequest
		} else if strings.Contains(err.Error(), "no file") {
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

// extractTitle pulls the note title from a URL path by removing
// the given prefix.
func extractTitle(path, prefix string) string {
	raw := strings.TrimPrefix(path, prefix)
	if raw == "" {
		return ""
	}
	return raw
}
