// Package rcs provides automatic RCS version control for tiddler files.
// Each .tid file gets a companion ,v file storing its full revision
// history with default keyword expansion.
package rcs

import (
	"fmt"
	"os"
	"os/exec"
)

// Checkin runs ci -l on the given file, creating the ,v archive on
// first use. The working file is kept locked (writable). Stdin is
// closed so ci never prompts interactively.
func Checkin(path string) error {
	cmd := exec.Command("ci", "-l", path)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ci %s: %w", path, err)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
