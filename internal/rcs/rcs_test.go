package rcs

import (
	"os"
	"path/filepath"
	"testing"
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
