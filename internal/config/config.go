// Package config handles YAML configuration and CLI flag parsing.
package config

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultYAML is the embedded default configuration file.
// It is the single source of truth for all default values.
//
//go:embed defaults.yaml
var DefaultYAML []byte

// Theme maps semantic names to CSS values (colors, fonts, sizes).
type Theme map[string]string

// Config holds all server configuration.
type Config struct {
	WikiPath     string        `yaml:"wiki_path"`
	Listen       string        `yaml:"listen"`
	EditorCmd    string        `yaml:"editor_cmd"`
	PollInterval time.Duration `yaml:"poll_interval"`
	TWCorePath   string        `yaml:"tw_core_path"`
	IndexHTML    string        `yaml:"index_html"`
	CacheSize    int           `yaml:"cache_size"`
	Theme        Theme         `yaml:"theme"`
}

// Defaults returns a Config parsed from the embedded defaults.yaml.
func Defaults() Config {
	var cfg Config
	yaml.Unmarshal(DefaultYAML, &cfg)
	return cfg
}

// DefaultTheme returns the theme from the embedded defaults.
func DefaultTheme() Theme {
	return Defaults().Theme
}

// cliFlags holds parsed CLI flag values for merging into Config.
type cliFlags struct {
	configFile   string
	wikiPath     string
	listen       string
	editorCmd    string
	pollInterval time.Duration
	twCorePath   string
	indexHTML    string
	cacheSize    int
}

func parseFlags(args []string) (cliFlags, error) {
	var f cliFlags
	fs := flag.NewFlagSet("tibbitwiki", flag.ContinueOnError)
	fs.StringVar(&f.configFile, "config", "", "path to YAML config file")
	fs.StringVar(&f.wikiPath, "wiki-path", "", "path to wiki folder")
	fs.StringVar(&f.listen, "listen", "", "host:port to listen on")
	fs.StringVar(&f.editorCmd, "editor-cmd", "", "editor command (%s = file path)")
	fs.DurationVar(&f.pollInterval, "poll-interval", 0, "browser poll interval")
	fs.StringVar(&f.twCorePath, "tw-core-path", "", "path to TiddlyWiki core (legacy)")
	fs.StringVar(&f.indexHTML, "index-html", "", "path to pre-rendered index.html")
	fs.IntVar(&f.cacheSize, "cache-size", 0, "LRU body cache size per wiki")
	if err := fs.Parse(args); err != nil {
		return f, fmt.Errorf("parsing flags: %w", err)
	}
	return f, nil
}

func (f *cliFlags) applyTo(cfg *Config) {
	if f.wikiPath != "" {
		cfg.WikiPath = f.wikiPath
	}
	if f.listen != "" {
		cfg.Listen = f.listen
	}
	if f.editorCmd != "" {
		cfg.EditorCmd = f.editorCmd
	}
	if f.pollInterval != 0 {
		cfg.PollInterval = f.pollInterval
	}
	if f.twCorePath != "" {
		cfg.TWCorePath = f.twCorePath
	}
	if f.indexHTML != "" {
		cfg.IndexHTML = f.indexHTML
	}
	if f.cacheSize > 0 {
		cfg.CacheSize = f.cacheSize
	}
}

// Load parses the embedded defaults, merges an optional YAML config
// file on top, then merges CLI flags on top. Priority:
// embedded defaults < config file < CLI flags.
func Load(args []string) (Config, error) {
	cfg := Defaults()

	f, err := parseFlags(args)
	if err != nil {
		return cfg, err
	}

	if f.configFile != "" {
		data, err := os.ReadFile(f.configFile)
		if err != nil {
			return cfg, fmt.Errorf("reading config: %w", err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parsing config: %w", err)
		}
	}

	f.applyTo(&cfg)

	// Fill missing theme keys from embedded defaults.
	defaults := Defaults().Theme
	if cfg.Theme == nil {
		cfg.Theme = defaults
	} else {
		for k, v := range defaults {
			if _, ok := cfg.Theme[k]; !ok {
				cfg.Theme[k] = v
			}
		}
	}

	return cfg, nil
}
