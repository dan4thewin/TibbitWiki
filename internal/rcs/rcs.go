// Package rcs provides automatic RCS version control for tiddler files.
// Each .tid file gets a companion ,v file storing its full revision
// history. Keyword expansion is suppressed (rcs -ko) so checkins
// never modify the working file.
package rcs

import (
	"fmt"
	"os"
	"os/exec"
)

// Checkin runs ci -l on the given file, creating the ,v archive on
// first use. On first checkin, sets -ko on the archive to suppress
// keyword expansion in all future checkouts. The working file is
// kept locked (writable). Stdin is closed so ci never prompts.
func Checkin(path string) error {
	first := !fileExists(path + ",v")

	cmd := exec.Command("ci", "-l", path)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ci %s: %w", path, err)
	}

	if first {
		rcsCmd := exec.Command("rcs", "-ko", path)
		rcsCmd.Stdin = nil
		rcsCmd.Stdout = nil
		rcsCmd.Stderr = nil
		rcsCmd.Run() // best-effort
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
