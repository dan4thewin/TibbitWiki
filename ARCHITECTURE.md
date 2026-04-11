# Architecture

## Package layout

- `main.go` — wiring: config → Wiki → loader → watcher → Hub → Server
- `internal/config` — YAML config + CLI flag parsing. `go:embed defaults.yaml`.
  Theme is nested: `theme.shared` (base) + `theme.{wiki}` (overrides).
- `internal/server` — thin HTTP adapter delegating to wiki via Hub.
  `go:embed index.html` for the browser client.
- `internal/wiki` — Wiki type with goroutine event loop (ops chan).
  Hub maps names → Wiki instances. All domain logic: Get, Put, Delete,
  List, LineEdit, Search, Revisions, Checkout, Promote.
- `internal/store` — in-memory note store with revision tracking.
  Single `entry` struct. Metadata-only (body text stripped on Put).
- `internal/tid` — .md file parser/serializer (RFC822-ish headers
  + blank line + body).
- `internal/render` — goldmark + chroma markdown → HTML rendering,
  ThemeCSS generation, `data-line` annotation on list items.
- `internal/search` — query parser + rg wrapper. Supports literal,
  NOT, tag, title, regex, OR, AND.
- `internal/watcher` — fsnotify file watcher with debounce,
  callback API `func(string)`.
- `internal/loader` — startup file loader. Detects symlinks,
  quarantines dangling ones, RCS checkin on load via NeedsCheckin.
- `internal/rcs` — RCS wrapper: Checkin, Log, HeadRev, DiffRange,
  Checkout, CheckoutHead, NeedsCheckin, HasArchive.
- `internal/editor` — external editor launcher (template-based).

## Key design decisions

**No database.** All state is in-memory + filesystem. Note files
live on disk as .md (RFC822-ish headers + blank line + markdown
body). The store holds metadata; bodies are in an LRU cache backed
by disk.

**One goroutine per wiki.** Channel-based event loop, no mutexes.

**Multi-wiki.** Subdirectories under `--wiki-path` are auto-discovered.
Each becomes a wiki at `/{name}/`. Root `/` shows a wiki selector.

**Symlink delegation.** A symlink like `work/Note.md → ../hold/Note.md`
is detected at load time. The borrower wiki's List() and Get()
delegate to the owning wiki. The client sees an `owner` field and
routes writes to the owner wiki directly.

**RCS versioning.** Every note write is versioned via golang-rcs
(pure Go, no external commands). Revisions browser
shows diffs, supports checkout (read-only) and promote (non-destructive
rollback). Startup checkins any files with stale archives (mtime
comparison via NeedsCheckin).

**Config over constants.** Listen addresses, wiki path, editor command,
poll interval, cache size, theme, max_diffs — all in YAML with CLI
flag overrides. The `listen` key accepts a scalar or array for
multi-bind.

**Server-side rendering.** Markdown → HTML via goldmark with syntax
highlighting via chroma (OneDark). List items annotated with
`data-line="N"` for line-level editing. No CDN dependencies.

**Search.** Delegates to ripgrep (must be on PATH). Query syntax:
literal words, `!` NOT, `#tag`, `@title`, `/regex/`, `m/regex/`,
`a|b` OR, space for AND (set intersection).

## Future: cross-wiki text search from wiki picker

`search.Search(dir, query)` is almost general enough to run at the
wiki root — rg doesn't care what directory you point it at. Two
issues to resolve:

1. `normalizeHitPath` treats subdirectories as attachment folders
   (maps `hold/Note.md` → `hold.md`). Needs a depth-aware mode or
   the cross-wiki caller handles path parsing itself.

2. `wiki.Search()` looks up each result in `w.store.Get(name)` to
   get the display title. A cross-wiki search has no single store.
   But the store lookup is only used for `filename → title`, and
   `search.FileToTitle()` already does that without the store.
   The store lookup also filters out unknown files — a cross-wiki
   caller would skip that filter or route results to per-wiki stores
   via Hub.

Design direction: the wiki picker screen gets a search box. Results
show note title + wiki name (derived from path prefix). Clicking a
result navigates into the owning wiki and opens the note. Per-wiki
search is unchanged. This avoids architecture changes — the
cross-wiki search is a thin layer at the Hub/server level, not a
change to the search package or wiki goroutine model.

## Build system

Plan 9 mk (dcjones/mk Go port). `mkfile` lists all .go and go:embed
files as deps. No backslash line continuation in mk — single-line
variable assignments only.

## Scripts

- `scripts/tw2md` — TiddlyWiki .tid → .md (Perl)
- `scripts/enex2tid` — Evernote ENEX → .md with attachments (Python)
- `scripts/vide` — headless nvim + neovide launcher (bash)
- `scripts/tw-start` — detached server launcher with logging (bash)
- `scripts/pre-commit` — fmt, vet, test, coverage, complexity gate
