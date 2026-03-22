// TibbitWiki: a Go replacement for TiddlyWiki's Node.js server.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

	var ed *editor.Editor
	if cfg.EditorCmd != "" {
		ed = editor.New(cfg.EditorCmd)
		log.Printf("editor: %s", cfg.EditorCmd)
	}

	wikis := discoverWikis(cfg.WikiPath, ed)
	if len(wikis) == 0 {
		log.Fatalf("no wikis found in %s (need subdirs with .md files)", cfg.WikiPath)
	}

	hub := wiki.NewHub(wikis)
	srv := server.New(hub, cfg.IndexHTML)
	log.Printf("listening on http://%s", cfg.Listen)
	log.Printf("wikis: %v", hub.Names())
	if err := http.ListenAndServe(cfg.Listen, srv); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// discoverWikis scans dir for subdirs containing .md files and
// creates a Wiki with loader and watcher for each.
func discoverWikis(dir string, ed *editor.Editor) map[string]*wiki.Wiki {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Fatalf("reading wiki path %s: %v", dir, err)
	}

	wikis := make(map[string]*wiki.Wiki)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		subdir := filepath.Join(dir, entry.Name())
		mds, _ := filepath.Glob(filepath.Join(subdir, "*.md"))
		if len(mds) == 0 {
			continue
		}
		name := entry.Name()
		wk := wiki.New(name, subdir, ed)
		n, err := loader.Load(subdir, wk.Store())
		if err != nil {
			log.Fatalf("wiki %s: loading: %v", name, err)
		}
		log.Printf("wiki %s: loaded %d notes from %s", name, n, subdir)
		wk.Start()
		startWatcher(name, subdir, wk)
		wikis[name] = wk
	}
	return wikis
}

func startWatcher(name, dir string, wk *wiki.Wiki) {
	w, err := watcher.New(dir, wk.Reload)
	if err != nil {
		log.Fatalf("wiki %s: watcher: %v", name, err)
	}
	if err := w.Start(); err != nil {
		log.Fatalf("wiki %s: watcher start: %v", name, err)
	}
	log.Printf("wiki %s: watching for changes", name)
}
