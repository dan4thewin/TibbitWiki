# TibbitWiki

A Go replacement for TiddlyWiki's Node.js server with a minimal
browser client, filesystem watching, external editor integration,
and RCS version control.

## Features

- **Minimal browser client** — single HTML+JS file, dark theme, no
  TiddlyWiki browser code. Server-side markdown rendering with CamelCase
  WikiWord auto-linking, syntax highlighting, full-text search, tag
  pills, tabbed sidebar, and line-level quick edit.
- **REST API** — `/notes` CRUD plus line-edit, search, edit-external
- **Filesystem watching** — edit `.tid` files in any editor (Neovide,
  vim, etc.) and changes appear in the browser within 3 seconds
- **External editor launch** — click "edit" in the browser to open
  a note in your configured editor
- **RCS version control** — every note save is automatically
  checked into RCS; full revision history per file
- **Simple configuration** — YAML config file with CLI flag overrides

## Quick start

```bash
# Build
go build -o tibbitwiki .

# Run with the built-in minimal client
./tibbitwiki \
  --wiki-path ~/notes \
  --listen 127.0.0.1:8080 \
  --index-html exp/025-v3-integration/index.html \
  --editor-cmd "neovide -- %s"
```

Then open http://127.0.0.1:8080 in your browser.

## Browser client

The browser client (`exp/025-v3-integration/index.html`) is a single
self-contained HTML file with inline JS. No build step, no external
dependencies — markdown rendering and syntax highlighting are handled
server-side.

**Features:**
- Story river — multiple notes open in a scrollable column (new
  content appended at bottom)
- Server-side markdown rendering via goldmark
- Syntax highlighting via chroma (OneDark theme, served as `/theme.css`)
- CamelCase WikiWord auto-linking (blue for existing, red for missing)
- Full-text search — `/` opens vi-style search bar; results appear as
  a pseudo-note with clickable titles and matched lines
- Tag pills — colored pills on notes, click to search by tag
- Tabbed sidebar — Recent (non-journals), Journals, Tags with
  per-tag groups; ellipsis to expand; limits via CSS custom properties
- Line-level quick edit — hover ✏️/➕ icons on list items for
  inline edit and insert-below without leaving the browser
- 3-second polling for external changes
- Edit button per note (launches neovim via server)
- New note and Journal buttons
- Dark theme

**Non-features (by design):**
- No full browser-based editing — line edits for list items only;
  everything else via neovim
- No TiddlyWiki wikitext rendering — markdown only
- No plugin system, no syncer, no complex widget tree

## Configuration

### CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | | Path to YAML config file |
| `--wiki-path` | `.` | Path to wiki folder containing `notes/` |
| `--listen` | `127.0.0.1:8080` | Host:port to listen on |
| `--editor-cmd` | `neovide -- %s` | Editor command (`%s` = file path) |
| `--index-html` | | Path to HTML file served for GET / |
| `--poll-interval` | `3s` | Browser poll interval (informational) |

### YAML config file

```yaml
wiki_path: ~/notes
listen: 127.0.0.1:8080
editor_cmd: "neovide -- %s"
index_html: exp/025-v3-integration/index.html
```

CLI flags override YAML values.

## API endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | Serve browser client HTML |
| GET | `/status` | Server status JSON |
| GET | `/notes` | Skinny note list |
| GET | `/notes/:title` | Full note JSON (with rendered HTML) |
| PUT | `/notes/:title` | Create/save note |
| DELETE | `/notes/:title` | Delete note |
| POST | `/line-edit/:title` | Replace or insert a body line |
| GET | `/search?q=...` | Full-text search (JSON) |
| GET | `/theme.css` | Syntax highlighting CSS |
| GET | `/favicon.ico` | Favicon |
| POST | `/edit-external/:title` | Launch external editor |

## Editing notes

Two ways:

1. **Direct file edit** — open any `.tid` file in your editor, save,
   the server picks up changes via fsnotify within 3 seconds.
2. **Browser button** — click "edit" in a note's toolbar to launch
   neovim via the server's edit-external endpoint.

## RCS version control

Every note write creates/updates an RCS archive in an `RCS/`
subdirectory alongside the notes. View history with:

```bash
rlog notes/MyTiddler.tid
rcsdiff notes/MyTiddler.tid
co -r1.2 -p notes/MyTiddler.tid
```

Requires `rcs` (`brew install rcs` on macOS).

## Search

Full-text search uses ripgrep (`rg`) as the backend, searching `.tid`
files in the notes directory. Requires `rg` on PATH (`brew install
ripgrep` on macOS).

**Query syntax:**

| Syntax | Meaning |
|--------|---------|
| `word` | Literal (case-insensitive) |
| `!word` | NOT |
| `#tag` | Tag search |
| `@title` | Title substring |
| `/regex/` | Raw regex |
| `m/regex/` or `m\|regex\|` | Regex with custom delimiter |
| `a\|b` | OR |
| `a b` | AND (space = set intersection) |

## Architecture

```
internal/
  config/   — YAML config + CLI flag parsing
  tid/      — .tid file parser/serializer
  store/    — in-memory note store with revision tracking
  watcher/  — fsnotify file watcher with debounce
  server/   — HTTP server implementing TiddlyWeb API
  editor/   — external editor launcher
  rcs/      — RCS version control integration
  loader/   — startup file loader
  render/   — goldmark + chroma markdown→HTML rendering
  search/   — query parser + rg wrapper for full-text search
main.go     — wiring
exp/        — browser client experiments (001–018)
```

No database. All state is in-memory + filesystem.

## Development

```bash
mk build      # go build ./...
mk test       # go test with inlining disabled
mk lint       # go vet + gofmt check
mk coverage   # statement coverage report
mk complexity # gocyclo top 20
mk check      # all of the above
```

Build tool: [mk](https://github.com/dcjones/mk) (Plan 9 mk in Go).
Install: `go install github.com/dcjones/mk@latest`

See [TESTING.md](TESTING.md) for browser automation and integration
testing techniques.
