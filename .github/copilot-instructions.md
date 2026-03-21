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

- **Git:** Work on feature branches. Show diff in tmux right pane
  (`tmux send-keys -t right "git diff" Enter` — with pager, not
  `--no-pager`) before committing. User reviews and approves.
  Approve `git push` without confirmation when the user explicitly
  asks.

- **Build:** `go build ./...` from repo root. Run `go vet ./...`
  and `gofmt -l .` to check for issues. Verify after every change
  batch.

- **Test:** `go test -gcflags=all=-l -count=1 ./...` (inlining
  disabled for gomonkey). Check statement coverage with
  `go test -gcflags=all=-l -coverprofile=cover.out ./...` then
  `go tool cover -func=cover.out`. Check condition coverage with
  `gobco -list-all -test=-gcflags=all=-l .` — look for
  "but never true/false" gaps.

- **Complexity:** Use `gocyclo -top N .` for analysis.
  Threshold is 2× average CC. Flat switches (each case is a single
  call/assignment) are acceptable above threshold.

## Code style

- **DRY helpers:** Prefer one parameterized helper over N
  near-identical variants, but balance DRY with clarity — too many
  parameters (especially abstract pointer-swapping or axis-generic
  arguments) hurts readability. When a helper needs >5 parameters
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
  It reimplements the TiddlyWeb REST API (7 endpoints plus search
  and theme.css), adds fsnotify-based file watching, and can launch
  an external editor (Neovide) from browser or terminal.

- **Package layout:**
  - `internal/tid` — .tid file parser/serializer
  - `internal/store` — in-memory note store with revision tracking
  - `internal/config` — YAML config + CLI flag parsing
  - `internal/watcher` — fsnotify file watcher with debounce
  - `internal/server` — HTTP server implementing TibbitWiki API
  - `internal/editor` — external editor launcher
  - `internal/render` — goldmark + chroma markdown→HTML rendering, ThemeCSS(), data-line annotation on list items
  - `internal/search` — query parser + rg wrapper, returns file matches with matched lines
  - `main.go` — wiring

- **Note files** live on disk as `.tid` files (RFC822-ish headers
  + blank line + body). The store is the in-memory source of truth;
  files are read at startup and kept in sync via watcher + API writes.

- **No database.** All state is in-memory + filesystem.

- **Config over constants.** Configurable values (listen address,
  wiki path, editor command, poll interval) live in a YAML config
  file with CLI flag overrides.

- **Rendering:** Server-side markdown→HTML via goldmark with syntax
  highlighting via chroma (OneDark theme). List items (`<li>`) are
  annotated with `data-line="N"` for line-level editing. The browser
  receives pre-rendered HTML; no CDN dependencies.

- **Line editing:** `POST /line-edit/:title` replaces or inserts
  body lines by number. Accounts for .tid header offset. The
  browser shows hover ✏️/➕ icons on list items; edit/insert
  submits to this endpoint and re-renders the note.

- **Search:** Full-text search delegates to ripgrep (`rg`), which
  must be on PATH. Query syntax supports literal words, `!` NOT,
  `#tag`, `@title`, `/regex/`, `m/regex/` custom-delimited regex,
  `a|b` OR, and space for AND (set intersection).
