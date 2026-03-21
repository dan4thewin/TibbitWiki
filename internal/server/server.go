// Package server implements the TiddlyWeb-compatible HTTP API.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dan4thewin/go-tiddle/internal/editor"
	"github.com/dan4thewin/go-tiddle/internal/rcs"
	"github.com/dan4thewin/go-tiddle/internal/store"
	"github.com/dan4thewin/go-tiddle/internal/tid"
)

// Server is the HTTP handler for the TiddlyWeb API.
type Server struct {
	store       *store.Store
	indexHTML   string // path to pre-rendered index.html
	editor     *editor.Editor
	tiddlerDir string // directory for writing new .tid files
	mux        *http.ServeMux
}

// New creates a Server with the given store, index HTML path, and
// optional editor for external editing.
func New(s *store.Store, indexHTML string, ed *editor.Editor) *Server {
	srv := &Server{
		store:     s,
		indexHTML:  indexHTML,
		editor:    ed,
		mux:       http.NewServeMux(),
	}
	srv.routes()
	return srv
}

// SetTiddlerDir sets the directory for writing new tiddler files.
func (s *Server) SetTiddlerDir(dir string) {
	s.tiddlerDir = dir
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/status", s.handleStatus)
	s.mux.HandleFunc("/favicon.ico", s.handleFavicon)
	s.mux.HandleFunc("/recipes/default/tiddlers.json", s.handleSkinnyTiddlers)
	// These use prefix matching + manual method dispatch.
	s.mux.HandleFunc("/recipes/default/tiddlers/", s.handleTiddler)
	s.mux.HandleFunc("/bags/default/tiddlers/", s.handleDeleteTiddler)
	s.mux.HandleFunc("/edit-external/", s.handleEditExternal)
}

// ServeHTTP delegates to the internal mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// handleIndex serves the pre-rendered TiddlyWiki HTML.
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
		"username":          "DG",
		"anonymous":         false,
		"read_only":         false,
		"tiddlywiki_version": "5.3.1",
		"space":             map[string]string{"recipe": "default"},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleFavicon serves the favicon from the $:/favicon.ico tiddler.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	td := s.store.Get("$:/favicon.ico")
	if td == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	w.Write([]byte(td.Text()))
}

// handleSkinnyTiddlers returns all tiddlers without their text
// field, used by the browser's polling sync.
func (s *Server) handleSkinnyTiddlers(w http.ResponseWriter, r *http.Request) {
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

// handleTiddler dispatches GET and PUT for a single tiddler.
func (s *Server) handleTiddler(w http.ResponseWriter, r *http.Request) {
	title := extractTitle(r.URL.Path, "/recipes/default/tiddlers/")
	if title == "" {
		http.Error(w, "missing tiddler title", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getTiddler(w, title)
	case http.MethodPut:
		s.putTiddler(w, r, title)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// getTiddler returns a single tiddler as JSON with all fields.
func (s *Server) getTiddler(w http.ResponseWriter, title string) {
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
	w.Header().Set("Content-Type", "application/json")
	etag := fmt.Sprintf("\"default/%s/%d:\"", title, s.store.Revision(title))
	w.Header().Set("ETag", etag)
	json.NewEncoder(w).Encode(fields)
}

// putTiddler saves a tiddler from the request body.
func (s *Server) putTiddler(w http.ResponseWriter, r *http.Request, title string) {
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

	// Build tiddler fields as strings.
	tFields := make(map[string]string)
	for k, v := range fields {
		if k == "revision" {
			continue
		}
		tFields[k] = fmt.Sprintf("%v", v)
	}
	tFields["title"] = title

	// Handle skinny tiddlers — preserve existing text if the
	// incoming tiddler is marked as skinny.
	if tFields["_is_skinny"] != "" {
		delete(tFields, "_is_skinny")
		if existing := s.store.Get(title); existing != nil {
			if existingText := existing.Text(); existingText != "" {
				tFields["text"] = existingText
			}
		}
	}

	td := &tid.Tiddler{Fields: tFields}

	// Write to disk.
	filePath := s.store.FilePath(title)
	if filePath == "" && s.tiddlerDir != "" {
		// New tiddler — derive filename from title.
		filePath = filepath.Join(s.tiddlerDir, tid.TitleToFilename(title))
	}
	if filePath != "" {
		if err := os.WriteFile(filePath, tid.Serialize(td), 0644); err != nil {
			log.Printf("error writing tiddler %q: %v", title, err)
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

// handleDeleteTiddler handles DELETE for /bags/default/tiddlers/:title.
func (s *Server) handleDeleteTiddler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	title := extractTitle(r.URL.Path, "/bags/default/tiddlers/")
	if title == "" {
		http.Error(w, "missing tiddler title", http.StatusBadRequest)
		return
	}

	// Delete the file on disk.
	if filePath := s.store.FilePath(title); filePath != "" {
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			log.Printf("error deleting file for %q: %v", title, err)
		}
	}

	s.store.Delete(title)
	w.WriteHeader(http.StatusNoContent)
}

// handleEditExternal launches the configured editor for a tiddler.
func (s *Server) handleEditExternal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	title := extractTitle(r.URL.Path, "/edit-external/")
	if title == "" {
		http.Error(w, "missing tiddler title", http.StatusBadRequest)
		return
	}

	filePath := s.store.FilePath(title)
	if filePath == "" {
		http.Error(w, "no file path for tiddler", http.StatusNotFound)
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

// extractTitle pulls the tiddler title from a URL path by removing
// the given prefix and URL-decoding the remainder.
func extractTitle(path, prefix string) string {
	raw := strings.TrimPrefix(path, prefix)
	if raw == "" {
		return ""
	}
	// The title may be URL-encoded in the request path.
	// Use the raw value — TiddlyWiki titles can contain any chars.
	return raw
}
