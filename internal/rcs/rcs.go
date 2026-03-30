// Package rcs provides automatic RCS version control for note files.
// Archives are stored in an RCS/ subdirectory alongside the working
// files. Uses github.com/arran4/golang-rcs as a pure-Go RCS library
// instead of shelling out to external RCS commands.
package rcs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	gorcs "github.com/arran4/golang-rcs"
)

var validRev = regexp.MustCompile(`^\d+\.\d+$`)

// ValidRevision reports whether rev looks like an RCS revision (e.g. "1.5").
func ValidRevision(rev string) bool {
	return validRev.MatchString(rev)
}

// archivePath returns the ,v archive path in RCS/ for a working file.
func archivePath(path string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	return filepath.Join(dir, "RCS", base+",v")
}

// parseArchive reads and parses the ,v archive for path.
func parseArchive(path string) (*gorcs.File, string, error) {
	archive := archivePath(path)
	f, err := os.Open(archive)
	if err != nil {
		return nil, archive, err
	}
	defer f.Close()
	file, err := gorcs.ParseFile(f)
	if err != nil {
		return nil, archive, fmt.Errorf("parse %s: %w", archive, err)
	}
	return file, archive, nil
}

// writeArchive serializes a File back to the ,v archive.
// Removes first because external RCS creates read-only archives.
func writeArchive(file *gorcs.File, archive string) error {
	data := []byte(file.String())
	os.Remove(archive)
	if err := os.WriteFile(archive, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", archive, err)
	}
	return nil
}

