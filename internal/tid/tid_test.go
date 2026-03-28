package tid

import (
	"strings"
	"testing"
)

// TestParseValidNote verifies that a standard .md file with
// headers and body is parsed correctly into fields.
func TestParseValidNote(t *testing.T) {
	input := "created: 20230710174528965\ntags: Journal\ntitle: My Note\n\nBody text here.\nSecond line."
	got, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Title() != "My Note" {
		t.Errorf("title = %q, want %q", got.Title(), "My Note")
	}
	if got.Fields["created"] != "20230710174528965" {
		t.Errorf("created = %q, want %q", got.Fields["created"], "20230710174528965")
	}
	if got.Fields["tags"] != "Journal" {
		t.Errorf("tags = %q, want %q", got.Fields["tags"], "Journal")
	}
	if got.Text() != "Body text here.\nSecond line." {
		t.Errorf("text = %q, want %q", got.Text(), "Body text here.\nSecond line.")
	}
}

// TestParseWindowsLineEndings verifies that \r\n line endings are
// handled correctly in both headers and body.
func TestParseWindowsLineEndings(t *testing.T) {
	input := "title: WinNote\r\ntags: Test\r\n\r\nBody with CRLF."
	got, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Title() != "WinNote" {
		t.Errorf("title = %q, want %q", got.Title(), "WinNote")
	}
	if got.Fields["tags"] != "Test" {
		t.Errorf("tags = %q, want %q", got.Fields["tags"], "Test")
	}
	if got.Text() != "Body with CRLF." {
		t.Errorf("text = %q, want %q", got.Text(), "Body with CRLF.")
	}
}

// TestParseHeadersOnly verifies that a note with no body text
// (no blank line separator) parses correctly with no text field.
func TestParseHeadersOnly(t *testing.T) {
	input := "title: NoBody\ntags: Empty"
	got, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Title() != "NoBody" {
		t.Errorf("title = %q, want %q", got.Title(), "NoBody")
	}
	if got.Text() != "" {
		t.Errorf("text = %q, want empty", got.Text())
	}
}

// TestParseEmptyBody verifies that a blank line followed by nothing
// results in empty text (not stored).
func TestParseEmptyBody(t *testing.T) {
	input := "title: EmptyBody\n\n"
	got, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Title() != "EmptyBody" {
		t.Errorf("title = %q, want %q", got.Title(), "EmptyBody")
	}
	if got.Text() != "" {
		t.Errorf("text = %q, want empty", got.Text())
	}
}

