# TibbitWiki

A Go replacement for TiddlyWiki's Node.js server, with a minimal
browser client, filesystem watching, and Neovide integration.

## Architecture

```
internal/
  wiki/     — Wiki type: goroutine event loop, domain logic, Hub
  config/   — YAML config + CLI flag parsing
  tid/      — .md file parser/serializer (RFC822 headers + body)
  store/    — in-memory note store with revision tracking
  watcher/  — fsnotify file watcher with debounce, callback API
  server/   — thin HTTP adapter delegating to wiki via Hub
  editor/   — external editor launcher
  rcs/      — RCS version control integration
  loader/   — startup file loader
  render/   — goldmark + chroma markdown→HTML, plain render mode
  search/   — query parser + rg wrapper
main.go     — wiring: Wiki→loader→watcher→Hub→Server
exp/        — browser client experiments (001–027)
playground/ — theme playground (standalone HTML)
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
- **No backwards compatibility obligation** — free to innovate and
  break any existing API, file format, or config until a full
  release. All content will be converted forward.

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

Single-wiki: if `--wiki-path` contains .md files directly (no
subdirs with .md), treat it as a single wiki named "default" and
serve at `/` (current URL scheme unchanged).

Multi-wiki: subdirs auto-discovered, served at `/{name}/notes/...`
etc. Root `/` shows wiki selector or redirects to a default.

### URL routing

| URL pattern | Behaviour |
|-------------|-----------|
| `/` | Wiki selector (list of links) |
| `/{wiki}/` | Serve browser client HTML |
| `/{wiki}/notes` | Skinny note list |
| `/{wiki}/notes/{title}` | Get/put/delete note |
| `/{wiki}/line-edit/{title}` | In-place line edit |
| `/{wiki}/edit-external/{title}` | Launch editor |
| `/{wiki}/search?q=...` | Full-text search |
| `/{wiki}/theme.css` | Syntax highlighting CSS |
| `/{wiki}/status` | Server status JSON |

### Phase 1: Wiki type + multi-wiki routing

- [x] New package `internal/wiki/wiki.go`
  - Wiki struct with goroutine event loop (ops channel)
  - All domain logic: Get, Put, Delete, List, LineEdit, Search,
    EditExternal, Reload, persistNote, timestamps, atime
  - Hub maps wiki names to Wiki instances
- [x] Refactor watcher to callback API `New(dir, func(string))`
  - Wiki.Reload is the callback — fire-and-forget into event loop
  - Watcher does debouncing; wiki goroutine does parse + store + RCS
- [x] Refactor server to thin HTTP adapter
  - `New(hub *wiki.Hub, indexHTML string)`
  - All handlers delegate to `s.wiki()` via Hub
- [x] Update main.go wiring: Wiki→loader→watcher→Hub→Server
- [x] Tests: 100% coverage on wiki, server, watcher packages
- [x] Config changes: removed legacy `tiddlers` subdir, WikiPath
  is the parent dir containing wiki subdirs
- [x] Multi-wiki hub routing
  - `ServeHTTP` extracts wiki prefix, injects Wiki into context
  - `splitPrefix` splits `/{wiki}/rest` into name + remaining path
  - `handleWikiSelector` at `/` lists wikis as links
  - No single-wiki special case — one subdir = one wiki
  - `main.go` scans WikiPath for subdirs with .md files

### Phase 2: LRU cache

- [x] Add `github.com/hashicorp/golang-lru/v2` dependency
- [x] Split store into metadata index + body cache
  - Store.Put/PutWithPath strip "text" field (metadata-only)
  - Wiki.fetchBody: check LRU → miss → read .md from disk → cache
  - Wiki.SeedBody for loader/test seeding
- [x] Loader seeds body cache via BodySeeder interface
- [x] Watcher invalidates cache via metaFieldsEqual + body check
- [x] Config: `--cache-size N` (default 200)
- [x] Tests: cache hit/miss/eviction, read error, parse error,
  metadata-only listing, disk fallback. 100% coverage.

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

- [x] Multi-wiki URL awareness
  - Client detects wiki prefix from `window.location.pathname`
  - All fetch calls prepend `PREFIX` (`/{wiki}`)
  - Wiki name shown in document title
  - theme.css href patched via JS
- [x] Wiki selector page (server-side: list of wiki names as links)

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

---

## v5: Theming + Config

### Problem

- Browser client has 14+ hardcoded hex color values — no way to
  theme without editing HTML
- No config.yaml auto-discovery — requires `--config` flag
- Bootstrap Icons loaded from CDN — breaks offline use

### Approach

`config.yaml` with a `theme:` section drives CSS custom properties.
Server generates `theme.css` from the theme map + chroma syntax CSS.
Config file watcher detects saves, does RCS checkin, regenerates CSS.
Client uses `var(--tw-*)` everywhere. Gear icon opens config in neovide.
Poll-based theme revision avoids extra fetches.

### Completed

- [x] `Theme map[string]string` in Config with 20 semantic keys
  (12 colors + 4 typography + 2 layout + 2 limits)
- [x] Embedded `defaults.yaml` — single source of truth for all defaults
- [x] Config loading: embedded defaults < config file < CLI flags
- [x] `render.GenerateThemeCSS()` — `:root { --tw-*: ...; }` + chroma
- [x] `render.WriteThemeFile()` — writes CSS to disk
- [x] Server serves theme.css via `http.ServeFile`
- [x] Client: all hardcoded values → `var(--tw-*)` CSS custom properties
- [x] Config file watcher (fsnotify, debounce, RCS checkin)
- [x] Theme revision via `X-Theme-Rev` header on `/notes`
- [x] Client polls revision, fetches theme.css only on change
- [x] `POST /edit-config` endpoint + gear icon in client
- [x] Auto-create config.yaml from embedded defaults if absent
- [x] Auto-load config.yaml from wiki path (no `--config` needed)
- [x] `X-Poll-Interval` header — poll interval from config
- [x] Theme playground (`playground/index.html`) — standalone page
  with color pickers, font inputs, live preview using real wiki DOM
- [x] Tests: config theme parsing, CSS generation, WriteThemeFile,
  edit-config handler (all branches), 100% render coverage

### Remaining

- [ ] Vendor Bootstrap Icons (go:embed, remove CDN)

### Future: Embed + Restart + Gear Dropdown

- [ ] `go:embed` index.html into binary — zero-config startup
  (`--index-html` becomes optional dev override)
- [ ] `POST /restart` — `server.Shutdown()` then `syscall.Exec(self)`
- [ ] Gear icon becomes dropdown: "Edit config" + "Restart server"
- [ ] All config settings changeable without terminal access
