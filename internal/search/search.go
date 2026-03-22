// Package search provides full-text search over .md files using ripgrep.
package search

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Result holds a matched file and its matching lines.
type Result struct {
	File  string
	Lines []string
}

// Token types from query parsing.
const (
	tokLiteral = iota
	tokNot
	tokTag
	tokNotTag
	tokTitle
	tokNotTitle
	tokRegex
	tokNotRegex
)

type token struct {
	kind    int
	pattern string
}

// parseMPattern checks for mXpatternX syntax where X is any non-alnum char.
func parseMPattern(s string) (string, bool) {
	if len(s) < 4 || s[0] != 'm' {
		return "", false
	}
	delim := rune(s[1])
	if !unicode.IsPunct(delim) && !unicode.IsSymbol(delim) {
		return "", false
	}
	if rune(s[len(s)-1]) != delim {
		return "", false
	}
	return s[2 : len(s)-1], true
}

// parseQuery splits a query string into typed tokens.
//
// Syntax:
//
//	word         literal (case-insensitive)
//	!word        NOT literal
//	#tag         tag search
//	!#tag        NOT tag
//	@title       title substring
//	!@title      NOT title
//	/regex/      raw regex
//	!/regex/     NOT regex
//	mXregexX     raw regex with delimiter X
//	!mXregexX    NOT regex with delimiter X
//	a|b          OR (kept as single literal for rg -e)
func parseQuery(q string) []token {
	fields := strings.Fields(q)
	var tokens []token
	for _, f := range fields {
		neg := false
		if strings.HasPrefix(f, "!") {
			neg = true
			f = f[1:]
		}
		kind, pattern := classifyToken(f)
		if neg {
			switch kind {
			case tokLiteral:
				kind = tokNot
			case tokTag:
				kind = tokNotTag
			case tokTitle:
				kind = tokNotTitle
			case tokRegex:
				kind = tokNotRegex
			}
		}
		tokens = append(tokens, token{kind: kind, pattern: pattern})
	}
	return tokens
}

func classifyToken(f string) (int, string) {
	if strings.HasPrefix(f, "#") {
		return tokTag, f[1:]
	}
	if strings.HasPrefix(f, "@") {
		return tokTitle, f[1:]
	}
	if len(f) >= 2 && f[0] == '/' && f[len(f)-1] == '/' {
		return tokRegex, f[1 : len(f)-1]
	}
	if pat, ok := parseMPattern(f); ok {
		return tokRegex, pat
	}
	return tokLiteral, f
}

// toRgPattern converts a token to an rg pattern and flags.
func toRgPattern(t token) (pattern string, caseInsensitive bool) {
	switch t.kind {
	case tokTag, tokNotTag:
		return `^tags:.*\b` + regexp.QuoteMeta(t.pattern) + `\b`, false
	case tokTitle, tokNotTitle:
		return `^title:.*` + regexp.QuoteMeta(t.pattern), true
	case tokRegex, tokNotRegex:
		return t.pattern, false
	default:
		// Literals with | are OR queries — escape each alternative separately
		// so rgSearch can split on the unescaped |.
		if strings.Contains(t.pattern, "|") {
			parts := strings.Split(t.pattern, "|")
			for i, p := range parts {
				parts[i] = regexp.QuoteMeta(p)
			}
			return strings.Join(parts, "|"), true
		}
		return regexp.QuoteMeta(t.pattern), true
	}
}

func isNegated(kind int) bool {
	return kind == tokNot || kind == tokNotTag || kind == tokNotTitle || kind == tokNotRegex
}

