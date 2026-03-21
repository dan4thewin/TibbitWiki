package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- parseMPattern ---

// TestParseMPatternSlash verifies m/regex/ syntax.
func TestParseMPatternSlash(t *testing.T) {
	pat, ok := parseMPattern("m/foo/")
	if !ok || pat != "foo" {
		t.Errorf("expected (foo, true), got (%q, %v)", pat, ok)
	}
}

// TestParseMPatternPipe verifies m|regex| syntax.
func TestParseMPatternPipe(t *testing.T) {
	pat, ok := parseMPattern("m|bar|")
	if !ok || pat != "bar" {
		t.Errorf("expected (bar, true), got (%q, %v)", pat, ok)
	}
}

// TestParseMPatternAlphaDelimiterRejected verifies that alphabetic
// characters after m are not treated as delimiters (e.g. "mooo").
func TestParseMPatternAlphaDelimiterRejected(t *testing.T) {
	_, ok := parseMPattern("mooo")
	if ok {
		t.Error("expected false for 'mooo'")
	}
}

// TestParseMPatternDigitDelimiterRejected verifies "m1foo1" is not regex.
func TestParseMPatternDigitDelimiterRejected(t *testing.T) {
	_, ok := parseMPattern("m1foo1")
	if ok {
		t.Error("expected false for 'm1foo1'")
	}
}

// TestParseMPatternTooShort verifies strings shorter than 4 chars fail.
func TestParseMPatternTooShort(t *testing.T) {
	_, ok := parseMPattern("m//")
	if ok {
		t.Error("expected false for 'm//'")
	}
}

// TestParseMPatternMismatchedDelimiter verifies unmatched delimiters fail.
func TestParseMPatternMismatchedDelimiter(t *testing.T) {
	_, ok := parseMPattern("m/foo|")
	if ok {
		t.Error("expected false for mismatched delimiters")
	}
}

// TestParseMPatternNotStartingWithM verifies non-m prefix fails.
func TestParseMPatternNotStartingWithM(t *testing.T) {
	_, ok := parseMPattern("x/foo/")
	if ok {
		t.Error("expected false for 'x/foo/'")
	}
}

// --- classifyToken ---

// TestClassifyTokenLiteral verifies plain words are literal.
func TestClassifyTokenLiteral(t *testing.T) {
	kind, pat := classifyToken("hello")
	if kind != tokLiteral || pat != "hello" {
		t.Errorf("expected (tokLiteral, hello), got (%d, %q)", kind, pat)
	}
}

// TestClassifyTokenTag verifies #tag syntax.
func TestClassifyTokenTag(t *testing.T) {
	kind, pat := classifyToken("#Journal")
	if kind != tokTag || pat != "Journal" {
		t.Errorf("expected (tokTag, Journal), got (%d, %q)", kind, pat)
	}
}

// TestClassifyTokenTitle verifies @title syntax.
func TestClassifyTokenTitle(t *testing.T) {
	kind, pat := classifyToken("@ToDo")
	if kind != tokTitle || pat != "ToDo" {
		t.Errorf("expected (tokTitle, ToDo), got (%d, %q)", kind, pat)
	}
}

// TestClassifyTokenSlashRegex verifies /regex/ syntax.
func TestClassifyTokenSlashRegex(t *testing.T) {
	kind, pat := classifyToken("/foo.*bar/")
	if kind != tokRegex || pat != "foo.*bar" {
		t.Errorf("expected (tokRegex, foo.*bar), got (%d, %q)", kind, pat)
	}
}

// TestClassifyTokenMRegex verifies m|regex| syntax passes through.
func TestClassifyTokenMRegex(t *testing.T) {
	kind, pat := classifyToken("m|test|")
	if kind != tokRegex || pat != "test" {
		t.Errorf("expected (tokRegex, test), got (%d, %q)", kind, pat)
	}
}

// --- parseQuery ---

// TestParseQuerySingleLiteral verifies a single word query.
func TestParseQuerySingleLiteral(t *testing.T) {
	tokens := parseQuery("hello")
	if len(tokens) != 1 || tokens[0].kind != tokLiteral || tokens[0].pattern != "hello" {
		t.Errorf("unexpected tokens: %+v", tokens)
	}
}

