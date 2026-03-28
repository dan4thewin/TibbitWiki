// Package editor launches external editors for note files.
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
// The template must contain at least one %s placeholder which will
// be replaced with the file path. Templates should include "--"
// before %s to prevent flag injection (e.g. "neovide -- %s").
func New(cmdTemplate string) (*Editor, error) {
	parts := strings.Fields(cmdTemplate)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty editor command")
	}
	count := strings.Count(cmdTemplate, "%s")
	if count == 0 {
		return nil, fmt.Errorf("editor command must contain %%s placeholder")
	}
	return &Editor{cmdTemplate: cmdTemplate}, nil
}

// Open launches the editor with the given file path.
// The command is run asynchronously (fire and forget).
func (e *Editor) Open(filePath string) error {
	// Split template first, then substitute %s so file paths with
	// spaces stay as a single argument.
	parts := strings.Fields(e.cmdTemplate)
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
