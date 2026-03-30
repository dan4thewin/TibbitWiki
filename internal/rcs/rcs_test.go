package rcs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	gorcs "github.com/arran4/golang-rcs"
)

// TestCheckinCreatesArchive verifies that the first Checkin creates
// a ,v archive file and keeps the working copy.
func TestCheckinCreatesArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nBody."), 0644)

	if err := Checkin(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Archive should exist in the RCS/ subdirectory.
	archive := filepath.Join(dir, "RCS", "test.md,v")
	if !fileExists(archive) {
		t.Fatal("archive not created")
	}
	// Working file should still exist (ci -l keeps it).
	if !fileExists(path) {
		t.Fatal("working file missing after checkin")
	}
}

// TestCheckinSubsequent verifies that a second Checkin on an
// existing archive succeeds.
func TestCheckinSubsequent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nVersion 1."), 0644)

	if err := Checkin(path); err != nil {
		t.Fatalf("first checkin: %v", err)
	}

	// Modify and checkin again.
	os.WriteFile(path, []byte("title: Test\n\nVersion 2."), 0644)
	if err := Checkin(path); err != nil {
		t.Fatalf("second checkin: %v", err)
	}

	// Working file should still be present.
	if !fileExists(path) {
		t.Fatal("working file missing after second checkin")
	}
}

// TestCheckinBadPath verifies that Checkin returns an error for a
// nonexistent file.
func TestCheckinBadPath(t *testing.T) {
	err := Checkin("/no/such/file.md")
	if err == nil {
		t.Fatal("expected error for bad path, got nil")
	}
}

// TestFileExists verifies the fileExists helper.
func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exists.txt")
	os.WriteFile(path, []byte("hi"), 0644)

	if !fileExists(path) {
		t.Error("returned false for existing file")
	}
	if fileExists(filepath.Join(dir, "nope.txt")) {
		t.Error("returned true for nonexistent file")
	}
}

// setupMultiRev creates a file with 3 checkins and returns the path.
func setupMultiRev(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	for i, body := range []string{"Version 1.", "Version 2.", "Version 3."} {
		os.WriteFile(path, []byte("title: Test\n\n"+body), 0644)
		if err := Checkin(path); err != nil {
			t.Fatalf("checkin %d: %v", i+1, err)
		}
	}
	return path
}

// TestLogReturnsOutput verifies Log returns rlog output containing
// revision entries.
func TestLogReturnsOutput(t *testing.T) {
	path := setupMultiRev(t)
	out, err := Log(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "revision 1.1") {
		t.Error("expected 'revision 1.1' in rlog output")
	}
	if !strings.Contains(out, "revision 1.3") {
		t.Error("expected 'revision 1.3' in rlog output")
	}
}

// TestLogBadPath verifies Log returns an error for a nonexistent file.
func TestLogBadPath(t *testing.T) {
	_, err := Log("/no/such/file.md")
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}

