package tid

import (
	"strings"
	"testing"
)

// TestParseValidTiddler verifies that a standard .tid file with
// headers and body is parsed correctly into fields.
func TestParseValidTiddler(t *testing.T) {
	input := "created: 20230710174528965\ntags: Journal\ntitle: My Tiddler\n\nBody text here.\nSecond line."
	got, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Title() != "My Tiddler" {
		t.Errorf("title = %q, want %q", got.Title(), "My Tiddler")
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
	input := "title: WinTiddler\r\ntags: Test\r\n\r\nBody with CRLF."
	got, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Title() != "WinTiddler" {
		t.Errorf("title = %q, want %q", got.Title(), "WinTiddler")
	}
	if got.Fields["tags"] != "Test" {
		t.Errorf("tags = %q, want %q", got.Fields["tags"], "Test")
	}
	if got.Text() != "Body with CRLF." {
		t.Errorf("text = %q, want %q", got.Text(), "Body with CRLF.")
	}
}

// TestParseHeadersOnly verifies that a tiddler with no body text
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

// TestParseMissingTitle verifies that a tiddler without a title
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
	input := "title: My: Tiddler: Title\n\nBody."
	got, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Title() != "My: Tiddler: Title" {
		t.Errorf("title = %q, want %q", got.Title(), "My: Tiddler: Title")
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

// TestSerializeHeaderOrder verifies that headers are sorted
// alphabetically for deterministic output.
func TestSerializeHeaderOrder(t *testing.T) {
	td := &Tiddler{Fields: map[string]string{
		"title":   "Sorted",
		"created": "20230101",
		"tags":    "Test",
		"text":    "Body.",
	}}
	out := string(Serialize(td))
	// Headers should appear in order: created, tags, title.
	createdIdx := strings.Index(out, "created:")
	tagsIdx := strings.Index(out, "tags:")
	titleIdx := strings.Index(out, "title:")
	if createdIdx > tagsIdx || tagsIdx > titleIdx {
		t.Errorf("headers not sorted: created@%d tags@%d title@%d\noutput:\n%s",
			createdIdx, tagsIdx, titleIdx, out)
	}
}

// TestSerializeNoText verifies serialization of a tiddler with no
// text field — should have headers, blank line, and nothing after.
func TestSerializeNoText(t *testing.T) {
	td := &Tiddler{Fields: map[string]string{
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
	td := &Tiddler{Fields: map[string]string{
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
// regular, system, and path-containing tiddlers.
func TestTitleToFilename(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "RegularTitle",
			title: "My Tiddler",
			want:  "My Tiddler.tid",
		},
		{
			name:  "SystemTiddler",
			title: "$:/config/SyncPollingInterval",
			want:  "$__config_SyncPollingInterval.tid",
		},
		{
			name:  "NestedSystemTiddler",
			title: "$:/themes/tiddlywiki/vanilla/options/sidebarlayout",
			want:  "$__themes_tiddlywiki_vanilla_options_sidebarlayout.tid",
		},
		{
			name:  "TitleWithSlash",
			title: "/Users/dg/notes/Test",
			want:  "%2FUsers%2Fdg%2Fnotes%2FTest.tid",
		},
		{
			name:  "SimpleTitleNoSlash",
			title: "Hello World",
			want:  "Hello World.tid",
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
			filename: "My Tiddler.tid",
			want:     "My Tiddler",
		},
		{
			name:     "SystemTiddler",
			filename: "$__config_SyncPollingInterval.tid",
			want:     "$:/config/SyncPollingInterval",
		},
		{
			name:     "URLEncodedFilename",
			filename: "%2FUsers%2Fdg%2Fnotes%2FTest.tid",
			want:     "/Users/dg/notes/Test",
		},
		{
			name:     "WithDirectoryPath",
			filename: "/some/path/$__palette.tid",
			want:     "$:/palette",
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

// TestTitleFilenameRoundTrip verifies that converting a title to a
// filename and back produces the original title.
func TestTitleFilenameRoundTrip(t *testing.T) {
	titles := []string{
		"Simple Title",
		"$:/config/Something",
		"$:/themes/tiddlywiki/vanilla/base",
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