// TestParseQueryNot verifies !word becomes tokNot.
func TestParseQueryNot(t *testing.T) {
	tokens := parseQuery("!spam")
	if len(tokens) != 1 || tokens[0].kind != tokNot || tokens[0].pattern != "spam" {
		t.Errorf("unexpected tokens: %+v", tokens)
	}
}

// TestParseQueryNotTag verifies !#tag becomes tokNotTag.
func TestParseQueryNotTag(t *testing.T) {
	tokens := parseQuery("!#Draft")
	if len(tokens) != 1 || tokens[0].kind != tokNotTag || tokens[0].pattern != "Draft" {
		t.Errorf("unexpected tokens: %+v", tokens)
	}
}

// TestParseQueryNotTitle verifies !@title becomes tokNotTitle.
func TestParseQueryNotTitle(t *testing.T) {
	tokens := parseQuery("!@ToDo")
	if len(tokens) != 1 || tokens[0].kind != tokNotTitle || tokens[0].pattern != "ToDo" {
		t.Errorf("unexpected tokens: %+v", tokens)
	}
}

// TestParseQueryNotRegex verifies !/regex/ becomes tokNotRegex.
func TestParseQueryNotRegex(t *testing.T) {
	tokens := parseQuery("!/foo/")
	if len(tokens) != 1 || tokens[0].kind != tokNotRegex || tokens[0].pattern != "foo" {
		t.Errorf("unexpected tokens: %+v", tokens)
	}
}

// TestParseQueryMultipleTokens verifies space-separated AND query.
func TestParseQueryMultipleTokens(t *testing.T) {
	tokens := parseQuery("linux #Journal !spam")
	if len(tokens) != 3 {
		t.Fatalf("expected 3 tokens, got %d", len(tokens))
	}
	if tokens[0].kind != tokLiteral || tokens[0].pattern != "linux" {
		t.Errorf("token 0: %+v", tokens[0])
	}
	if tokens[1].kind != tokTag || tokens[1].pattern != "Journal" {
		t.Errorf("token 1: %+v", tokens[1])
	}
	if tokens[2].kind != tokNot || tokens[2].pattern != "spam" {
		t.Errorf("token 2: %+v", tokens[2])
	}
}

// TestParseQueryEmpty verifies empty input returns nil.
func TestParseQueryEmpty(t *testing.T) {
	tokens := parseQuery("")
	if len(tokens) != 0 {
		t.Errorf("expected empty, got %+v", tokens)
	}
}

// TestParseQueryOrLiteral verifies a|b stays as one literal token.
func TestParseQueryOrLiteral(t *testing.T) {
	tokens := parseQuery("cat|dog")
	if len(tokens) != 1 || tokens[0].kind != tokLiteral || tokens[0].pattern != "cat|dog" {
		t.Errorf("unexpected tokens: %+v", tokens)
	}
}

// --- FileToTitle ---

// TestFileToTitle verifies .tid extension is stripped.
func TestFileToTitle(t *testing.T) {
	got := FileToTitle("/some/path/MyNote.tid")
	if got != "MyNote" {
		t.Errorf("expected MyNote, got %q", got)
	}
}

// TestFileToTitleNoExtension verifies a file without .tid extension.
func TestFileToTitleNoExtension(t *testing.T) {
	got := FileToTitle("/some/path/Readme")
	if got != "Readme" {
		t.Errorf("expected Readme, got %q", got)
	}
}

// --- isHeaderLine ---

