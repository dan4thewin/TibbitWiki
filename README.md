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
- **Multi-wiki** — auto-discovers wiki subdirectories, routes at
  `/{wiki}/notes/...`; wiki selector at `/`
- **Filesystem watching** — edit `.md` files in any editor (Neovide,
  vim, etc.) and changes appear in the browser within 3 seconds
- **External editor launch** — click "edit" in the browser to open
  a note in your configured editor
- **RCS version control** — every note save is automatically
  checked into RCS; full revision history per file
- **Revisions browser** — view syntax-highlighted diffs between
  consecutive revisions, checkout old revisions (read-only), promote
  an old revision as new head (non-destructive rollback), and restore
  head. Revision number shown on card when viewing a prior revision.
  Toolbar chevron reveals revisions and delete buttons.
- **Config-driven theming** — all colors, fonts, and layout values
  live in `config.yaml`; edit and save to see changes live
- **Theme playground** — standalone HTML page for previewing theme
  tweaks with color pickers and live preview
- **LRU body cache** — bounded memory; metadata always in RAM,
  note bodies cached with configurable size (default 200)
- **Simple configuration** — YAML config file with CLI flag overrides;
  auto-created on first run with commented defaults

## Quick start

```bash
# Build
go build -o tibbitwiki .

# Run (auto-creates config.yaml in wiki path)
./tibbitwiki --wiki-path ~/notes
```

Then open http://127.0.0.1:8080 in your browser.

The server auto-discovers wiki subdirectories under `--wiki-path`.
Each subdirectory with `.md` files becomes a wiki at `/{name}/`.
Wikis are flat — only `.md` files directly in the subdirectory are
loaded as notes (no nested directories).

## Browser client

The browser client is a single self-contained HTML file with inline
JS, embedded in the binary (`internal/server/index.html`). No build
step — markdown rendering and syntax highlighting are handled
server-side. The `--index-html` flag overrides with an external file
for development.

**Features:**
- Story river — multiple notes open in a scrollable column
- Server-side markdown rendering via goldmark
- Syntax highlighting via chroma (OneDark theme, served as `/theme.css`)
- CamelCase WikiWord auto-linking (blue for existing, red for missing)
- Full-text search — `/` opens vi-style search bar; results appear as
  a pseudo-note with clickable titles and matched lines
- Tag pills — colored pills on notes, click to search by tag
- Tabbed sidebar — Recent (non-journals), Journals, Tags with
  per-tag groups; ellipsis to expand
- Line-level quick edit — hover ✏️/➕ icons on list items for
  inline edit and insert-below without leaving the browser; Escape
  or click-away to cancel
- Configurable poll interval (default 3s) for external changes
- Edit button per note (launches neovim via server)
- New note and Journal buttons
- Gear icon to edit config.yaml in your editor
- Live theme reloading — edit config.yaml, save, see changes

**Non-features (by design):**
- No full browser-based editing — line edits for list items only;
  everything else via neovim
- No TiddlyWiki wikitext rendering — markdown only
- No plugin system, no syncer, no complex widget tree

## Theme playground

`playground/index.html` is a standalone page for tweaking theme
values with immediate visual feedback. No server integration —
just open it in a browser (or serve it alongside the wiki).

- Left panel: color pickers + text inputs for all theme variables
- Right panel: pixel-perfect mock of the real wiki UI (rendered
  DOM captured from a live instance)
- **Copy YAML** button outputs a ready-to-paste `theme:` block
- Falls back to built-in defaults when opened standalone. To seed
  from an existing wiki theme, symlink `playground/` into a wiki
  directory and browse to `/{wiki}/playground/index.html`

## Configuration

All defaults live in `internal/config/defaults.yaml` (embedded in the
binary). On first run, this file is copied to `{wiki_path}/config.yaml`
so you have a commented config to edit. Priority: embedded defaults <
config file < CLI flags.

### CLI flags

| Flag | Description |
|------|-------------|
| `--config` | Path to YAML config file |
| `--wiki-path` | Path to wiki folder containing subdirs |
| `--listen` | Host:port to listen on |
| `--editor-cmd` | Editor command (`%s` = file path) |
| `--index-html` | Override embedded HTML (for development) |
| `--poll-interval` | Browser poll interval |
| `--cache-size` | LRU body cache size per wiki |

### config.yaml

```yaml
# These settings require a server restart:
wiki_path: .
listen: 127.0.0.1:8080
editor_cmd: "neovide -- %s"
poll_interval: 3s
cache_size: 200

# Theme applies live on save:
theme:
  bg:           "#1a1a2e"       # Page background
  fg:           "#e0e0e0"       # Main text color
  accent:       "#6cacee"       # Links, active tabs, focus rings
  # ... 17 more keys (colors, typography, layout)
```