// TestHeadRevReturnsLatest verifies HeadRev returns the head
// revision string.
func TestHeadRevReturnsLatest(t *testing.T) {
	path := setupMultiRev(t)
	rev, err := HeadRev(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rev != "1.3" {
		t.Errorf("expected 1.3, got %s", rev)
	}
}

// TestHeadRevBadPath verifies HeadRev returns an error for a
// nonexistent file.
func TestHeadRevBadPath(t *testing.T) {
	_, err := HeadRev("/no/such/file.md")
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}

// TestDiffRangeShowsChanges verifies DiffRange returns a unified
// diff between two revisions.
func TestDiffRangeShowsChanges(t *testing.T) {
	path := setupMultiRev(t)
	diff, err := DiffRange(path, "1.1", "1.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(diff, "-Version 1.") {
		t.Error("expected removed line in diff")
	}
	if !strings.Contains(diff, "+Version 2.") {
		t.Error("expected added line in diff")
	}
}

// TestDiffRangeIdentical verifies DiffRange returns empty for
// identical revisions.
func TestDiffRangeIdentical(t *testing.T) {
	path := setupMultiRev(t)
	diff, err := DiffRange(path, "1.2", "1.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff != "" {
		t.Errorf("expected empty diff, got: %s", diff)
	}
}

// TestDiffRangeBadRev verifies DiffRange returns an error for a
// nonexistent revision.
func TestDiffRangeBadRev(t *testing.T) {
	path := setupMultiRev(t)
	_, err := DiffRange(path, "1.1", "9.9")
	if err == nil {
		t.Fatal("expected error for bad revision")
	}
}

// TestDiffRangeBadPath verifies DiffRange returns an error for a
// nonexistent file.
func TestDiffRangeBadPath(t *testing.T) {
	_, err := DiffRange("/no/such/file.md", "1.1", "1.2")
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}

// TestCheckoutMakesReadOnly verifies Checkout checks out a prior
// revision and the file is read-only (no owner write bit).
func TestCheckoutMakesReadOnly(t *testing.T) {
	path := setupMultiRev(t)
	if err := Checkout(path, "1.1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Version 1.") {
		t.Errorf("expected Version 1 content, got: %s", data)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm()&0200 != 0 {
		t.Errorf("expected read-only, got mode %o", fi.Mode().Perm())
	}
}

// TestCheckoutBadRev verifies Checkout returns an error for a
// nonexistent revision.
func TestCheckoutBadRev(t *testing.T) {
	path := setupMultiRev(t)
	err := Checkout(path, "9.9")
	if err == nil {
		t.Fatal("expected error for bad revision")
	}
}

// TestCheckoutBadPath verifies Checkout returns an error for a
// nonexistent file.
func TestCheckoutBadPath(t *testing.T) {
	err := Checkout("/no/such/file.md", "1.1")
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}

// TestCheckoutHeadRestoresWritable verifies CheckoutHead checks out
// the head revision with a lock, making the file writable.
func TestCheckoutHeadRestoresWritable(t *testing.T) {
	path := setupMultiRev(t)
	// First checkout an old revision (read-only).
	Checkout(path, "1.1")

	if err := CheckoutHead(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Version 3.") {
		t.Errorf("expected Version 3 content, got: %s", data)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm()&0200 == 0 {
		t.Errorf("expected writable, got mode %o", fi.Mode().Perm())
	}
}

// TestCheckoutHeadBadPath verifies CheckoutHead returns an error
// for a nonexistent file.
func TestCheckoutHeadBadPath(t *testing.T) {
	err := CheckoutHead("/no/such/file.md")
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}

// TestSetTagAndCheckoutTag verifies that a symbolic name can be
// set and read back from rlog -h.
func TestSetTagAndCheckoutTag(t *testing.T) {
	path := setupMultiRev(t)
	if err := SetTag(path, "CHECKOUT", "1.1"); err != nil {
		t.Fatalf("SetTag: %v", err)
	}
	rev, err := CheckoutTag(path, "CHECKOUT")
	if err != nil {
		t.Fatalf("CheckoutTag: %v", err)
	}
	if rev != "1.1" {
		t.Errorf("tag rev = %q, want 1.1", rev)
	}
}

// TestDeleteTag verifies that a symbolic name can be removed.
func TestDeleteTag(t *testing.T) {
	path := setupMultiRev(t)
	SetTag(path, "CHECKOUT", "1.2")
	if err := DeleteTag(path, "CHECKOUT"); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	rev, err := CheckoutTag(path, "CHECKOUT")
	if err != nil {
		t.Fatalf("CheckoutTag after delete: %v", err)
	}
	if rev != "" {
		t.Errorf("tag rev after delete = %q, want empty", rev)
	}
}

// TestCheckoutTagMissing verifies that CheckoutTag returns empty
// for a tag that doesn't exist.
func TestCheckoutTagMissing(t *testing.T) {
	path := setupMultiRev(t)
	rev, err := CheckoutTag(path, "NOSUCHTAG")
	if err != nil {
		t.Fatalf("CheckoutTag: %v", err)
	}
	if rev != "" {
		t.Errorf("tag rev = %q, want empty", rev)
	}
}

// TestSetTagBadPath verifies SetTag returns an error for a bad path.
func TestSetTagBadPath(t *testing.T) {
	if err := SetTag("/no/such/file.md", "X", "1.1"); err == nil {
		t.Fatal("expected error")
	}
}

// TestDeleteTagBadPath verifies DeleteTag returns an error for a bad path.
func TestDeleteTagBadPath(t *testing.T) {
	if err := DeleteTag("/no/such/file.md", "X"); err == nil {
		t.Fatal("expected error")
	}
}

// TestCheckoutTagBadPath verifies CheckoutTag returns an error for a bad path.
func TestCheckoutTagBadPath(t *testing.T) {
	_, err := CheckoutTag("/no/such/file.md", "X")
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestHasArchive verifies HasArchive returns true when an RCS
// archive exists and false when it does not.
func TestHasArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nBody."), 0644)

	// No archive yet.
	if HasArchive(path) {
		t.Error("HasArchive returned true before any checkin")
	}

	// Create archive via Checkin.
	if err := Checkin(path); err != nil {
		t.Fatalf("Checkin: %v", err)
	}

	// Archive should now exist.
	if !HasArchive(path) {
		t.Error("HasArchive returned false after checkin")
	}
}

// TestNeedsCheckinNoArchive verifies NeedsCheckin returns true
// when no archive exists.
func TestNeedsCheckinNoArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nBody."), 0644)

	if !NeedsCheckin(path) {
		t.Error("NeedsCheckin should return true when no archive exists")
	}
}

// TestNeedsCheckinUpToDate verifies NeedsCheckin returns false
// when the archive is newer than the working file (normal state
// after a checkin).
func TestNeedsCheckinUpToDate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nBody."), 0644)

	if err := Checkin(path); err != nil {
		t.Fatalf("Checkin: %v", err)
	}

	if NeedsCheckin(path) {
		t.Error("NeedsCheckin should return false after a clean checkin")
	}
}

// TestNeedsCheckinStaleArchive verifies NeedsCheckin returns true
// when the working file is newer than the archive (edited while
// the server was down).
func TestNeedsCheckinStaleArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nVersion 1."), 0644)

	if err := Checkin(path); err != nil {
		t.Fatalf("Checkin: %v", err)
	}

	// Simulate an edit while the server is down: write new content
	// and set mtime to the future to guarantee it's newer.
	os.WriteFile(path, []byte("title: Test\n\nVersion 2."), 0644)
	archive := filepath.Join(dir, "RCS", "test.md,v")
	ai, _ := os.Stat(archive)
	future := ai.ModTime().Add(2 * time.Second)
	os.Chtimes(path, future, future)

	if !NeedsCheckin(path) {
		t.Error("NeedsCheckin should return true when file is newer than archive")
	}
}