// TestIsHeaderLine verifies metadata lines are detected.
func TestIsHeaderLine(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"title: Hello", true},
		{"tags: Journal", true},
		{"modified: 20260321", true},
		{"created: 20260101", true},
		{"type: text/vnd.tiddlywiki", true},
		{"This is body text", false},
		{"The title is important", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isHeaderLine(tc.line)
		if got != tc.want {
			t.Errorf("isHeaderLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// --- isNegated ---

// TestIsNegated verifies the negation check for all token types.
func TestIsNegated(t *testing.T) {
	cases := []struct {
		kind int
		want bool
	}{
		{tokLiteral, false},
		{tokNot, true},
		{tokTag, false},
		{tokNotTag, true},
		{tokTitle, false},
		{tokNotTitle, true},
		{tokRegex, false},
		{tokNotRegex, true},
	}
	for _, tc := range cases {
		if got := isNegated(tc.kind); got != tc.want {
			t.Errorf("isNegated(%d) = %v, want %v", tc.kind, got, tc.want)
		}
	}
}

// --- toRgPattern ---

// TestToRgPatternTag verifies tag tokens produce header-anchored patterns.
func TestToRgPatternTag(t *testing.T) {
	pat, ci := toRgPattern(token{kind: tokTag, pattern: "Journal"})
	if ci {
		t.Error("tag search should be case-sensitive")
	}
	if !strings.Contains(pat, "^tags:") {
		t.Errorf("expected ^tags: prefix, got %q", pat)
	}
}

// TestToRgPatternTitle verifies title tokens produce title-anchored patterns.
func TestToRgPatternTitle(t *testing.T) {
	pat, ci := toRgPattern(token{kind: tokTitle, pattern: "ToDo"})
	if !ci {
		t.Error("title search should be case-insensitive")
	}
	if !strings.Contains(pat, "^title:") {
		t.Errorf("expected ^title: prefix, got %q", pat)
	}
}

// TestToRgPatternRegex verifies regex tokens pass through unchanged.
func TestToRgPatternRegex(t *testing.T) {
	pat, ci := toRgPattern(token{kind: tokRegex, pattern: "foo.*bar"})
	if ci {
		t.Error("regex should be case-sensitive")
	}
	if pat != "foo.*bar" {
		t.Errorf("expected raw pattern, got %q", pat)
	}
}

// TestToRgPatternNotTag verifies negated tag tokens.
func TestToRgPatternNotTag(t *testing.T) {
	pat, ci := toRgPattern(token{kind: tokNotTag, pattern: "Draft"})
	if ci {
		t.Error("not-tag search should be case-sensitive")
	}
	if !strings.Contains(pat, "^tags:") {
		t.Errorf("expected ^tags: prefix, got %q", pat)
	}
}

// TestToRgPatternNotRegex verifies negated regex tokens.
func TestToRgPatternNotRegex(t *testing.T) {
	pat, ci := toRgPattern(token{kind: tokNotRegex, pattern: "baz"})
	if ci {
		t.Error("not-regex should be case-sensitive")
	}
	if pat != "baz" {
		t.Errorf("expected raw pattern, got %q", pat)
	}
}

// --- Search edge cases ---

// TestSearchNotOnly verifies a NOT-only query (starts with all files,
// subtracts matches).
func TestSearchNotOnly(t *testing.T) {
	checkRg(t)
	dir := makeTidDir(t,
		"A.tid", "title: A\n\ngopher here.",
		"B.tid", "title: B\n\nnothing here.",
	)
	results, err := Search(dir, "!gopher")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if FileToTitle(results[0].File) != "B" {
		t.Errorf("expected B, got %s", results[0].File)
	}
}

// TestSearchOR verifies a|b OR syntax matches both terms.
func TestSearchOR(t *testing.T) {
	checkRg(t)
	dir := makeTidDir(t,
		"A.tid", "title: A\n\ncat owner.",
		"B.tid", "title: B\n\ndog owner.",
		"C.tid", "title: C\n\nfish owner.",
	)
	results, err := Search(dir, "cat|dog")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

// TestSearchRegex verifies /regex/ search works end-to-end.
func TestSearchRegex(t *testing.T) {
	checkRg(t)
	dir := makeTidDir(t,
		"A.tid", "title: A\n\ngopher123.",
		"B.tid", "title: B\n\nnothing.",
	)
	results, err := Search(dir, "/gopher[0-9]+/")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

// TestSearchANDIntersection verifies two positive terms intersect.
func TestSearchANDIntersection(t *testing.T) {
	checkRg(t)
	dir := makeTidDir(t,
		"A.tid", "title: A\n\nalpha beta.",
		"B.tid", "title: B\n\nalpha only.",
		"C.tid", "title: C\n\nbeta only.",
	)
	results, err := Search(dir, "alpha beta")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 (intersection), got %d", len(results))
	}
	if FileToTitle(results[0].File) != "A" {
		t.Errorf("expected A, got %s", results[0].File)
	}
}

// TestSearchLiteral verifies Search finds a file containing a literal word.
func TestSearchLiteral(t *testing.T) {
	checkRg(t)
	dir := makeTidDir(t,
		"Alpha.tid", "title: Alpha\n\nHello gopher world.",
		"Beta.tid", "title: Beta\n\nNo match here.",
	)
	results, err := Search(dir, "gopher")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if FileToTitle(results[0].File) != "Alpha" {
		t.Errorf("expected Alpha, got %s", results[0].File)
	}
	if len(results[0].Lines) == 0 {
		t.Error("expected match lines, got none")
	}
}

// TestSearchTag verifies #tag search matches the tags header.
func TestSearchTag(t *testing.T) {
	checkRg(t)
	dir := makeTidDir(t,
		"A.tid", "title: A\ntags: Journal\n\nBody.",
		"B.tid", "title: B\ntags: Draft\n\nBody.",
	)
	results, err := Search(dir, "#Journal")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if FileToTitle(results[0].File) != "A" {
		t.Errorf("expected A, got %s", results[0].File)
	}
}

// TestSearchTitle verifies @title search matches the title header.
func TestSearchTitle(t *testing.T) {
	checkRg(t)
	dir := makeTidDir(t,
		"Foo.tid", "title: Foo\n\nBody.",
		"Bar.tid", "title: Bar\n\nBody.",
	)
	results, err := Search(dir, "@Foo")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

// TestSearchNot verifies !word excludes matching files.
func TestSearchNot(t *testing.T) {
	checkRg(t)
	dir := makeTidDir(t,
		"A.tid", "title: A\n\ngopher and golang.",
		"B.tid", "title: B\n\ngopher only.",
	)
	results, err := Search(dir, "gopher !golang")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if FileToTitle(results[0].File) != "B" {
		t.Errorf("expected B, got %s", results[0].File)
	}
}

// TestSearchNoMatch verifies empty results for a query with no hits.
func TestSearchNoMatch(t *testing.T) {
	checkRg(t)
	dir := makeTidDir(t,
		"A.tid", "title: A\n\nHello.",
	)
	results, err := Search(dir, "zzzznothere")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

// TestSearchEmpty verifies empty query returns nil.
func TestSearchEmpty(t *testing.T) {
	results, err := Search("/tmp", "")
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Errorf("expected nil, got %+v", results)
	}
}

// TestSearchHeaderLinesFiltered verifies that tag/title header matches
// return the file but not the header line itself in the match lines.
func TestSearchHeaderLinesFiltered(t *testing.T) {
	checkRg(t)
	dir := makeTidDir(t,
		"A.tid", "title: A\ntags: Journal\n\nBody text.",
	)
	results, err := Search(dir, "#Journal")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// The match was on the tags: header, which should be filtered out.
	if len(results[0].Lines) != 0 {
		t.Errorf("expected 0 match lines (header filtered), got %d: %v",
			len(results[0].Lines), results[0].Lines)
	}
}

// --- helpers ---

// checkRg skips the test if rg is not installed.
func checkRg(t *testing.T) {
	t.Helper()
	if _, err := findRg(); err != nil {
		t.Skip("rg not installed, skipping")
	}
}

// findRg checks if rg is on PATH.
func findRg() (string, error) {
	return filepath.Abs("rg") // just a path check
}

// makeTidDir creates a temp directory with .tid files. Arguments are
// alternating (filename, content) pairs.
func makeTidDir(t *testing.T, pairs ...string) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < len(pairs); i += 2 {
		if err := os.WriteFile(filepath.Join(dir, pairs[i]), []byte(pairs[i+1]), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
