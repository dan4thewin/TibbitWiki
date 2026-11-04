# TibbitWiki

A Go replacement for TiddlyWiki's Node.js server with a minimal
browser client, filesystem watching, external editor integration,
and RCS version control.

## Features

- **Minimal browser client** — single HTML+JS file, dark theme, no
  TiddlyWiki browser code. Server-side markdown rendering with CamelCase
  WikiWord auto-linking, syntax highlighting, tables, full-text search,
  tag pills, tabbed sidebar, and line-level quick edit.
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
- **Attachment listings** — notes with a subdirectory show a collapsible
  list of files; configurable suffix exclusions via `attachment_exclude`.
  Markdown (`.md`) attachments are rendered through goldmark (tables,
  syntax highlighting, linkify — but no raw HTML pass-through).
- **PDF auto-conversion** — optionally enqueue unconverted PDFs for
  background conversion to searchable `.pdf.md` shadow files via
  `pdf_convert_cmd`. Pipeline: `pdftotext` extracts raw text, ollama
  cleans it into markdown. Image-only PDFs get a metadata stub.
- **Simple configuration** — YAML config file with CLI flag overrides;
  auto-created on first run with commented defaults

## Quick start

The server supports macOS and Linux (including WSL). You need Git,
Go matching the version in `go.mod`, and ripgrep (`rg`) for search.
No external RCS installation is required.

```bash
# Get the source (no GitHub account or SSH key required)
git clone https://github.com/dan4thewin/TibbitWiki.git tibbitwiki
cd tibbitwiki

# Build
go build -o tibbitwiki .

# Run (creates ~/TibbitWiki/demo and config.yaml on first use)
./tibbitwiki
```

Then open http://127.0.0.1:8080 in your browser.
To use an existing notes directory, run
`./tibbitwiki --wiki-path ~/notes` instead.

