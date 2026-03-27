// Package rcs provides automatic RCS version control for note files.
// Archives are stored in an RCS/ subdirectory alongside the working
// files. Keyword expansion is suppressed (rcs -ko) so checkins never
// modify the working file.
package rcs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// HasArchive reports whether an RCS archive exists for path.
func HasArchive(path string) bool {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	return fileExists(filepath.Join(dir, "RCS", base+",v"))
}

// NeedsCheckin reports whether path should be checked in.
// Returns true if no archive exists, or if the working file is
// newer than the archive (edited while the server was down).
func NeedsCheckin(path string) bool {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	archive := filepath.Join(dir, "RCS", base+",v")
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

// Log runs rlog on the given file and returns its output.
func Log(path string) (string, error) {
	cmd := exec.Command("rlog", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("rlog %s: %w", path, err)
	}
	return string(out), nil
}

// HeadRev returns the head revision string (e.g. "1.5") by
// parsing the output of rlog -h.
func HeadRev(path string) (string, error) {
	cmd := exec.Command("rlog", "-h", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("rlog -h %s: %w", path, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "head:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "head:")), nil
		}
	}
	return "", fmt.Errorf("no head revision in rlog -h output for %s", path)
}

// DiffRange runs rcsdiff -u between two revisions and returns the
// unified diff output. An empty diff (exit 0) returns "".
func DiffRange(path, rev1, rev2 string) (string, error) {
	cmd := exec.Command("rcsdiff", "-u", "-r"+rev1, "-r"+rev2, path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// rcsdiff exits 1 when files differ — that's normal.
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return stdout.String(), nil
		}
		return "", fmt.Errorf("rcsdiff %s: %w: %s", path, err, stderr.String())
	}
	return stdout.String(), nil
}

// Checkout checks out a specific revision without a lock (read-only).
// The working file becomes 0444.
func Checkout(path, rev string) error {
	cmd := exec.Command("co", "-f", "-r"+rev, path)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("co -r%s %s: %w", rev, path, err)
	}
	return nil
}

// CheckoutHead checks out the head revision with a lock (writable).
func CheckoutHead(path string) error {
	cmd := exec.Command("co", "-l", "-f", path)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("co -l %s: %w", path, err)
	}
	return nil
}

// SetTag assigns the symbolic name to a revision (rcs -n<name>:<rev>).
func SetTag(path, name, rev string) error {
	cmd := exec.Command("rcs", "-n"+name+":"+rev, path)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rcs -n%s:%s %s: %w", name, rev, path, err)
	}
	return nil
}

// DeleteTag removes a symbolic name (rcs -n<name>).
func DeleteTag(path, name string) error {
	cmd := exec.Command("rcs", "-n"+name, path)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rcs -n%s %s: %w", name, path, err)
	}
	return nil
}

// CheckoutTag returns the revision associated with a symbolic name
// by parsing rlog -h output. Returns "" if the tag is not found.
func CheckoutTag(path, name string) (string, error) {
	cmd := exec.Command("rlog", "-h", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("rlog -h %s: %w", path, err)
	}
	// Symbolic names appear as "\t<name>: <rev>" after "symbolic names:".
	inSymbols := false
	prefix := "\t" + name + ": "
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "symbolic names:") {
			inSymbols = true
			continue
		}
		if inSymbols {
			if strings.HasPrefix(line, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(line, prefix)), nil
			}
			if line != "" && line[0] != '\t' {
				break // past symbolic names section
			}
		}
	}
	return "", nil
}
