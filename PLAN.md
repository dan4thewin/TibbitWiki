# TibbitWiki

A Go replacement for TiddlyWiki's Node.js server, with a minimal
browser client, filesystem watching, and Neovide integration.

## Architecture

```
internal/
  config/   — YAML config + CLI flag parsing
  tid/      — .md file parser/serializer (RFC822 headers + body)
  store/    — in-memory note store with revision tracking
  watcher/  — fsnotify file watcher with debounce
  server/   — HTTP server implementing REST API
  editor/   — external editor launcher
  rcs/      — RCS version control integration
  loader/   — startup file loader
  render/   — goldmark + chroma markdown→HTML, plain render mode
  search/   — query parser + rg wrapper
main.go     — wiring
exp/        — browser client experiments (001–025)
```

### REST API

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | Serve browser client HTML |
| GET | `/status` | Server status JSON |
| GET | `/notes` | Skinny note list (poll) |
| GET | `/notes/{title}` | Full note JSON (?render=plain) |
| PUT | `/notes/{title}` | Create/save note |
| DELETE | `/notes/{title}` | Delete note |
| POST | `/line-edit/{title}` | In-place line edit/insert |
| POST | `/edit-external/{title}` | Launch external editor |
| GET | `/search?q=...` | Full-text search (rg) |
| GET | `/theme.css` | Syntax highlighting CSS |

### Design decisions

- **No database** — all state in-memory + filesystem
- **.md files** — RFC822-style headers + blank line + markdown body
- **Server-side rendering** — goldmark + chroma, no CDN deps
- **rg for search** — ripgrep on disk, no index maintenance
- **Polling over push** — 3s interval, trivial for single user
- **RCS versioning** — `ci -l` on every write, `RCS/` subdir
- **ISO 8601 timestamps** — created/modified in file,
  accessed via filesystem atime (touch -a on GET)

## Completed

- **v1:** Browser client experiments (001–012)
- **v2:** Server-side rendering, rg search, tag pills (013–018)
- **v3:** Bootstrap Icons, tag add/remove, line-editing (lists,
  blockquotes, code blocks), ISO timestamps, atime-based recent
  sort, .tid→.md extension (019–025). 100% statement coverage.

---

## v4: Multi-Wiki + LRU Cache

### Problem

- All-in-memory store doesn't scale for large collections
  (target: Raspberry Pi Zero W, 512MB RAM)
- Single wiki only — want multiple wikis under one server
- Symlinked notes between wikis need correct RCS + cache behaviour

### Approach

Introduce a **Wiki** type: one goroutine per wiki with a
channel-based event loop. The wiki owns its store, watcher, and
note directory. Symlinked notes delegate to the owning wiki's
goroutine. LRU cache keeps memory bounded.

### Directory layout

```
/path/to/wikis/           ← --wiki-path
  work/
    *.md
    RCS/
  personal/
    *.md
    RCS/
  evernote/
    Meeting.md → ../work/Meeting.md   (symlink)
```

Single-wiki backwards compat: if `--wiki-path` contains .md files
directly (no subdirs with .md), treat it as a single wiki named
"default" and serve at `/` (current URL scheme unchanged).

Multi-wiki: subdirs auto-discovered, served at `/{name}/notes/...`
etc. Root `/` shows wiki selector or redirects to a default.

### URL routing

| Mode | URL pattern | Behaviour |
|------|-------------|-----------|
| Single | `/notes/{title}` | Current API unchanged |
| Multi | `/{wiki}/notes/{title}` | Prefix selects wiki |
| Multi | `/` | Wiki selector page |

### Phase 1: Wiki type + multi-wiki routing

- [ ] New package `internal/wiki/wiki.go`
  - `Wiki` struct: name, noteDir, store, watcher ref
  - Goroutine event loop with typed request/response channels
  - Operations: Get, Put, Delete, List, LineEdit, Search,
    EditExternal, Reload (from watcher)
  - Request types carry a response channel for the caller to block on
