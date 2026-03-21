// Package rcs provides automatic RCS version control for note files.
// Archives are stored in an RCS/ subdirectory alongside the working
// files. Keyword expansion is suppressed (rcs -ko) so checkins never
// modify the working file.
package rcs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Checkin runs ci -l on the given file, creating the ,v archive on
// first use inside the RCS/ subdirectory. On first checkin, sets -ko
// on the archive to suppress keyword expansion in all future
// checkouts. The working file is kept locked (writable). Stdin is
// closed so ci never prompts.
func Checkin(path string) error {
	dir := filepath.Dir(path)
	rcsDir := filepath.Join(dir, "RCS")
	base := filepath.Base(path)
	archive := filepath.Join(rcsDir, base+",v")

	first := !fileExists(archive)
	if first {
		os.MkdirAll(rcsDir, 0755)
	}

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
