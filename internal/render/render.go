// Package render converts note markdown text to HTML.
package render

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"os"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"

	highlighting "github.com/yuin/goldmark-highlighting/v2"
)

var md = goldmark.New(
	goldmark.WithExtensions(
		extension.Linkify,
		extension.Table,
		highlighting.NewHighlighting(
			highlighting.WithStyle("onedark"),
			highlighting.WithFormatOptions(
				chromahtml.WithClasses(true),
			),
		),
	),
	goldmark.WithRendererOptions(
		html.WithUnsafe(),
		renderer.WithNodeRenderers(
			util.Prioritized(&lineAnnotator{}, 200),
		),
	),
)

// lineAnnotator adds data-line attributes to <li>, blockquote, and
// fenced code block elements.
type lineAnnotator struct{}

func (r *lineAnnotator) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindListItem, r.renderListItem)
	reg.Register(ast.KindBlockquote, r.renderBlockquote)
}

func (r *lineAnnotator) renderListItem(
	w util.BufWriter, source []byte, n ast.Node, entering bool,
) (ast.WalkStatus, error) {
	if entering {
		li := n.(*ast.ListItem)
		line := byteOffsetToLine(source, li)
		if line > 0 {
			_, _ = fmt.Fprintf(w, "<li data-line=\"%d\"", line)
		} else {
			_, _ = w.WriteString("<li")
		}
		html.RenderAttributes(w, n, html.ListItemAttributeFilter)
		_ = w.WriteByte('>')
		fc := n.FirstChild()
		if fc != nil {
			if _, ok := fc.(*ast.TextBlock); !ok {
				_ = w.WriteByte('\n')
			}
		}
	} else {
		_, _ = w.WriteString("</li>\n")
	}
	return ast.WalkContinue, nil
}

func (r *lineAnnotator) renderBlockquote(
	w util.BufWriter, source []byte, n ast.Node, entering bool,
) (ast.WalkStatus, error) {
	if entering {
		line := byteOffsetToLine(source, n)
		if line > 0 {
			_, _ = fmt.Fprintf(w, "<blockquote data-line=\"%d\">\n", line)
		} else {
			_, _ = w.WriteString("<blockquote>\n")
		}
	} else {
		_, _ = w.WriteString("</blockquote>\n")
	}
	return ast.WalkContinue, nil
}

// list item node by counting newlines before its first segment.
func byteOffsetToLine(source []byte, n ast.Node) int {
	lines := n.Lines()
	if lines.Len() > 0 {
		offset := lines.At(0).Start
		return bytes.Count(source[:offset], []byte{'\n'}) + 1
	}
	// Tight list items may have no direct lines — walk to first
	// child that has line info.
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		cl := c.Lines()
		if cl.Len() > 0 {
			offset := cl.At(0).Start
			return bytes.Count(source[:offset], []byte{'\n'}) + 1
		}
	}
	return 0
}

// ToHTML renders markdown source to HTML.
func ToHTML(source string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(source), &buf); err != nil {
		return source
	}
	return buf.String()
}

// plainMd is a second renderer without chroma highlighting.
// Fenced code blocks are rendered with per-line data-line attributes
// so the browser can offer line-level editing.
var plainMd = goldmark.New(
	goldmark.WithRendererOptions(
		renderer.WithNodeRenderers(
			util.Prioritized(&lineAnnotator{}, 200),
			util.Prioritized(&plainCodeAnnotator{}, 100),
		),
	),
)

// plainCodeAnnotator renders fenced code blocks with per-line
// data-line attributes instead of syntax highlighting.
type plainCodeAnnotator struct{}

func (r *plainCodeAnnotator) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
}

func (r *plainCodeAnnotator) renderFencedCodeBlock(
	w util.BufWriter, source []byte, n ast.Node, entering bool,
) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<pre><code>")
		lines := n.Lines()
		// Find the fence opening line (one line before the first content line).
		startLine := 0
		if lines.Len() > 0 {
			startLine = bytes.Count(source[:lines.At(0).Start], []byte{'\n'})
		}
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			lineNum := startLine + i + 1
			content := seg.Value(source)
			// Trim trailing newline for display, we close the div ourselves.
			content = bytes.TrimRight(content, "\n")
			_, _ = fmt.Fprintf(w, "<div data-line=\"%d\">", lineNum)
			// Escape HTML entities in code content.
			html.DefaultWriter.RawWrite(w, content)
			_, _ = w.WriteString("</div>")
		}
	} else {
		_, _ = w.WriteString("</code></pre>\n")
	}
	return ast.WalkContinue, nil
}

// ToPlainHTML renders markdown without syntax highlighting, with
// per-line data-line attributes on fenced code block lines.
func ToPlainHTML(source string) string {
	var buf bytes.Buffer
	if err := plainMd.Convert([]byte(source), &buf); err != nil {
		return source
	}
	return buf.String()
}

// ThemeCSS returns the CSS for the configured syntax highlighting theme.
func ThemeCSS() string {
	style := styles.Get("onedark")
	formatter := chromahtml.New(chromahtml.WithClasses(true))
	var buf bytes.Buffer
	formatter.WriteCSS(&buf, style)
	return buf.String()
}

// sanitizeCSSValue strips characters that could break out of a CSS
// property value context: }, <, >, ;, {, and newlines.
func sanitizeCSSValue(s string) string {
	return strings.NewReplacer(
		"}", "", "{", "", "<", "", ">", "", ";", "", "\n", "", "\r", "",
	).Replace(s)
}

// GenerateThemeCSS produces a complete CSS file: custom properties
// from the theme map on :root, followed by chroma syntax highlighting.
func GenerateThemeCSS(theme map[string]string) string {
	var buf bytes.Buffer
	buf.WriteString(":root {\n")

	// Sorted keys for deterministic output.
	keys := make([]string, 0, len(theme))
	for k := range theme {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(&buf, "  --tw-%s: %s;\n",
			sanitizeCSSValue(k), sanitizeCSSValue(theme[k]))
	}
	buf.WriteString("}\n")

	// Append chroma syntax highlighting CSS.
	buf.WriteString(ThemeCSS())
	return buf.String()
}

// WriteThemeFile generates theme CSS and writes it to path.
func WriteThemeFile(theme map[string]string, path string) error {
	return os.WriteFile(path, []byte(GenerateThemeCSS(theme)), 0644)
}

// coalesceFunc wraps chroma.Coalesce; replaceable for testing.
var coalesceFunc = chroma.Coalesce

// DiffToHTML renders unified diff text as syntax-highlighted HTML
// using the chroma Diff lexer and OneDark theme.
func DiffToHTML(diff string) string {
	lexer := lexers.Get("Diff")
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = coalesceFunc(lexer)
	style := styles.Get("onedark")
	formatter := chromahtml.New(chromahtml.WithClasses(true))
	iter, err := lexer.Tokenise(nil, diff)
	if err != nil {
		return "<pre>" + stdhtml.EscapeString(diff) + "</pre>"
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iter); err != nil {
		return "<pre>" + stdhtml.EscapeString(diff) + "</pre>"
	}
	return buf.String()
}
