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
  render/   — goldmark + chroma markdown→HTML, attachment rendering, ThemeCSS
  search/   — query parser + rg wrapper
main.go     — wiring: Wiki→loader→watcher→Hub→Server
exp/        — browser client experiments (001–029)
playground/ — theme playground (standalone HTML)
```

### REST API

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | Wiki selector (list of links) |
| GET | `/{wiki}/` | Serve browser client HTML |
| GET | `/{wiki}/status` | Server status JSON |
| GET | `/{wiki}/notes` | Skinny note list (poll) |
| GET | `/{wiki}/notes/{title}` | Full note JSON (?render=plain) |
| PUT | `/{wiki}/notes/{title}` | Create/save note |
| DELETE | `/{wiki}/notes/{title}` | Delete note |
| POST | `/{wiki}/line-edit/{title}` | In-place line edit/insert |
| POST | `/{wiki}/edit-external/{title}` | Launch external editor |
| POST | `/{wiki}/new-draft` | Create draft note |
| POST | `/{wiki}/edit-config` | Launch editor on config.yaml |
| POST | `/{wiki}/restart` | Graceful restart (re-exec) |
| GET | `/{wiki}/search?q=...` | Full-text search (rg) |
| GET | `/{wiki}/theme.css` | Generated CSS (theme + chroma) |
| GET | `/{wiki}/Title/file.ext` | Static attachment from subdir |
| GET | `/{wiki}/revisions/{title}` | Revision history + diffs (JSON) |
| POST | `/{wiki}/revisions/{title}/checkout` | Checkout revision (?rev=1.3) or head |
| POST | `/{wiki}/revisions/{title}/promote` | Promote old rev as new head |

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

Wikis are subdirectories of `--wiki-path`, auto-discovered at
startup. Each subdir with `.md` files becomes a wiki at `/{name}/`.
Root `/` shows the wiki selector.

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

- [x] At load time, detect symlinks with `os.Lstat` + `os.Readlink`
  - `loader.SymlinkOwner()` resolves relative symlinks to owner wiki name
  - `loader.Load()` returns symlinks map alongside note count
- [x] Wiki maintains `symlinks map[title]ownerWiki` for read-time delegation
  - Borrower's `List()` appends symlinked notes from owner wiki's store
  - Client sees `owner` field, routes all ops to `/{owner}/...`
  - No Get/Put/Delete delegation needed — client goes direct
- [x] Watcher: reloadFile detects new/deleted symlinks at runtime
  - Short-circuits known symlinks to avoid cross-wiki watcher noise
- [x] Edge case: symlink target outside any wiki → skip with warning
- [x] Tests: symlink detection, list delegation, missing owner,
  new symlink, deleted symlink, reloadFile short-circuit

### Phase 4: Browser client updates

- [x] Multi-wiki URL awareness
  - Client detects wiki prefix from `window.location.pathname`
  - All fetch calls prepend `PREFIX` (`/{wiki}`)
  - Wiki name shown in document title
  - Relative `theme.css` URL (no JS rewrite, no FOUC)
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

All v5 items complete.

### Completed (post-theming)

- [x] Vendor Bootstrap Icons — replaced CDN with inline SVGs (8 icons)
- [x] `go:embed` index.html into binary — zero-config startup
  (`--index-html` becomes optional dev override)
- [x] `POST /restart` — graceful `http.Server.Shutdown()` then
  `syscall.Exec(self)` to re-exec with same args
- [x] Gear icon becomes dropdown: "Edit config" + "Restart server"
  - Click-outside-to-close, restart shows toast, polls until back
- [x] Static file attachments from note subdirectories
  (`/{wiki}/Title/file.pdf`)
- [x] Raw HTML passthrough in goldmark (for `<embed>`, `<iframe>`)
- [x] Search covers all text files, maps subdir hits to parent note
- [x] Fix normalizeHitPath bug (top-level non-.md files dropped)
- [x] Fix theme CSS FOUC (relative URL, no JS rewrite)
- [x] `scripts/vide` — generic headless nvim + neovide launcher
- [x] `scripts/tw-start` — detached server launcher with logging

---

## v6: Per-Wiki Theming

### Problem

All wikis share one `theme.css`. No visual distinction between
personal, work, etc. Wiki title is hardcoded to "TibbitWiki".

### Approach

`config.yaml` theme section becomes nested: `shared` base plus
optional per-wiki overrides. Server merges shared + wiki-specific,
writes `{wiki_path}/{wiki}.css` for each wiki. Client reads
`--tw-wiki-title` from CSS custom property.

### Config structure

```yaml
theme:
  shared:
    bg: "#1a1a2e"
    wiki_title: "TibbitWiki"
    # ... all 24 keys
  personal:
    accent: "#ff6b6b"
    wiki_title: "Personal Notes"
  work:
    accent: "#4ecdc4"
    wiki_title: "Work"
