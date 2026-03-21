# TibbitWiki

A Go replacement for TiddlyWiki's Node.js server, with a minimal
browser client, filesystem watching, and Neovide integration.

## Problem

TiddlyWiki's Node.js server has no filesystem watching — edits made
outside the browser are invisible until restart. The browser client
is deeply interconnected (~4.5MB of JS), making it hard to debug or
extend. The syncer assumes the browser is authoritative, overwriting
external edits with stale baked-in content.

## Approach

Reimplement the TiddlyWeb REST API (7 endpoints) in Go. Add
fsnotify-based file watching. Replace TiddlyWiki's browser client
entirely with a ~6KB HTML+JS viewer that uses markdown + CamelCase
WikiWord links. The server is the single source of truth; the
browser is view-only, polling every 3 seconds.

## Architecture

```
┌──────────────┐     HTTP (3s poll)     ┌──────────────┐
│   Browser    │ ◄──────────────────► │  TibbitWiki   │
│  (view-only) │                        │   server     │
└──────────────┘                        └──────┬───────┘
                                               │
                               ┌───────────────┼───────────────┐
                               │               │               │
                          fsnotify        .tid files      editor launch
                          watcher      ~/notes/   (neovide)
                                        tiddlers/
```

### Package layout

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
  render/   — goldmark + chroma markdown→HTML rendering, ThemeCSS()
  search/   — query parser + rg wrapper, returns file matches with matched lines
main.go     — wiring
exp/        — browser client experiments (001–018)
```

### REST API (TiddlyWeb compatible)

| Method   | Path                                | Purpose                    |
|----------|-------------------------------------|----------------------------|
| `GET`    | `/`                                 | Serve browser client HTML  |
| `GET`    | `/status`                           | Server status + user info  |
| `GET`    | `/recipes/default/tiddlers.json`    | Skinny tiddler list (poll) |
| `GET`    | `/recipes/default/tiddlers/:title`  | Full tiddler as JSON       |
| `PUT`    | `/recipes/default/tiddlers/:title`  | Create/save tiddler        |
| `DELETE` | `/bags/default/tiddlers/:title`     | Delete tiddler             |
| `GET`    | `/search?q=...`                     | Full-text search (JSON)    |
| `GET`    | `/theme.css`                        | Syntax highlighting CSS    |
| `GET`    | `/favicon.ico`                      | Favicon                    |
| `POST`   | `/edit-external/:title`             | Launch external editor     |

### Browser client

Single HTML file (`exp/017-tag-pills/index.html`).
No build step, no TiddlyWiki code.

- **Rendering:** server-side markdown via goldmark, syntax highlighting
  via chroma (OneDark theme served as `/theme.css`), regex for CamelCase
  WikiWord auto-linking
- **Navigation:** story river (new content appended at bottom), recent
  sidebar (top 20 by modified date), WikiWord click-to-open
- **Search:** `/` opens vi-style search bar; results appear as a
  pseudo-tiddler in the story river with clickable titles and matched
  lines. Rich query syntax (literal, NOT, tag, title, regex, OR, AND).
- **Tag pills:** colored pills on tiddlers, click to search by tag
- **Polling:** GET tiddlers.json every 3s, compare revisions, re-fetch
  changed tiddlers
- **Actions:** edit (POST /edit-external), close, + New (prompt for
  title, PUT), + Journal (auto date title, PUT + edit)
- **Theme:** dark (navy background, styled cards/sidebar/code blocks)

### File watching

- fsnotify on the tiddlers directory
- Debounce for cloud drive quirks (OneDrive temp files, atomic
  renames, rapid successive events)
- On change: re-read `.tid` file, update in-memory store, bump
  revision counter
- Browser's 3-second poll picks up new revision naturally

### RCS versioning

Every write creates/updates an RCS archive in `RCS/` subdirectory
(not inside `tiddlers/` — avoids watcher treating archives as
tiddlers). Requires `rcs` command.

### Editor integration

Command template with `%s` placeholder (e.g. `neovide -- %s`).
Template is split into args first, then `%s` is substituted — this
keeps file paths with spaces as a single argument.

## Design decisions

- **No database** — all state in-memory + filesystem
- **No TiddlyWiki browser code** — the syncer's "browser is
  authoritative" assumption is fundamentally incompatible with
  server-as-truth external editing
- **Markdown over wikitext** — simpler, widely known, no lock-in
- **Server-side rendering** — goldmark + chroma on the server avoids
  CDN dependencies and keeps the browser client minimal
- **rg for search** — ripgrep searches .tid files directly on disk;
  no in-memory index to build or maintain, handles large wikis easily
- **Polling over push** — 3s interval is trivial load for single
  user; no WebSocket complexity
- **View-only browser** — all editing via neovim; no textarea,
  no contenteditable, no conflict resolution needed
- **Config over constants** — listen address, wiki path, editor
  command, poll interval all configurable

## Out of scope (v2)

- Browser-based editing (textarea)
- Hugo integration
- Auto-conversion of wikitext to markdown
- Multiple simultaneous browser clients
