// Package loader scans a tiddlers directory and loads all .tid files
// into the store at startup.
package loader

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dan4thewin/tibbitwiki/internal/store"
	"github.com/dan4thewin/tibbitwiki/internal/tid"
)

// Load reads all .tid files from dir and populates the store.
// Returns the number of tiddlers loaded.
func Load(dir string, s *store.Store) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("reading tiddlers directory: %w", err)
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".tid") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("loader: skipping %s: %v", path, err)
			continue
		}

		td, err := tid.Parse(data)
		if err != nil {
			log.Printf("loader: skipping %s: %v", path, err)
			continue
		}

		s.PutWithPath(td, path)
		count++
	}

	return count, nil
}