// TestParseMissingTitle verifies that a note without a title
// field returns an error.
func TestParseMissingTitle(t *testing.T) {
	input := "tags: Oops\n\nNo title here."
	_, err := Parse([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing title, got nil")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("error = %q, want mention of 'title'", err.Error())
	}
}

// TestParseInvalidHeader verifies that a malformed header line
// (no colon-space separator) returns an error.
func TestParseInvalidHeader(t *testing.T) {
	input := "title: Good\nthis is not a header\n\nBody."
	_, err := Parse([]byte(input))
	if err == nil {
		t.Fatal("expected error for invalid header, got nil")
	}
	if !strings.Contains(err.Error(), "invalid header") {
		t.Errorf("error = %q, want mention of 'invalid header'", err.Error())
	}
}

// TestParseColonInValue verifies that colons within the header value
// are preserved (only the first ": " splits key from value).
func TestParseColonInValue(t *testing.T) {
	input := "title: My: Note: Title\n\nBody."
	got, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Title() != "My: Note: Title" {
		t.Errorf("title = %q, want %q", got.Title(), "My: Note: Title")
	}
}

// TestSerializeRoundTrip verifies that parsing then serializing
// produces equivalent output.
func TestSerializeRoundTrip(t *testing.T) {
	original := "created: 20230710174528965\ntags: Journal\ntitle: Round Trip\n\nBody text."
	parsed, err := Parse([]byte(original))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	serialized := Serialize(parsed)
	reparsed, err := Parse(serialized)
	if err != nil {
		t.Fatalf("unexpected error on reparse: %v", err)
	}
	// Compare all fields.
	for k, v := range parsed.Fields {
		if reparsed.Fields[k] != v {
			t.Errorf("field %q: got %q, want %q", k, reparsed.Fields[k], v)
		}
	}
}

// TestSerializeHeaderOrder verifies that title comes first, then
// tags, then remaining headers sorted alphabetically.
func TestSerializeHeaderOrder(t *testing.T) {
	td := &Note{Fields: map[string]string{
		"title":   "Sorted",
		"created": "20230101",
		"tags":    "Test",
		"text":    "Body.",
	}}
	out := string(Serialize(td))
	// Headers should appear in order: title, tags, created.
	titleIdx := strings.Index(out, "title:")
	tagsIdx := strings.Index(out, "tags:")
	createdIdx := strings.Index(out, "created:")
	if titleIdx > tagsIdx || tagsIdx > createdIdx {
		t.Errorf("wrong header order: title@%d tags@%d created@%d\noutput:\n%s",
			titleIdx, tagsIdx, createdIdx, out)
	}
}

// TestSerializeNoText verifies serialization of a note with no
// text field — should have headers, blank line, and nothing after.
func TestSerializeNoText(t *testing.T) {
	td := &Note{Fields: map[string]string{
		"title": "NoText",
		"tags":  "Empty",
	}}
	out := string(Serialize(td))
	if !strings.HasSuffix(out, "\n\n") {
		t.Errorf("expected trailing blank line, got %q", out)
	}
	if strings.Contains(out, "text:") {
		t.Error("text field should not appear in headers")
	}
}

// TestSkinnyFields verifies that SkinnyFields returns all fields
// except "text".
func TestSkinnyFields(t *testing.T) {
	td := &Note{Fields: map[string]string{
		"title": "Skinny",
		"tags":  "Test",
		"text":  "Should be excluded",
	}}
	skinny := td.SkinnyFields()
	if _, ok := skinny["text"]; ok {
		t.Error("SkinnyFields should not include 'text'")
	}
	if skinny["title"] != "Skinny" {
		t.Errorf("title = %q, want %q", skinny["title"], "Skinny")
	}
	if skinny["tags"] != "Test" {
		t.Errorf("tags = %q, want %q", skinny["tags"], "Test")
	}
}

// TestTitleToFilename verifies title-to-filename conversion for
// regular, system, and path-containing notes.
func TestTitleToFilename(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "RegularTitle",
			title: "My Note",
			want:  "My Note.md",
		},
		{
			name:  "TitleWithSlash",
			title: "/Users/dg/notes/Test",
			want:  "%2FUsers%2Fdg%2Fnotes%2FTest.md",
		},
		{
			name:  "SimpleTitleNoSlash",
			title: "Hello World",
			want:  "Hello World.md",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TitleToFilename(tc.title)
			if got != tc.want {
				t.Errorf("TitleToFilename(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

// TestFilenameToTitle verifies filename-to-title conversion for
// regular, system, and URL-encoded filenames.
func TestFilenameToTitle(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{
			name:     "RegularFilename",
			filename: "My Note.md",
			want:     "My Note",
		},
		{
			name:     "URLEncodedFilename",
			filename: "%2FUsers%2Fdg%2Fnotes%2FTest.md",
			want:     "/Users/dg/notes/Test",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FilenameToTitle(tc.filename)
			if got != tc.want {
				t.Errorf("FilenameToTitle(%q) = %q, want %q", tc.filename, got, tc.want)
			}
		})
	}
}

// TestParseTrailingCRInHeaders verifies that a trailing \r on the
// header section (e.g. "title: Foo\r\n\n") produces an empty string
// after splitting on \n, which exercises the continue on line 60-61
// of the header parsing loop.
func TestParseTrailingCRInHeaders(t *testing.T) {
	// splitHeaderBody splits on \n\n, giving headerEnd = "title: Foo\r".
	// strings.Split("title: Foo\r", "\n") → ["title: Foo\r"]
	// That doesn't produce an empty line. We need the header section
	// itself to contain an empty line after splitting on \n.
	//
	// With input "title: Foo\r\r\n\nBody", splitHeaderBody finds \n\n
	// at index 12, giving headerEnd = "title: Foo\r\r".
	// strings.Split("title: Foo\r\r", "\n") → ["title: Foo\r\r"]
	// Still one element. We need a \n inside the header section.
	//
	// Simplest approach: "title: Foo\n\r\n\nBody".
	// splitHeaderBody finds \n\n first at index 11 (after "Foo\n\r"),
	// giving headerEnd = "title: Foo\n\r".
	// strings.Split on \n → ["title: Foo", "\r"].
	// TrimRight("\r", "\r") → "" → hits line 60-61.
	input := "title: Foo\n\r\n\nBody here."
	got, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Title() != "Foo" {
		t.Errorf("title = %q, want %q", got.Title(), "Foo")
	}
	if got.Text() != "Body here." {
		t.Errorf("text = %q, want %q", got.Text(), "Body here.")
	}
}

// TestFilenameToTitleBadPercent verifies that a filename with invalid
// percent-encoding falls through to return the raw name unchanged.
func TestFilenameToTitleBadPercent(t *testing.T) {
	got := FilenameToTitle("bad%zztitle.md")
	if got != "bad%zztitle" {
		t.Errorf("FilenameToTitle(%q) = %q, want %q", "bad%zztitle.md", got, "bad%zztitle")
	}
}

// TestTitleFilenameRoundTrip verifies that converting a title to a
// filename and back produces the original title.
func TestTitleFilenameRoundTrip(t *testing.T) {
	titles := []string{
		"Simple Title",
		"path/with/slashes",
		"100%25 Done",
	}
	for _, title := range titles {
		t.Run(title, func(t *testing.T) {
			filename := TitleToFilename(title)
			got := FilenameToTitle(filename)
			if got != title {
				t.Errorf("round-trip failed: %q → %q → %q", title, filename, got)
			}
		})
	}
}

// TestParseTextHeaderIgnored verifies that a "text:" header line
// is ignored — "text" is reserved for the body field.
func TestParseTextHeaderIgnored(t *testing.T) {
	input := "title: Test\ntext: shouldBeIgnored\n\nActual body."
	note, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if note.Text() != "Actual body." {
		t.Errorf("Text() = %q, want %q", note.Text(), "Actual body.")
	}
}