- [ ] Refactor watcher to use callback instead of direct store ref
  - `onChange func(path string, deleted bool)` parameter
  - Wiki registers a callback that sends to its event channel
  - Watcher still does debouncing; wiki goroutine does
    parse + store update + RCS checkin
- [ ] Refactor server to route through Wiki
  - `Hub` struct: `map[string]*Wiki`, resolves wiki from URL prefix
  - Single-wiki mode: one wiki named "default", no prefix
  - Multi-wiki mode: prefix extraction middleware
  - Server handlers call `hub.Wiki(name).Get(title)` etc.
- [ ] Config changes
  - `wiki-path` points to parent dir (multi) or note dir (single)
  - Auto-detect mode: scan for subdirs containing .md files
- [ ] Update main.go wiring
  - Scan wiki-path, create Wiki per subdir (or one for single mode)
  - Start each wiki's goroutine + watcher
  - Create hub, pass to server
- [ ] Tests: wiki event loop, hub routing, single/multi mode detection

### Phase 2: LRU cache

- [ ] Add `github.com/hashicorp/golang-lru/v2` dependency
- [ ] Split store into metadata index + body cache
  - Metadata (`title`, `tags`, `created`, `modified`, `type`,
    `revision`, `filePath`): always in memory, ~200 bytes/note
  - Full note (with `text`): LRU cache, configurable size
    (default 200 entries, flag `--cache-size`)
  - `Get()`: check LRU → miss → read .md from disk → parse → cache
  - `Put()`: write-through (update cache + disk, like today)
  - `Delete()`: evict from cache + remove metadata
  - `All()` / listing: served from metadata index (no body needed)
- [ ] Loader becomes optional warm-up
  - Always load metadata; body loaded into LRU up to capacity
  - Could skip body for huge wikis (lazy load on demand)
- [ ] Watcher invalidates cache entry (not full reload)
  - On file change: evict from LRU, update metadata from headers
  - Next Get() re-reads full note from disk
- [ ] Config: `--cache-size N` (default 200)
- [ ] Tests: cache hit/miss/eviction, metadata-only listing,
  disk fallback, warm-up vs lazy load

### Phase 3: Symlink delegation

- [ ] At load time, detect symlinks with `os.Lstat` + `os.Readlink`
- [ ] Resolve target path → identify owning wiki
  - `hub.WikiForPath(absPath)` matches against wiki noteDirs
- [ ] Wiki maintains `symlinks map[title]targetWiki` redirect table
- [ ] On Get/Put/Delete of a symlinked title: forward request to
  target wiki's channel, relay response back to caller
- [ ] Listing includes symlinked titles (marked as symlink in metadata)
- [ ] RCS: handled by owning wiki (correct by construction)
- [ ] Watcher: owning wiki watches the real file; source wiki
  doesn't watch the symlink (delegates on access)
- [ ] Edge case: symlink target outside any wiki → skip with warning
- [ ] Tests: symlink detection, delegation round-trip, cross-wiki
  Get/Put, broken symlink handling

### Phase 4: Browser client updates

- [ ] Multi-wiki URL awareness
  - Client detects wiki prefix from `window.location.pathname`
  - API calls prepend wiki prefix
  - Wiki name shown in header/title
- [ ] Wiki selector page (simple: list of wiki names as links)

### Design notes

**Why goroutine per wiki (not shared mutex)?**
- Symlink delegation = "send message to another wiki's channel"
  — natural in the goroutine model, awkward with shared locks
- No possibility of deadlock between wikis
- Watcher events and HTTP requests serialized — no races
- Single user, 3s poll — zero contention concern

**Why not a database?**
- rg already handles search (fast, no index maintenance)
- .md files are the source of truth (editable with nvim, git-friendly)
- RCS provides version history on the filesystem
- Adding SQLite would mean syncing two sources of truth

**Cache sizing for Pi Zero W:**
- 50K notes × 200B metadata = 10MB (always in memory)
- 200 cached full notes × 5KB avg = 1MB (LRU)
- Total: ~11MB — trivial even on 512MB