// TestNeedsCheckinMissingWorkFile verifies NeedsCheckin returns false
// when the archive exists but the working file has been removed.
func TestNeedsCheckinMissingWorkFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nBody."), 0644)

	if err := Checkin(path); err != nil {
		t.Fatalf("Checkin: %v", err)
	}
	os.Remove(path)

	if NeedsCheckin(path) {
		t.Error("NeedsCheckin should return false when working file is missing")
	}
}

// TestHeadRevNoHeadLine verifies HeadRev returns an error when the
// archive file does not exist.
func TestHeadRevNoHeadLine(t *testing.T) {
	_, err := HeadRev("/nonexistent/file.md")
	if err == nil {
		t.Fatal("expected error for missing archive")
	}
}

// TestValidRevision verifies the revision format validator.
func TestValidRevision(t *testing.T) {
	valid := []string{"1.1", "1.2", "2.10", "99.99"}
	for _, r := range valid {
		if !ValidRevision(r) {
			t.Errorf("ValidRevision(%q) = false, want true", r)
		}
	}
	invalid := []string{
		"", "abc", "1", "1.", ".1", "1.1.1", "-r1.1",
		"1.1; rm -rf /", "1.1\n", "--version",
	}
	for _, r := range invalid {
		if ValidRevision(r) {
			t.Errorf("ValidRevision(%q) = true, want false", r)
		}
	}
}

