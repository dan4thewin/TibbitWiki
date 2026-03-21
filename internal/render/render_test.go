package render

import (
	"strings"
	"testing"
)

// TestToHTMLHeading verifies that a markdown heading becomes an <h1>.
func TestToHTMLHeading(t *testing.T) {
	got := ToHTML("# Hello")
	if !strings.Contains(got, "<h1>Hello</h1>") {
		t.Errorf("expected <h1>, got: %s", got)
	}
}

// TestToHTMLBold verifies bold markdown renders to <strong>.
func TestToHTMLBold(t *testing.T) {
	got := ToHTML("some **bold** text")
	if !strings.Contains(got, "<strong>bold</strong>") {
		t.Errorf("expected <strong>, got: %s", got)
	}
}

// TestToHTMLCodeBlock verifies fenced code blocks get syntax classes.
func TestToHTMLCodeBlock(t *testing.T) {
	got := ToHTML("```python\nprint('hi')\n```")
	// chroma with WithClasses produces class-based spans.
	if !strings.Contains(got, "chroma") {
		t.Errorf("expected chroma class in code block, got: %s", got)
	}
}

// TestToHTMLPlainText verifies plain text is wrapped in a paragraph.
func TestToHTMLPlainText(t *testing.T) {
	got := ToHTML("just text")
	if !strings.Contains(got, "<p>just text</p>") {
		t.Errorf("expected <p>, got: %s", got)
	}
}

// TestToHTMLEmpty verifies empty input returns empty output.
func TestToHTMLEmpty(t *testing.T) {
	got := ToHTML("")
	if got != "" {
		t.Errorf("expected empty, got: %q", got)
	}
}

// TestThemeCSSNotEmpty verifies ThemeCSS returns non-empty CSS
// containing chroma class selectors.
func TestThemeCSSNotEmpty(t *testing.T) {
	css := ThemeCSS()
	if len(css) == 0 {
		t.Fatal("ThemeCSS returned empty string")
	}
	if !strings.Contains(css, ".chroma") {
		t.Errorf("expected .chroma selector, got: %s", css[:200])
	}
}

// TestThemeCSSContainsBackground verifies the theme CSS includes
// background color rules (OneDark has a dark background).
func TestThemeCSSContainsBackground(t *testing.T) {
	css := ThemeCSS()
	if !strings.Contains(css, "background") {
		t.Errorf("expected background rule in theme CSS")
	}
}
