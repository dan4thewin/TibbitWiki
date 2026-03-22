package config

import (
	"os"
	"testing"
	"time"
)

// TestDefaults verifies that Defaults returns expected values.
func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Listen != "127.0.0.1:8080" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, "127.0.0.1:8080")
	}
	if cfg.WikiPath != "." {
		t.Errorf("WikiPath = %q, want %q", cfg.WikiPath, ".")
	}
	if cfg.EditorCmd != "neovide -- %s" {
		t.Errorf("EditorCmd = %q, want %q", cfg.EditorCmd, "neovide -- %s")
	}
	if cfg.PollInterval != 3*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 3*time.Second)
	}
	if cfg.CacheSize != 200 {
		t.Errorf("CacheSize = %d, want 200", cfg.CacheSize)
	}
}

// TestLoadFlags verifies that CLI flags override defaults.
func TestLoadFlags(t *testing.T) {
	args := []string{
		"--wiki-path", "/my/wiki",
		"--listen", "0.0.0.0:9090",
		"--editor-cmd", "vim %s",
		"--poll-interval", "5s",
	}
	cfg, err := Load(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WikiPath != "/my/wiki" {
		t.Errorf("WikiPath = %q, want %q", cfg.WikiPath, "/my/wiki")
	}
	if cfg.Listen != "0.0.0.0:9090" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, "0.0.0.0:9090")
	}
	if cfg.EditorCmd != "vim %s" {
		t.Errorf("EditorCmd = %q, want %q", cfg.EditorCmd, "vim %s")
	}
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 5*time.Second)
	}
}

// TestLoadCacheSizeFlag verifies that --cache-size overrides default.
func TestLoadCacheSizeFlag(t *testing.T) {
	cfg, err := Load([]string{"--cache-size", "500"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CacheSize != 500 {
		t.Errorf("CacheSize = %d, want 500", cfg.CacheSize)
	}
}

// TestLoadYAMLFile verifies that a YAML config file is parsed.
func TestLoadYAMLFile(t *testing.T) {
	content := `wiki_path: /yaml/wiki
listen: "127.0.0.1:3000"
editor_cmd: "code %s"
poll_interval: 2s
`
	f, err := os.CreateTemp("", "tibbitwiki-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := Load([]string{"--config", f.Name()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WikiPath != "/yaml/wiki" {
		t.Errorf("WikiPath = %q, want %q", cfg.WikiPath, "/yaml/wiki")
	}
	if cfg.Listen != "127.0.0.1:3000" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, "127.0.0.1:3000")
	}
	if cfg.EditorCmd != "code %s" {
		t.Errorf("EditorCmd = %q, want %q", cfg.EditorCmd, "code %s")
	}
	if cfg.PollInterval != 2*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 2*time.Second)
	}
}

// TestLoadFlagsOverrideYAML verifies that CLI flags take precedence
// over values from the YAML file.
func TestLoadFlagsOverrideYAML(t *testing.T) {
	content := `wiki_path: /yaml/path
listen: "127.0.0.1:3000"
`
	f, err := os.CreateTemp("", "tibbitwiki-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := Load([]string{
		"--config", f.Name(),
		"--listen", "0.0.0.0:5555",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// wiki_path from YAML should persist.
	if cfg.WikiPath != "/yaml/path" {
		t.Errorf("WikiPath = %q, want %q", cfg.WikiPath, "/yaml/path")
	}
	// listen from flag should override YAML.
	if cfg.Listen != "0.0.0.0:5555" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, "0.0.0.0:5555")
	}
}

// TestLoadBadFlags verifies that invalid flags return an error.
func TestLoadBadFlags(t *testing.T) {
	_, err := Load([]string{"--bogus-flag"})
	if err == nil {
		t.Fatal("expected error for bad flag, got nil")
	}
}

// TestLoadMissingConfigFile verifies that a missing config file
// returns an error.
func TestLoadMissingConfigFile(t *testing.T) {
	_, err := Load([]string{"--config", "/no/such/file.yaml"})
	if err == nil {
		t.Fatal("expected error for missing config, got nil")
	}
}

// TestLoadBadYAML verifies that malformed YAML returns an error.
func TestLoadBadYAML(t *testing.T) {
	f, err := os.CreateTemp("", "tibbitwiki-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString("{{not yaml"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, err = Load([]string{"--config", f.Name()})
	if err == nil {
		t.Fatal("expected error for bad YAML, got nil")
	}
}

// TestLoadNoArgs verifies that Load with no args returns defaults.
func TestLoadNoArgs(t *testing.T) {
	cfg, err := Load([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defaults := Defaults()
	if cfg.Listen != defaults.Listen {
		t.Errorf("Listen = %q, want default %q", cfg.Listen, defaults.Listen)
	}
	if cfg.WikiPath != defaults.WikiPath {
		t.Errorf("WikiPath = %q, want default %q", cfg.WikiPath, defaults.WikiPath)
	}
}

// TestLoadDefaultTheme verifies that Load with no args returns the
// default theme with all expected keys.
func TestLoadDefaultTheme(t *testing.T) {
	cfg, err := Load([]string{})
	if err != nil {
		t.Fatal(err)
	}
	expected := DefaultTheme()
	for k, v := range expected {
		if cfg.Theme[k] != v {
			t.Errorf("Theme[%q] = %q, want %q", k, cfg.Theme[k], v)
		}
	}
}

// TestLoadYAMLTheme verifies that a theme section in YAML overrides
// defaults, and missing keys are filled from defaults.
func TestLoadYAMLTheme(t *testing.T) {
	content := `theme:
  bg: "#000000"
  accent: "#ff0000"
`
	f, err := os.CreateTemp("", "tibbitwiki-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(content)
	f.Close()

	cfg, err := Load([]string{"--config", f.Name()})
	if err != nil {
		t.Fatal(err)
	}
	// Overridden keys.
	if cfg.Theme["bg"] != "#000000" {
		t.Errorf("Theme[bg] = %q, want %q", cfg.Theme["bg"], "#000000")
	}
	if cfg.Theme["accent"] != "#ff0000" {
		t.Errorf("Theme[accent] = %q, want %q", cfg.Theme["accent"], "#ff0000")
	}
	// Missing key should be filled from defaults.
	defaults := DefaultTheme()
	if cfg.Theme["fg"] != defaults["fg"] {
		t.Errorf("Theme[fg] = %q, want default %q", cfg.Theme["fg"], defaults["fg"])
	}
	if cfg.Theme["border"] != defaults["border"] {
		t.Errorf("Theme[border] = %q, want default %q", cfg.Theme["border"], defaults["border"])
	}
}

// TestLoadTWCorePathAndIndexHTMLFlags verifies that the --tw-core-path
// and --index-html flags override the config values.
func TestLoadTWCorePathAndIndexHTMLFlags(t *testing.T) {
	cfg, err := Load([]string{
		"--tw-core-path", "/tw",
		"--index-html", "/index.html",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TWCorePath != "/tw" {
		t.Errorf("TWCorePath = %q, want %q", cfg.TWCorePath, "/tw")
	}
	if cfg.IndexHTML != "/index.html" {
		t.Errorf("IndexHTML = %q, want %q", cfg.IndexHTML, "/index.html")
	}
}
