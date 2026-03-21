// go-tiddle: a Go replacement for TiddlyWiki's Node.js server.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/dan4thewin/go-tiddle/internal/config"
	"github.com/dan4thewin/go-tiddle/internal/editor"
	"github.com/dan4thewin/go-tiddle/internal/loader"
	"github.com/dan4thewin/go-tiddle/internal/server"
	"github.com/dan4thewin/go-tiddle/internal/store"
	"github.com/dan4thewin/go-tiddle/internal/watcher"
)

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	s := store.New()

	// Load tiddlers from disk.
	tiddlersDir := cfg.TiddlersPath()
	n, err := loader.Load(tiddlersDir, s)
	if err != nil {
		log.Fatalf("loading tiddlers: %v", err)
	}
	log.Printf("loaded %d tiddlers from %s", n, tiddlersDir)

	// Start file watcher.
	w, err := watcher.New(tiddlersDir, s)
	if err != nil {
		log.Fatalf("creating watcher: %v", err)
	}
	if err := w.Start(); err != nil {
		log.Fatalf("starting watcher: %v", err)
	}
	defer w.Stop()
	log.Printf("watching %s for changes", tiddlersDir)

	// Set up editor.
	var ed *editor.Editor
	if cfg.EditorCmd != "" {
		ed = editor.New(cfg.EditorCmd)
		log.Printf("editor: %s", cfg.EditorCmd)
	}

	// Start HTTP server.
	srv := server.New(s, cfg.IndexHTML, ed)
	srv.SetTiddlerDir(tiddlersDir)
	log.Printf("listening on http://%s", cfg.Listen)
	if err := http.ListenAndServe(cfg.Listen, srv); err != nil {
		log.Fatalf("server: %v", err)
	}
}
