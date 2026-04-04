**Surface contradictions immediately.** If stored facts, code
comments, documentation, or architecture notes conflict with actual
code behavior, flag the contradiction to the user at high priority
before proceeding. Do not silently trust one source over another—the
user decides which is correct.

Use short, concise commit messages.

As the codebase evolves, frequently suggest brief updates to the
architecture notes below so they stay current. Propose changes early
and often—small, incremental updates are easier to review than
large rewrites.

## Workflow

- **Git:** Work on main branch (no feature branches currently).
  Commit directly without showing diff for review.
  Approve `git push` without confirmation when the user explicitly
  asks.

- **Build:** `mk` (Plan 9 mk, Go port) from repo root.
  `go build ./...` and `go vet ./...` for quick checks.
  `gofmt -l .` to check formatting. Verify after every change batch.
  See `mkfile` for all targets (test, lint, coverage, complexity, etc.).

- **Test:** `go test -gcflags=all=-l -count=1 ./...` (inlining
  disabled for gomonkey). Check statement coverage with
  `go test -gcflags=all=-l -coverprofile=cover.out ./...` then
  `go tool cover -func=cover.out`. Check condition coverage with
  `gobco -list-all -test=-gcflags=all=-l .` — look for
  "but never true/false" gaps.

- **Complexity:** Use `gocyclo -top N .` for analysis.
  Threshold is max=12 (pre-commit hook). Flat switches (each case
  is a single call/assignment) are acceptable above threshold.

## Code style

- **DRY helpers:** Prefer one parameterized helper over N
  near-identical variants, but balance DRY with clarity — too many
  parameters (especially abstract pointer-swapping or axis-generic
  arguments) hurts readability. When a helper needs &gt;5 parameters
  to stay generic, consider inlining with explicit per-case code.
  Helpers should target CC ≤ half the project average.

- **No backup files** in the repo.

## Unit testing

- **Clarity over DRY in tests.** Each test should be self-contained
  and obvious. Copious comments explain what is being tested, why,
  and how it maps to server behavior. Prefer readability over
  eliminating duplication.

- **Coverage targets.** Aim for 100% statement coverage (`go tool
  cover`) and 100% condition coverage (`gobco`). When adding or
  modifying production code, update tests to maintain both.
  All 11 internal/ packages are currently at 100% statement coverage.

- **Mock external dependencies with gomonkey/v2.** For functions
  that call methods outside the file under test, mock them with
  `gomonkey.ApplyMethodFunc` to test the caller's branching in
  isolation. Tests must run with `-gcflags=all=-l` (disable
  inlining).

- **gomonkey + `-coverprofile` caveat.** On darwin/arm64, patching
  certain functions (e.g. `fsnotify.NewWatcher`, `exec.Command`)
  causes SIGBUS under `-coverprofile`. Use package-level function
  variables instead (see `newFsWatcher` in watcher.go), or clear
  `PATH` to force lookup failure (see `TestSearchRgNonExitError`).

- **Minimal test helpers.** Use raw structs when only a few fields
  matter (especially with mocks).

- **Test naming.** Subtests use descriptive CamelCase names that
  describe the scenario (e.g. `ParseValidNote`,
  `MissingTitleReturnsError`, `WatcherIgnoresTempFiles`).

## Architecture notes

- **TibbitWiki** is a Go replacement for TiddlyWiki's Node.js server.
  It serves a minimal browser client, implements a REST API (18+
  endpoints), provides multi-wiki support with symlink delegation,
  RCS version control, fsnotify-based file watching, external editor
  launch (Neovide), full-text search via ripgrep, per-wiki theming,
  and an LRU body cache for bounded memory.

