// Package config handles YAML configuration and CLI flag parsing.
package config

import (
	"flag"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all server configuration.
type Config struct {
	WikiPath     string        `yaml:"wiki_path"`
	Listen       string        `yaml:"listen"`
	EditorCmd    string        `yaml:"editor_cmd"`
	PollInterval time.Duration `yaml:"poll_interval"`
	TWCorePath   string        `yaml:"tw_core_path"`
	IndexHTML    string        `yaml:"index_html"`
}

// Defaults returns a Config with sensible default values.
func Defaults() Config {
	return Config{
		WikiPath:     ".",
		Listen:       "127.0.0.1:8080",
		EditorCmd:    "neovide -- %s",
		PollInterval: 3 * time.Second,
		TWCorePath:   "",
		IndexHTML:    "",
	}
}

// Load reads a YAML config file and merges CLI flags on top.
// Flags take precedence over the file; the file takes precedence
// over defaults.
func Load(args []string) (Config, error) {
	cfg := Defaults()

	fs := flag.NewFlagSet("tibbitwiki", flag.ContinueOnError)
	configFile := fs.String("config", "", "path to YAML config file")
	wikiPath := fs.String("wiki-path", "", "path to wiki folder")
	listen := fs.String("listen", "", "host:port to listen on")
	editorCmd := fs.String("editor-cmd", "", "editor command (%s = file path)")
	pollInterval := fs.Duration("poll-interval", 0, "browser poll interval")
	twCorePath := fs.String("tw-core-path", "", "path to TiddlyWiki core (legacy)")
	indexHTML := fs.String("index-html", "", "path to pre-rendered index.html")

	if err := fs.Parse(args); err != nil {
		return cfg, fmt.Errorf("parsing flags: %w", err)
	}

	// Load YAML config file if specified.
	if *configFile != "" {
		data, err := os.ReadFile(*configFile)
		if err != nil {
			return cfg, fmt.Errorf("reading config: %w", err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parsing config: %w", err)
		}
	}

	// CLI flags override config file values.
	if *wikiPath != "" {
		cfg.WikiPath = *wikiPath
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *editorCmd != "" {
		cfg.EditorCmd = *editorCmd
	}
	if *pollInterval != 0 {
		cfg.PollInterval = *pollInterval
	}
	if *twCorePath != "" {
		cfg.TWCorePath = *twCorePath
	}
	if *indexHTML != "" {
		cfg.IndexHTML = *indexHTML
	}

	return cfg, nil
}

// NotesPath returns the full path to the tiddlers subdirectory.
func (c Config) NotesPath() string {
	return c.WikiPath + "/tiddlers"
}
