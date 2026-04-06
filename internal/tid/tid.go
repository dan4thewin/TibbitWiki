// Package tid parses and serializes .md files.
//
// The .md format uses RFC822-style headers (key: value), a blank
// line separator, then the body text. Example:
//
//	created: 2023-07-10T17:45:28Z
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
		// "text" is reserved for the body — ignore if in headers.
		if key == "text" {
			continue
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
	// deterministic output. Title and tags come first.
	keys := make([]string, 0, len(t.Fields))
	for k := range t.Fields {
		if k != "text" && k != "title" && k != "tags" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	// Write title first, tags second (always present), then the rest.
	if v, ok := t.Fields["title"]; ok {
		fmt.Fprintf(&b, "title: %s\n", v)
	}
	fmt.Fprintf(&b, "tags: %s\n", t.Fields["tags"])
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, t.Fields[k])
	}

	b.WriteString("\n")
	if text, ok := t.Fields["text"]; ok {
		b.WriteString(text)
	}

	return []byte(b.String())
}

// ValidTitle reports whether a note title is safe for use in file
// paths. Titles containing ".." path components or leading "/" are
// rejected to prevent path traversal.
func ValidTitle(title string) bool {
	if title == "" {
		return false
	}
	if strings.HasPrefix(title, "/") {
		return false
	}
	for _, seg := range strings.Split(title, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// TitleToFilename converts a note title to its .md filename.
// Titles containing path separators get URL-encoded.
func TitleToFilename(title string) string {
	if strings.Contains(title, "/") {
		return url.PathEscape(title) + ".md"
	}
	return title + ".md"
}

// FilenameToTitle converts a .md filename back to a note title.
// Only names containing %2F (encoded slash) are decoded, matching
// the encoding done by TitleToFilename. Other percent sequences
// are left as-is to avoid corrupting titles with literal %XX.
func FilenameToTitle(filename string) string {
	name := strings.TrimSuffix(filepath.Base(filename), ".md")

	if strings.Contains(name, "%2F") || strings.Contains(name, "%2f") {
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
