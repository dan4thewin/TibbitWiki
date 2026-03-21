// Package render converts note markdown text to HTML.
package render

import (
	"bytes"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
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
)

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
