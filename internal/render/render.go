// Package render converts note markdown text to HTML.
package render

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"os"
	"sort"
	"strings"
	"text/template"

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
		extension.TaskList,
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

// protectFences replaces fenced code blocks (``` or ~~~) with
// placeholders so template expansion doesn't process them.
// Returns the protected text and the list of original fences.
func protectFences(source string) (string, []string) {
	var fences []string
	var result strings.Builder
	lines := strings.SplitAfter(source, "\n")
	inFence := false
	var fenceMarker string
	var fenceBuf strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inFence {
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = true
				fenceMarker = trimmed[:3]
				fenceBuf.Reset()
				fenceBuf.WriteString(line)
				continue
			}
			result.WriteString(line)
		} else {
			fenceBuf.WriteString(line)
			if strings.HasPrefix(trimmed, fenceMarker) && strings.TrimLeft(trimmed, fenceMarker[:1]) == "" {
				idx := len(fences)
				fences = append(fences, fenceBuf.String())
				fmt.Fprintf(&result, "\x00FENCE%d\x00", idx)
				inFence = false
			}
		}
	}
	// Unclosed fence: emit as-is.
	if inFence {
		result.WriteString(fenceBuf.String())
	}
	return result.String(), fences
}

// ExpandTemplates processes Go text/template directives in source,
// using the provided template definitions as functions.  Code blocks
// (fenced with ``` or ~~~) are protected from expansion.
func ExpandTemplates(source string, templates map[string]string) string {
	if len(templates) == 0 || !strings.Contains(source, "{{") {
		return source
	}
	funcMap := buildFuncMap(templates)
	if len(funcMap) == 0 {
		return source
	}
	// Replace code fences with placeholders to protect them.
	protected, fences := protectFences(source)

	t, err := template.New("note").Funcs(funcMap).Parse(protected)
	if err != nil {
		return templateError(err) + source
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, nil); err != nil {
		return templateError(err) + source
	}
	result := buf.String()

	// Restore code fences.
	for i, f := range fences {
		result = strings.Replace(result, fmt.Sprintf("\x00FENCE%d\x00", i), f, 1)
	}
	return result
}

func templateError(err error) string {
	msg := stdhtml.EscapeString(err.Error())
	return fmt.Sprintf("<div style=\"background:#fee;color:#900;padding:0.5em;"+
		"border:1px solid #c00;margin-bottom:1em\">⚠ Template error: %s</div>\n\n", msg)
}

// buildFuncMap compiles template definitions into a FuncMap.
// Each entry is a function that takes a string and returns a string,
// enabling pipeline composition: {{img "pic.png" | half | center}}.
func buildFuncMap(templates map[string]string) template.FuncMap {
	fm := template.FuncMap{}
	for name, tmplStr := range templates {
		t, err := template.New(name).Parse(tmplStr)
		if err != nil {
			continue
		}
		// Capture t for the closure.
		fn := t
		fm[name] = func(s string) string {
			var buf bytes.Buffer
			if err := fn.Execute(&buf, s); err != nil {
				return s
			}
			return buf.String()
		}
	}
	return fm
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

// attachMd is a safe renderer for attachment .md files: no raw HTML
// passthrough, no line annotations.  Tables, linkify, and syntax
// highlighting are preserved.
var attachMd = goldmark.New(
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
)

// AttachmentToHTML renders markdown to HTML using a safe goldmark
// instance that escapes raw HTML (suitable for untrusted content
// like LLM-generated shadow files).
func AttachmentToHTML(source string) string {
	var buf bytes.Buffer
	if err := attachMd.Convert([]byte(source), &buf); err != nil {
		return stdhtml.EscapeString(source)
	}
	return buf.String()
}

// AttachmentPage wraps rendered HTML in a minimal themed page shell.
func AttachmentPage(body string) string {
	var buf bytes.Buffer
	buf.WriteString(`<!DOCTYPE html>
<html><head>
<meta charset="utf-8">
<link rel="stylesheet" href="../theme.css">
<style>
body { max-width: 800px; margin: 2em auto; padding: 0 1em;
       background: var(--tw-bg); color: var(--tw-fg);
       font-family: var(--tw-font_family); font-size: var(--tw-font_size);
       line-height: var(--tw-line_height); }
a { color: var(--tw-link); }
a:hover { color: var(--tw-link_hover); }
table { border-collapse: collapse; margin: 1em 0; }
th, td { border: 1px solid var(--tw-border); padding: 6px 12px; }
code { background: var(--tw-code_bg); padding: 2px 6px; border-radius: 3px; font-family: var(--tw-code_font); }
pre { background: var(--tw-code_bg); padding: 1em; border-radius: 6px; overflow-x: auto; font-family: var(--tw-code_font); tab-size: var(--tw-code_tab_size); }
</style>
</head><body>
`)
	buf.WriteString(body)
	buf.WriteString("\n</body></html>\n")
	return buf.String()
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