// Checkin checks in the working file, creating the ,v archive on
// first use inside the RCS/ subdirectory. Keyword expansion is
// suppressed (-ko). The working file is kept locked (writable).
func Checkin(path string) error {
	archive := archivePath(path)
	rcsDir := filepath.Dir(archive)

	text, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	first := !fileExists(archive)

	var file *gorcs.File
	if first {
		if err := os.MkdirAll(rcsDir, 0755); err != nil {
			return fmt.Errorf("mkdir RCS %s: %w", rcsDir, err)
		}
		file = gorcs.NewFile()
		file.Expand = "o" // -ko: suppress keyword expansion
		_, err = file.Checkin("tibbitwiki", "", string(text),
			gorcs.WithInitial{}, gorcs.WithSetLock)
	} else {
		file, _, err = parseArchive(path)
		if err != nil {
			return err
		}
		_, err = file.Checkin("tibbitwiki", "", string(text),
			gorcs.WithSetLock, gorcs.WithForce{})
	}
	if err != nil {
		return fmt.Errorf("checkin %s: %w", path, err)
	}
	if err := writeArchive(file, archive); err != nil {
		return err
	}
	// ci -l leaves the working file writable (locked).
	return os.Chmod(path, 0644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// HasArchive reports whether an RCS archive exists for path.
func HasArchive(path string) bool {
	return fileExists(archivePath(path))
}

// NeedsCheckin reports whether path should be checked in.
// Returns true if no archive exists, or if the working file is
// newer than the archive (edited while the server was down).
func NeedsCheckin(path string) bool {
	archive := archivePath(path)
	ai, err := os.Stat(archive)
	if err != nil {
		return true // no archive
	}
	fi, err := os.Stat(path)
	if err != nil {
		return false // can't stat working file
	}
	return fi.ModTime().After(ai.ModTime())
}

// Log returns formatted revision log output for the given file,
// matching the format of rlog output.
func Log(path string) (string, error) {
	file, archive, err := parseArchive(path)
	if err != nil {
		return "", fmt.Errorf("rlog %s: %w", path, err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\nRCS file: %s\n", archive)
	fmt.Fprintf(&sb, "Working file: %s\n", path)
	fmt.Fprintf(&sb, "head: %s\n", file.Head)

	// Symbolic names
	if len(file.Symbols) > 0 {
		sb.WriteString("symbolic names:\n")
		for _, sym := range file.Symbols {
			fmt.Fprintf(&sb, "\t%s: %s\n", sym.Name, sym.Revision)
		}
	}

	sb.WriteString("----------------------------\n")
	// Revisions in head-first order
	for _, rh := range file.RevisionHeads {
		rev := rh.Revision.String()
		logMsg, _ := file.GetLogMessage(rev)
		fmt.Fprintf(&sb, "revision %s\n", rev)
		fmt.Fprintf(&sb, "date: %s;  author: %s;  state: %s;\n",
			string(rh.Date), string(rh.Author), string(rh.State))
		sb.WriteString(logMsg)
		sb.WriteString("----------------------------\n")
	}
	sb.WriteString("=============================================================================\n")
	return sb.String(), nil
}

// HeadRev returns the head revision string (e.g. "1.5").
func HeadRev(path string) (string, error) {
	file, _, err := parseArchive(path)
	if err != nil {
		return "", fmt.Errorf("rlog -h %s: %w", path, err)
	}
	if file.Head == "" {
		return "", fmt.Errorf("no head revision for %s", path)
	}
	return file.Head, nil
}

// DiffRange returns a unified diff between two revisions.
func DiffRange(path, rev1, rev2 string) (string, error) {
	if !ValidRevision(rev1) {
		return "", fmt.Errorf("invalid revision %q", rev1)
	}
	if !ValidRevision(rev2) {
		return "", fmt.Errorf("invalid revision %q", rev2)
	}

	file, _, err := parseArchive(path)
	if err != nil {
		return "", fmt.Errorf("rcsdiff %s: %w", path, err)
	}

	v1, err := file.Checkout("tibbitwiki", gorcs.WithRevision(rev1))
	if err != nil {
		return "", fmt.Errorf("checkout %s rev %s: %w", path, rev1, err)
	}
	v2, err := file.Checkout("tibbitwiki", gorcs.WithRevision(rev2))
	if err != nil {
		return "", fmt.Errorf("checkout %s rev %s: %w", path, rev2, err)
	}

	if v1.Content == v2.Content {
		return "", nil
	}

	return unifiedDiff(path, rev1, rev2, v1.Content, v2.Content), nil
}

// unifiedDiff produces a unified diff header and hunks.
func unifiedDiff(path, rev1, rev2, text1, text2 string) string {
	a := splitLines(text1)
	b := splitLines(text2)

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\t%s\n", path, rev1)
	fmt.Fprintf(&sb, "+++ %s\t%s\n", path, rev2)

	// Simple LCS-based unified diff with context.
	const ctx = 3
	lcs := lcsMatrix(a, b)
	edits := diffEdits(lcs, a, b, len(a), len(b))
	hunks := makeHunks(edits, len(a), len(b), ctx)

	for _, h := range hunks {
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n",
			h.aStart+1, h.aLen, h.bStart+1, h.bLen)
		for _, e := range h.lines {
			sb.WriteString(e)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type edit struct {
	op   byte // ' ', '+', '-'
	aIdx int
	bIdx int
	line string
}

func lcsMatrix(a, b []string) [][]int {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return dp
}

func diffEdits(dp [][]int, a, b []string, i, j int) []edit {
	var edits []edit
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			edits = append(edits, edit{' ', i - 1, j - 1, a[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			edits = append(edits, edit{'+', i, j - 1, b[j-1]})
			j--
		} else {
			edits = append(edits, edit{'-', i - 1, j, a[i-1]})
			i--
		}
	}
	// Reverse to get forward order.
	for l, r := 0, len(edits)-1; l < r; l, r = l+1, r-1 {
		edits[l], edits[r] = edits[r], edits[l]
	}
	return edits
}

type hunk struct {
	aStart, aLen int
	bStart, bLen int
	lines        []string
}

// region identifies a range of edit indices that belong together.
type region struct{ start, end int }

// findRegions groups nearby changed edits into regions, merging
// those within 2*ctx of each other.
func findRegions(edits []edit, ctx int) []region {
	var regions []region
	for i, e := range edits {
		if e.op != ' ' {
			if len(regions) == 0 || i > regions[len(regions)-1].end+2*ctx {
				regions = append(regions, region{i, i})
			} else {
				regions[len(regions)-1].end = i
			}
		}
	}
	return regions
}

// buildHunk builds a single hunk from a slice of edits.
func buildHunk(edits []edit) hunk {
	var h hunk
	var aCount, bCount int
	aFirst, bFirst := -1, -1
	for _, e := range edits {
		h.lines = append(h.lines, string(e.op)+e.line)
		switch e.op {
		case ' ':
			if aFirst < 0 {
				aFirst = e.aIdx
			}
			if bFirst < 0 {
				bFirst = e.bIdx
			}
			aCount++
			bCount++
		case '-':
			if aFirst < 0 {
				aFirst = e.aIdx
			}
			aCount++
		case '+':
			if bFirst < 0 {
				bFirst = e.bIdx
			}
			bCount++
		}
	}
	if aFirst < 0 {
		aFirst = 0
	}
	if bFirst < 0 {
		bFirst = 0
	}
	h.aStart = aFirst
	h.aLen = aCount
	h.bStart = bFirst
	h.bLen = bCount
	return h
}

func makeHunks(edits []edit, aLen, bLen, ctx int) []hunk {
	if len(edits) == 0 {
		return nil
	}
	regions := findRegions(edits, ctx)

	var hunks []hunk
	for _, r := range regions {
		lo := r.start - ctx
		if lo < 0 {
			lo = 0
		}
		hi := r.end + ctx + 1
		if hi > len(edits) {
			hi = len(edits)
		}
		hunks = append(hunks, buildHunk(edits[lo:hi]))
	}
	return hunks
}

// Checkout checks out a specific revision without a lock (read-only).
// The working file becomes 0444.
func Checkout(path, rev string) error {
	if !ValidRevision(rev) {
		return fmt.Errorf("invalid revision %q", rev)
	}
	file, _, err := parseArchive(path)
	if err != nil {
		return fmt.Errorf("co -r%s %s: %w", rev, path, err)
	}
	v, err := file.Checkout("tibbitwiki", gorcs.WithRevision(rev))
	if err != nil {
		return fmt.Errorf("co -r%s %s: %w", rev, path, err)
	}
	// Remove first — WriteFile does not change permissions on existing files.
	os.Remove(path)
	if err := os.WriteFile(path, []byte(v.Content), 0444); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// CheckoutHead checks out the head revision with a lock (writable).
func CheckoutHead(path string) error {
	file, _, err := parseArchive(path)
	if err != nil {
		return fmt.Errorf("co -l %s: %w", path, err)
	}
	v, err := file.Checkout("tibbitwiki", gorcs.WithSetLock)
	if err != nil {
		return fmt.Errorf("co -l %s: %w", path, err)
	}
	if err := writeArchive(file, archivePath(path)); err != nil {
		return err
	}
	os.Remove(path)
	if err := os.WriteFile(path, []byte(v.Content), 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// SetTag assigns the symbolic name to a revision.
func SetTag(path, name, rev string) error {
	if !ValidRevision(rev) {
		return fmt.Errorf("invalid revision %q", rev)
	}
	file, archive, err := parseArchive(path)
	if err != nil {
		return fmt.Errorf("rcs -n%s:%s %s: %w", name, rev, path, err)
	}
	// Update existing or append.
	found := false
	for _, sym := range file.Symbols {
		if sym.Name == name {
			sym.Revision = rev
			found = true
			break
		}
	}
	if !found {
		file.Symbols = append(file.Symbols, &gorcs.Symbol{Name: name, Revision: rev})
	}
	return writeArchive(file, archive)
}

// DeleteTag removes a symbolic name.
func DeleteTag(path, name string) error {
	file, archive, err := parseArchive(path)
	if err != nil {
		return fmt.Errorf("rcs -n%s %s: %w", name, path, err)
	}
	filtered := file.Symbols[:0]
	for _, sym := range file.Symbols {
		if sym.Name != name {
			filtered = append(filtered, sym)
		}
	}
	file.Symbols = filtered
	return writeArchive(file, archive)
}

// CheckoutTag returns the revision associated with a symbolic name.
// Returns "" if the tag is not found.
func CheckoutTag(path, name string) (string, error) {
	file, _, err := parseArchive(path)
	if err != nil {
		return "", fmt.Errorf("rlog -h %s: %w", path, err)
	}
	m := file.SymbolMap()
	return m[name], nil
}