- **Package layout:**
  - `internal/wiki` — Wiki type with goroutine event loop (ops chan),
    Hub maps names→Wiki instances. All domain logic: Get, Put, Delete,
    List, LineEdit, Search, Revisions, Checkout, Promote. Symlinked
    notes delegate to the owning wiki for both List() and Get().
  - `internal/server` — thin HTTP adapter delegating to wiki via Hub.
    `go:embed index.html` for the browser client.
  - `internal/store` — in-memory note store with revision tracking.
    Collapsed 3-map design into single `entry` struct. Metadata-only
    (body text stripped on Put).
  - `internal/config` — YAML config + CLI flag parsing. `go:embed
    defaults.yaml`. Theme is nested: `theme.shared` + `theme.{wiki}`.
  - `internal/tid` — .md file parser/serializer (RFC822-ish headers
    + blank line + body)
  - `internal/render` — goldmark + chroma markdown→HTML rendering,
    ThemeCSS generation, `data-line` annotation on list items
  - `internal/search` — query parser + rg wrapper; supports literal,
    NOT, tag, title, regex, OR, AND
  - `internal/watcher` — fsnotify file watcher with debounce,
    callback API `func(string)`
  - `internal/loader` — startup file loader; detects symlinks,
    quarantines dangling ones, RCS checkin on load via NeedsCheckin
  - `internal/rcs` — RCS wrapper: Checkin, Log, HeadRev, DiffRange,
    Checkout, CheckoutHead, NeedsCheckin, HasArchive
  - `internal/editor` — external editor launcher (template-based)
  - `main.go` — wiring: config→Wiki→loader→watcher→Hub→Server

- **Note files** live on disk as `.md` files (RFC822-ish headers
  + blank line + markdown body). The store is the in-memory source
  of truth for metadata; bodies are in an LRU cache backed by disk.
  Files are read at startup and kept in sync via watcher + API writes.

- **Multi-wiki:** Subdirectories under `--wiki-path` are auto-discovered.
  Each becomes a wiki at `/{name}/`. Root `/` shows a wiki selector.
  One goroutine per wiki (channel-based event loop, no mutexes).

- **Symlink delegation:** A symlink like `work/Note.md → ../hold/Note.md`
  is detected at load time. The borrower wiki's List() and Get()
  delegate to the owning wiki. The client sees an `owner` field and
  routes writes to the owner wiki directly.

- **No database.** All state is in-memory + filesystem.

- **RCS versioning:** Every note write does `ci -l`. Revisions browser
  shows diffs, supports checkout (read-only) and promote (non-destructive
  rollback). Startup loads all files and checkins any with stale archives
  (mtime comparison via NeedsCheckin).

- **Config over constants.** Configurable values (listen addresses,
  wiki path, editor command, poll interval, cache size, theme,
  max_diffs) live in a YAML config file with CLI flag overrides.
  The `listen` key accepts a scalar (`host:port`) or an array
  of addresses for multi-bind.
  Theme section is nested: `theme.shared` (base) + `theme.{wiki}`
  (overrides). CSS files written as `{wiki_path}/{wiki}.css`.

- **Rendering:** Server-side markdown→HTML via goldmark with syntax
  highlighting via chroma (OneDark theme). List items (`<li>`) are
  annotated with `data-line="N"` for line-level editing. The browser
  receives pre-rendered HTML; no CDN dependencies.

- **Search:** Full-text search delegates to ripgrep (`rg`), which
  must be on PATH. Query syntax supports literal words, `!` NOT,
  `#tag`, `@title`, `/regex/`, `m/regex/` custom-delimited regex,
  `a|b` OR, and space for AND (set intersection).

- **Build system:** Plan 9 mk (dcjones/mk Go port). `mkfile` lists
  all .go and go:embed files as deps. No backslash line continuation
  in mk — single-line variable assignments only.

- **Converter scripts:**
  - `scripts/tw2md` — TiddlyWiki .tid → .md (Perl). Converts
    wikitext to markdown including inline transforms in list items.
  - `scripts/enex2tid` — Evernote ENEX → .md with attachments (Python).
  - `scripts/vide` — headless nvim + neovide launcher (bash).
  - `scripts/tw-start` — detached server launcher with logging (bash).
  - `scripts/pre-commit` — fmt, vet, test, coverage, complexity gate.