// TestDiffRangeInvalidRevision verifies DiffRange rejects bad revisions.
func TestDiffRangeInvalidRevision(t *testing.T) {
	// Bad first rev.
	_, err := DiffRange("/any", "bad", "1.1")
	if err == nil || !strings.Contains(err.Error(), "invalid revision") {
		t.Errorf("DiffRange bad rev1: err = %v", err)
	}
	// Bad second rev.
	_, err = DiffRange("/any", "1.1", "bad")
	if err == nil || !strings.Contains(err.Error(), "invalid revision") {
		t.Errorf("DiffRange bad rev2: err = %v", err)
	}
}

// TestCheckoutInvalidRevision verifies Checkout rejects bad revisions.
func TestCheckoutInvalidRevision(t *testing.T) {
	err := Checkout("/any", "injected;cmd")
	if err == nil || !strings.Contains(err.Error(), "invalid revision") {
		t.Errorf("Checkout bad rev: err = %v", err)
	}
}

// TestSetTagInvalidRevision verifies SetTag rejects bad revisions.
func TestSetTagInvalidRevision(t *testing.T) {
	err := SetTag("/any", "TAG", "--version")
	if err == nil || !strings.Contains(err.Error(), "invalid revision") {
		t.Errorf("SetTag bad rev: err = %v", err)
	}
}

// TestCheckinMkdirError verifies that a MkdirAll failure in Checkin
// propagates the error (e.g. when RCS dir can't be created).
func TestCheckinMkdirError(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file named "RCS" to block MkdirAll.
	rcsBlocker := filepath.Join(dir, "RCS")
	os.WriteFile(rcsBlocker, []byte("blocker"), 0644)

	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nBody."), 0644)

	err := Checkin(path)
	if err == nil {
		t.Fatal("expected MkdirAll error, got nil")
	}
	if !strings.Contains(err.Error(), "mkdir RCS") {
		t.Errorf("error = %v, want 'mkdir RCS'", err)
	}
}

// TestCheckinCiError verifies Checkin returns an error when the
// working file does not exist.
func TestCheckinCiError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ghost.md")

	err := Checkin(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "read") && !strings.Contains(err.Error(), "ghost.md") {
		t.Errorf("error = %v, want read error for ghost.md", err)
	}
}

// TestCheckinCorruptArchive verifies Checkin returns an error when
// the existing archive is corrupt (covers parseArchive parse error
// and Checkin's subsequent-checkin error branch).
func TestCheckinCorruptArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nBody."), 0644)

	// Create a corrupt archive.
	rcsDir := filepath.Join(dir, "RCS")
	os.MkdirAll(rcsDir, 0755)
	os.WriteFile(filepath.Join(rcsDir, "test.md,v"), []byte("garbage"), 0644)

	err := Checkin(path)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

// TestCheckinWriteArchiveError verifies Checkin propagates a
// writeArchive error on subsequent checkins.
func TestCheckinWriteArchiveError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nVersion 1."), 0644)

	if err := Checkin(path); err != nil {
		t.Fatalf("first checkin: %v", err)
	}

	// Make archive + directory unwritable so writeArchive fails.
	rcsDir := filepath.Join(dir, "RCS")
	archive := filepath.Join(rcsDir, "test.md,v")
	os.Chmod(archive, 0444)
	os.Chmod(rcsDir, 0555)
	defer func() {
		os.Chmod(rcsDir, 0755)
		os.Chmod(archive, 0644)
	}()

	os.WriteFile(path, []byte("title: Test\n\nVersion 2."), 0644)
	err := Checkin(path)
	if err == nil {
		t.Fatal("expected writeArchive error, got nil")
	}
}

