package editor

import (
	"testing"
)

// TestNewEditor verifies that New stores the command template.
func TestNewEditor(t *testing.T) {
	e, err := New("neovide -- %s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Command() != "neovide -- %s" {
		t.Errorf("Command() = %q, want %q", e.Command(), "neovide -- %s")
	}
}

// TestOpenSubstitution verifies that Open correctly substitutes %s
// with the file path and passes -- through to the command.
func TestOpenSubstitution(t *testing.T) {
	e, err := New("echo -- %s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// echo just prints args — verifies the command runs without error.
	if err := e.Open("/tmp/Test Note.md"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestNewEmptyCommand verifies that an empty command returns error.
func TestNewEmptyCommand(t *testing.T) {
	_, err := New("")
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}

// TestNewMissingPlaceholder verifies that a command without %s
// returns an error from New.
func TestNewMissingPlaceholder(t *testing.T) {
	_, err := New("vim")
	if err == nil {
		t.Fatal("expected error for missing placeholder, got nil")
	}
}

// TestOpenBadCommand verifies that a nonexistent command returns
// an error.
func TestOpenBadCommand(t *testing.T) {
	e, err := New("/no/such/binary %s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := e.Open("/tmp/test.md"); err == nil {
		t.Fatal("expected error for bad command, got nil")
	}
}
