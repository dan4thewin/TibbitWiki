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
	"github.com/dan4thewin/tibbitwiki/internal/watcher"
	"github.com/dan4thewin/tibbitwiki/internal/wiki"
)

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Set up editor.
	var ed *editor.Editor
	if cfg.EditorCmd != "" {
		ed = editor.New(cfg.EditorCmd)
		log.Printf("editor: %s", cfg.EditorCmd)
	}

	// Create wiki with event loop.
	notesDir := cfg.NotesPath()
	wk := wiki.New("default", notesDir, ed)

	n, err := loader.Load(notesDir, wk.Store())
	if err != nil {
		log.Fatalf("loading notes: %v", err)
	}
	log.Printf("loaded %d notes from %s", n, notesDir)

	wk.Start()
	defer wk.Stop()

	// Start file watcher.
	w, err := watcher.New(notesDir, wk.Reload)
	if err != nil {
		log.Fatalf("creating watcher: %v", err)
	}
	if err := w.Start(); err != nil {
		log.Fatalf("starting watcher: %v", err)
	}
	defer w.Stop()
	log.Printf("watching %s for changes", notesDir)

	// Start HTTP server.
	hub := wiki.NewSingleHub(wk)
	srv := server.New(hub, cfg.IndexHTML)
	log.Printf("listening on http://%s", cfg.Listen)
	if err := http.ListenAndServe(cfg.Listen, srv); err != nil {
		log.Fatalf("server: %v", err)
	}
}
