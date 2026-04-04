// TibbitWiki: a Go replacement for TiddlyWiki's Node.js server.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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

// version is set at build time via -ldflags, or read from the
// embedded module info populated by go install.
var version = ""

func getVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

//go:embed TibbitWiki.md
var demoTibbitWiki []byte

//go:embed QuickRef.md
var demoQuickRef []byte

//go:embed HotKeys.md
var demoHotKeys []byte

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-version" {
		fmt.Println("tibbitwiki", getVersion())
		os.Exit(0)
	}
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	seedDemoWiki(cfg.WikiPath)
	cfg, configPath := loadConfig(cfg, os.Args[1:])
	ed := initEditor(cfg.EditorCmd)
	if ed == nil {
		ed = detectEditor()
	}

	wikis := discoverWikis(cfg.WikiPath, ed, cfg.CacheSize)
	if len(wikis) == 0 {
		log.Fatalf("no wikis found in %s (need subdirs with .md files)", cfg.WikiPath)
	}

	wikiNames := generateThemeCSS(wikis, cfg)

	var themeRev atomic.Int64
	go watchConfig(configPath, cfg.WikiPath, wikiNames, &themeRev)

	hub := configureHub(wikis, cfg)
	srv := server.New(hub, ed, &themeRev, int(cfg.PollInterval.Milliseconds()), cfg.MaxDiffs, cfg.IndexHTML, cfg.WikiPath, configPath)

	// Pre-bind all listen addresses before serving any.
	listeners := make([]net.Listener, 0, len(cfg.Listen))
	for _, addr := range cfg.Listen {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			for _, prev := range listeners {
				_ = prev.Close()
			}
			log.Fatalf("listen %s: %v", addr, err)
		}
		listeners = append(listeners, ln)
		log.Printf("listening on http://%s", ln.Addr())
	}
	log.Printf("wikis: %v", hub.Names())

	httpSrv := &http.Server{Handler: srv}

	go func() {
		<-srv.RestartCh()
		log.Printf("restart requested — shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	var wg sync.WaitGroup
	for _, ln := range listeners {
		wg.Add(1)
		go func(ln net.Listener) {
			defer wg.Done()
			if err := httpSrv.Serve(ln); err != http.ErrServerClosed {
				log.Fatalf("server %s: %v", ln.Addr(), err)
			}
		}(ln)
	}
	wg.Wait()

	// Re-exec the same binary with the same args.
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("restart: %v", err)
	}
	log.Printf("restarting: %s %v", exe, os.Args[1:])
	if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
		log.Fatalf("exec: %v", err)
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

// loadConfig resolves the config file path and optionally reloads
// the config when a config.yaml is found in the wiki directory.
func loadConfig(cfg config.Config, args []string) (config.Config, string) {
	configPath := resolveConfigPath(args)
	if configPath == "" {
		configPath = filepath.Join(cfg.WikiPath, "config.yaml")
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		writeDefaultConfig(configPath)
	} else if resolveConfigPath(args) == "" {
		reloaded, err := config.Load(append(args, "--config", configPath))
		if err != nil {
			log.Printf("warning: ignoring %s: %v", configPath, err)
		} else {
			cfg = reloaded
		}
	}
	return cfg, configPath
}

// initEditor creates an Editor from the configured command, or
// returns nil when no editor command is set.
func initEditor(cmd string) *editor.Editor {
	if cmd == "" {
		return nil
	}
	ed, err := editor.New(cmd)
	if err != nil {
		log.Fatalf("editor: %v", err)
	}
	log.Printf("editor: %s", cmd)
	return ed
}

// seedDemoWiki creates the wiki_path and a demo/ subdirectory with
// sample notes when the wiki path does not yet exist (first run).
func seedDemoWiki(wikiPath string) {
	if _, err := os.Stat(wikiPath); err == nil {
		return // already exists
	}
	demoDir := filepath.Join(wikiPath, "demo")
	if err := os.MkdirAll(demoDir, 0755); err != nil {
		log.Fatalf("creating %s: %v", demoDir, err)
	}
	for name, data := range map[string][]byte{
		"TibbitWiki.md": demoTibbitWiki,
		"QuickRef.md":   demoQuickRef,
		"HotKeys.md":    demoHotKeys,
	} {
		fp := filepath.Join(demoDir, name)
		if err := os.WriteFile(fp, data, 0644); err != nil {
			log.Fatalf("writing %s: %v", fp, err)
		}
	}
	log.Printf("created %s with sample notes", demoDir)
}

// detectEditor probes for a system file opener (open on macOS,
// xdg-open on Linux) and returns an Editor if found, or nil.
func detectEditor() *editor.Editor {
	for _, cmd := range []string{"open", "xdg-open"} {
		if _, err := exec.LookPath(cmd); err == nil {
			ed, err := editor.New(cmd + " %s")
			if err == nil {
				log.Printf("editor: detected %s", cmd)
				return ed
			}
		}
	}
	log.Printf("editor: none detected (textarea editing only)")
	return nil
}

// watchConfig watches the config file for changes. On save it does
// an RCS checkin, re-reads the theme section, and regenerates per-wiki CSS.
func watchConfig(configPath, wikiPath string, wikiNames []string, themeRev *atomic.Int64) {
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
				reloadConfig(configPath, wikiPath, wikiNames, themeRev)
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
// regenerates per-wiki CSS files.
func reloadConfig(configPath, wikiPath string, wikiNames []string, themeRev *atomic.Int64) {
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
	// Fill missing shared keys from defaults.
	theme := config.FillThemeDefaults(partial.Theme)
	for _, name := range wikiNames {
		merged := config.MergedTheme(theme, name)
		cssPath := filepath.Join(wikiPath, name+".css")
		if err := render.WriteThemeFile(merged, cssPath); err != nil {
			log.Printf("config reload: writing %s: %v", cssPath, err)
			continue
		}
	}
	themeRev.Add(1)
	log.Printf("config reload: theme CSS regenerated (rev %d)", themeRev.Load())
}

// generateThemeCSS writes per-wiki CSS files and returns the wiki names.
func generateThemeCSS(wikis map[string]*wiki.Wiki, cfg config.Config) []string {
	names := make([]string, 0, len(wikis))
	for name := range wikis {
		names = append(names, name)
		merged := config.MergedTheme(cfg.Theme, name)
		cssPath := filepath.Join(cfg.WikiPath, name+".css")
		if err := render.WriteThemeFile(merged, cssPath); err != nil {
			log.Fatalf("writing %s: %v", cssPath, err)
		}
		log.Printf("theme: %s", cssPath)
	}
	return names
}

// configureHub creates a Hub and configures each wiki with shared settings.
func configureHub(wikis map[string]*wiki.Wiki, cfg config.Config) *wiki.Hub {
	hub := wiki.NewHub(wikis)
	for _, wk := range wikis {
		wk.SetHub(hub)
		wk.SetAttachExclude(cfg.AttachmentExclude)
		wk.SetPdfConvertCmd(cfg.PdfConvertCmd)
		wk.SetTemplates(cfg.Templates)
	}
	return hub
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
		wk.ScanRCS()
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
