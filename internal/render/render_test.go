package render

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
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

// TestGenerateThemeCSS verifies that GenerateThemeCSS produces CSS
// custom properties on :root followed by chroma syntax CSS.
func TestGenerateThemeCSS(t *testing.T) {
	theme := map[string]string{
		"bg":     "#111",
		"fg":     "#eee",
		"accent": "#0af",
	}
	css := GenerateThemeCSS(theme)

	// Should have :root block with custom properties.
	if !strings.Contains(css, ":root {") {
		t.Error("expected :root block")
	}
	if !strings.Contains(css, "--tw-bg: #111;") {
		t.Errorf("expected --tw-bg, got:\n%s", css[:200])
	}
	if !strings.Contains(css, "--tw-fg: #eee;") {
		t.Errorf("expected --tw-fg, got:\n%s", css[:200])
	}
	if !strings.Contains(css, "--tw-accent: #0af;") {
		t.Errorf("expected --tw-accent, got:\n%s", css[:200])
	}

	// Keys should be sorted: accent before bg before fg.
	ai := strings.Index(css, "--tw-accent")
	bi := strings.Index(css, "--tw-bg")
	fi := strings.Index(css, "--tw-fg")
	if !(ai < bi && bi < fi) {
		t.Error("expected sorted keys: accent < bg < fg")
	}

	// Should also contain chroma syntax CSS.
	if !strings.Contains(css, ".chroma") {
		t.Error("expected chroma CSS after custom properties")
	}
}

// TestGenerateThemeCSSEmpty verifies an empty theme still produces
// a :root block and chroma CSS.
func TestGenerateThemeCSSEmpty(t *testing.T) {
	css := GenerateThemeCSS(map[string]string{})
	if !strings.Contains(css, ":root {") {
		t.Error("expected :root block for empty theme")
	}
	if !strings.Contains(css, ".chroma") {
		t.Error("expected chroma CSS for empty theme")
	}
}

// TestWriteThemeFile verifies WriteThemeFile writes CSS to disk.
func TestWriteThemeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "theme.css")
	theme := map[string]string{"bg": "#000"}

	if err := WriteThemeFile(theme, path); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	if !strings.Contains(css, "--tw-bg: #000;") {
		t.Errorf("expected --tw-bg in written file, got:\n%s", css[:200])
	}
}

