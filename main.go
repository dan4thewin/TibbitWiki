// TibbitWiki: a Go replacement for TiddlyWiki's Node.js server.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"

	"github.com/dan4thewin/tibbitwiki/internal/config"
	"github.com/dan4thewin/tibbitwiki/internal/editor"
	"github.com/dan4thewin/tibbitwiki/internal/loader"
	"github.com/dan4thewin/tibbitwiki/internal/rcs"
	"github.com/dan4thewin/tibbitwiki/internal/render"
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

	// Resolve config file path: explicit --config flag, or
	// config.yaml in wiki path (auto-created if absent).
	configPath := resolveConfigPath(os.Args[1:])
	if configPath == "" {
		configPath = filepath.Join(cfg.WikiPath, "config.yaml")
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		writeDefaultConfig(configPath)
	} else if resolveConfigPath(os.Args[1:]) == "" {
		// Found config.yaml in wiki path but wasn't loaded — reload with it.
		cfg, err = config.Load(append(os.Args[1:], "--config", configPath))
		if err != nil {
			log.Printf("warning: ignoring %s: %v", configPath, err)
		}
	}

	var ed *editor.Editor
	if cfg.EditorCmd != "" {
		ed = editor.New(cfg.EditorCmd)
		log.Printf("editor: %s", cfg.EditorCmd)
	}

	wikis := discoverWikis(cfg.WikiPath, ed, cfg.CacheSize)
	if len(wikis) == 0 {
		log.Fatalf("no wikis found in %s (need subdirs with .md files)", cfg.WikiPath)
	}

	// Generate theme.css in the wiki path directory.
	themeCSSPath := filepath.Join(cfg.WikiPath, "theme.css")
	if err := render.WriteThemeFile(cfg.Theme, themeCSSPath); err != nil {
		log.Fatalf("writing theme.css: %v", err)
	}
	log.Printf("theme: %s", themeCSSPath)

	// Watch config.yaml for changes — regenerate theme.css on save.
	var themeRev atomic.Int64
	go watchConfig(configPath, themeCSSPath, &themeRev)

	pollMS := int(cfg.PollInterval.Milliseconds())
	hub := wiki.NewHub(wikis)
	for _, wk := range wikis {
		wk.SetHub(hub)
	}
	srv := server.New(hub, ed, &themeRev, pollMS, cfg.IndexHTML, themeCSSPath, configPath)
	log.Printf("listening on http://%s", cfg.Listen)
	log.Printf("wikis: %v", hub.Names())
	if err := http.ListenAndServe(cfg.Listen, srv); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// resolveConfigPath finds the --config flag value from args.
func resolveConfigPath(args []string) string {
	for i, arg := range args {
		if arg == "--config" || arg == "-config" {
			if i+1 < len(args) {
				return args[i+1]
			}
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
		if strings.HasPrefix(arg, "-config=") {
			return strings.TrimPrefix(arg, "-config=")
		}
	}
	return ""
}

// writeDefaultConfig copies the embedded defaults.yaml to disk
// so the user has a commented config file to edit.
func writeDefaultConfig(path string) {
	if err := os.WriteFile(path, config.DefaultYAML, 0644); err != nil {
		log.Printf("warning: could not write default config: %v", err)
	} else {
		log.Printf("created default config: %s", path)
	}
}

// watchConfig watches the config file for changes. On save it does
// an RCS checkin, re-reads the theme section, and regenerates theme.css.
func watchConfig(configPath, themeCSSPath string, themeRev *atomic.Int64) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("config watcher: %v", err)
		return
	}
	// Watch the directory (not the file) because editors often
	// write-to-temp-then-rename, which removes the original inode.
	dir := filepath.Dir(configPath)
	base := filepath.Base(configPath)
	if err := w.Add(dir); err != nil {
		log.Printf("config watcher: watching %s: %v", dir, err)
		return
	}
	log.Printf("config watcher: watching %s", configPath)

	var debounce *time.Timer
	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) != base {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			// Debounce: wait 500ms for editor to finish writing.
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(500*time.Millisecond, func() {
				reloadConfig(configPath, themeCSSPath, themeRev)
			})
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("config watcher error: %v", err)
		}
	}
}

// reloadConfig re-reads config.yaml, does an RCS checkin, and
// regenerates theme.css.
func reloadConfig(configPath, themeCSSPath string, themeRev *atomic.Int64) {
	if err := rcs.Checkin(configPath); err != nil {
		log.Printf("config rcs checkin: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Printf("config reload: reading %s: %v", configPath, err)
		return
	}
	// Parse just enough to extract the theme.
	var partial struct {
		Theme config.Theme `yaml:"theme"`
	}
	if err := yaml.Unmarshal(data, &partial); err != nil {
		log.Printf("config reload: parsing %s: %v", configPath, err)
		return
	}
	// Fill missing keys from defaults.
	theme := partial.Theme
	if theme == nil {
		theme = config.DefaultTheme()
	} else {
		for k, v := range config.DefaultTheme() {
			if _, ok := theme[k]; !ok {
				theme[k] = v
			}
		}
	}
	if err := render.WriteThemeFile(theme, themeCSSPath); err != nil {
		log.Printf("config reload: writing theme.css: %v", err)
		return
	}
	themeRev.Add(1)
	log.Printf("config reload: theme.css regenerated (rev %d)", themeRev.Load())
}

// discoverWikis scans dir for subdirs containing .md files and
// creates a Wiki with loader and watcher for each.
func discoverWikis(dir string, ed *editor.Editor, cacheSize int) map[string]*wiki.Wiki {
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
		wk := wiki.New(name, subdir, ed, cacheSize)
		n, sym, err := loader.Load(subdir, wk.Store(), wk)
		if err != nil {
			log.Fatalf("wiki %s: loading: %v", name, err)
		}
		if len(sym) > 0 {
			wk.SetSymlinks(sym)
			log.Printf("wiki %s: %d symlinked notes", name, len(sym))
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