// TestLogWithSymbols verifies Log includes symbolic names in output.
func TestLogWithSymbols(t *testing.T) {
	path := setupMultiRev(t)
	if err := SetTag(path, "RELEASE", "1.2"); err != nil {
		t.Fatalf("SetTag: %v", err)
	}

	out, err := Log(path)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if !strings.Contains(out, "symbolic names:") {
		t.Error("expected 'symbolic names:' in output")
	}
	if !strings.Contains(out, "RELEASE") {
		t.Error("expected 'RELEASE' in output")
	}
}

// TestSetTagUpdatesExisting verifies SetTag updates an existing
// tag rather than appending a duplicate.
func TestSetTagUpdatesExisting(t *testing.T) {
	path := setupMultiRev(t)
	if err := SetTag(path, "TAG", "1.1"); err != nil {
		t.Fatalf("SetTag 1.1: %v", err)
	}
	if err := SetTag(path, "TAG", "1.2"); err != nil {
		t.Fatalf("SetTag 1.2: %v", err)
	}
	rev, err := CheckoutTag(path, "TAG")
	if err != nil {
		t.Fatalf("CheckoutTag: %v", err)
	}
	if rev != "1.2" {
		t.Errorf("tag rev = %q, want 1.2", rev)
	}
}

// TestDeleteTagNoMatch verifies DeleteTag is a no-op when the
// named tag does not exist — other tags are preserved.
func TestDeleteTagNoMatch(t *testing.T) {
	path := setupMultiRev(t)
	if err := SetTag(path, "KEEP", "1.1"); err != nil {
		t.Fatalf("SetTag: %v", err)
	}
	// Delete a tag that doesn't exist; KEEP should survive.
	if err := DeleteTag(path, "NOSUCHTAG"); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	rev, _ := CheckoutTag(path, "KEEP")
	if rev != "1.1" {
		t.Errorf("KEEP tag = %q, want 1.1", rev)
	}
}

// TestCheckoutWriteError verifies Checkout propagates a write error
// (e.g. read-only parent directory).
func TestCheckoutWriteError(t *testing.T) {
	path := setupMultiRev(t)
	dir := filepath.Dir(path)
	// Remove file first, then make dir unwritable so WriteFile fails.
	os.Remove(path)
	os.Chmod(dir, 0555)
	defer os.Chmod(dir, 0755)

	err := Checkout(path, "1.1")
	if err == nil {
		t.Fatal("expected write error, got nil")
	}
}

// TestCheckoutHeadCheckoutError verifies CheckoutHead propagates
// checkout errors (e.g. when the archive is corrupt).
func TestCheckoutHeadCheckoutError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nBody."), 0644)

	// Create a corrupt archive.
	rcsDir := filepath.Join(dir, "RCS")
	os.MkdirAll(rcsDir, 0755)
	os.WriteFile(filepath.Join(rcsDir, "test.md,v"), []byte("garbage"), 0644)

	err := CheckoutHead(path)
	if err == nil {
		t.Fatal("expected error for corrupt archive, got nil")
	}
}

// TestCheckoutHeadWriteArchiveError verifies CheckoutHead propagates
// a writeArchive error.
func TestCheckoutHeadWriteArchiveError(t *testing.T) {
	path := setupMultiRev(t)
	rcsDir := filepath.Join(filepath.Dir(path), "RCS")
	archive := filepath.Join(rcsDir, "test.md,v")
	os.Chmod(archive, 0444)
	os.Chmod(rcsDir, 0555)
	defer func() {
		os.Chmod(rcsDir, 0755)
		os.Chmod(archive, 0644)
	}()

	err := CheckoutHead(path)
	if err == nil {
		t.Fatal("expected writeArchive error, got nil")
	}
}

