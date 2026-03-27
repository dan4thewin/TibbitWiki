// Package loader scans a notes directory and loads all .md files
// into the store at startup.
package loader

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dan4thewin/tibbitwiki/internal/rcs"
	"github.com/dan4thewin/tibbitwiki/internal/store"
	"github.com/dan4thewin/tibbitwiki/internal/tid"
)

// BodySeeder populates the body cache during loading.
type BodySeeder interface {
	SeedBody(title, text string)
}

// quarantine moves a file to {dir}/lost+found/ and logs a warning.
func quarantine(dir, path, reason string) {
	lf := filepath.Join(dir, "lost+found")
	if err := os.MkdirAll(lf, 0o755); err != nil {
		log.Printf("loader: cannot create lost+found: %v", err)
		return
	}
	dest := filepath.Join(lf, filepath.Base(path))
	if err := os.Rename(path, dest); err != nil {
		log.Printf("loader: cannot quarantine %s: %v", path, err)
		return
	}
	log.Printf("loader: quarantined %s → lost+found/ (%s)", filepath.Base(path), reason)
}

// isDangling returns true if path is a symlink whose target does not exist.
func isDangling(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	_, err = os.Stat(path)
	return err != nil
}

// Load reads all .md files from dir and populates the store.
// If seeder is non-nil, note bodies are also cached.
// Returns the number of notes loaded and a map of symlinked titles
// to their owning wiki names (for delegation at read time).
// Dangling symlinks are moved to {dir}/lost+found/.
func Load(dir string, s *store.Store, seeder BodySeeder) (int, map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, nil, fmt.Errorf("reading notes directory: %w", err)
	}

	symlinks := map[string]string{}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(dir, entry.Name())

		if isDangling(path) {
			quarantine(dir, path, "dangling symlink")
			continue
		}

		// Symlinks are not loaded into the store; they are delegated
		// to the owning wiki at read time.
		if owner := SymlinkOwner(path); owner != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("loader: skipping symlink %s: %v", path, err)
				continue
			}
			td, err := tid.Parse(data)
			if err != nil {
				log.Printf("loader: skipping symlink %s: %v", path, err)
				continue
			}
			symlinks[td.Title()] = owner
			continue
		}

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
		if seeder != nil {
			seeder.SeedBody(td.Title(), td.Text())
		}
		if rcs.NeedsCheckin(path) {
			if err := rcs.Checkin(path); err != nil {
				log.Printf("loader: rcs checkin %s: %v", path, err)
			}
		}
		count++
	}

	return count, symlinks, nil
}

// SymlinkOwner returns the owning wiki name if path is a symlink
// to a sibling directory (e.g. ../work/Note.md → "work").
// Returns "" for regular files or unresolvable symlinks.
func SymlinkOwner(path string) string {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return ""
	}
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	// Expect relative path like ../work/Note.md
	parts := strings.Split(filepath.ToSlash(target), "/")
	if len(parts) >= 2 && parts[0] == ".." {
		return parts[1]
	}
	return ""
}
