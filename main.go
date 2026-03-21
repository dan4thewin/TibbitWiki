// TibbitWiki: a Go replacement for TiddlyWiki's Node.js server.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/dan4thewin/tibbitwiki/internal/config"
	"github.com/dan4thewin/tibbitwiki/internal/editor"
	"github.com/dan4thewin/tibbitwiki/internal/loader"
	"github.com/dan4thewin/tibbitwiki/internal/server"
	"github.com/dan4thewin/tibbitwiki/internal/store"
	"github.com/dan4thewin/tibbitwiki/internal/watcher"
)

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	s := store.New()

	// Load notes from disk.
	notesDir := cfg.NotesPath()
	n, err := loader.Load(notesDir, s)
	if err != nil {
		log.Fatalf("loading notes: %v", err)
	}
	log.Printf("loaded %d notes from %s", n, notesDir)

	// Start file watcher.
	w, err := watcher.New(notesDir, s)
	if err != nil {
		log.Fatalf("creating watcher: %v", err)
	}
	if err := w.Start(); err != nil {
		log.Fatalf("starting watcher: %v", err)
	}
	defer w.Stop()
	log.Printf("watching %s for changes", notesDir)

	// Set up editor.
	var ed *editor.Editor
	if cfg.EditorCmd != "" {
		ed = editor.New(cfg.EditorCmd)
		log.Printf("editor: %s", cfg.EditorCmd)
	}

	// Start HTTP server.
	srv := server.New(s, cfg.IndexHTML, ed)
	srv.SetNoteDir(notesDir)
	log.Printf("listening on http://%s", cfg.Listen)
	if err := http.ListenAndServe(cfg.Listen, srv); err != nil {
		log.Fatalf("server: %v", err)
	}
}
