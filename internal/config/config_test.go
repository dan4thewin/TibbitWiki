package config

import (
	"os"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
)

// TestDefaultsBadYAMLPanics verifies that Defaults panics when the
// embedded YAML is corrupt (a broken binary safety check).
func TestDefaultsBadYAMLPanics(t *testing.T) {
	patches := gomonkey.ApplyGlobalVar(&DefaultYAML, []byte("{{invalid yaml"))
	defer patches.Reset()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for bad embedded YAML")
		}
	}()
	Defaults()
}

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
	if cfg.MaxDiffs != 10 {
		t.Errorf("MaxDiffs = %d, want 10", cfg.MaxDiffs)
	}
	if cfg.AttachmentExclude == nil {
		// YAML [] unmarshals as nil; both nil and empty mean "no exclusions".
	} else if len(cfg.AttachmentExclude) != 0 {
		t.Errorf("AttachmentExclude = %v, want empty", cfg.AttachmentExclude)
	}
	// Theme must have a "shared" section with wiki_title.
	theme := DefaultTheme()
	if theme["shared"] == nil {
		t.Fatal("DefaultTheme missing shared section")
	}
	if theme["shared"]["wiki_title"] != "TibbitWiki" {
		t.Errorf("wiki_title = %q, want %q", theme["shared"]["wiki_title"], "TibbitWiki")
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
// default theme with all expected shared keys.
func TestLoadDefaultTheme(t *testing.T) {
	cfg, err := Load([]string{})
	if err != nil {
		t.Fatal(err)
	}
	expected := DefaultSharedTheme()
	shared := cfg.Theme["shared"]
	for k, v := range expected {
		if shared[k] != v {
			t.Errorf("Theme[shared][%q] = %q, want %q", k, shared[k], v)
		}
	}
}

// TestLoadYAMLTheme verifies that a theme section in YAML overrides
// shared defaults, and missing keys are filled from defaults.
func TestLoadYAMLTheme(t *testing.T) {
	content := `theme:
  shared:
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
	shared := cfg.Theme["shared"]
	// Overridden keys.
	if shared["bg"] != "#000000" {
		t.Errorf("Theme[shared][bg] = %q, want %q", shared["bg"], "#000000")
	}
	if shared["accent"] != "#ff0000" {
		t.Errorf("Theme[shared][accent] = %q, want %q", shared["accent"], "#ff0000")
	}
	// Missing key should be filled from defaults.
	defaults := DefaultSharedTheme()
	if shared["fg"] != defaults["fg"] {
		t.Errorf("Theme[shared][fg] = %q, want default %q", shared["fg"], defaults["fg"])
	}
	if shared["border"] != defaults["border"] {
		t.Errorf("Theme[shared][border] = %q, want default %q", shared["border"], defaults["border"])
	}
}

// TestMergedThemeSharedOnly verifies MergedTheme with no wiki overrides.
func TestMergedThemeSharedOnly(t *testing.T) {
	theme := Theme{
		"shared": {"bg": "#111", "fg": "#eee"},
	}
	merged := MergedTheme(theme, "work")
	// Should have the shared values overlaid on defaults.
	if merged["bg"] != "#111" {
		t.Errorf("bg = %q, want %q", merged["bg"], "#111")
	}
	if merged["fg"] != "#eee" {
		t.Errorf("fg = %q, want %q", merged["fg"], "#eee")
	}
	// Missing keys filled from defaults.
	defaults := DefaultSharedTheme()
	if merged["accent"] != defaults["accent"] {
		t.Errorf("accent = %q, want default %q", merged["accent"], defaults["accent"])
	}
}

// TestMergedThemeWikiOverride verifies wiki-specific values override shared.
func TestMergedThemeWikiOverride(t *testing.T) {
	theme := Theme{
		"shared": {"bg": "#111", "accent": "#aaa"},
		"work":   {"accent": "#bbb", "wiki_title": "Work"},
	}
	merged := MergedTheme(theme, "work")
	if merged["bg"] != "#111" {
		t.Errorf("bg = %q, want shared %q", merged["bg"], "#111")
	}
	if merged["accent"] != "#bbb" {
		t.Errorf("accent = %q, want wiki override %q", merged["accent"], "#bbb")
	}
	if merged["wiki_title"] != "Work" {
		t.Errorf("wiki_title = %q, want %q", merged["wiki_title"], "Work")
	}
}

// TestLoadYAMLThemeWikiOnly verifies that a config with wiki-specific
// theme but no explicit shared section gets defaults filled in.
func TestLoadYAMLThemeWikiOnly(t *testing.T) {
	content := `theme:
  work:
    accent: "#00ff00"
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
	// Shared section should be filled from defaults.
	defaults := DefaultSharedTheme()
	if cfg.Theme["shared"]["bg"] != defaults["bg"] {
		t.Errorf("shared bg = %q, want default %q", cfg.Theme["shared"]["bg"], defaults["bg"])
	}
	// Wiki override preserved.
	if cfg.Theme["work"]["accent"] != "#00ff00" {
		t.Errorf("work accent = %q, want %q", cfg.Theme["work"]["accent"], "#00ff00")
	}
}

// TestLoadYAMLThemeNull verifies that when a YAML config file
// explicitly nulls the theme section, Load fills it from defaults.
// This covers the cfg.Theme == nil branch (line 163).
func TestLoadYAMLThemeNull(t *testing.T) {
	content := "listen: \"127.0.0.1:9999\"\ntheme:\n"
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
	// Theme should be filled from defaults despite YAML nulling it.
	defaults := DefaultSharedTheme()
	if cfg.Theme == nil {
		t.Fatal("Theme should not be nil after Load")
	}
	if cfg.Theme["shared"]["bg"] != defaults["bg"] {
		t.Errorf("Theme[shared][bg] = %q, want default %q",
			cfg.Theme["shared"]["bg"], defaults["bg"])
	}
}

// TestLoadYAMLThemeSharedNull verifies that when a YAML config has
// a theme section but explicitly nulls the shared key, Load fills
// shared from defaults. Covers cfg.Theme["shared"] == nil (line 165).
func TestLoadYAMLThemeSharedNull(t *testing.T) {
	content := "theme:\n  shared:\n  work:\n    accent: \"#00ff00\"\n"
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
	// Theme is non-nil (has "work"), but shared was null → filled.
	defaults := DefaultSharedTheme()
	if cfg.Theme["shared"] == nil {
		t.Fatal("shared should be filled from defaults")
	}
	if cfg.Theme["shared"]["bg"] != defaults["bg"] {
		t.Errorf("shared bg = %q, want default %q",
			cfg.Theme["shared"]["bg"], defaults["bg"])
	}
	// Wiki override preserved.
	if cfg.Theme["work"]["accent"] != "#00ff00" {
		t.Errorf("work accent = %q, want %q",
			cfg.Theme["work"]["accent"], "#00ff00")
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

// TestLoadYAMLAttachmentExclude verifies that attachment_exclude
// is parsed from the YAML config file.
func TestLoadYAMLAttachmentExclude(t *testing.T) {
	content := `attachment_exclude: [".pdf.md", ".bak"]
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
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.AttachmentExclude) != 2 {
		t.Fatalf("AttachmentExclude len = %d, want 2", len(cfg.AttachmentExclude))
	}
	if cfg.AttachmentExclude[0] != ".pdf.md" {
		t.Errorf("AttachmentExclude[0] = %q, want %q", cfg.AttachmentExclude[0], ".pdf.md")
	}
	if cfg.AttachmentExclude[1] != ".bak" {
		t.Errorf("AttachmentExclude[1] = %q, want %q", cfg.AttachmentExclude[1], ".bak")
	}
}
