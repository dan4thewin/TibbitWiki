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

// TestListItemLineAnnotation verifies <li> elements get data-line
// attributes matching their 1-based source line number.
func TestListItemLineAnnotation(t *testing.T) {
	// Line 1: heading, line 2: blank, line 3-5: list items.
	src := "# Title\n\n- alpha\n- beta\n- gamma"
	got := ToHTML(src)
	if !strings.Contains(got, `data-line="3"`) {
		t.Errorf("expected data-line=\"3\" for alpha, got:\n%s", got)
	}
	if !strings.Contains(got, `data-line="4"`) {
		t.Errorf("expected data-line=\"4\" for beta, got:\n%s", got)
	}
	if !strings.Contains(got, `data-line="5"`) {
		t.Errorf("expected data-line=\"5\" for gamma, got:\n%s", got)
	}
}

// TestListItemLineAnnotationNested verifies nested list items also
// get correct data-line attributes.
func TestListItemLineAnnotationNested(t *testing.T) {
	src := "- parent\n  - child\n- sibling"
	got := ToHTML(src)
	if !strings.Contains(got, `data-line="1"`) {
		t.Errorf("expected data-line=\"1\" for parent, got:\n%s", got)
	}
	if !strings.Contains(got, `data-line="2"`) {
		t.Errorf("expected data-line=\"2\" for child, got:\n%s", got)
	}
	if !strings.Contains(got, `data-line="3"`) {
		t.Errorf("expected data-line=\"3\" for sibling, got:\n%s", got)
	}
}

// TestListItemLineAnnotationOrdered verifies ordered lists get
// data-line attributes too.
func TestListItemLineAnnotationOrdered(t *testing.T) {
	src := "1. first\n2. second"
	got := ToHTML(src)
	if !strings.Contains(got, `data-line="1"`) {
		t.Errorf("expected data-line=\"1\", got:\n%s", got)
	}
	if !strings.Contains(got, `data-line="2"`) {
		t.Errorf("expected data-line=\"2\", got:\n%s", got)
	}
}

// TestNonListNoAnnotation verifies paragraphs and headings do NOT
// get data-line attributes.
func TestNonListNoAnnotation(t *testing.T) {
	src := "# Heading\n\nA paragraph."
	got := ToHTML(src)
	if strings.Contains(got, "data-line") {
		t.Errorf("paragraphs/headings should not have data-line, got:\n%s", got)
	}
}