```

No backward compatibility — flat `theme:` is gone.

### File layout

```
~/notes/                 ← wiki_path
  config.yaml
  personal.css           ← merged shared + personal overrides
  work.css               ← merged shared (no overrides)
  personal/*.md
  work/*.md
```

CSS files at wiki_path level — invisible to rg (runs inside wiki subdirs).

### Todos

- [x] Config: `Theme map[string]map[string]string`, nested defaults.yaml
- [x] Render: `MergeTheme(shared, override)` helper
- [x] main.go: per-wiki CSS generation + config watcher regeneration
- [x] Server: per-wiki CSS path resolution
- [x] Client: wiki title from `--tw-wiki-title` CSS property
- [x] defaults.yaml: restructured with `shared:` wrapper

---

## v7: Cleanup and Hardening

### Completed

- [x] `scripts/rotate-log` — standalone log rotation library
- [x] `scripts/vide` — bash 5.3 compat, nvim SIGABRT fix
- [x] `scripts/tw-start` — simplified (no health check)
- [x] `scripts/pre-commit` — rewritten in bash with proper idioms
- [x] Apache 2.0 license
- [x] Dangling symlinks quarantined to `lost+found/` at startup
- [x] Relative `wiki_path` resolved against config file directory
- [x] Removed TiddlyWiki `$:/` system note convention and favicon handler
- [x] Sanitized docs (removed personal paths from history)
- [x] 100% statement coverage restored on all packages
- [x] Theme playground: relative CSS fetch for per-wiki support

### TODO

- [x] Delete note button in browser client (API exists)
- [x] Revisions browser (see v8 below)
- [x] Per-wiki theming not yet tested end-to-end in browser

---

## v8: Revisions Browser

### Design

1. **Diff display:** Concatenated `rcsdiff -u` for most recent N
   revision pairs. Chroma diff syntax highlighting. Capped by
   `max_diffs` config (default 10).

2. **Non-head detection:** Read-only file permission. `co -r1.3`
   (no `-l`) → 0444. Checked via `fi.Mode().Perm()&0200 == 0`
   at load time. No xattr, no sidecar.

3. **Checkout flow:** Clickable revision numbers in diff history
   + text input fallback. `co -r{rev}` (no lock) → read-only →
   watcher fires → poll updates card. Restore head via `co -l`.
   Edit/delete/line-edit disabled when viewing old revision.

4. **Toolbar:** `[▼] [edit ✏️] [close ✕]` — chevron expands to
   show `[revisions 🕐] [delete 🗑]` inline left of edit.

5. **Config:** Top-level `max_diffs: 10`, config-only.

### TODO

- [x] Add `max_diffs` to config
- [x] Add `Log`, `HeadRev`, `DiffRange`, `Checkout`, `CheckoutHead` to rcs
- [x] Tests for new rcs functions
- [x] Wiki methods: `Revisions`, `Checkout`, `IsHead`
- [x] Wiki method tests
- [x] Server endpoints: `GET revisions/{title}`, `POST revisions/{title}/checkout`
- [x] Server endpoint tests
- [x] Refactor toolbar: chevron hides delete + revisions
- [x] Revisions dropdown UI
- [x] Diff display with chroma highlighting
- [x] Checkout + restore head UI
- [x] Non-head visual indicator (read-only → disabled buttons + badge)
- [x] Update README.md
- [x] Store refactor: collapsed 3 maps into single `entry` struct
- [x] RCS symbolic tags (`-N`) for tracking checked-out revision
- [x] Promote revision: `ci -l` on read-only file → new trunk head
- [x] `POST /revisions/{title}/promote` endpoint (204/409/404)
- [x] Browser: promote button ("1.1 → 1.4") + restore button when
  viewing non-head revision
- [x] `rcsRev`/`rcsHead` in GET /notes response for non-head files
- [x] `ScanRCS()` at startup: parse CHECKOUT tag from `rlog -h`
- [x] Revision number shown on note card (muted, next to date)
- [x] Simpler clock icon for revisions button
- [x] Line-edit cancel: Escape and click-away (blur) dismiss input
- [x] Removed hardcoded `openNote('ToDo')` startup; `alert→console.warn`
- [x] Integration tests: 37/37 (API + Safari DOM)

---

## v9: Hardening and Fixes

### Completed

- [x] `mkfile` — track all .go and go:embed files as deps (was only
  main.go, causing stale binaries after editing internal/ packages)
- [x] RCS on load — `NeedsCheckin()` mtime comparison so files
  edited while server is down get checked in on restart
- [x] Symlink Get() delegation — `Get()` now delegates to owning
  wiki (was 404; `List()` already worked)
- [x] `tw2md` inline transform fix — links, bold, italic, images
  inside list items, headings, blockquotes, and definition lists
  were silently skipped due to `next` before inline regexes.
  Extracted `tw_inline()` sub called in all branches.
- [x] One-off link fixup — 32 files in ~/notes patched (140 links)
- [x] Evernote migration docs (README + SETUP)
- [x] `scripts/enex2tid` committed
- [x] PLAN.md future considerations (watcher fd alternatives)
- [x] copilot-instructions.md updated with full architecture notes

---

## v10: Remediation (from adversarial audit)

An adversarial test audit (5 models × 10 packages, 270 findings, 0
false alarms) revealed that 100% statement coverage hides significant
test quality gaps, 8 production bugs, and 8 security findings. Full
lab notebook: `AdversarialTestAudit` in the hold wiki.

Three models independently composed remediation plans (Opus 4.6,
GPT-5.4, Opus 4.5 arbiter). The reconciled plan prioritizes security
boundaries first (they are prerequisites for trustworthy tests), then
crash/data bugs, then test harness repair, then coverage expansion.

### Phase 1: Command boundaries ✓

- [x] RCS: `--` before paths, validate rev with `^\d+\.\d+$`
- [x] Search: `--` before dir arg, regex-aware OR parser
- [x] Editor: enforce `%s` contract, require `--` in template

### Phase 2: Output encoding ✓

- [x] Render: `html.EscapeString` on diff fallback
- [x] Server: escape wiki names in selector HTML
- [x] Render: sanitize theme CSS values

### Phase 3: Core invariants ✓

- [x] Store: prevent ghost entries from SetFilePath/SetRCS
- [x] Wiki: move symlinks read inside w.do()
- [x] Wiki: compare content before ci -l on reload

### Phase 4: Persistence/config ✓

- [x] Tid: fix percent-encoding roundtrip (`%2F` corruption)
- [x] RCS: propagate MkdirAll error
- [x] Config: propagate Defaults() error
- [x] Tid: handle "text" header collision

### Phase 5: Test harness rehab ✓

- [x] Replace broken gomonkey patches with function-var seams
- [x] Editor: argument-capturing test helper (replace echo)

### Phase 6: Coverage expansion ✓

- [x] Store, search, tid, watcher, rcs, editor adversarial tests
- [x] Return-value immutability policy (store Get, tid SkinnyFields)

---

## v11: PDF Searchability + Attachment Rendering

### Problem

174 PDFs in note subdirectories were invisible to search. Attachment
`.md` files (LLM-cleaned PDF shadows) were served as raw text.
No automatic conversion for newly added PDFs.

### Approach

Shadow `.pdf.md` files alongside each PDF. rg finds them naturally;
`normalizeHitPath` maps hits back to the parent note. A safe goldmark
instance renders `.md` attachments as HTML. A fire-and-forget enqueue
system triggers conversion on first access.

### Completed

- [x] `scripts/pdf2md` — Python converter: pdftotext → ollama → shadow `.pdf.md`
  - Metadata stubs for image-only PDFs (pdfinfo/pdfimages)
  - Single-file and `--batch` modes; `--model`, `--host`, `--force` flags
  - Truncation at 15K chars, 120s timeout, raw-text fallback
- [x] Batch conversion of 174 PDFs on ROVE (qwen2.5:3b, 100% GPU)
  - 114 LLM-cleaned, 5 raw-text fallback, 53 image-only stubs
- [x] `normalizeHitPath` maps subdir `.md` to parent note
- [x] Attachment listing UI (collapsible, JSON array in note response)
- [x] Configurable `attachment_exclude` (suffix list, hides `.pdf.md` etc.)
- [x] Goldmark table extension + table CSS in embedded stylesheet
- [x] Sentinel errors (`ErrNoFile`, `ErrOutOfRange`, `ErrAtHead`)
- [x] Render `.md` attachments via safe goldmark instance
  - No `html.WithUnsafe()` (LLM content untrusted), no line annotations
  - Themed HTML page shell with CSS custom properties
- [x] Auto-enqueue unconverted PDFs via `pdf_convert_cmd` config
  - `noteAttachments()` detects `.pdf` without `.pdf.md` → fire-and-forget exec
  - `scripts/pdf2md-enqueue` — bash queue runner with flock
- [x] `ParseFunc` variable replaces `tid.Parse` (gomonkey SIGBUS fix)
- [x] 100% statement coverage maintained across all 11 packages

### Audit remediation (targeted post-v11 audit)

Three-model adversarial review (Opus 4.6, GPT-5.4, Sonnet 4.6) of
all changes since commit 80502cf. 8 findings, 6 fixed, 1 accepted,
1 deferred.

- [x] XSS on error path — `AttachmentToHTML` fallback now uses `html.EscapeString`
- [x] Goroutine leak — `pdf2md-enqueue` double-forks; Go parent exits immediately
- [x] Basename collision — queue keys use full mangled path (`/` → `%`)
- [x] Case-insensitive glob — `shopt -s nocaseglob` in queue runner
- [x] Queue race — rescan loop replaces single-pass `for`
- [x] Silent failure — `|| true` replaced with logged warnings
- [x] Event-loop fork — accepted (fire-and-forget is correct for single-user)
- [ ] Nested path traversal — deferred (no nested attachment dirs exist)

### End-to-end tests

- [x] `exp/028-attachments/test.sh` — 24 checks: table rendering, attachment
  listing, attachment_exclude, .md goldmark rendering, XSS stripping,
  Content-Type, raw file passthrough, search shadow mapping, Safari DOM
- [x] `exp/029-pdf-convert/test.sh` — 21 checks: pdf2md single/batch/stub/
  dry-run/force, pdf2md-enqueue queue/flock, server auto-enqueue

### Documentation

- [x] README.md — tables, .md attachment rendering, PDF pipeline, config
- [x] SETUP.md — PDF prerequisites, setup steps, manual usage

---

## Future Considerations

### Watcher: eliminate per-file fd overhead

The current watcher uses fsnotify, which on macOS/BSD opens one
kqueue fd per file (not just per directory). With hundreds of notes
this is fine, but thousands would hit the per-process fd limit
(default ~10K on macOS). Two alternatives:

**Option A: Directory-only kqueue + mtime scan.**
Drop fsnotify. Use raw kqueue with a single fd on the wiki
directory. kqueue's `NOTE_WRITE` fires when any file in the
directory is created, deleted, or modified. On that strobe, scan
the directory for files with `mtime > last_check` and reload
those. One fd per wiki instead of one per file. Still
event-driven (no wasted work when nothing changes), but requires
platform-specific syscall code (kqueue on macOS/BSD, inotify on
Linux — inotify already does directory-level watching natively).

**Option B: Pure mtime polling.**
Drop fsnotify entirely. A goroutine does `os.ReadDir` + `os.Stat`
every N seconds (matching the browser poll interval), compares
mtimes against a map, and reloads changed files. Zero platform
dependencies, zero fd overhead, identical behavior on every OS.
Cost: one readdir + N stat calls per interval — for 500 files
this is <1ms, negligible. Latency up to one poll interval (already
accepted for the browser). Simplest possible implementation.

### Vision model fallback for image-only PDFs

53 of 174 PDFs are scanned images with no extractable text. A future
pass could use a vision-capable model (e.g., LLaVA, GPT-4V) to OCR
these into searchable markdown. The stub infrastructure is already
in place — replace the metadata stub with actual content.