// TestWriteThemeFileBadPath verifies WriteThemeFile returns an error
// for an invalid path.
func TestWriteThemeFileBadPath(t *testing.T) {
	err := WriteThemeFile(map[string]string{}, "/no/such/dir/theme.css")
	if err == nil {
		t.Error("expected error for bad path")
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

// TestListItemTightList verifies tight lists (items without blank
// lines between them) still get data-line annotations via child walk.
func TestListItemTightList(t *testing.T) {
	// Tight list with paragraphs inside (goldmark wraps in <p>).
	src := "- one\n\n- two\n\n- three"
	got := ToHTML(src)
	// Loose list items: goldmark wraps content in <p>, so the <li>
	// may not have direct lines — the child <p> does.
	if !strings.Contains(got, "data-line=") {
		t.Errorf("loose list items should have data-line, got:\n%s", got)
	}
	// All three items should be annotated.
	count := strings.Count(got, "data-line=")
	if count != 3 {
		t.Errorf("expected 3 data-line attrs, got %d in:\n%s", count, got)
	}
}

// TestListItemMixedContent verifies annotated list items alongside
// other content types.
func TestListItemMixedContent(t *testing.T) {
	src := "Paragraph.\n\n- item\n\n> quote"
	got := ToHTML(src)
	if !strings.Contains(got, `data-line="3"`) {
		t.Errorf("expected data-line on list item, got:\n%s", got)
	}
	// Two data-line attrs: one for the li, one for the blockquote.
	count := strings.Count(got, "data-line=")
	if count != 2 {
		t.Errorf("expected 2 data-line attrs, got %d", count)
	}
}

// TestByteOffsetToLineDirectLines verifies the primary path where a
// node has direct Lines() info (lines 71-73 of render.go). Goldmark's
// current parser never sets Lines on ListItem, but a future version
// might. We build a synthetic AST node with Lines set directly.
func TestByteOffsetToLineDirectLines(t *testing.T) {
	source := []byte("line one\nline two\nline three\n")
	// Create a ListItem and set a line segment starting at
	// "line two" (offset 9). That's after one newline → line 2.
	node := ast.NewListItem(0)
	seg := text.NewSegment(9, 17) // "line two"
	node.Lines().Append(seg)

	got := byteOffsetToLine(source, node)
	if got != 2 {
		t.Errorf("byteOffsetToLine = %d, want 2", got)
	}
}

// TestByteOffsetToLineNoLines verifies the return-0 path when
// neither the node nor any child has line info (line 84).
func TestByteOffsetToLineNoLines(t *testing.T) {
	source := []byte("anything")
	// A ListItem with no lines and no children.
	node := ast.NewListItem(0)
	got := byteOffsetToLine(source, node)
	if got != 0 {
		t.Errorf("byteOffsetToLine = %d, want 0", got)
	}
}

// TestRenderListItemNoLineInfo verifies that renderListItem emits
// a bare <li> (no data-line) when byteOffsetToLine returns 0
// (lines 50-52). We render a list whose ListItem has no source
// mapping by constructing minimal AST programmatically.
func TestRenderListItemNoLineInfo(t *testing.T) {
	doc := ast.NewDocument()
	list := ast.NewList('-')
	item := ast.NewListItem(0)
	tb := ast.NewTextBlock()
	// No Lines set on item or tb → byteOffsetToLine returns 0.
	item.AppendChild(item, tb)
	list.AppendChild(list, item)
	doc.AppendChild(doc, list)

	source := []byte("hello")
	var buf bytes.Buffer
	r := md.Renderer()
	if err := r.Render(&buf, source, doc); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	// Should have <li> without data-line attribute.
	if strings.Contains(got, "data-line") {
		t.Errorf("expected no data-line on lineless item, got:\n%s", got)
	}
	if !strings.Contains(got, "<li>") {
		t.Errorf("expected bare <li>, got:\n%s", got)
	}
}

// TestToHTMLConvertError verifies the error fallback that returns
// the original source when md.Convert fails (lines 90-92).
func TestToHTMLConvertError(t *testing.T) {
	patches := gomonkey.ApplyMethodFunc(
		md, "Convert",
		func(source []byte, writer io.Writer, opts ...parser.ParseOption) error {
			return fmt.Errorf("forced convert error")
		},
	)
	defer patches.Reset()

	input := "# Should be returned as-is"
	got := ToHTML(input)
	if got != input {
		t.Errorf("ToHTML = %q, want original %q", got, input)
	}
}

// TestBlockquoteLineAnnotation verifies blockquotes get data-line
// attributes with correct 1-based source line numbers.
func TestBlockquoteLineAnnotation(t *testing.T) {
	src := "# Heading\n\n> quoted text"
	got := ToHTML(src)
	// Blockquote starts on line 3.
	if !strings.Contains(got, `<blockquote data-line="3">`) {
		t.Errorf("expected blockquote with data-line=\"3\", got:\n%s", got)
	}
}

// TestBlockquoteMultiLine verifies a multi-line blockquote gets a
// single data-line pointing to the first line.
func TestBlockquoteMultiLine(t *testing.T) {
	src := "> line one\n> line two\n> line three"
	got := ToHTML(src)
	if !strings.Contains(got, `data-line="1"`) {
		t.Errorf("expected data-line=\"1\", got:\n%s", got)
	}
	// Only one blockquote → one data-line.
	count := strings.Count(got, "data-line=")
	if count != 1 {
		t.Errorf("expected 1 data-line, got %d in:\n%s", count, got)
	}
}

// TestBlockquoteNested verifies nested blockquotes each get their
// own data-line attribute.
func TestBlockquoteNested(t *testing.T) {
	src := "> outer\n>\n>> inner"
	got := ToHTML(src)
	count := strings.Count(got, "data-line=")
	if count < 2 {
		t.Errorf("expected at least 2 data-line attrs for nested quotes, got %d in:\n%s", count, got)
	}
}

// TestBlockquoteAndListAnnotation verifies both list items and
// blockquotes get correct data-line in the same document.
func TestBlockquoteAndListAnnotation(t *testing.T) {
	src := "- item one\n- item two\n\n> a quote"
	got := ToHTML(src)
	// Items on lines 1, 2; blockquote on line 4.
	if !strings.Contains(got, `<li data-line="1"`) {
		t.Errorf("expected li data-line=\"1\", got:\n%s", got)
	}
	if !strings.Contains(got, `<li data-line="2"`) {
		t.Errorf("expected li data-line=\"2\", got:\n%s", got)
	}
	if !strings.Contains(got, `<blockquote data-line="4">`) {
		t.Errorf("expected blockquote data-line=\"4\", got:\n%s", got)
	}
}

// TestRenderBlockquoteNoLineInfo verifies that renderBlockquote emits
// a bare <blockquote> (no data-line) when byteOffsetToLine returns 0.
func TestRenderBlockquoteNoLineInfo(t *testing.T) {
	doc := ast.NewDocument()
	bq := ast.NewBlockquote()
	// No children with line info → byteOffsetToLine returns 0.
	doc.AppendChild(doc, bq)

	source := []byte("hello")
	var buf bytes.Buffer
	r := md.Renderer()
	if err := r.Render(&buf, source, doc); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if strings.Contains(got, "data-line") {
		t.Errorf("expected no data-line on lineless blockquote, got:\n%s", got)
	}
	if !strings.Contains(got, "<blockquote>") {
		t.Errorf("expected bare <blockquote>, got:\n%s", got)
	}
}

// TestToPlainHTMLCodeBlockLines verifies that ToPlainHTML renders
// fenced code blocks with per-line data-line attributes.
func TestToPlainHTMLCodeBlockLines(t *testing.T) {
	src := "# Title\n\n```go\nfmt.Println(\"hello\")\nreturn nil\n```"
	got := ToPlainHTML(src)
	// Code starts on line 4 (line 3 is the opening fence).
	if !strings.Contains(got, `data-line="4"`) {
		t.Errorf("expected data-line=\"4\" for first code line, got:\n%s", got)
	}
	if !strings.Contains(got, `data-line="5"`) {
		t.Errorf("expected data-line=\"5\" for second code line, got:\n%s", got)
	}
	// Should NOT have chroma classes.
	if strings.Contains(got, "chroma") {
		t.Errorf("plain render should not have chroma classes, got:\n%s", got)
	}
}

// TestToPlainHTMLPreservesListAnnotation verifies that ToPlainHTML
// still annotates list items (the lineAnnotator is shared).
func TestToPlainHTMLPreservesListAnnotation(t *testing.T) {
	src := "- alpha\n- beta"
	got := ToPlainHTML(src)
	if !strings.Contains(got, `data-line="1"`) {
		t.Errorf("expected data-line on list items in plain mode, got:\n%s", got)
	}
}

// TestToPlainHTMLEscapesHTML verifies that code content is HTML-escaped
// in the plain render to prevent XSS.
func TestToPlainHTMLEscapesHTML(t *testing.T) {
	src := "```\n<script>alert(1)</script>\n```"
	got := ToPlainHTML(src)
	if strings.Contains(got, "<script>") {
		t.Errorf("expected HTML escaping in code block, got:\n%s", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("expected escaped <script> tag, got:\n%s", got)
	}
}

// TestToPlainHTMLEmpty verifies empty input returns empty output.
func TestToPlainHTMLEmpty(t *testing.T) {
	got := ToPlainHTML("")
	if got != "" {
		t.Errorf("expected empty, got: %q", got)
	}
}

// TestToPlainHTMLConvertError verifies the error fallback returns
// the original source when plainMd.Convert fails.
func TestToPlainHTMLConvertError(t *testing.T) {
	patches := gomonkey.ApplyMethodFunc(
		plainMd, "Convert",
		func(source []byte, writer io.Writer, opts ...parser.ParseOption) error {
			return fmt.Errorf("forced convert error")
		},
	)
	defer patches.Reset()

	input := "# Should be returned as-is"
	got := ToPlainHTML(input)
	if got != input {
		t.Errorf("ToPlainHTML = %q, want original %q", got, input)
	}
}

// TestDiffToHTMLHighlights verifies DiffToHTML produces syntax-
// highlighted HTML for unified diff input.
func TestDiffToHTMLHighlights(t *testing.T) {
	diff := `--- a/test.md
+++ b/test.md
@@ -1,3 +1,3 @@
 unchanged
-old line
+new line
 unchanged
`
	got := DiffToHTML(diff)
	// Should produce chroma-highlighted HTML, not raw text.
	if !strings.Contains(got, "<") {
		t.Error("expected HTML output")
	}
	if strings.Contains(got, "<pre>"+diff) {
		t.Error("should not fall back to plain <pre> for valid diff")
	}
}

// TestDiffToHTMLEmpty verifies DiffToHTML handles empty input.
func TestDiffToHTMLEmpty(t *testing.T) {
	got := DiffToHTML("")
	if got == "" {
		t.Error("expected some HTML output even for empty diff")
	}
}