CLI flags override config file values.

## API endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | Wiki selector (list of wikis) |
| GET | `/{wiki}/` | Serve browser client HTML |
| GET | `/{wiki}/status` | Server status JSON |
| GET | `/{wiki}/notes` | Skinny note list (with theme/poll headers) |
| GET | `/{wiki}/notes/{title}` | Full note JSON (with rendered HTML) |
| PUT | `/{wiki}/notes/{title}` | Create/save note |
| DELETE | `/{wiki}/notes/{title}` | Delete note |
| POST | `/{wiki}/line-edit/{title}` | Replace or insert a body line |
| GET | `/{wiki}/search?q=...` | Full-text search (JSON) |
| GET | `/{wiki}/theme.css` | Theme CSS (custom properties + syntax) |
| POST | `/{wiki}/edit-external/{title}` | Launch external editor |
| POST | `/{wiki}/edit-config` | Launch editor on config.yaml |
| GET | `/{wiki}/revisions/{title}` | Revision history + diffs (JSON) |
| POST | `/{wiki}/revisions/{title}/checkout?rev=1.3` | Checkout revision (or head if no rev) |
| POST | `/{wiki}/revisions/{title}/promote` | Promote checked-out revision as new head |

## Editing notes

Two ways:

1. **Direct file edit** — open any `.md` file in your editor, save,
   the server picks up changes via fsnotify within 3 seconds.
2. **Browser button** — click "edit" in a note's toolbar to launch
   neovim via the server's edit-external endpoint.

## RCS version control

Every note write creates/updates an RCS archive in an `RCS/`
subdirectory alongside the notes. View history with:

```bash
rlog notes/MyNote.md
rcsdiff notes/MyNote.md
co -r1.2 -p notes/MyNote.md
```

Requires `rcs` (`brew install rcs` or `apt install rcs`).

## Search

Full-text search uses ripgrep (`rg`) as the backend, searching `.md`
files in the notes directory. Requires `rg` on PATH (`brew install
ripgrep` or `apt install ripgrep`).

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
  wiki/     — Wiki type: goroutine event loop, Hub
  config/   — YAML config + CLI flag parsing (embedded defaults)
  tid/      — .md file parser/serializer (RFC822 headers + body)
  store/    — in-memory note store with revision tracking
  watcher/  — fsnotify file watcher with debounce
  server/   — thin HTTP adapter delegating to wiki via Hub
  editor/   — external editor launcher
  rcs/      — RCS version control integration
  loader/   — startup file loader
  render/   — goldmark + chroma markdown→HTML, ThemeCSS generation
  search/   — query parser + rg wrapper for full-text search
main.go     — wiring: config→Wiki→loader→watcher→Hub→Server
exp/        — browser client experiments (001–027)
playground/ — theme playground (standalone HTML)
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

### Migrating from TiddlyWiki

`scripts/tw2md` converts TiddlyWiki `.tid` files to TibbitWiki `.md`
format — timestamps, headers, wikitext→markdown, and journal
filename conversion (e.g. "10th July 2023" → "2023-07-10").

### Migrating from Evernote

`scripts/enex2tid` converts Evernote ENEX exports to `.md` notes
with attachments. See [SETUP.md](SETUP.md) for details.

## Known Limitations

- **macOS file watching uses kqueue**, which opens a file descriptor per
  file. Wikis with thousands of notes may hit the per-process fd limit
  (default ~10K). Raise it with `ulimit -n 65536` if needed. Linux
  uses inotify and has no such limit.
- **Dangling symlinks** in a wiki directory are moved to
  `lost+found/` at startup with a log warning. This prevents macOS
  kqueue (which must open every file) from blocking the file watcher.
- **Single-user design.** No authentication, no concurrent-writer
  conflict resolution. Intended for one person on localhost.
- **Search requires ripgrep** (`rg`) on PATH.
- **macOS ships bash 3.2.** The helper scripts (`vide`, `tw-start`,
  `rotate-log`) require bash 4.3+ features (`local -n`). Install a
  modern bash via `brew install bash` and ensure it appears first on
  PATH.

## How this was built

TibbitWiki was designed and directed by a human; all code, tests, and
documentation were written by GitHub Copilot (Claude) through
interactive conversation. Every commit carries a `Co-authored-by:
Copilot` trailer. The human contribution was architecture, design
decisions, domain knowledge, and relentless quality standards. The
AI's contribution was typing really fast and getting told to try again.
