// Package editor launches external editors for tiddler files.
package editor

import (
	"fmt"
	"os/exec"
	"strings"
)

// Editor manages launching an external editor process.
type Editor struct {
	cmdTemplate string // e.g. "neovide -- %s"
}

// New creates an Editor with the given command template.
// The template must contain %s which will be replaced with the
// file path.
func New(cmdTemplate string) *Editor {
	return &Editor{cmdTemplate: cmdTemplate}
}

// Open launches the editor with the given file path.
// The command is run asynchronously (fire and forget).
func (e *Editor) Open(filePath string) error {
	// Split template first, then substitute %s so file paths with
	// spaces stay as a single argument.
	parts := strings.Fields(e.cmdTemplate)
	if len(parts) == 0 {
		return fmt.Errorf("empty editor command")
	}
	for i, p := range parts {
		if strings.Contains(p, "%s") {
			parts[i] = strings.Replace(p, "%s", filePath, 1)
		}
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting editor: %w", err)
	}

	// Don't wait — editor runs independently.
	go cmd.Wait()
	return nil
}

// Command returns the configured command template.
func (e *Editor) Command() string {
	return e.cmdTemplate
}
