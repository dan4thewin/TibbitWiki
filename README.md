# go-tiddle

A Go replacement for TiddlyWiki's Node.js server with a minimal
browser client, filesystem watching, external editor integration,
and RCS version control.

## Features

- **Minimal browser client** — ~6KB HTML+JS, dark theme, no TiddlyWiki
  browser code. Markdown rendering with CamelCase WikiWord auto-linking.
  View-only in browser; all editing via neovim.
- **TiddlyWeb-compatible REST API** — 7 endpoints plus edit-external
- **Filesystem watching** — edit `.tid` files in any editor (Neovide,
  vim, etc.) and changes appear in the browser within 3 seconds
- **External editor launch** — click "edit" in the browser to open
  a tiddler in your configured editor
- **RCS version control** — every tiddler save is automatically
  checked into RCS; full revision history per file
- **Simple configuration** — YAML config file with CLI flag overrides

## Quick start

```bash
# Build
go build -o go-tiddle .

# Run with the built-in minimal client
./go-tiddle \
  --wiki-path ~/notes \
  --listen 127.0.0.1:8080 \
  --index-html exp/012-integration/index.html \
  --editor-cmd "neovide -- %s"
```

Then open http://127.0.0.1:8080 in your browser.

## Browser client

The browser client (`exp/012-integration/index.html`) is a single
self-contained HTML file with inline JS. No build step, no
dependencies beyond a CDN link to marked.js.

**Features:**
- Story river — multiple tiddlers open in a scrollable column
- Markdown rendering via marked.js
- CamelCase WikiWord auto-linking (blue for existing, red for missing)
- Recent tiddlers sidebar (top 20 by modified date)
- 3-second polling for external changes
- Edit button per tiddler (launches neovim via server)
- New tiddler and Journal buttons
- Dark theme

**Non-features (by design):**
- No browser-based editing — all editing via neovim
- No TiddlyWiki wikitext rendering — markdown only
- No plugin system, no syncer, no complex widget tree

## Configuration

### CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | | Path to YAML config file |
| `--wiki-path` | `.` | Path to wiki folder containing `tiddlers/` |
| `--listen` | `127.0.0.1:8080` | Host:port to listen on |
| `--editor-cmd` | `neovide -- %s` | Editor command (`%s` = file path) |
| `--index-html` | | Path to HTML file served for GET / |
| `--poll-interval` | `3s` | Browser poll interval (informational) |

### YAML config file

```yaml
wiki_path: ~/notes
listen: 127.0.0.1:8080
editor_cmd: "neovide -- %s"
index_html: exp/012-integration/index.html
```

CLI flags override YAML values.

## API endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | Serve browser client HTML |
| GET | `/status` | Server status JSON |
| GET | `/recipes/default/tiddlers.json` | Skinny tiddler list |
| GET | `/recipes/default/tiddlers/:title` | Full tiddler JSON |
| PUT | `/recipes/default/tiddlers/:title` | Create/save tiddler |
| DELETE | `/bags/default/tiddlers/:title` | Delete tiddler |
| GET | `/favicon.ico` | Favicon |
| POST | `/edit-external/:title` | Launch external editor |

## Editing tiddlers

Two ways:

1. **Direct file edit** — open any `.tid` file in your editor, save,
   the server picks up changes via fsnotify within 3 seconds.
2. **Browser button** — click "edit" in a tiddler's toolbar to launch
   neovim via the server's edit-external endpoint.

## RCS version control

Every tiddler write creates/updates an RCS archive in an `RCS/`
subdirectory alongside the tiddlers. View history with:

```bash
rlog tiddlers/MyTiddler.tid
rcsdiff tiddlers/MyTiddler.tid
co -r1.2 -p tiddlers/MyTiddler.tid
```

Requires `rcs` (`brew install rcs` on macOS).

## Architecture

```
internal/
  config/   — YAML config + CLI flag parsing
  tid/      — .tid file parser/serializer
  store/    — in-memory tiddler store with revision tracking
  watcher/  — fsnotify file watcher with debounce
  server/   — HTTP server implementing TiddlyWeb API
  editor/   — external editor launcher
  rcs/      — RCS version control integration
  loader/   — startup file loader
main.go     — wiring
exp/        — browser client experiments (001–012)
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
