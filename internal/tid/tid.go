// Package tid parses and serializes .md files.
//
// The .md format uses RFC822-style headers (key: value), a blank
// line separator, then the body text. Example:
//
//	created: 20230710174528965
//	tags: Journal
//	title: My Note
//
//	Body text here.
package tid

import (
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
)

// Note represents a single note with its fields and body text.
type Note struct {
	Fields map[string]string
}

// Title returns the note's title field.
func (t *Note) Title() string {
	return t.Fields["title"]
}

// Text returns the note's body text.
func (t *Note) Text() string {
	return t.Fields["text"]
}

// SkinnyFields returns all fields except "text", suitable for the
// skinny note list endpoint.
func (t *Note) SkinnyFields() map[string]string {
	out := make(map[string]string, len(t.Fields))
	for k, v := range t.Fields {
		if k != "text" {
			out[k] = v
		}
	}
	return out
}

// Parse reads a .md file's content and returns a Note.
// The format is: header lines (key: value), blank line, body text.
func Parse(data []byte) (*Note, error) {
	content := string(data)
	fields := make(map[string]string)

	// Split into header section and body at the first blank line.
	headerEnd, body := splitHeaderBody(content)

	// Parse header lines.
	for _, line := range strings.Split(headerEnd, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		key, val, ok := parseHeaderLine(line)
		if !ok {
			return nil, fmt.Errorf("invalid header line: %q", line)
		}
		fields[key] = val
	}

	if _, ok := fields["title"]; !ok {
		return nil, fmt.Errorf("missing required field: title")
	}

	if body != "" {
		fields["text"] = body
	}

	return &Note{Fields: fields}, nil
}

// Serialize writes a Note back to .md format bytes.
func Serialize(t *Note) []byte {
	var b strings.Builder

	// Collect header keys (everything except "text"), sorted for
	// deterministic output.
	keys := make([]string, 0, len(t.Fields))
	for k := range t.Fields {
		if k != "text" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, t.Fields[k])
	}

	b.WriteString("\n")
	if text, ok := t.Fields["text"]; ok {
		b.WriteString(text)
	}

	return []byte(b.String())
}

// TitleToFilename converts a note title to its .md filename.
// $:/foo/bar becomes $__foo_bar.md. Titles containing path
// separators get URL-encoded.
func TitleToFilename(title string) string {
	// System notes: $:/ prefix → $__ prefix, / → _
	if strings.HasPrefix(title, "$:/") {
		name := "$__" + strings.ReplaceAll(title[3:], "/", "_")
		return name + ".md"
	}
	// Non-system notes with slashes get URL-encoded.
	if strings.Contains(title, "/") {
		return url.PathEscape(title) + ".md"
	}
	return title + ".md"
}

// FilenameToTitle converts a .md filename back to a note title.
// $__foo_bar.md becomes $:/foo/bar. URL-encoded names are decoded.
func FilenameToTitle(filename string) string {
	name := strings.TrimSuffix(filepath.Base(filename), ".md")

	// System notes: $__ prefix → $:/ prefix, _ → /
	if strings.HasPrefix(name, "$__") {
		return "$:/" + strings.ReplaceAll(name[3:], "_", "/")
	}

	// Try URL-decoding for names with percent-encoded characters.
	if strings.Contains(name, "%") {
		decoded, err := url.PathUnescape(name)
		if err == nil {
			return decoded
		}
	}

	return name
}

// splitHeaderBody splits content at the first blank line into
// headers and body. If no blank line, everything is headers.
func splitHeaderBody(content string) (headers string, body string) {
	// Look for \n\n (Unix) or \r\n\r\n (Windows).
	if idx := strings.Index(content, "\n\n"); idx >= 0 {
		return content[:idx], content[idx+2:]
	}
	if idx := strings.Index(content, "\r\n\r\n"); idx >= 0 {
		return content[:idx], content[idx+4:]
	}
	return content, ""
}

// parseHeaderLine splits "key: value" into its parts.
func parseHeaderLine(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ": ")
	if idx < 1 {
		return "", "", false
	}
	return line[:idx], line[idx+2:], true
}