**Using it across machines or on your phone?** Run it over Tailscale
rather than exposing it on the open network — see [Access across
devices](#access-across-devices-tailscale), the recommended setup.

The server auto-discovers wiki subdirectories under `--wiki-path`.
Each subdirectory with `.md` files becomes a wiki at `/{name}/`.
Wikis are flat — only `.md` files directly in the subdirectory are
loaded as notes (no nested directories).

See [SETUP.md](SETUP.md) for the full setup guide: prerequisites,
editor integration, PDF conversion, theme playground, and Evernote
migration. Copy `TibbitWiki.md` and `QuickRef.md` from the repo into
your first wiki directory for a welcome note and markdown reference.

## Access across devices (Tailscale)

TibbitWiki has no built-in authentication or TLS, so it should never be
bound to `0.0.0.0` or exposed directly on the LAN or internet. The
recommended way to use it across machines and phones is over a
[Tailscale](https://tailscale.com) tailnet: traffic is wrapped in
WireGuard encryption and only the devices you have enrolled can reach it
— covering the transport encryption and (device-level) authentication
the server itself doesn't provide.

Bind the server to both localhost and the host's Tailscale IP with the
`listen` array in `config.yaml`, so it stays off the wider LAN while
remaining reachable inside the encrypted tailnet:

```yaml
wiki_path: .
listen:
  - "127.0.0.1:8080"     # local access on the host
  - "100.x.y.z:8080"     # the host's Tailscale IP
```

Then open the wiki from any device on the tailnet — laptop, desktop, or
mobile browser — at `http://100.x.y.z:8080/`, or the MagicDNS hostname
(e.g. `http://myhost:8080/`). The client is touch-friendly on phones:
single-column layout, icon-only toolbar, and a per-line ✎ handle to edit.

Notes:
- Don't run `tailscale serve` on port 8080 — it also binds the Tailscale
  IP there and collides with the direct bind (`tailscale serve reset`
  clears it).
- If device policy prevents installing native Tailscale on Windows,
  a localhost→tailnet proxy inside WSL can provide access — e.g.
  `systemd-socket-proxyd` forwarding `127.0.0.1:8080` to
  `<host>.<tailnet>.ts.net:8080`. Windows itself supports native Tailscale.
- `--listen` overrides the whole list with a single address; use the
  config array for multi-bind.

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
- Tables via goldmark table extension
- CamelCase WikiWord auto-linking (blue for existing, red for missing)
- Full-text search — `/` opens vi-style search bar; results appear as
  a pseudo-note with clickable titles and matched lines
- Tag pills — colored pills on notes, click to search by tag
- Tabbed sidebar — Recent (non-journals), Journals, Tags with
  per-tag groups; ellipsis to expand
- Line-level quick edit — Shift-click a line to edit it in place (on touch,
  tap the ✎ edit handle in the margin); a per-line insert handle adds a
  line below; Escape or click-away to cancel
- Configurable poll interval (default 3s) for external changes
- Edit button per note (configured or auto-detected external editor;
  browser editor on touch or when no external editor is available)
- Full-body browser editing — Shift-click the pencil on desktop,
  or tap it on touch; the toolbar becomes Save/Cancel
- New note and Journal buttons
- Gear icon to edit config.yaml in your editor
- Live theme reloading — edit config.yaml, save, see changes

**Non-features (by design):**
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
| `--listen` | Host:port to listen on (overrides all config addresses) |
| `--editor-cmd` | Editor command (`%s` = file path) |
| `--index-html` | Override embedded HTML (for development) |
| `--poll-interval` | Browser poll interval |
| `--cache-size` | LRU body cache size per wiki |

### Attachment listings

Notes with a subdirectory of the same name automatically show a
collapsible **Attachments** section. Files in that subdirectory are
listed as clickable links. Markdown (`.md`) files are rendered through
goldmark with tables and syntax highlighting (raw HTML is stripped for
safety). Use `attachment_exclude` in config.yaml to hide files by
suffix (e.g. `[".pdf.md"]` to hide PDF shadow files).

### PDF conversion

When `pdf_convert_cmd` is set, the server auto-enqueues unconverted
PDFs whenever a note's attachments are listed. The pipeline:

1. Server detects a `.pdf` with no sibling `.pdf.md` → exec
   `pdf_convert_cmd <pdf_path> <queue_dir>`
2. `scripts/pdf2md-enqueue` symlinks the PDF into a queue directory
   and starts the queue runner (flock ensures one runner at a time)
3. `scripts/pdf2md` extracts text via `pdftotext`, sends it to
   ollama for markdown cleanup, writes a `.pdf.md` shadow file
4. Image-only PDFs (no extractable text) get a metadata stub
   with page count, image summary, and file size

Shadow files are indexed by ripgrep search. Hide them from the
attachment listing with `attachment_exclude: [".pdf.md"]`.

### config.yaml

```yaml
# These settings require a server restart:
wiki_path: ~/TibbitWiki
listen: 127.0.0.1:8080             # or array: ["addr1:port", "addr2:port"]
editor_cmd: ""                    # auto-detect system file opener
poll_interval: 3s
cache_size: 200
max_diffs: 10
attachment_exclude: []       # suffixes to hide, e.g. [".pdf.md"]
pdf_convert_cmd: ""         # template: %s=path %n=wiki %d=dir

# Theme applies live on save:
theme:
  shared:
    bg:           "#1a1a2e"     # Page background
    fg:           "#e0e0e0"     # Main text color
    accent:       "#6cacee"     # Links, active tabs, focus rings
    # ... see config.yaml for all keys
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

## Note format

Notes are `.md` files with RFC 822-style headers, a blank line, and a
markdown body:

```
title: My Note
tags: code example
tab-size: 8

# Heading

Body text with **markdown**.
```

Standard headers: `title`, `tags`, `created`, `modified`.

Any custom header is preserved through the API as a top-level JSON
field. The client currently recognises one custom header:

| Header | Effect |
|--------|--------|
| `tab-size` | Override the theme default (`code_tab_size`) for code blocks in this note. Useful for notes containing Go or Makefile snippets that use 8-space tabs. |

## Editing notes

Three ways:

1. **Direct file edit** — open any `.md` file in your editor, save,
   the server picks up changes via fsnotify within 3 seconds.
2. **Toolbar pencil** — opens the configured or auto-detected external
   editor. Shift-click it on desktop, or tap it on touch, to edit the
   entire body in the browser. The pencil becomes Save and X becomes
   Cancel while editing.
3. **Quick line edit** — Shift-click a body line on desktop, or tap
   its margin pencil on touch. Ordinary taps and clicks still follow
   links and toggle checkboxes.

## RCS version control

Every note write creates/updates an RCS archive in an `RCS/`
subdirectory alongside the notes. This uses a built-in Go library —
no external `rcs` install is required.

If you have `rcs` installed (`brew install rcs` or `apt install rcs`),
you can inspect archives manually:

```bash
rlog notes/MyNote.md
rcsdiff notes/MyNote.md
co -r1.2 -p notes/MyNote.md
```

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
| `a\|b\|c` | any term (OR, no spaces) |
| `a b` | AND (space = set intersection) |

Space separates tokens — no quoting. To match a literal space in a
regex use `\s` or `\x20` (e.g. `/hello\sworld/`).

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
  render/   — goldmark + chroma markdown→HTML, attachment rendering, ThemeCSS
  search/   — query parser + rg wrapper for full-text search
main.go     — wiring: config→Wiki→loader→watcher→Hub→Server
exp/        — browser client experiments (001–029)
playground/ — theme playground (standalone HTML)
```

No database. All state is in-memory + filesystem.

## Development

```bash
go tool mk build      # go build ./...
go tool mk test       # go test with inlining disabled
go tool mk lint       # go vet + gofmt check
go tool mk coverage   # statement coverage report
go tool mk complexity # gocyclo top 20
go tool mk check      # lint + test + complexity
```

Build tool: [mk](https://github.com/sgrankin/mk) (Plan 9 mk in Go).
Pinned as a tool dependency in `go.mod` — use `go tool mk` or
install to PATH with `go install github.com/sgrankin/mk@latest`.
The `complexity` and `check` targets also need `gocyclo` on PATH
(`go install github.com/fzipp/gocyclo/cmd/gocyclo@latest`).

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
- **Single-user design.** No built-in authentication or TLS, and no
  concurrent-writer conflict resolution — it is built for a single
  writer. For access beyond the host (other machines, phones), run it
  over Tailscale (see [Access across
  devices](#access-across-devices-tailscale)) for encrypted transport
  and tailnet-scoped access rather than exposing it on the LAN.
- **Search requires ripgrep** (`rg`) on PATH.
- **PDF conversion requires** `pdftotext` (poppler) and optionally
  [ollama](https://ollama.com) with a local model for markdown cleanup.
  Without ollama, raw extracted text is used as-is.
- **macOS ships bash 3.2.** The helper scripts (`vide`, `tw-start`,
  `rotate-log`) require bash 4.3+ features (`local -n`). Install a
  modern bash via `brew install bash` and ensure it appears first on
  PATH.

## How this was built

TibbitWiki was designed and directed by a human; all code, tests, and
documentation were written using GitHub Copilot with multiple AI models
through interactive conversation. AI-assisted commits carry a
`Co-authored-by: Copilot` trailer. The human contribution was architecture,
design decisions, domain knowledge, and relentless quality standards. The
AI's contribution was typing really fast and getting told to try again.

The historical experiments include Marked.js under its
[upstream license](exp/marked.LICENSE.md); it is not part of the embedded client.
