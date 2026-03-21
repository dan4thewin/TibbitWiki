// Package render converts note markdown text to HTML.
package render

import (
	"bytes"
	"fmt"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"

	highlighting "github.com/yuin/goldmark-highlighting/v2"
)

var md = goldmark.New(
	goldmark.WithExtensions(
		highlighting.NewHighlighting(
			highlighting.WithStyle("onedark"),
			highlighting.WithFormatOptions(
				chromahtml.WithClasses(true),
			),
		),
	),
	goldmark.WithRendererOptions(
		renderer.WithNodeRenderers(
			util.Prioritized(&lineAnnotator{}, 200),
		),
	),
)

// lineAnnotator adds data-line attributes to <li> elements.
type lineAnnotator struct{}

func (r *lineAnnotator) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindListItem, r.renderListItem)
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

// byteOffsetToLine returns the 1-based source line number for a
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

// ThemeCSS returns the CSS for the configured syntax highlighting theme.
func ThemeCSS() string {
	style := styles.Get("onedark")
	formatter := chromahtml.New(chromahtml.WithClasses(true))
	var buf bytes.Buffer
	formatter.WriteCSS(&buf, style)
	return buf.String()
}
