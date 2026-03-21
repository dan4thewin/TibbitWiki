package editor

import (
	"testing"
)

// TestNewEditor verifies that New stores the command template.
func TestNewEditor(t *testing.T) {
	e := New("neovide -- %s")
	if e.Command() != "neovide -- %s" {
		t.Errorf("Command() = %q, want %q", e.Command(), "neovide -- %s")
	}
}

// TestOpenWithEcho verifies that Open successfully launches a
// simple command (echo) without error.
func TestOpenWithEcho(t *testing.T) {
	e := New("echo %s")
	err := e.Open("/tmp/test.md")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestOpenEmptyCommand verifies that an empty command returns error.
func TestOpenEmptyCommand(t *testing.T) {
	e := New("")
	err := e.Open("/tmp/test.md")
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}

// TestOpenBadCommand verifies that a nonexistent command returns
// an error.
func TestOpenBadCommand(t *testing.T) {
	e := New("/no/such/binary %s")
	err := e.Open("/tmp/test.md")
	if err == nil {
		t.Fatal("expected error for bad command, got nil")
	}
}