// TestCheckoutHeadWriteFileError verifies CheckoutHead propagates
// a file write error.
func TestCheckoutHeadWriteFileError(t *testing.T) {
	path := setupMultiRev(t)
	dir := filepath.Dir(path)
	// Remove file so os.Remove in CheckoutHead is a no-op error,
	// then make dir unwritable so WriteFile (create) fails.
	os.Remove(path)
	os.Chmod(dir, 0555)
	defer os.Chmod(dir, 0755)

	err := CheckoutHead(path)
	if err == nil {
		t.Fatal("expected write error, got nil")
	}
}

// TestDiffRangeCheckoutRev1Error verifies DiffRange returns an
// error when the first revision doesn't exist.
func TestDiffRangeCheckoutRev1Error(t *testing.T) {
	path := setupMultiRev(t)
	_, err := DiffRange(path, "9.9", "1.1")
	if err == nil {
		t.Fatal("expected error for nonexistent rev1")
	}
	if !strings.Contains(err.Error(), "checkout") {
		t.Errorf("error = %v, want 'checkout' in message", err)
	}
}

// TestDiffRangePureInsert verifies unified diff for pure insertion
// (covers makeHunks with no aFirst context lines).
func TestDiffRangePureInsert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\na"), 0644)
	Checkin(path)
	os.WriteFile(path, []byte("title: Test\n\na\nb\nc"), 0644)
	Checkin(path)

	diff, err := DiffRange(path, "1.1", "1.2")
	if err != nil {
		t.Fatalf("DiffRange: %v", err)
	}
	if !strings.Contains(diff, "+b") {
		t.Errorf("expected '+b' in diff, got: %s", diff)
	}
}

// TestDiffRangePureDelete verifies unified diff for pure deletion
// (covers makeHunks with no bFirst context lines).
func TestDiffRangePureDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\na\nb\nc"), 0644)
	Checkin(path)
	os.WriteFile(path, []byte("title: Test\n\na"), 0644)
	Checkin(path)

	diff, err := DiffRange(path, "1.1", "1.2")
	if err != nil {
		t.Fatalf("DiffRange: %v", err)
	}
	if !strings.Contains(diff, "-b") {
		t.Errorf("expected '-b' in diff, got: %s", diff)
	}
}

// TestSplitLinesEmpty verifies splitLines returns nil for empty input.
func TestSplitLinesEmpty(t *testing.T) {
	if got := splitLines(""); got != nil {
		t.Errorf("splitLines(\"\") = %v, want nil", got)
	}
}

// TestSplitLinesNoTrailingNewline verifies splitLines preserves
// content without a trailing newline.
func TestSplitLinesNoTrailingNewline(t *testing.T) {
	got := splitLines("a\nb")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("splitLines(\"a\\nb\") = %v, want [a b]", got)
	}
}

// TestHeadRevEmptyHead verifies HeadRev returns an error when the
// archive parses but has no head revision set.
func TestHeadRevEmptyHead(t *testing.T) {
	dir := t.TempDir()
	rcsDir := filepath.Join(dir, "RCS")
	os.MkdirAll(rcsDir, 0755)
	// Minimal ,v file with empty head field but valid structure.
	archive := filepath.Join(rcsDir, "test.md,v")
	os.WriteFile(archive, []byte("head\t;\naccess;\nsymbols;\nlocks;\nstrict;\n\ndesc\n@@\n\n"), 0644)

	path := filepath.Join(dir, "test.md")
	_, err := HeadRev(path)
	if err == nil {
		t.Fatal("expected error for empty head")
	}
	if !strings.Contains(err.Error(), "no head revision") {
		t.Errorf("error = %v, want 'no head revision'", err)
	}
}

// TestDiffRangeCompleteRewrite covers makeHunks boundary where the
// entire file content changes (all lines are '-' then '+', exercising
// the aFirst < 0 / bFirst < 0 fallback paths).
func TestDiffRangeCompleteRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nold1\nold2"), 0644)
	Checkin(path)
	os.WriteFile(path, []byte("title: Test\n\nnew1\nnew2\nnew3"), 0644)
	Checkin(path)

	diff, err := DiffRange(path, "1.1", "1.2")
	if err != nil {
		t.Fatalf("DiffRange: %v", err)
	}
	if !strings.Contains(diff, "-old1") || !strings.Contains(diff, "+new1") {
		t.Errorf("unexpected diff:\n%s", diff)
	}
}

