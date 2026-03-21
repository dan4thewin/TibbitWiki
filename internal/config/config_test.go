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

// TestLoadYAMLFile verifies that a YAML config file is parsed.
func TestLoadYAMLFile(t *testing.T) {
	content := `wiki_path: /yaml/wiki
listen: "127.0.0.1:3000"
editor_cmd: "code %s"
poll_interval: 2s
`
	f, err := os.CreateTemp("", "go-tiddle-*.yaml")
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
	f, err := os.CreateTemp("", "go-tiddle-*.yaml")
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
	f, err := os.CreateTemp("", "go-tiddle-*.yaml")
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

// TestTiddlersPath verifies the tiddlers subdirectory path.
func TestTiddlersPath(t *testing.T) {
	cfg := Config{WikiPath: "/my/wiki"}
	if got := cfg.TiddlersPath(); got != "/my/wiki/tiddlers" {
		t.Errorf("TiddlersPath() = %q, want %q", got, "/my/wiki/tiddlers")
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