// Search runs a query against .md files in dir and returns matching
// files with their matched lines.
func Search(dir, query string) ([]Result, error) {
	tokens := parseQuery(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	// Collect match lines from positive terms keyed by file.
	matches := map[string][]string{}
	var result map[string]bool

	for _, tok := range tokens {
		pattern, ci := toRgPattern(tok)
		hits, err := rgSearch(dir, pattern, ci)
		if err != nil {
			return nil, err
		}

		fileSet := make(map[string]bool, len(hits))
		for _, h := range hits {
			fileSet[h.File] = true
		}

		if isNegated(tok.kind) {
			result, err = subtractFiles(result, fileSet, dir)
			if err != nil {
				return nil, err
			}
		} else {
			result = intersectFiles(result, fileSet)
			for _, h := range hits {
				matches[h.File] = appendUnique(matches[h.File], h.Lines)
			}
		}
	}

	out := make([]Result, 0, len(result))
	for f := range result {
		out = append(out, Result{File: f, Lines: matches[f]})
	}
	return out, nil
}

// subtractFiles removes fileSet entries from result. If result is
// nil (first negated token), it initializes from all .md files.
func subtractFiles(result, fileSet map[string]bool, dir string) (map[string]bool, error) {
	if result == nil {
		all, err := allTidFiles(dir)
		if err != nil {
			return nil, err
		}
		result = toSet(all)
	}
	for f := range fileSet {
		delete(result, f)
	}
	return result, nil
}

// intersectFiles keeps only files present in both result and
// fileSet. If result is nil (first positive token), uses fileSet.
func intersectFiles(result, fileSet map[string]bool) map[string]bool {
	if result == nil {
		return fileSet
	}
	for f := range result {
		if !fileSet[f] {
			delete(result, f)
		}
	}
	return result
}

func appendUnique(dst, src []string) []string {
	seen := make(map[string]bool, len(dst))
	for _, s := range dst {
		seen[s] = true
	}
	for _, s := range src {
		if !seen[s] {
			dst = append(dst, s)
			seen[s] = true
		}
	}
	return dst
}

// rgMatch is the subset of rg --json output we care about.
type rgMatch struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type rgMatchData struct {
	Path  rgText `json:"path"`
	Lines rgText `json:"lines"`
}

type rgText struct {
	Text string `json:"text"`
}

func rgSearch(dir, pattern string, caseInsensitive bool) ([]Result, error) {
	args := []string{"--json"}
	if caseInsensitive {
		args = append(args, "-i")
	}
	if strings.Contains(pattern, "|") && !strings.HasPrefix(pattern, "^") {
		parts := strings.Split(pattern, "|")
		for _, p := range parts {
			args = append(args, "-e", p)
		}
	} else {
		args = append(args, pattern)
	}
	args = append(args, dir)

	cmd := exec.Command("rg", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	return parseRgJSON(out), nil
}

func parseRgJSON(out []byte) []Result {
	byFile := map[string][]string{}
	// Track all files that had any match (including header-only).
	filesSeen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m rgMatch
		if json.Unmarshal([]byte(line), &m) != nil || m.Type != "match" {
			continue
		}
		var d rgMatchData
		if json.Unmarshal(m.Data, &d) != nil {
			continue
		}
		filePath := normalizeHitPath(d.Path.Text)
		filesSeen[filePath] = true
		text := strings.TrimRight(d.Lines.Text, "\n")
		// Skip header lines (tags:, title:, etc.) — show body matches only.
		if isHeaderLine(text) {
			continue
		}
		byFile[filePath] = append(byFile[filePath], text)
	}
	results := make([]Result, 0, len(filesSeen))
	for f := range filesSeen {
		results = append(results, Result{File: f, Lines: byFile[f]})
	}
	return results
}

// normalizeHitPath maps attachment hits back to the parent note.
// e.g. /path/GoTesting/spec.txt → /path/GoTesting.md
// Regular .md files pass through unchanged.
func normalizeHitPath(path string) string {
	if strings.HasSuffix(path, ".md") {
		return path
	}
	// Attachment in a subdir: dir/NoteTitle/file.txt → dir/NoteTitle.md
	parent := filepath.Dir(path)
	return parent + ".md"
}

// isHeaderLine returns true for .md metadata lines we don't want in results.
func isHeaderLine(line string) bool {
	for _, prefix := range []string{"title:", "tags:", "modified:", "created:", "type:"} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func allTidFiles(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func toSet(files []string) map[string]bool {
	s := make(map[string]bool, len(files))
	for _, f := range files {
		s[f] = true
	}
	return s
}

// FileToTitle extracts a note title from a .md filename by reading
// the title header. This is a fallback — prefer store lookup.
func FileToTitle(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".md")
}