// TestSetTagWriteArchiveError verifies SetTag propagates writeArchive error.
func TestSetTagWriteArchiveError(t *testing.T) {
	path := setupMultiRev(t)
	rcsDir := filepath.Join(filepath.Dir(path), "RCS")
	archive := filepath.Join(rcsDir, "test.md,v")
	// Make archive read-only AND directory read-only so neither
	// remove nor overwrite can succeed.
	os.Chmod(archive, 0444)
	os.Chmod(rcsDir, 0555)
	defer func() {
		os.Chmod(rcsDir, 0755)
		os.Chmod(archive, 0644)
	}()

	err := SetTag(path, "TAG", "1.1")
	if err == nil {
		t.Fatal("expected writeArchive error, got nil")
	}
}

// TestUnifiedDiffEmptyToContent covers the edge case where the
// old file is completely empty and the new file has content,
// producing a hunk of pure insertions (aFirst < 0 fallback).
func TestUnifiedDiffEmptyToContent(t *testing.T) {
	diff := unifiedDiff("f", "1.1", "1.2", "", "new\nlines\n")
	if !strings.Contains(diff, "+new") {
		t.Errorf("expected '+new' in diff, got:\n%s", diff)
	}
}

// TestUnifiedDiffContentToEmpty covers the edge case where the
// old file has content and the new file is empty, producing a
// hunk of pure deletions (bFirst < 0 fallback).
func TestUnifiedDiffContentToEmpty(t *testing.T) {
	diff := unifiedDiff("f", "1.1", "1.2", "old\nlines\n", "")
	if !strings.Contains(diff, "-old") {
		t.Errorf("expected '-old' in diff, got:\n%s", diff)
	}
}

// TestCheckinLibraryError verifies that Checkin propagates an error
// from the gorcs Checkin method (e.g. internal library failure).
func TestCheckinLibraryError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("title: Test\n\nV1."), 0644)
	Checkin(path) // create archive

	os.WriteFile(path, []byte("title: Test\n\nV2."), 0644)
	patches := gomonkey.ApplyMethodFunc(
		&gorcs.File{}, "Checkin",
		func(_ string, _ string, _ string, _ ...any) (*gorcs.CIVerdict, error) {
			return nil, errors.New("mock ci error")
		})
	defer patches.Reset()

	err := Checkin(path)
	if err == nil {
		t.Fatal("expected library checkin error, got nil")
	}
	if !strings.Contains(err.Error(), "checkin") {
		t.Errorf("error = %v, want 'checkin' in message", err)
	}
}

// TestCheckoutHeadLibraryError verifies that CheckoutHead propagates
// an error from the gorcs Checkout method.
func TestCheckoutHeadLibraryError(t *testing.T) {
	path := setupMultiRev(t)
	patches := gomonkey.ApplyMethodFunc(
		&gorcs.File{}, "Checkout",
		func(_ string, _ ...any) (*gorcs.COVerdict, error) {
			return nil, errors.New("mock co error")
		})
	defer patches.Reset()

	err := CheckoutHead(path)
	if err == nil {
		t.Fatal("expected library checkout error, got nil")
	}
	if !strings.Contains(err.Error(), "co -l") {
		t.Errorf("error = %v, want 'co -l' in message", err)
	}
}

// TestMakeHunksEmptyEdits verifies makeHunks returns nil for empty
// input (defensive early-return path).
func TestMakeHunksEmptyEdits(t *testing.T) {
	got := makeHunks(nil, 0, 0, 3)
	if got != nil {
		t.Errorf("makeHunks(nil) = %v, want nil", got)
	}
}
