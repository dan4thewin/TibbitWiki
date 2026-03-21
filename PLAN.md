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

## Out of scope

- Hugo integration
- Auto-conversion of wikitext to markdown
- Multiple simultaneous browser clients

---

## v3 Plan

### Feature 1: Neovide Wrapper Script

**Goal:** Reuse an existing Neovide instance instead of spawning a
new window for every edit.

**Approach:** Shell script used as `--editor-cmd`:
- Check if a Neovide process is running
- If yes: use `nvim --server` (Neovim's remote) to open the file
  in the existing instance (`--remote` or `nvim --server <addr>
  --remote <file>`)
- If no: launch `neovide -- <file>` normally
- Neovide connects to Neovim, so the server address is a socket
  (look for `$NVIM_LISTEN_ADDRESS` or a known socket path)

**Files:**
- `scripts/neovide-wrapper.sh` — the script
- Update SETUP.md with usage

**Complexity:** Low. One shell script, no Go changes.

---

### Feature 2: Sidebar Tabs

**Goal:** Three-tab sidebar replacing the current Recent list.

#### Tab 1: Recent (non-journals)
- Sorted by modified date (current behavior)
- Show first `--sidebar-limit` items (default 20)
- Ellipsis button at bottom expands to show ALL non-journal tibbits
- "Non-journal" = does NOT have #Journal tag AND title doesn't
  match date format (e.g. "21st March 2026")

#### Tab 2: Journals
- Sorted by modified date
- Show first `--sidebar-limit` items
- Ellipsis expands to all journals
- "Journal" = has #Journal tag OR title matches date format

#### Tab 3: Tags
- Each tag is a collapsible group, sorted by most-recent tibbit
  in that group
- Show first `--tag-limit` tibbits per tag (default 5)
- Ellipsis per tag expands that tag's full list
- Skip system tags ($:/ prefix)

#### Config (CSS custom properties)
```css
:root {
  --sidebar-limit: 20;
  --tag-limit: 5;
}
```
JS reads via `getComputedStyle`. Change one line, no recompile.

#### Naming
Rename "tiddlers" → "tibbits" throughout: UI text, Go types/vars,
API endpoint paths, docs. This is a breaking rename of the REST API.

#### Implementation
- Tab UI: three clickable tab headers at top of sidebar, one active
  panel visible at a time
- Journal detection: function in client JS that checks tag OR date
  regex (`/^\d{1,2}(?:st|nd|rd|th) \w+ \d{4}$/`)
- Server changes: tiddlers.json already returns tags — no new
  endpoint needed, client filters/groups locally
- Rename: `tiddlers` → `tibbits` across Go packages, API routes,
  client JS, docs (big but mechanical)

---

### Feature 3: Line-Level Quick Edit

**Goal:** Edit individual list items in-place without launching
neovide. Hover a list item → icons appear → click to edit or
insert below.

#### Render annotation
- Extend `internal/render/render.go` to walk the goldmark AST
- For each `ListItem` node, wrap the output `<li>` with
  `data-line="N"` where N is the source line number
- Only annotate list items (not paragraphs, headings, etc.)

#### Client interaction
- On hover over an `<li data-line>`, show two auto-hiding icons:
  - ✏️ **Edit** — replace the `<li>` content with a single-line
    `<input>` containing the raw source line. Enter = save,
    Escape = cancel.
  - ➕ **Insert below** — open an empty `<input>` below the
    current `<li>`. Enter = save new line, Escape = cancel.
- Icons are positioned to the left/right of the list item,
  appear on hover, hidden otherwise

#### Server endpoint
- `PATCH /tibbits/:title/line/:n` — edit line N of the .tid body
  - Body: `{"text": "new line content"}`
  - Server reads the file, replaces line N, writes back, bumps
    revision
- `POST /tibbits/:title/line/:n/insert` — insert after line N
  - Body: `{"text": "new line content"}`
  - Server reads, inserts after line N, writes back

#### Re-render
- After PATCH/POST succeeds, client re-fetches the full tibbit
  (same as existing `refreshTiddler`) — the whole tibbit body
  re-renders with correct markdown context (list numbering, etc.)

#### Scope limits
- List items only (for now)
- Paragraphs: skip or stretch goal (multi-line box, low priority)
- No quick-edit for headings, code blocks, blockquotes

---

### Experiment sequence

| # | Feature | Description |
|---|---------|-------------|
| 019 | Sidebar tabs | Recent (non-journal) + Journal tabs |
| 020 | Tags tab | Tag groups with expand |
| 021 | Rename | tiddlers → tibbits everywhere |
| 022 | Line annotation | data-line on `<li>` in rendered HTML |
| 023 | Line edit | Hover icons + inline edit input |
| 024 | Insert below | Add new list item inline |
| 025 | v3 integration test | Full automated test |

Neovide wrapper is standalone (scripts/), no experiment needed.

### Open questions

- Rename timing: do the big rename (exp-021) before or after
  line-edit? Before keeps the API consistent; after avoids
  merge pain if we're iterating. Suggest: before.
- PATCH endpoint: should it use line numbers (fragile if file
  changes between render and edit) or content matching? Line
  numbers are simpler; re-render on conflict handles staleness.
